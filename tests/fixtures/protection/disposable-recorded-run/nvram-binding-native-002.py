"""Single-use, parent-operated ADR 0021 recipe. Authoring is not execution evidence."""

import argparse
import hashlib
import json
import math
import os
import pathlib
import re
import selectors
import signal
import sqlite3
import stat
import subprocess
import sys
import time
import uuid
import xml.etree.ElementTree as ET
from xml.sax.saxutils import escape, unescape


ROOT = pathlib.Path("<test-vm-home>/virmill-tests/run-65930c6-20260907")
URI = "qemu:///system"
POOL_ID = "95b94843-0db0-46ac-9bbf-a2fa3c183818"
POOL_NAME = "virmill-cold-probe-v1"
POOL_DIRECTORY = pathlib.Path("/var/lib/libvirt/images") / POOL_NAME
SOURCE_OPERATION = "39078e3f-672b-4451-90dd-7f1fe1824e5a"
SOURCE_SHA = "1945000ea87359d92f6fcb3413549fe477884012d631c66a058c71a1dda9e483"
PREPARED_SHA = "f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51"
FIRMWARE = {
    "mode": "uefi", "code": "/usr/share/edk2/ovmf/OVMF_CODE_4M.qcow2",
    "template": "/usr/share/edk2/ovmf/OVMF_VARS_4M.qcow2",
    "format": "qcow2", "secureBoot": False, "tpm": True,
}
ACKS = ["host-mutation", "copy-managed-volumes", "new-vm-identity",
        "new-firmware-state", "creation-device-policy"]
TABLES = {
    "plans": ("id,digest,body,input", "id", 1),
    "jobs": ("id,plan_id,body", "id", 1),
    "dedup": ("key,request_digest,job_id,created_at", "key", 1),
    "locks": ("resource,job_id", "resource", 1),
    "events": ("job_id,seq,body", "job_id,seq", 2),
    "metadata": ("kind,id,body", "kind,id", 2),
}
MAX_OUTPUT = 8 << 20


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def canonical_uuid(value):
    require(isinstance(value, str) and str(uuid.UUID(value)) == value and
            uuid.UUID(value).int != 0, "noncanonical or zero UUID")
    return value


def strict_json(raw):
    require(len(raw) <= MAX_OUTPUT, "JSON exceeds 8 MiB")

    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "duplicate JSON key: " + key)
            result[key] = value
        return result

    def invalid_constant(value):
        raise ValueError("non-finite JSON constant: " + value)

    def finite_float(value):
        parsed = float(value)
        require(math.isfinite(parsed), "non-finite JSON number")
        return parsed

    if isinstance(raw, bytes):
        raw = raw.decode("utf-8")
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid_constant, parse_float=finite_float)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def read_regular(path, limit=MAX_OUTPUT):
    # Inputs and outputs are private recipe/package files, never auxiliary state.
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_size <= limit,
                "input is not a bounded regular file: " + str(path))
        raw = stream.read(limit + 1)
        after = os.fstat(stream.fileno())
        require(len(raw) <= limit and file_generation(before) == file_generation(after),
                "input changed during read: " + str(path))
        return raw


def file_generation(value):
    return (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns)


def regular_hash(path, limit=128 << 20, proc_executable=False):
    flags = os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC
    if not proc_executable:
        flags |= os.O_NOFOLLOW
    fd = os.open(path, flags)
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_size <= limit,
                "hash input is not a bounded regular file: " + str(path))
        hashed, size = hashlib.sha256(), 0
        while block := stream.read(1 << 20):
            size += len(block)
            require(size <= limit, "hash input grew beyond bound")
            hashed.update(block)
        require(file_generation(before) == file_generation(os.fstat(stream.fileno())),
                "hash input changed while read")
        return hashed.hexdigest()


def replace_nvram(raw, original, replacement):
    """Replace one native text node, retaining every other original XML byte."""
    matches = list(re.finditer(rb"(<nvram\b[^>]*>)([^<]+)(</nvram>)", raw))
    require(len(matches) == 1, "expected one simple native NVRAM text node")
    match = matches[0]
    require(unescape(match[2].decode("utf-8"), {"&apos;": "'", "&quot;": '"'}) == original,
            "native NVRAM text differs from bound declaration")
    changed = raw[:match.start(2)] + escape(replacement).encode() + raw[match.end(2):]
    parsed = ET.fromstring(changed)
    require(parsed.findtext("./os/nvram") == replacement, "NVRAM replacement failed")
    return changed


def parse_probe_volume_list(raw, allowed_names):
    """Parse the observed C-locale Name/Path table for this generated-name pool.

    This is deliberately not a general virsh table parser. Whitespace cannot
    occur inside these reviewed names or paths, and any future unknown volume
    makes the observation fail instead of disappearing from the inventory.
    """
    require(isinstance(raw, bytes) and len(raw) <= 65536 and raw.endswith(b"\n"),
            "volume table is oversized, incomplete or not bytes")
    text = raw.decode("ascii")
    require(all(character == "\n" or 32 <= ord(character) <= 126 for character in text),
            "volume table contains control characters")
    lines = text.splitlines()
    while lines and not lines[0].strip():
        lines.pop(0)
    while lines and not lines[-1].strip():
        lines.pop()
    require(2 <= len(lines) <= 34 and all(len(line) <= 4096 for line in lines),
            "volume table has missing headers or exceeds 32 rows")
    require(lines[0].split() == ["Name", "Path"], "unknown volume table header")
    require(re.fullmatch("-{3,}", lines[1].strip()), "invalid volume table separator")
    require(isinstance(allowed_names, set) and len(allowed_names) <= 32,
            "invalid volume name allowlist")

    def valid_name(name):
        match = re.fullmatch(r"virmill-([a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12})-disk-[0-9]{3}\.qcow2", name)
        require(match is not None, "unexpected probe-pool volume name")
        canonical_uuid(match[1])

    for name in allowed_names:
        valid_name(name)
    volumes = {}
    for line in lines[2:]:
        columns = line.split()
        require(len(columns) == 2, "volume row must have exactly Name and Path")
        name, path = columns
        valid_name(name)
        require(name in allowed_names, "unknown future volume in generated probe pool")
        require(name not in volumes, "duplicate volume table row")
        require(path == str(POOL_DIRECTORY / name), "volume path does not match its exact name/pool")
        volumes[name] = path
    return dict(sorted(volumes.items()))


