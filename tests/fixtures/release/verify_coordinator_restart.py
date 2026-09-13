#!/usr/bin/env python3
"""Read-only follow-up to a saved disposable-host coordinator restart attempt.

This does not restart services, apply plans, write the journal, or touch guests.
It compares the original restart report's four complete journal table digests,
VM inventory digest and top-level source-media metadata with current readings.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import sqlite3
import subprocess
import sys
import time


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False



TERMINAL_STATES = {"succeeded", "failed", "partial", "canceled", "recovery-required"}
JOURNAL_TABLES = ("jobs", "locks", "events", "plans")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def file_sha(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def run(*args, timeout=10):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout,
                            check=False, stdin=subprocess.DEVNULL)
    if result.returncode:
        raise RuntimeError(f"{args[0]} failed ({result.returncode}): {result.stderr[:2000]}")
    return result.stdout


def cli(*args, timeout=10):
    response = json.loads(run("/usr/bin/virmill", *args, "--output", "json",
                              "--non-interactive", timeout=timeout))
    if response.get("error") is not None:
        raise RuntimeError(f"coordinator request failed: {response['error']}")
    return response["data"]


def wait_for_ipc(request, timeout=10, monotonic=time.monotonic, pause=time.sleep):
    deadline = monotonic() + timeout
    attempts = 0
    last_error = None
    while monotonic() < deadline:
        attempts += 1
        try:
            # operation list dispatches to the coordinator; version does not.
            request("operation", "list", timeout=max(0.01, min(2, deadline - monotonic())))
            return attempts
        except (OSError, RuntimeError, ValueError, KeyError, subprocess.TimeoutExpired) as error:
            last_error = str(error)
            remaining = deadline - monotonic()
            if remaining > 0:
                pause(min(0.1, remaining))
    raise RuntimeError(f"coordinator IPC was not ready within {timeout}s: {last_error}")


def journal_snapshot(home, allowed_plan_ids=(), intentional_plans=None):
    path = home / ".local/state/virmill/journal.db"
    with sqlite3.connect(path.as_uri() + "?mode=ro", uri=True, timeout=2) as connection:
        connection.execute("PRAGMA query_only=ON")
        connection.execute("BEGIN")
        version = connection.execute("PRAGMA user_version").fetchone()[0]
        if version != 3:
            raise RuntimeError(f"journal schema is {version}, expected 3")
        states = [json.loads(row[0])["state"] for row in connection.execute("SELECT body FROM jobs")]
        if any(state not in TERMINAL_STATES for state in states):
            raise RuntimeError("nonterminal jobs exist; this verification requires an idle coordinator")
        for plan_id in allowed_plan_ids:
            row = connection.execute("SELECT body FROM plans WHERE id=?", (plan_id,)).fetchone()
            if row is None:
                raise RuntimeError(f"explicitly allowed preview does not exist: {plan_id}")
            plan = json.loads(row[0])
            if plan.get("planID") != plan_id or plan.get("operation") != "import.prepare-install":
                raise RuntimeError(f"allowed preview is not an import.prepare-install plan: {plan_id}")
            if intentional_plans is not None:
                intentional_plans.append({"id": plan_id, "operation": plan["operation"],
                                          "bodySHA256": sha(row[0] if isinstance(row[0], bytes) else row[0].encode())})
        tables = {}
        for table in JOURNAL_TABLES:
            query = "SELECT * FROM " + table
            parameters = ()
            if table == "plans" and allowed_plan_ids:
                query += " WHERE id NOT IN (" + ",".join("?" for _ in allowed_plan_ids) + ")"
                parameters = tuple(allowed_plan_ids)
            rows = connection.execute(query + " ORDER BY rowid", parameters).fetchall()
            tables[table] = sha(json.dumps(rows, default=lambda value: value.hex(), sort_keys=True).encode())
        return {"jobCount": len(states), "tables": tables}


def media_snapshot(home):
    result = {}
    for path in (home / "images").iterdir():
        observed = path.stat()
        result[path.name] = [observed.st_ino, observed.st_size, observed.st_mtime_ns, observed.st_ctime_ns]
    return result


def verify(root, expected_daemon_sha256, allowed_plan_ids=()):
    original = root / "restart/report.json"
    if original.is_symlink() or not original.is_file() or original.stat().st_size > 16 << 20:
        raise RuntimeError("original restart report must be a bounded regular file")
    original_bytes = original.read_bytes()
    baseline = json.loads(original_bytes)
    before = baseline["before"]
    if set(before) != {"journal", "vms", "media"} or set(before["journal"]["tables"]) != set(JOURNAL_TABLES):
        raise RuntimeError("original restart report lacks the complete required baseline")
    report = {
        "status": "failed",
        "scope": "read-only verification after prior coordinator restart",
        "originalReportSHA256": sha(original_bytes),
        "originalReportStatus": baseline.get("status"),
        "guestMutationSubmitted": False,
        "serviceMutationSubmitted": False,
        "journalWritesSubmitted": False,
        "mediaCoverage": "same top-level inode, size, mtime and ctime metadata as original baseline",
        "journalCoverage": list(JOURNAL_TABLES),
        "intentionalPostRestartPreviews": [],
        "before": before,
    }
    try:
        report["ipcReadinessAttempts"] = wait_for_ipc(cli)
        report["ipcReadinessMethod"] = "operation list"
        pid = run("systemctl", "--user", "show", "virmilld.service", "-p", "MainPID", "--value").strip()
        if not pid.isdecimal() or int(pid) <= 1 or pid == str(baseline.get("oldPID")):
            raise RuntimeError("the restarted coordinator PID is not a distinct running process")
        process = Path("/proc") / pid
        uid_map = (process / "uid_map").read_text()
        gid_map = (process / "gid_map").read_text()
        if uid_map != Path("/proc/self/uid_map").read_text() or gid_map != Path("/proc/self/gid_map").read_text():
            raise RuntimeError("coordinator UID/GID namespace differs from the ordinary host user's namespace")
        process_uids = re.search(r"^Uid:\s+(.+)$", (process / "status").read_text(), re.MULTILINE)
        if process_uids is None or any(int(value) != os.getuid() for value in process_uids.group(1).split()):
            raise RuntimeError("coordinator must run entirely as the authorized ordinary user")
        running_hash = file_sha(process / "exe")
        if running_hash != expected_daemon_sha256:
            raise RuntimeError("running coordinator executable digest differs from expected build")
        report.update({"newPID": pid, "newUIDMap": uid_map, "newGIDMap": gid_map,
                       "runningDaemonSHA256": running_hash, "journalSchemaVersion": 3})
        after = {
            "journal": journal_snapshot(Path.home(), allowed_plan_ids, report["intentionalPostRestartPreviews"]),
            "vms": sha(json.dumps(cli("vm", "list"), sort_keys=True).encode()),
            "media": media_snapshot(Path.home()),
        }
        report["after"] = after
        if before != after:
            report["differentSections"] = [key for key in before if before[key] != after[key]]
            raise RuntimeError("observed state differs from the original pre-restart baseline")
        # Ensure the evidence source itself was not rewritten during verification.
        if original.read_bytes() != original_bytes:
            raise RuntimeError("original restart report changed during verification")
        final_pid = run("systemctl", "--user", "show", "virmilld.service", "-p", "MainPID", "--value").strip()
        if final_pid != pid:
            raise RuntimeError("coordinator restarted again during verification")
        report.update({"status": "passed", "guestsJobsMediaPreserved": True})
    except (OSError, ValueError, RuntimeError, KeyError, sqlite3.Error, subprocess.TimeoutExpired) as error:
        report["error"] = str(error)
    return report


def self_test():
    import tempfile
    import unittest

    class Tests(unittest.TestCase):
        def test_actual_ipc_method_and_retries(self):
            calls = []
            now = [0.0]

            def request(*args, **kwargs):
                calls.append(args)
                if len(calls) < 3:
                    raise RuntimeError("socket not ready")

            attempts = wait_for_ipc(request, monotonic=lambda: now[0], pause=lambda value: now.__setitem__(0, now[0] + value))
            self.assertEqual(attempts, 3)
            self.assertEqual(calls, [("operation", "list")] * 3)

        def test_readiness_bound(self):
            now = [0.0]

            def request(*args, **kwargs):
                raise RuntimeError("offline")

            with self.assertRaisesRegex(RuntimeError, "within 10s"):
                wait_for_ipc(request, monotonic=lambda: now[0], pause=lambda value: now.__setitem__(0, now[0] + value))
            self.assertLessEqual(now[0], 10)

        def test_journal_terminal_states_and_no_writes(self):
            with tempfile.TemporaryDirectory() as directory:
                home = Path(directory)
                path = home / ".local/state/virmill/journal.db"
                path.parent.mkdir(parents=True)
                with sqlite3.connect(path) as connection:
                    connection.execute("PRAGMA user_version=3")
                    for table in JOURNAL_TABLES:
                        connection.execute("CREATE TABLE " + table + " (body BLOB)")
                    for state in sorted(TERMINAL_STATES):
                        connection.execute("INSERT INTO jobs VALUES (?)", (json.dumps({"state": state}).encode(),))
                before = file_sha(path)
                snapshot = journal_snapshot(home)
                self.assertEqual(snapshot["jobCount"], len(TERMINAL_STATES))
                self.assertEqual(file_sha(path), before)
                with sqlite3.connect(path) as connection:
                    connection.execute("INSERT INTO jobs VALUES (?)", (json.dumps({"state": "running"}).encode(),))
                with self.assertRaisesRegex(RuntimeError, "nonterminal"):
                    journal_snapshot(home)

        def test_only_exact_allowed_import_preview_is_excluded(self):
            with tempfile.TemporaryDirectory() as directory:
                home = Path(directory)
                path = home / ".local/state/virmill/journal.db"
                path.parent.mkdir(parents=True)
                with sqlite3.connect(path) as connection:
                    connection.execute("PRAGMA user_version=3")
                    for table in JOURNAL_TABLES:
                        connection.execute("CREATE TABLE " + table + " (id TEXT, body BLOB)")
                baseline = journal_snapshot(home)
                plan_id = "11111111-1111-4111-8111-111111111111"
                with sqlite3.connect(path) as connection:
                    connection.execute("INSERT INTO plans VALUES (?,?)", (plan_id, json.dumps({"planID": plan_id, "operation": "import.prepare-install"}).encode()))
                self.assertNotEqual(journal_snapshot(home), baseline)
                intentional = []
                self.assertEqual(journal_snapshot(home, [plan_id], intentional), baseline)
                self.assertEqual(intentional[0]["id"], plan_id)
                with self.assertRaisesRegex(RuntimeError, "does not exist"):
                    journal_snapshot(home, ["22222222-2222-4222-8222-222222222222"])
                with sqlite3.connect(path) as connection:
                    connection.execute("UPDATE plans SET body=?", (json.dumps({"planID": plan_id, "operation": "vm.start"}).encode(),))
                with self.assertRaisesRegex(RuntimeError, "not an import.prepare-install"):
                    journal_snapshot(home, [plan_id])

    result = unittest.TextTestRunner().run(unittest.defaultTestLoader.loadTestsFromTestCase(Tests))
    return 0 if result.wasSuccessful() else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--report-name", default="restart-verification.json")
    parser.add_argument("--execute-disposable", action="store_true")
    parser.add_argument("--root", type=Path)
    parser.add_argument("--expected-daemon-sha256")
    parser.add_argument("--allowed-new-plan-id", action="append", default=[],
                        help="exact import.prepare-install preview created intentionally after restart; repeat for each")
    args = parser.parse_args()
    if args.self_test:
        return self_test()
    if not args.execute_disposable or not authorized_test_host() or os.getuid() != 1000:
        parser.error("requires explicit disposable execution on a designated test host as UID 1000")
    if args.root is None or not re.fullmatch(r"[a-f0-9]{64}", args.expected_daemon_sha256 or ""):
        parser.error("a root directory and exact daemon SHA256 are required")
    if len(args.allowed_new_plan_id) != len(set(args.allowed_new_plan_id)) or any(
        not re.fullmatch(r"[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}", value)
        for value in args.allowed_new_plan_id
    ):
        parser.error("allowed new plan IDs must be distinct exact UUIDs")
    root = args.root.resolve(strict=True)
    base = Path.home() / "virmill-tests"
    if root != args.root or not root.is_relative_to(base) or root == base or not root.is_dir():
        parser.error("root must be an existing canonical directory beneath ~/virmill-tests")
    if not re.fullmatch(r"restart-verification(?:-[0-9]+)?\.json", args.report_name):
        parser.error("report name must be restart-verification[-N].json")
    destination = root / args.report_name
    # Reserve only this new evidence file; never alter the original failed report.
    fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as output:
        try:
            report = verify(root, args.expected_daemon_sha256, args.allowed_new_plan_id)
        except (OSError, ValueError, RuntimeError, KeyError) as error:
            report = {"status": "failed", "error": str(error)}
        json.dump(report, output, indent=2)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())
    print(json.dumps({"status": report["status"], "evidence": str(destination),
                      "error": report.get("error"), "newPID": report.get("newPID")}, sort_keys=True))
    return report["status"] != "passed"


if __name__ == "__main__":
    sys.exit(main())