def self_test():
    """Pure fixture checks only: no Recipe, filesystem, subprocess or native API."""
    old_ids = ["d4c95f21-28bc-428d-9e5f-ceda025d279e", "f78674f3-bf3a-43e5-81f9-4283e2472024"]
    names = ["virmill-" + value + "-disk-000.qcow2" for value in old_ids]
    allowed = set(names)
    # Synthetic spacing around the exact two rows observed by the parent on
    # libvirt 12.0.0; this does not claim to be a byte-captured native response.
    header = " Name                                                       Path\n" + "-" * 165 + "\n"
    rows = [" " + name + "   " + str(POOL_DIRECTORY / name) + "\n" for name in names]
    sample = (header + "".join(rows) + "\n").encode()
    expected = {name: str(POOL_DIRECTORY / name) for name in names}
    passed = []

    def accept(label, raw, accepted, wanted):
        require(parse_probe_volume_list(raw, accepted) == wanted, "self-test failed: " + label)
        passed.append(label)

    def reject(label, raw, accepted=allowed):
        try:
            parse_probe_volume_list(raw, accepted)
        except (RuntimeError, ValueError, UnicodeError):
            passed.append(label)
            return
        raise RuntimeError("self-test accepted malformed inventory: " + label)

    accept("observed two-column layout", sample, allowed, expected)
    accept("deterministic row order", (header + "".join(reversed(rows)) + "\n").encode(), allowed, expected)
    accept("bounded surrounding whitespace", b"\n" + sample + b" \n", allowed, expected)
    new_name = "virmill-3a5d0fce-9c04-44e2-b60d-b8e37ef2ec09-disk-000.qcow2"
    new_path = str(POOL_DIRECTORY / new_name)
    accept("only reviewed new identity allowed", (header + "".join(rows) + new_name + " " + new_path + "\n\n").encode(),
           allowed | {new_name}, {**expected, new_name: new_path})
    accept("empty table grammar", header.encode(), allowed, {})
    reject("unknown header", sample.replace(b"Name", b"NAME", 1))
    reject("extra header column", sample.replace(b"Path\n", b"Path Type\n", 1))
    reject("missing header", "".join(rows).encode())
    reject("missing separator", (header.splitlines()[0] + "\n" + "".join(rows)).encode())
    reject("malformed separator", sample.replace(b"---", b"-=-", 1))
    reject("empty output", b"\n")
    reject("missing row column", (header + names[0] + "\n").encode())
    reject("extra row column", (header + rows[0].rstrip() + " qcow2\n").encode())
    reject("interior blank row", (header + rows[0] + "\n" + rows[1]).encode())
    reject("duplicate name", (header + rows[0] + rows[0]).encode())
    reject("future unknown generated volume", (header + new_name + " " + new_path + "\n").encode())
    reject("unknown non-generated volume", (header + "unrelated.qcow2 /var/lib/libvirt/images/unrelated.qcow2\n").encode())
    reject("different generated disk index", sample.replace(b"-disk-000", b"-disk-001"))
    reject("noncanonical UUID", sample.replace(b"d4c95f21", b"D4C95F21"))
    reject("zero UUID", sample.replace(old_ids[0].encode(), b"00000000-0000-0000-0000-000000000000"))
    reject("wrong path/name pairing", sample.replace(str(POOL_DIRECTORY / names[0]).encode(), str(POOL_DIRECTORY / names[1]).encode()))
    reject("outside pool path", sample.replace(str(POOL_DIRECTORY).encode(), b"/tmp"))
    reject("unclean pool path", sample.replace(str(POOL_DIRECTORY).encode(), str(POOL_DIRECTORY / ".." / POOL_NAME).encode()))
    reject("terminal control", sample.replace(b" Name", b"\x1b[31mName", 1))
    reject("non-ASCII table", sample.replace(b" Name", b" N\xffme", 1))
    reject("incomplete final line", sample.rstrip(b"\n"))
    reject("more than 32 rows", (header + rows[0] * 33).encode())
    reject("oversized output", b" " * 65536 + b"\n")
    reject("oversized line", (header + " " * 4097 + "\n" + rows[0]).encode())
    print(json.dumps({"selfTest": "passed", "passed": len(passed), "failed": 0, "skipped": 0,
                      "checks": passed, "nativeRecipeExecuted": False}, sort_keys=True), flush=True)
    return 0


class Recipe:
    def __init__(self, arguments):
        self.arguments = arguments
        self.started = time.monotonic()
        self.output = ROOT / "nvram-binding-native-002"
        self.output_created = False
        self.database = ROOT / "state/virmill/journal.db"
        self.host_env = {**os.environ, "LC_ALL": "C", "LANG": "C", "PATH": "/usr/bin:/usr/sbin:/bin:/sbin"}
        self.env = dict(self.host_env)
        self.sequence = 0
        self.stage = "preflight"
        self.trigger_name = None
        self.trigger_sql = None
        self.baseline = None
        self.guests = None
        self.plan = None
        self.job_id = None
        self.vm = None
        self.binding = None
        self.receipt = None
        self.xml_a = None
        self.xml_b = None
        self.disks = None
        self.report = {
            "recipe": "nvram-binding-native-002", "revision": arguments.revision,
            "deploymentManifestSHA256": arguments.deployment_sha256,
            "fault": "injected SQLite receipt-publication failure; not a crash or power loss",
            "guestStarted": False, "nvramInitializationVerified": False,
            "completeCaptureVerified": False, "independentRecoveryVerified": False,
            "guestBootVerified": False, "nativeCallCountsObserved": False,
            "checks": [], "failures": [],
        }

    def save(self, name, value):
        raw = value if isinstance(value, bytes) else (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
        with (self.output / name).open("xb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())

    def command(self, arguments, timeout=30):
        require(time.monotonic() - self.started < 1200, "recipe exceeded 20-minute command budget")
        self.sequence += 1
        output = [bytearray(), bytearray()]
        environment = self.env if arguments[0] == "/usr/bin/virmill" else self.host_env
        process = subprocess.Popen(arguments, env=environment, stdin=subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        deadline = time.monotonic() + timeout
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ, 0)
        selector.register(process.stderr, selectors.EVENT_READ, 1)
        try:
            while selector.get_map():
                require(time.monotonic() < deadline, "command timeout; accepted effects may remain")
                for event, _ in selector.select(min(0.1, max(0, deadline - time.monotonic()))):
                    block = os.read(event.fileobj.fileno(), 65536)
                    if not block:
                        selector.unregister(event.fileobj)
                    else:
                        output[event.data].extend(block)
                        require(sum(map(len, output)) <= MAX_OUTPUT, "command output exceeded 8 MiB")
            code = process.wait(timeout=max(0.01, deadline - time.monotonic()))
        except BaseException:
            # This terminates only this recipe's CLI/read command, never the
            # coordinator, hypervisor or accepted Virmill operation.
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass  # The command process group has already exited.
            process.wait(timeout=5)
            raise
        finally:
            selector.close()
            process.stdout.close()
            process.stderr.close()
            self.save("command-%03d.json" % self.sequence, {
                "stage": self.stage, "arguments": arguments, "exitCode": process.returncode,
                "stdout": bytes(output[0]).decode("utf-8", errors="replace"),
                "stderr": bytes(output[1]).decode("utf-8", errors="replace"),
            })
        return code, bytes(output[0]), bytes(output[1])

    def checked(self, arguments, timeout=30):
        code, out, err = self.command(arguments, timeout)
        require(code == 0, "command failed: %s; see private command log" % arguments[0])
        require(not err, "unexpected command stderr; see private command log")
        return out

    def cli(self, *arguments, expected_error=None, timeout=40):
        code, out, err = self.command(["/usr/bin/virmill", *arguments, "--connection", URI,
                                       "--output", "json", "--non-interactive", "--timeout", "2m"], timeout)
        response = strict_json(out)
        require(isinstance(response, dict) and set(response) == {"apiVersion", "data", "warnings", "error"},
                "unexpected CLI response envelope")
        require(response["apiVersion"] == "virmill/v1" and response["warnings"] == [],
                "unexpected CLI version or warnings")
        if expected_error is None:
            require(code == 0 and response["error"] is None and not err, "CLI did not succeed cleanly; see command response")
        else:
            failure = response["error"]
            require(code != 0 and isinstance(failure, dict) and failure["code"] == expected_error,
                    "CLI did not return expected " + expected_error)
            # cmd/virmill prints the returned typed error after the JSON
            # envelope. Require that exact line, with no unrelated diagnostics.
            require(err.decode("utf-8") == failure["code"] + ": " + failure["message"] + "\n",
                    "CLI stderr differs from its typed error")
        return response

    def virsh(self, *arguments):
        return self.checked(["/usr/bin/virsh", "--readonly", "-c", URI, *arguments])

    def connect(self, writable=False):
        connection = sqlite3.connect(self.database.as_uri() + ("?mode=rw" if writable else "?mode=ro"),
                                     uri=True, timeout=5)
        deadline = time.monotonic() + 10
        connection.set_progress_handler(lambda: int(time.monotonic() > deadline), 1000)
        return connection

    def snapshot(self):
        connection = self.connect()
        try:
            connection.execute("BEGIN")
            result, total, size = {}, 0, 0
            for table, (columns, order, keys) in TABLES.items():
                rows = {}
                for row in connection.execute("SELECT " + columns + " FROM " + table + " ORDER BY " + order):
                    total += 1
                    size += sum(len(x) if isinstance(x, (str, bytes)) else 8 for x in row)
                    require(total <= 100000 and size <= 32 << 20, "journal snapshot bound exceeded")
                    rows[row[:keys]] = row
                result[table] = rows
            return result
        finally:
            connection.close()

    def baseline_summary(self, snapshot):
        def cell(value):
            return {"bytesHex": value.hex()} if isinstance(value, bytes) else value
        return {table: {"count": len(rows), "rows": [
            {"key": list(key), "sha256": digest(json.dumps([cell(v) for v in row], separators=(",", ":")).encode())}
            for key, row in rows.items()]} for table, rows in snapshot.items()}

    def preserve_journal(self, allow_job=False, complete=False):
        current = self.snapshot()
        for table, old in self.baseline.items():
            require(all(current[table].get(key) == row for key, row in old.items()),
                    "pre-existing " + table + " row changed; preserve and investigate")
        plan_ids = set(self.baseline["plans"])
        if self.plan is not None:
            plan_ids.add((self.plan["planID"],))
        require(set(current["plans"]) == plan_ids, "unexpected new plan")
        if self.plan is not None:
            row = current["plans"][(self.plan["planID"],)]
            require(row == self.plan_row, "reviewed creation plan or input changed")
        added_jobs = set(current["jobs"]) - set(self.baseline["jobs"])
        if allow_job:
            require(added_jobs == {(self.job_id,)}, "unexpected new operation")
        else:
            require(not added_jobs, "preview changed jobs")
        for table in ("events", "dedup"):
            for key in set(current[table]) - set(self.baseline[table]):
                row = current[table][key]
                require(allow_job and (row[0] if table == "events" else row[2]) == self.job_id,
                        "unexpected new " + table + " row")
        allowed_metadata = set()
        if allow_job:
            allowed_metadata = {("vm-creation", self.plan["planID"]),
                                ("creation-nvram-declaration", self.plan["planID"])}
            if complete:
                allowed_metadata.add(("created-vm", "libvirt|" + URI + "|vm|" + self.vm))
        require(set(current["metadata"]) - set(self.baseline["metadata"]) <= allowed_metadata,
                "unrelated metadata was added")
        expected_locks = {} if complete or not allow_job else {
            (resource,): (resource, self.job_id) for resource in self.plan["resourceIDs"]}
        require(current["locks"] == expected_locks, "resource locks differ from retained operation")
        return current

    def stopped_xml(self, vm):
        canonical_uuid(vm)
        require(self.virsh("domstate", vm).strip() == b"shut off", "guest is not stopped: " + vm)
        info = {}
        for line in self.virsh("dominfo", vm).decode().splitlines():
            if ":" in line:
                key, value = line.split(":", 1)
                require(key not in info, "duplicate dominfo field")
                info[key] = value.strip()
        require(info["Persistent"] == "yes" and info["Autostart"] == "disable" and info["Managed save"] == "no",
                "guest persistence/autostart/managed-save boundary changed: " + vm)
        raw = self.virsh("dumpxml", "--inactive", vm)
        require(ET.fromstring(raw).findtext("uuid") == vm, "native XML identity mismatch")
        return raw

    def preserve_guests(self, new_xml=None):
        ids = set(self.virsh("list", "--all", "--uuid").decode().split())
        wanted = set(self.guests)
        if new_xml is not None:
            wanted.add(self.vm)
        require(ids == wanted, "native domain inventory changed unexpectedly")
        for vm, raw in self.guests.items():
            require(self.stopped_xml(vm) == raw, "pre-existing stopped native XML changed: " + vm)
        if new_xml is not None:
            require(self.stopped_xml(self.vm) == new_xml, "new guest native XML changed unexpectedly")

    def disk_metadata(self, path):
        # Only paths obtained from stopped disk declarations or fixed source
        # fixtures reach this boundary. NVRAM/TPM paths never do.
        path = pathlib.Path(path)
        require(path.is_absolute() and str(path) == os.path.normpath(path) and
                (path.is_relative_to(ROOT) or path.is_relative_to(pathlib.Path("/var/lib/libvirt/images"))),
                "disk path is outside approved disposable storage")
        before = self.checked(["/usr/bin/sudo", "-n", "/usr/bin/stat", "-c", "%F|%d|%i|%s|%y|%z", "--", str(path)])
        fields = before.decode().strip().split("|")
        require(len(fields) == 6 and fields[0] == "regular file" and 0 < int(fields[3]) <= 16 << 40,
                "disk is not a regular file within the metadata inventory bound")
        return {"stat": before.decode().strip()}

    def disk_evidence(self, path):
        before = self.disk_metadata(path)
        require(int(before["stat"].split("|")[3]) <= 64 << 20, "probe content hash exceeds 64 MiB")
        hashed = self.checked(["/usr/bin/sudo", "-n", "/usr/bin/sha256sum", "--", str(path)], timeout=120).decode()
        require(re.fullmatch("[a-f0-9]{64}  " + re.escape(str(path)) + "\n", hashed),
                "unexpected disk checksum output")
        after = self.disk_metadata(path)
        require(before == after, "stopped disk generation changed during hash")
        return {**before, "sha256": hashed[:64]}

    def disk_paths(self, xml):
        paths = set()
        for disk in ET.fromstring(xml).findall("./devices/disk"):
            source = disk.find("source")
            if source is None:  # Empty removable drive has no source bytes.
                continue
            require(disk.attrib["device"] in ("disk", "cdrom"), "unsupported existing disk class")
            if disk.attrib["type"] == "file":
                require(set(source.attrib) <= {"file", "index"} and source.get("file"), "ambiguous disk source")
                paths.add(source.attrib["file"])
            elif disk.attrib["type"] == "volume":
                require(set(source.attrib) <= {"pool", "volume", "index"}, "ambiguous volume source")
                paths.add(self.virsh("vol-path", "--pool", source.attrib["pool"], source.attrib["volume"]).decode().strip())
            else:
                raise RuntimeError("unsupported existing disk storage; no mutation authorized by recipe")
        return paths

    def preserve_disks(self):
        for path, before in self.disks.items():
            after = self.disk_evidence(path) if "sha256" in before else self.disk_metadata(path)
            require(after == before, "pre-existing disk/media evidence changed: " + path)

    def pool_volumes(self):
        old_names = {"virmill-" + vm + "-disk-000.qcow2" for vm in
                     ("d4c95f21-28bc-428d-9e5f-ceda025d279e", "f78674f3-bf3a-43e5-81f9-4283e2472024")}
        allowed = old_names | ({"virmill-" + self.vm + "-disk-000.qcow2"} if self.vm else set())
        names = parse_probe_volume_list(self.virsh("vol-list", "--pool", POOL_ID), allowed)
        require(old_names <= set(names), "probe pool omits an existing declared volume")
        return {name: self.virsh("vol-dumpxml", "--pool", POOL_ID, name) for name in names}

    def runtime(self):
        manifest_path = ROOT / "packages" / self.arguments.revision[:7] / "deployment.json"
        raw = read_regular(manifest_path)
        require(digest(raw) == self.arguments.deployment_sha256, "deployment manifest digest differs")
        manifest = strict_json(raw)
        require(manifest["revision"] == self.arguments.revision, "deployment revision differs")
        hashes = {}
        for name, path in (("virmill", "/usr/bin/virmill"), ("virmilld", "/usr/bin/virmilld")):
            expected = manifest["artifacts"]["build/bin/" + name]
            require(re.fullmatch("[a-f0-9]{64}", expected), "invalid deployment binary digest")
            hashes[name] = regular_hash(path)
            require(hashes[name] == expected, "installed binary differs from reviewed deployment")
        unit = "<test-vm-login>-" + self.arguments.revision[:7] + ".service"
        def property_value(name):
            return self.checked(["/usr/bin/systemctl", "--user", "show", unit, "-p", name, "--value"]).decode().strip()
        require(property_value("ActiveState") == "active" and property_value("WorkingDirectory") == str(ROOT),
                "expected disposable coordinator unit is not active at this root")
        pid = property_value("MainPID")
        require(re.fullmatch("[1-9][0-9]*", pid), "invalid coordinator PID")
        require(regular_hash("/proc/" + pid + "/exe", proc_executable=True) == hashes["virmilld"] and
                property_value("MainPID") == pid, "running coordinator differs from deployment")
        require(self.cli("version")["data"]["revision"] == self.arguments.revision, "runtime version differs")
        observed = {"unit": unit, "pid": pid, "binarySHA256": hashes}
        if "runtime" in self.report:
            require(self.report["runtime"] == observed, "coordinator process or deployment changed during recipe")
        self.report["runtime"] = observed

    def fault(self):
        plan_id = canonical_uuid(self.plan["planID"])
        self.trigger_name = "nvram_binding_native_002_" + plan_id.replace("-", "")
        self.fault_message = "nvram-binding-native-002 receipt publication " + plan_id
        self.trigger_sql = (
            "CREATE TRIGGER " + self.trigger_name + " BEFORE UPDATE ON metadata\n"
            "WHEN OLD.kind='vm-creation' AND NEW.kind='vm-creation' AND OLD.id='" + plan_id + "'\n"
            "AND NEW.id='" + plan_id + "' AND json_extract(CAST(NEW.body AS TEXT),'$.defined')=1\n"
            "BEGIN SELECT RAISE(ABORT,'" + self.fault_message + "'); END"
        )
        self.save("trigger.json", {"name": self.trigger_name, "sql": self.trigger_sql, "planID": plan_id})
        connection = self.connect(writable=True)
        try:
            require(not connection.execute("SELECT name FROM sqlite_master WHERE type='trigger'").fetchall(),
                    "existing journal trigger prevents this isolated fault test")
            connection.execute(self.trigger_sql)
            connection.commit()
        finally:
            connection.close()

    def remove_fault(self):
        if self.trigger_name is None:
            return
        connection = self.connect(writable=True)
        try:
            rows = connection.execute("SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?",
                                      (self.trigger_name,)).fetchall()
            if rows:
                require(rows == [(self.trigger_sql,)], "scoped trigger changed; do not remove unknown trigger")
                connection.execute("DROP TRIGGER " + self.trigger_name)
                connection.commit()
            require(not connection.execute("SELECT name FROM sqlite_master WHERE type='trigger' AND name=?",
                                           (self.trigger_name,)).fetchall(), "scoped trigger remains installed")
        finally:
            connection.close()

    def read_bound(self, complete=False):
        current = self.preserve_journal(allow_job=True, complete=complete)
        row = current["metadata"][("creation-nvram-declaration", self.plan["planID"])]
        receipt = current["metadata"][("vm-creation", self.plan["planID"])][2]
        if self.binding is not None:
            require(row[2] == self.binding, "first recorded binding/path/fingerprint bytes changed")
        binding = strict_json(row[2])
        value = strict_json(receipt)
        require(binding["schemaVersion"] == 1 and binding["planID"] == self.plan["planID"] and
                binding["inputDigest"] == self.plan["inputDigest"] and binding["operationID"] == self.job_id and
                binding["resource"] == {"providerID": "libvirt", "connectionID": URI, "kind": "vm", "resourceUUID": self.vm} and
                binding["creationBinding"] == value["binding"] and
                binding["firmwareDigest"] == self.plan["review"]["target"]["firmwareDigest"] and
                re.fullmatch("[a-f0-9]{64}", binding["observedFingerprint"]), "declaration identity differs")
        require(value["planID"] == self.plan["planID"] and value["operationID"] == self.job_id and
                value["vmID"] == self.vm and value["connection"] == URI and
                value["defined"] is complete and value["volumesVerified"] is True and
                value["guestBootVerified"] is False and len(value["volumes"]) == 1 and
                value["volumes"][0]["verified"] is True, "receipt stage differs")
        return row[2], receipt

    def result(self, name, complete=False):
        response = self.cli("vm", "creation", "result", self.job_id,
                            expected_error=None if complete else "RECOVERY_REQUIRED")
        self.save(name, response)
        data = response["data"]
        require(data["complete"] is complete and data["nvramDeclarationBound"] is True and
                data["nvramDeclarationStatus"] == "declaration-bound", "wrong visible declaration stage")
        for field in ("nvramInitializationVerified", "guestBootVerified", "setupVerified", "connectivityVerified"):
            require(data[field] is False, "unverified completion stage was claimed: " + field)
        require(data["nvramDeclaration"] == strict_json(self.binding), "result changed stored declaration")
        return data

    def reconcile_fault(self, name, code):
        before = self.snapshot()
        response = self.cli("operation", "reconcile", self.job_id, expected_error=code, timeout=150)
        self.save(name, response)
        require(self.snapshot() == before, "failed reconciliation changed journal rows")
        if code == "OPERATION_FAILED":
            require(self.fault_message in response["error"]["message"], "failure did not reach injected receipt boundary")
        else:
            require("assigned NVRAM declaration changed" in response["error"]["message"],
                    "path drift failed before the durable binding comparison")
        self.read_bound()
        self.preserve_disks()
        require(self.disk_evidence(self.new_disk) == self.new_disk_before, "reconcile changed copied disk identity/content")
        require(self.pool_volumes() == self.pool_after, "reconcile changed pool inventory")

    def run(self):
        require(os.geteuid() == 1000 and pathlib.Path.home() == ROOT.parent.parent,
                "recipe is only for ordinary-user <test-vm-login> on the approved disposable root")
        require(ROOT.is_dir() and ROOT.resolve() == ROOT, "approved root absent or symlinked")
        os.umask(0o077)
        self.output.mkdir(mode=0o700)  # Single-use: no --force or automatic retry.
        self.output_created = True
        environment = strict_json(read_regular(ROOT / "environment.json"))
        for key in ("XDG_STATE_HOME", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"):
            value = pathlib.Path(environment[key])
            require(value.is_absolute() and value.is_relative_to(ROOT) and value.resolve() == value,
                    "environment directory escapes disposable root")
            self.env[key] = str(value)
        require(pathlib.Path(self.env["XDG_STATE_HOME"]) / "virmill/journal.db" == self.database,
                "environment selects another journal")
        self.runtime()
        self.baseline = self.snapshot()
        require(not self.baseline["locks"], "old resource locks exist")
        for row in self.baseline["jobs"].values():
            require(strict_json(row[2])["state"] in ("succeeded", "failed", "partial", "canceled"),
                    "existing operation is active/uncertain")
        ids = self.virsh("list", "--all", "--uuid").decode().split()
        require(len(ids) == len(set(ids)) == 6, "expected exactly six existing disposable definitions")
        self.guests = {canonical_uuid(vm): self.stopped_xml(vm) for vm in ids}
        self.save("baseline-journal.json", self.baseline_summary(self.baseline))
        self.save("baseline-guests.json", {vm: digest(raw) for vm, raw in self.guests.items()})
        for vm, raw in self.guests.items():
            self.save("baseline-" + vm + ".xml", raw)
        source_image = str(ROOT / "sources/uefi-tpm-probe-v1/probe-fat.img")
        paths = {source_image}
        for xml in self.guests.values():
            paths.update(self.disk_paths(xml))
        require(len(paths) <= 32, "too many existing media paths")
        # Large earlier guests are inventoried without repeatedly hashing GBs.
        # Content reads are restricted to the public EFI source and the two
        # already recorded tiny probe-pool disks; every other path is stat-only.
        hash_paths = {source_image} | {
            str(POOL_DIRECTORY / ("virmill-" + vm + "-disk-000.qcow2")) for vm in
            ("f78674f3-bf3a-43e5-81f9-4283e2472024", "d4c95f21-28bc-428d-9e5f-ceda025d279e")}
        require(hash_paths <= paths, "earlier EFI probe disk declarations are missing")
        self.disks = {path: self.disk_evidence(path) if path in hash_paths else self.disk_metadata(path)
                      for path in sorted(paths)}
        require(self.disks[source_image]["sha256"] == SOURCE_SHA,
                "original EFI source media differs")
        require(all(self.disks[path]["sha256"] == PREPARED_SHA for path in hash_paths - {source_image}),
                "earlier EFI probe disk content differs")
        self.save("baseline-disks.json", self.disks)
        require(self.virsh("pool-name", POOL_ID).strip() == POOL_NAME.encode(), "probe pool identity differs")
        pool_xml = self.virsh("pool-dumpxml", POOL_ID)
        pool_tree = ET.fromstring(pool_xml)
        require(pool_tree.get("type") == "dir" and pool_tree.findtext("uuid") == POOL_ID and
                pool_tree.findtext("target/path") == str(POOL_DIRECTORY), "probe pool target differs")
        self.pool_before = self.pool_volumes()
        request = strict_json(read_regular(ROOT / "cold-probe-4m-creation-input.json"))
        require(set(request) == {"identityMode", "hardware"} and request["identityMode"] == "clone",
                "source request has unexpected provisioning or semantics")
        hardware = request["hardware"]
        require(hardware["firmware"] == FIRMWARE and hardware["poolID"] == POOL_ID and
                hardware["machine"] == "pc-q35-10.2" and hardware["architecture"] == "x86_64" and
                hardware["vcpus"] == 1 and hardware["memoryMiB"] == 512 and
                hardware["cpu"] == {"mode": "host-passthrough"} and hardware["clock"] == "utc" and
                hardware["graphics"] == "vnc-unix" and
                hardware["devicePolicy"] == {"version": 1, "chipset": "q35", "pciPlacement": "libvirt-auto",
                    "usbController": "none", "memoryBalloon": "none", "watchdogAction": "none",
                    "input": "ps2", "audio": "none", "serial": "isa-serial"} and
                hardware["nics"] == [] and hardware.get("media", []) == [] and
                hardware["disks"] == [{"sourceID": "boot", "bus": "sata", "bootOrder": 1}], "probe request differs")
        require("uuid" not in hardware, "request unexpectedly chooses an existing identity")
        hardware["name"] = "Virmill NVRAM binding native 002"
        require(all(ET.fromstring(raw).findtext("name") != hardware["name"] for raw in self.guests.values()),
                "single-use guest name already exists")
        self.save("creation-input.json", request)
        self.stage = "plan"
        response = self.cli("vm", "create", SOURCE_OPERATION, "--input", json.dumps(request), "--plan", timeout=90)
        self.save("creation-plan-response.json", response)
        self.plan = response["data"]
        plan = self.plan
        canonical_uuid(plan["planID"])
        self.vm = canonical_uuid(plan["review"]["target"]["spec"]["uuid"])
        self.report.update(planID=plan["planID"], planDigest=plan["planDigest"], vmID=self.vm)
        require((plan["planID"],) not in self.baseline["plans"] and self.vm not in self.guests,
                "creation reused an existing identity")
        require(plan["operation"] == "vm.create.devices-v1" and plan["actorUID"] == os.getuid() and
                plan["connectionID"] == URI and plan["acknowledgements"] == ACKS,
                "plan operation/actor/connection or exact acknowledgement IDs differ")
        reviewed = plan["review"]
        require(reviewed["nvramDeclarationVersion"] == 1 and reviewed["nvramInitializationVerified"] is False and
                reviewed["startsVM"] is False and reviewed["guestBootVerified"] is False and
                reviewed["provisioning"] is None and reviewed["sourceOperationID"] == SOURCE_OPERATION and
                reviewed["sourceDirectory"] == str(ROOT / "prepared/uefi-tpm-probe-v1") and
                reviewed["target"]["spec"] == {**hardware, "uuid": self.vm}, "review differs from stopped probe intent")
        require(len(reviewed["volumes"]) == 1 and reviewed["volumes"][0]["sha256"] == PREPARED_SHA and
                reviewed["volumes"][0]["fileBytes"] == 458752 and
                reviewed["volumes"][0]["virtualBytes"] == 33554432 and
                plan["estimates"]["additionalBytes"] == reviewed["requiredFreeBytes"] == 125829120 and
                isinstance(plan["estimates"]["notes"], str) and plan["estimates"]["notes"], "volume/budget review differs")
        require(self.cli("plan", "show", plan["planID"])["data"] == plan, "plan.show changed reviewed plan")
        self.plan_row = self.snapshot()["plans"][(plan["planID"],)]
        encoded_input = strict_json(self.plan_row[3])
        require(self.plan_row[1] == plan["planDigest"] and strict_json(self.plan_row[2]) == plan and
                encoded_input["nvramDeclarationVersion"] == 1, "stored plan lacks v1 recipe")
        prepared = encoded_input["artifact"]["disks"]
        require(encoded_input["directory"] == reviewed["sourceDirectory"] and len(prepared) == 1 and
                prepared[0]["sourceID"] == "boot" and prepared[0]["sha256"] == PREPARED_SHA,
                "stored preparation artifact differs")
        relative = pathlib.PurePosixPath(prepared[0]["path"])
        require(not relative.is_absolute() and relative.parts and ".." not in relative.parts and
                str(relative) == prepared[0]["path"], "prepared disk path is not canonical relative")
        prepared_path = str(pathlib.Path(encoded_input["directory"]) / relative)
        prepared_evidence = self.disk_evidence(prepared_path)
        require(prepared_evidence["sha256"] == PREPARED_SHA, "prepared EFI disk differs")
        self.disks[prepared_path] = prepared_evidence
        self.save("prepared-disk-evidence.json", {"path": prepared_path, **prepared_evidence})
        self.preserve_journal()
        self.preserve_guests()
        self.preserve_disks()
        require(self.pool_volumes() == self.pool_before, "planning changed probe-pool inventory")
        self.report["checks"].append("immutable new v1 plan; matching reviewed pool budget and exact acknowledgement IDs")
        self.stage = "injected receipt-publication failure"
        self.fault()
        arguments = ["plan", "apply", plan["planID"], "--digest", plan["planDigest"],
                     "--idempotency-key", "nvram-binding-native-002-" + plan["planID"]]
        for acknowledgement in ACKS:
            arguments.extend(["--ack", acknowledgement])
        response = self.cli(*arguments, "--wait", expected_error="OPERATION_FAILED", timeout=150)
        self.save("creation-apply-response.json", response)
        job = response["data"]
        self.job_id = canonical_uuid(job["operationID"])
        self.report["operationID"] = self.job_id
        require(job["planID"] == plan["planID"] and job["state"] == "recovery-required" and
                self.fault_message in response["error"]["message"], "apply missed injected receipt boundary")
        self.binding, self.receipt = self.read_bound()
        self.save("first-binding.json", self.binding)
        self.save("uncertain-receipt.json", self.receipt)
        self.xml_a = self.stopped_xml(self.vm)
        self.save("native-A.xml", self.xml_a)
        tree = ET.fromstring(self.xml_a)
        binding = strict_json(self.binding)
        a = binding["firmware"]["nvram"]["path"]
        require(tree.findtext("./os/nvram") == a and tree.findtext("./os/loader") == FIRMWARE["code"] and
                not tree.findall("./devices/interface"), "native A differs from stopped no-NIC declaration")
        expected_nvram = {"path": a, "format": "qcow2", "template": FIRMWARE["template"], "templateFormat": "qcow2"}
        require(binding["firmware"] == {"loader": FIRMWARE["code"], "loaderType": "pflash",
                "loaderReadOnly": "yes", "loaderSecure": "no", "loaderFormat": "qcow2",
                "loaderStateless": "", "nvram": expected_nvram}, "bound firmware declaration differs")
        require(len(tree.findall("./os/nvram")) == len(tree.findall("./os/loader")) == 1 and
                tree.find("./os/nvram").attrib == {key: value for key, value in expected_nvram.items() if key != "path"} and
                tree.find("./os/loader").attrib == {"readonly": "yes", "secure": "no", "type": "pflash", "format": "qcow2"},
                "native firmware attributes differ")
        tpm = tree.findall("./devices/tpm")
        require(len(tpm) == 1 and tpm[0].get("model") == "tpm-crb" and
                tpm[0].find("backend").get("type") == "emulator" and
                tpm[0].find("backend").get("version") == "2.0" and
                tpm[0].find("backend").get("persistent_state") == "yes", "native TPM declaration differs")
        require(pathlib.Path(a).parent == pathlib.Path("/var/lib/libvirt/qemu/nvram") and a.endswith(".qcow2"),
                "observed NVRAM declaration is outside the reviewed fault location")
        self.new_disk = str(POOL_DIRECTORY / ("virmill-" + self.vm + "-disk-000.qcow2"))
        require(self.disk_paths(self.xml_a) == {self.new_disk} and
                strict_json(self.receipt)["volumes"][0]["allocated"]["path"] == self.new_disk,
                "new disk location differs from reviewed volume")
        self.new_disk_before = self.disk_evidence(self.new_disk)
        require(self.new_disk_before["sha256"] == PREPARED_SHA, "copied probe disk bytes differ")
        self.save("new-disk-evidence.json", self.new_disk_before)
        self.pool_after = self.pool_volumes()
        require(set(self.pool_after) - set(self.pool_before) == {pathlib.Path(self.new_disk).name} and
                all(self.pool_after.get(key) == value for key, value in self.pool_before.items()),
                "creation changed an old pool volume or allocated unexpected volumes")
        self.preserve_guests(self.xml_a)
        self.preserve_disks()
        self.result("result-after-fault.json")
        self.report["checks"].append("first A binding durable before failed Defined receipt; resources and locks retained")
        self.stage = "same-A reconciliation with receipt fault retained"
        self.reconcile_fault("reconcile-A-response.json", "OPERATION_FAILED")
        self.preserve_guests(self.xml_a)
        self.result("result-after-same-A.json")
        self.report["checks"].append("same-A observation is idempotent: exact binding and historical fingerprint bytes retained")
        self.remove_fault()
        self.stage = "new guest declaration-only B fault"
        b = str(pathlib.Path(a).parent / ("virmill-nvram-binding-002-" + self.vm + "-B.qcow2"))
        require(all(ET.fromstring(raw).findtext("./os/nvram") != b for raw in self.guests.values()) and a != b,
                "fault path aliases an earlier declaration")
        for predicate in ("-e", "-L"):
            self.checked(["/usr/bin/sudo", "-n", "/usr/bin/test", "!", predicate, b])
        self.xml_b = replace_nvram(self.xml_a, a, b)
        self.save("native-B.xml", self.xml_b)
        self.report.update(firstPath=a, faultPath=b, firstBindingSHA256=digest(self.binding),
                           firstObservedFingerprint=binding["observedFingerprint"])
        self.preserve_guests(self.xml_a)
        self.checked(["/usr/bin/virsh", "-c", URI, "define", "--validate", str(self.output / "native-B.xml")])
        self.preserve_guests(self.xml_b)
        self.stage = "B reconciliation must refuse rebinding"
        self.reconcile_fault("reconcile-B-response.json", "SOURCE_CHANGED")
        self.preserve_guests(self.xml_b)
        self.result("result-after-B.json")
        self.report["checks"].append("B rejected by durable binding comparison; A/fingerprint/receipt/locks preserved")
        self.stage = "restore exact saved native A declaration"
        self.preserve_guests(self.xml_b)
        self.checked(["/usr/bin/virsh", "-c", URI, "define", "--validate", str(self.output / "native-A.xml")])
        self.preserve_guests(self.xml_a)
        self.stage = "final A reconciliation"
        response = self.cli("operation", "reconcile", self.job_id, timeout=150)
        self.save("reconcile-restored-A-response.json", response)
        require(response["data"]["operationID"] == self.job_id and response["data"]["state"] == "succeeded",
                "restored A did not reconcile existing operation")
        _, final_receipt = self.read_bound(complete=True)
        require(strict_json(final_receipt) == {**strict_json(self.receipt), "defined": True},
                "reconciliation changed allocation/upload receipt identity")
        self.result("result-complete.json", complete=True)
        self.preserve_guests(self.xml_a)
        self.preserve_disks()
        require(self.disk_evidence(self.new_disk) == self.new_disk_before and self.pool_volumes() == self.pool_after,
                "reconciliation changed retained disk identity/content or pool inventory")
        require(self.virsh("pool-dumpxml", POOL_ID) == pool_xml, "existing pool XML changed")
        self.runtime()
        self.report["checks"].append("restored A reconciles same operation; exact allocation receipt/disk generation/content retained; locks released")
        self.report.update(status="passed", allSixEarlierStoppedXMLPreserved=True,
                           priorJournalRowsPreserved=True, priorDeclaredDiskMetadataPreserved=True,
                           selectedProbeSourceAndDiskHashesPreserved=True,
                           olderLargeDiskBytesRehashed=False,
                           noReplayEvidence="Only operation reconcile was requested after initial apply; unchanged volume receipt, key, stat generation, content, inventory and stopped XML. Production reconciliation path is observation-only; no native method counter was collected.")

    def finish(self):
        # Always remove only the exact trigger created by this run. Do not
        # undefine/delete/start anything or repair an uncertain guest automatically.
        try:
            self.remove_fault()
            self.report["scopedTriggerRemoved"] = True
        except Exception as error:
            self.report["failures"].append("trigger cleanup: " + str(error))
            self.report["scopedTriggerRemoved"] = False
        if self.baseline is not None:
            try:
                current = self.snapshot()
                for table, rows in self.baseline.items():
                    require(all(current[table].get(key) == row for key, row in rows.items()),
                            "pre-existing journal row differs: " + table)
                self.report["priorJournalRowsPreserved"] = True
                self.report["remainingLocks"] = list(current["locks"].values())
                if self.plan is not None and self.job_id is None:
                    observed_jobs = [row[0] for row in current["jobs"].values() if row[1] == self.plan["planID"]]
                    self.report["observedCreationOperationIDs"] = observed_jobs
                    if len(observed_jobs) == 1:
                        self.report["operationID"] = observed_jobs[0]
                self.save("final-journal-summary.json", self.baseline_summary(current))
            except Exception as error:
                self.report["failures"].append("final journal observation: " + str(error))
        if self.guests is not None:
            try:
                ids = set(self.virsh("list", "--all", "--uuid").decode().split())
                require(set(self.guests) <= ids and ids - set(self.guests) <= ({self.vm} if self.vm else set()),
                        "final native domain inventory changed unexpectedly")
                for vm, raw in self.guests.items():
                    require(self.stopped_xml(vm) == raw, "earlier stopped XML changed: " + vm)
                self.report["allSixEarlierStoppedXMLPreserved"] = True
                if self.vm in ids:
                    current_xml = self.stopped_xml(self.vm)
                    self.save("final-native-observation.xml", current_xml)
                    self.report["finalNativeXMLSHA256"] = digest(current_xml)
            except Exception as error:
                self.report["failures"].append("final native observation: " + str(error))
        if self.disks is not None:
            try:
                self.preserve_disks()
                self.report["priorDeclaredDiskMetadataPreserved"] = True
                self.report["selectedProbeSourceAndDiskHashesPreserved"] = True
                self.report["olderLargeDiskBytesRehashed"] = False
            except Exception as error:
                self.report["failures"].append("final disk preservation: " + str(error))
        if self.report["failures"]:
            self.report["status"] = "uncertain-preserve-resources"
            self.report["nextAction"] = (
                "Do not rerun this single-use recipe, boot, delete, resume creation or clear locks. "
                "Review private command responses and the original plan/job. Inspect only the new UUID's stopped XML. "
                "If B remains, compare it with native-B.xml before separately reviewing restoration of native-A.xml "
                "and explicit operation reconcile. If trigger cleanup failed, verify exact trigger.json SQL before dropping only that trigger."
            )
        self.report["lastStage"] = self.stage
        if self.output_created:
            self.save("report.json", self.report)
        print(json.dumps(self.report, sort_keys=True), flush=True)
        return 1 if self.report["failures"] else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true", help="Run pure volume-table parser checks without native operations")
    parser.add_argument("--revision")
    parser.add_argument("--deployment-sha256")
    arguments = parser.parse_args()
    if arguments.self_test:
        require(arguments.revision is None and arguments.deployment_sha256 is None,
                "--self-test cannot be combined with native execution arguments")
        return self_test()
    if arguments.revision is None or arguments.deployment_sha256 is None:
        parser.error("native execution requires --revision and --deployment-sha256")
    require(re.fullmatch("[a-f0-9]{40}", arguments.revision), "expected full frozen revision")
    require(re.fullmatch("[a-f0-9]{64}", arguments.deployment_sha256), "expected deployment SHA256")
    recipe = Recipe(arguments)
    def interrupted(signum, frame):
        raise KeyboardInterrupt("signal " + str(signum))
    signal.signal(signal.SIGTERM, interrupted)
    try:
        recipe.run()
    except Exception as error:
        recipe.report["failures"].append(str(error))
    except KeyboardInterrupt:
        recipe.report["failures"].append("operator interrupted recipe; accepted effects may remain")
    return recipe.finish()


if __name__ == "__main__":
    sys.exit(main())
