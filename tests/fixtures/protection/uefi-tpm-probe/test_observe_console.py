#!/usr/bin/python3
"""Synthetic parser, subprocess and private-PTY tests; never invoke virsh or a VM."""
import base64
import contextlib
import errno
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

sys.dont_write_bytecode = True
import observe_console as observer

VM_UUID = "f78674f3-bf3a-43e5-81f9-4283e2472024"
SOURCE = Path(__file__).resolve().parent
OBSERVER_V3_SHA256 = "dc96fbc1ca5dae19be1d1b6f8f9a6a1101ac95d133442e4227d12804a251c5e6"


def result_line(result="SEEDED", ending=b"\r\n"):
    return (b"VIRMILL_PROBE RESULT=" + result.encode("ascii") + b" INDEX=" +
            observer.INDEX + b" MARKER=" + observer.PUBLIC_MARKER + ending)


class MarkerTests(unittest.TestCase):
    def test_protocol_constants_match_original_c_source(self):
        wire = (SOURCE / "wire.c").read_text()
        header = (SOURCE / "wire.h").read_text()
        probe = (SOURCE / "probe.c").read_text()
        initializer = re.search(r"probe_marker\[PROBE_MARKER_SIZE\] = \{([^}]+)\}", wire)[1]
        marker = "".join(re.findall(r"'([^'])'", initializer)).encode("ascii")
        size = int(re.search(r"#define PROBE_MARKER_SIZE (\d+)u", header)[1])
        index = int(re.search(r"#define PROBE_NV_INDEX UINT32_C\((0x[0-9a-f]+)\)", header)[1], 16)
        self.assertEqual(len(marker), size)
        self.assertEqual(marker, observer.PUBLIC_MARKER)
        self.assertEqual(f"0x{index:016x}".encode(), observer.INDEX)
        self.assertIn('char out[19] = "0x0000000000000000"', probe)
        self.assertEqual(set(re.findall(r'finish\("([A-Z_]+)"\)', probe)), set(observer.RESULTS))
        stages = {name.encode() for name in re.findall(r'failure\("([A-Z0-9_]+)"', probe)}
        self.assertEqual(stages, observer.FAILURE_STAGES)

    def test_exact_seeded_and_preserved_and_duplicate_mirror(self):
        for result in ("SEEDED", "PRESERVED"):
            for ending in (b"\r\n", b"\n"):
                with self.subTest(result=result, ending=ending):
                    parsed = observer.parse_markers(b"firmware noise\r\n" + result_line(result, ending) * 2)
                    self.assertEqual(parsed["result"], result)
                    self.assertEqual(parsed["result_lines"], 2)
                    self.assertIsNone(parsed["initial"])

    def test_failure_and_guard_refusal(self):
        prefix = b"VIRMILL_PROBE FAILURE_STAGE=NV_READ STATUS=0x0000000000000001\r\n"
        parsed = observer.parse_markers(prefix * 2 + result_line("FAIL") * 2)
        self.assertEqual(parsed["failure"], {"stage": "NV_READ", "status": "0x0000000000000001"})
        self.assertEqual(parsed["result"], "FAIL")
        self.assertEqual(observer.parse_markers(result_line("FAIL_AUXILIARY_STATE_MISMATCH"))["result"],
                         "FAIL_AUXILIARY_STATE_MISMATCH")
        raw = b"VIRMILL_PROBE RESULT=REFUSED_GUEST_GUARD NO_TPM_OR_NVRAM_ACCESS\r\n"
        self.assertEqual(observer.parse_markers(raw)["result"], "REFUSED_GUEST_GUARD")

    def test_exact_initial_version_and_serial(self):
        early = (b"VIRMILL_PROBE VERSION=1 DISPOSABLE_GUEST_ONLY\r\n"
                 b"VIRMILL_PROBE SERIAL=COM1_115200_8N1_GUEST_ONLY\r\n"
                 b"VIRMILL_PROBE INITIAL_TPM=ABSENT INITIAL_NVRAM=ABSENT\r\n")
        parsed = observer.parse_markers(early + result_line())
        self.assertEqual(parsed["initial"], ["ABSENT", "ABSENT"])
        self.assertEqual(parsed["version"], 1)
        self.assertEqual(parsed["serial"], "COM1_115200_8N1_GUEST_ONLY")

    def test_malformed_changed_or_terminal_decorated_markers_are_rejected(self):
        exact = result_line()
        invalid = [
            exact[:-1], exact.rstrip(b"\r\n"), exact + b"VIR", b"x" + exact,
            b"\x1b[32m" + exact, exact.replace(b"SEEDED", b"SEEDED\x00"),
            exact.replace(b"SEEDED", b"seeded"), exact.replace(b"SEEDED", b"PASS"),
            exact.replace(observer.INDEX, b"0x01564d50"), exact.replace(b"1564d50", b"1564D50"),
            exact.replace(observer.PUBLIC_MARKER, observer.PUBLIC_MARKER[:-1]),
            exact.replace(b"001", b"002"), exact.replace(b" INDEX=", b"  INDEX="),
            exact.replace(b"\r\n", b" \r\n"), exact.replace(b"\r\n", b"\r\r\n"),
            exact.replace(b"\r\n", b" extra=yes\r\n"), exact.replace(b"MARKER=VIRMILL", b"MARKER=VIRMILL\xff"),
            b"VIRMILL_PROBE VERSION=2 DISPOSABLE_GUEST_ONLY\r\n",
            b"VIRMILL_PROBE FAILURE_STAGE=UNKNOWN STATUS=0x0000000000000001\r\n",
            b"VIRMILL_PROBE FAILURE_STAGE=NV_READ STATUS=0x1\r\n",
            b"VIRMILL_PROBE FAILURE_STAGE=NV_READ STATUS=0x00000000000000FF\r\n",
        ]
        for raw in invalid:
            with self.subTest(raw=raw), self.assertRaises(observer.MarkerError):
                observer.parse_markers(raw)

    def test_conflicting_results_failure_stages_and_initial_states(self):
        invalid = [
            result_line() + result_line("PRESERVED"), result_line() + result_line("FAIL"),
            b"VIRMILL_PROBE FAILURE_STAGE=NV_READ STATUS=0x0000000000000001\n" + result_line(),
            b"VIRMILL_PROBE FAILURE_STAGE=NV_READ STATUS=0x0000000000000001\n"
            b"VIRMILL_PROBE FAILURE_STAGE=NV_WRITE STATUS=0x0000000000000001\n" + result_line("FAIL"),
            b"VIRMILL_PROBE INITIAL_TPM=PRESENT INITIAL_NVRAM=PRESENT\n" + result_line(),
            b"VIRMILL_PROBE INITIAL_TPM=ABSENT INITIAL_NVRAM=ABSENT\n" + result_line("PRESERVED"),
            b"VIRMILL_PROBE INITIAL_TPM=ABSENT INITIAL_NVRAM=ABSENT\n" + result_line("FAIL_AUXILIARY_STATE_MISMATCH"),
            b"VIRMILL_PROBE INITIAL_TPM=ABSENT INITIAL_NVRAM=ABSENT\n"
            b"VIRMILL_PROBE INITIAL_TPM=PRESENT INITIAL_NVRAM=PRESENT\n" + result_line(),
            b"VIRMILL_PROBE SERIAL=COM1_115200_8N1_GUEST_ONLY\n"
            b"VIRMILL_PROBE SERIAL=UNAVAILABLE EFI_CONSOLE_ONLY\n" + result_line(),
        ]
        for raw in invalid:
            with self.subTest(raw=raw), self.assertRaises(observer.MarkerError):
                observer.parse_markers(raw)

    def test_parser_bound_and_absence(self):
        self.assertIsNone(observer.parse_markers(b"noise\n")["result"])
        self.assertIsNone(observer.parse_markers(b"")["result"])
        with self.assertRaises(observer.MarkerError):
            observer.parse_markers(b"\n" * (observer.OUTPUT_LIMIT + 1))


class HistoricalPatchTests(unittest.TestCase):
    def test_frozen_v3_reconstructs_exact_historical_files_without_execution(self):
        original = (SOURCE / "observe_console.py").read_bytes()
        self.assertEqual(hashlib.sha256(original).hexdigest(), OBSERVER_V3_SHA256,
                         "observer changed: preserve and review historical reconstruction before updating this pin")
        patch = shutil.which("patch")
        self.assertIsNotNone(patch, "offline historical reconstruction requires installed GNU patch")
        variants = {
            "debug": "ffa5ce3ebaaeb2360fb838d68b8895bc24aa62e84751034fae9ba02f1c2d58f4",
            "pty": "4d2232a9570e5089fb5704db1f7f398ec3702a48027526144f5978603a415261",
        }
        for variant, expected in variants.items():
            with self.subTest(variant=variant), tempfile.TemporaryDirectory(prefix="virmill-observer-replay-") as directory:
                copy = Path(directory) / "observe_console.py"
                copy.write_bytes(original)
                result = subprocess.run(
                    [patch, "--batch", "--fuzz=0", "--no-backup-if-mismatch", "-p0", "-i",
                     str(SOURCE / f"observed-{variant}-variant.patch")],
                    cwd=directory, capture_output=True, text=True, timeout=5, check=False,
                    env={"LC_ALL": "C", "PATH": os.defpath})
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(result.stdout, "patching file observe_console.py\n")
                self.assertEqual(result.stderr, "")
                self.assertEqual(hashlib.sha256(copy.read_bytes()).hexdigest(), expected)
                self.assertEqual(set(Path(directory).iterdir()), {copy})
        self.assertEqual((SOURCE / "observe_console.py").read_bytes(), original)


class ObservationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="virmill-console-test-")
        self.addCleanup(self.temp.cleanup)
        self.log = Path(self.temp.name) / "raw.log"
        self.children = []
        # Root refusal has its own tests. This seam never launches native tools,
        # even if a CI runner happens to invoke synthetic tests as root.
        self.uid = mock.patch.multiple(observer.os, getuid=lambda: 1000, geteuid=lambda: 1000)
        self.uid.start()
        self.addCleanup(self.uid.stop)
        self.addCleanup(self.reap_children)

    def reap_children(self):
        for child in self.children:
            if child.poll() is None:
                child.kill()
                child.wait(timeout=1)

    def spawn_scripts(self, scripts, hook=None):
        scripts = iter(scripts)

        def spawn(argv, **kwargs):
            self.assertEqual(argv, ["virsh", "--quiet", "--no-pkttyagent", "--connect",
                                    "qemu:///system", "console", VM_UUID, "--safe"])
            self.assertTrue(os.isatty(kwargs["stdin"]))
            self.assertTrue(kwargs["close_fds"])
            self.assertTrue(kwargs["start_new_session"])
            self.assertEqual(kwargs["env"]["LC_ALL"], "C")
            self.assertFalse("VIRSH_DEBUG" in kwargs["env"], "child environment contains VIRSH_DEBUG")
            self.assertFalse("VIRSH_LOG_FILE" in kwargs["env"], "child environment contains VIRSH_LOG_FILE")
            if hook:
                hook(kwargs)
            # Replace the fixed virsh argv at the seam: real local Python plus
            # ordinary-user PTYs/pipes, never native libvirt or guest execution.
            child = subprocess.Popen([sys.executable, "-I", "-c", next(scripts)], **kwargs)
            self.children.append(child)
            return child
        return spawn

    def run_observer(self, scripts, **kwargs):
        report = observer.observe(VM_UUID, "qemu:///system", "SEEDED", self.log,
                                  popen=self.spawn_scripts(scripts), **kwargs)
        for child in self.children:
            self.assertIsNotNone(child.poll(), "observer left its client alive")
            self.assertTrue(child.stdout.closed)
            self.assertTrue(child.stderr.closed)
        self.assertEqual(report["log_bytes"], self.log.stat().st_size)
        self.assertEqual(report["log_sha256"], hashlib.sha256(self.log.read_bytes()).hexdigest())
        self.assertLessEqual(report["log_bytes"], observer.OUTPUT_LIMIT)
        self.assertTrue(report["observation_only"])
        return report

    def test_stream_fragments_preserve_raw_without_guest_input(self):
        raw = b"\x1b[2Jfirmware noise\r\n" + result_line() * 2
        # The child requires a raw, non-echo terminal and proves no input bytes
        # arrive on it. Output includes CRLF and controls without PTY rewriting.
        script = ("import os,select,termios,time\n"
                  "a=termios.tcgetattr(0)\n"
                  "assert not a[3] & (termios.ECHO | termios.ICANON)\n"
                  "assert select.select([0],[],[],0.05)[0] == []\n"
                  f"raw={raw!r}\n"
                  "for start in range(0,len(raw),7):\n"
                  " os.write(1,raw[start:start+7]); time.sleep(0.001)\n")
        report = self.run_observer([script])
        self.assertEqual(report["status"], "observed")
        self.assertEqual(report["result_lines"], 2)
        self.assertEqual(self.log.read_bytes(), raw)
        self.assertEqual(stat.S_IMODE(self.log.stat().st_mode), 0o600)

    def test_not_running_retries_then_observes_same_uuid(self):
        stopped = "import os; os.write(2,b'error: The domain is not running\\n'); raise SystemExit(1)"
        report = self.run_observer([stopped, stopped, f"import os; os.write(1,{result_line()!r})"])
        self.assertEqual(report["status"], "observed")
        self.assertEqual(len(report["attempts"]), 3)
        self.assertEqual([a["log_start"] for a in report["attempts"]], [0, 0, 0])
        self.assertEqual(base64.b64decode(report["attempts"][0]["stderr_base64"]),
                         b"error: The domain is not running\n")

    def test_stopped_then_unassigned_pty_retries_then_observes_same_uuid(self):
        stopped = b"error: The domain is not running\n"
        unassigned = b"error: operation failed: PTY device is not yet assigned\n"
        scripts = [f"import os; os.write(2,{diagnostic!r}); raise SystemExit(1)"
                   for diagnostic in (stopped, unassigned)]
        report = self.run_observer(scripts + [f"import os; os.write(1,{result_line()!r})"])
        self.assertEqual(report["status"], "observed")
        self.assertEqual(len(report["attempts"]), 3)
        self.assertEqual([a["log_start"] for a in report["attempts"]], [0, 0, 0])
        self.assertEqual(base64.b64decode(report["attempts"][1]["stderr_base64"]), unassigned)

    def test_unassigned_pty_requires_exact_diagnostic_and_zero_stdout(self):
        exact = b"error: operation failed: PTY device is not yet assigned\n"
        invalid = [b"console: safe(bool)\n" + exact, exact + b"console: retry\n",
                   exact.replace(b"PTY", b"pty"), exact.replace(b"not yet", b"not"),
                   exact[:-1], exact.replace(b"\n", b"\r\n"), b" " + exact,
                   exact.replace(b"\n", b" \n")]
        cases = [(b"", diagnostic) for diagnostic in invalid]
        cases += [(output, exact) for output in (b"\n", b"firmware booted\n", result_line()[:35], result_line())]
        for output, diagnostic in cases:
            with self.subTest(output=output, diagnostic=diagnostic):
                self.log = Path(self.temp.name) / str(len(self.children))
                script = (f"import os; os.write(1,{output!r}); os.write(2,{diagnostic!r}); "
                          "raise SystemExit(1)")
                report = self.run_observer([script])
                self.assertNotEqual(report["status"], "observed")
                self.assertEqual(len(report["attempts"]), 1)
                self.assertEqual(self.log.read_bytes(), output)

    def test_inherited_debug_and_log_options_are_absent_from_child_environment(self):
        log_setting = Path(self.temp.name) / "ambient-virsh.log"
        script = ("import os; assert 'VIRSH_DEBUG' not in os.environ; "
                  "assert 'VIRSH_LOG_FILE' not in os.environ; "
                  f"os.write(1,{result_line()!r})")
        with mock.patch.dict(os.environ, {"VIRSH_DEBUG": "4", "VIRSH_LOG_FILE": str(log_setting)}):
            report = self.run_observer([script])
        self.assertEqual(report["status"], "observed")
        self.assertFalse(log_setting.exists())

    def test_only_exact_stopped_error_is_retryable(self):
        for diagnostic in (b"error: Active console session exists for this domain\n",
                           b"error: authentication failed\n", b"error: safe console unsupported\n",
                           b"console: safe(bool)\nerror: The domain is not running\n",
                           b"error: unrelated domain is not running\n"):
            with self.subTest(diagnostic=diagnostic):
                self.log = Path(self.temp.name) / str(len(self.children))
                report = self.run_observer([f"import os; os.write(2,{diagnostic!r}); raise SystemExit(1)"])
                self.assertEqual(report["status"], "console_error")
                self.assertEqual(len(report["attempts"]), 1)

    def test_stopped_attempt_ceiling(self):
        stopped = "import os; os.write(2,b'error: The domain is not running\\n'); raise SystemExit(1)"
        report = self.run_observer([stopped] * 3, max_attempts=3, retry_interval=0.05)
        self.assertEqual(report["status"], "attempt_limit")
        self.assertEqual(len(report["attempts"]), 3)

    def test_alternate_exact_stopped_diagnostic_is_retryable(self):
        stopped = ("import os; os.write(2,b'error: Requested operation is not valid: "
                   "domain is not running\\n'); raise SystemExit(1)")
        report = self.run_observer([stopped, f"import os; os.write(1,{result_line()!r})"])
        self.assertEqual(report["status"], "observed")
        self.assertEqual(len(report["attempts"]), 2)

    def test_no_reconnect_after_guest_output_or_partial_marker(self):
        for output in (b"firmware booted\n", result_line()[:35]):
            with self.subTest(output=output):
                self.log = Path(self.temp.name) / str(len(self.children))
                script = (f"import os; os.write(1,{output!r}); "
                          "os.write(2,b'error: The domain is not running\\n'); raise SystemExit(1)")
                report = self.run_observer([script])
                self.assertNotEqual(report["status"], "observed")
                self.assertEqual(len(report["attempts"]), 1)

    def test_stderr_cannot_supply_a_guest_marker(self):
        report = self.run_observer([f"import os; os.write(2,{result_line()!r})"])
        self.assertEqual(report["status"], "no_result")
        self.assertIsNone(report["result"])
        self.assertEqual(self.log.read_bytes(), b"")

    def test_expected_result_failure_and_nonzero_client_exit(self):
        cases = [(result_line("PRESERVED"), 0, "unexpected_result"),
                 (result_line("FAIL"), 0, "probe_failed"),
                 (result_line("FAIL_AUXILIARY_STATE_MISMATCH"), 0, "probe_failed"),
                 (result_line(), 1, "console_error"),
                 (result_line() + result_line("PRESERVED"), 0, "malformed_marker")]
        for raw, code, status in cases:
            with self.subTest(status=status):
                self.log = Path(self.temp.name) / str(len(self.children))
                report = self.run_observer([f"import os; os.write(1,{raw!r}); raise SystemExit({code})"])
                self.assertEqual(report["status"], status)
                self.assertEqual(self.log.read_bytes(), raw)

    def test_guest_output_cap_including_a_valid_marker_is_inconclusive(self):
        for size in (observer.OUTPUT_LIMIT, observer.OUTPUT_LIMIT + 8192):
            with self.subTest(size=size):
                self.log = Path(self.temp.name) / str(len(self.children))
                raw = result_line() + b"x" * (size - len(result_line()))
                report = self.run_observer([f"import os,time; os.write(1,{raw!r}); time.sleep(5)"])
                self.assertEqual(report["status"], "output_limit")
                self.assertEqual(self.log.read_bytes(), raw[:observer.OUTPUT_LIMIT])

    def test_console_reads_never_cross_guest_byte_budget(self):
        output_fds, reads = set(), []
        actual_read = observer.os.read
        spawn = self.spawn_scripts(["import os,time; os.write(1,b'x'*8192); time.sleep(5)"])

        def launched(*args, **kwargs):
            child = spawn(*args, **kwargs)
            output_fds.add(child.stdout.fileno())
            return child

        def read(fd, capacity):
            value = actual_read(fd, capacity)
            if fd in output_fds:
                reads.append((capacity, len(value)))
            return value

        with mock.patch.object(observer.os, "read", side_effect=read):
            report = observer.observe(VM_UUID, "qemu:///system", "SEEDED", self.log, popen=launched)
        self.assertEqual(report["status"], "output_limit")
        received = 0
        for requested, size in reads:
            self.assertLessEqual(requested, observer.OUTPUT_LIMIT - received)
            received += size
        self.assertEqual(received, observer.OUTPUT_LIMIT)

    def test_diagnostic_output_cap_is_independent_and_bounded(self):
        script = f"import os,time; os.write(2,b'x'*{observer.DIAGNOSTIC_LIMIT + 8192}); time.sleep(5)"
        report = self.run_observer([script])
        self.assertEqual(report["status"], "diagnostic_limit")
        self.assertEqual(report["diagnostic_bytes"], observer.DIAGNOSTIC_LIMIT)
        self.assertEqual(len(base64.b64decode(report["attempts"][0]["stderr_base64"])),
                         observer.DIAGNOSTIC_LIMIT)

    def test_hanging_client_is_killed_and_reaped_within_overall_timeout(self):
        script = ("import os,signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); "
                  f"os.write(1,{result_line()!r}); time.sleep(10)")
        started = time.monotonic()
        report = self.run_observer([script], timeout=1)
        self.assertEqual(report["status"], "timeout")
        self.assertLess(time.monotonic() - started, 1.5)
        self.assertEqual(report["result"], "SEEDED")

    def test_closed_pipes_do_not_mean_process_exit(self):
        script = "import os,time; os.close(1); os.close(2); time.sleep(10)"
        report = self.run_observer([script], timeout=1)
        self.assertEqual(report["status"], "timeout")

    def test_cancellation_preserves_log_reaps_only_client(self):
        cancel = threading.Event()
        timer = threading.Timer(0.15, cancel.set)
        timer.start()
        self.addCleanup(timer.cancel)
        script = f"import os,time; os.write(1,{result_line()!r}); time.sleep(10)"
        report = self.run_observer([script], cancelled=cancel.is_set)
        self.assertEqual(report["status"], "cancelled")
        self.assertEqual(self.log.read_bytes(), result_line())
        self.assertLess(report["elapsed_seconds"], 1)

    def test_cancellation_after_final_eof_cannot_become_observed(self):
        cancel = threading.Event()
        parse = observer.parse_markers

        def parsed(raw):
            result = parse(raw)
            if raw:
                self.assertTrue(all(child.poll() == 0 for child in self.children))
                cancel.set()
            return result

        with mock.patch.object(observer, "parse_markers", side_effect=parsed):
            report = self.run_observer([f"import os; os.write(1,{result_line()!r})"], cancelled=cancel.is_set)
        self.assertEqual(report["status"], "cancelled")
        self.assertEqual(report["result"], "SEEDED")

    def test_deadline_after_final_eof_cannot_become_observed(self):
        parse, clock = observer.parse_markers, observer.time.monotonic
        elapsed = [0]

        def parsed(raw):
            result = parse(raw)
            if raw:
                self.assertTrue(all(child.poll() == 0 for child in self.children))
                elapsed[0] = 2
            return result

        with mock.patch.object(observer, "parse_markers", side_effect=parsed), \
                mock.patch.object(observer.time, "monotonic", side_effect=lambda: clock() + elapsed[0]):
            report = self.run_observer([f"import os; os.write(1,{result_line()!r})"], timeout=1)
        self.assertEqual(report["status"], "timeout")
        self.assertEqual(report["result"], "SEEDED")

    def test_cleanup_leaves_an_unrelated_process_alive_and_sends_no_group_signal(self):
        unrelated = subprocess.Popen([sys.executable, "-I", "-c", "import time; time.sleep(10)"],
                                     stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                     stderr=subprocess.DEVNULL, close_fds=True)
        self.addCleanup(lambda: unrelated.wait(timeout=1))
        self.addCleanup(unrelated.kill)
        kill, targets = observer.os.kill, []

        def kill_client(pid, sig):
            self.assertIn(pid, [child.pid for child in self.children], "signal target is not the observer's child")
            targets.append(pid)
            return kill(pid, sig)

        with mock.patch.object(observer.os, "killpg", side_effect=AssertionError("process group signal")), \
                mock.patch.object(observer.os, "kill", side_effect=kill_client):
            script = "import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(10)"
            report = self.run_observer([script], timeout=1)
        self.assertEqual(report["status"], "timeout")
        self.assertTrue(targets)
        self.assertIsNone(unrelated.poll())

    def test_cancelled_before_spawn(self):
        report = observer.observe(VM_UUID, "qemu:///system", "SEEDED", self.log,
                                  cancelled=lambda: True, popen=mock.Mock(side_effect=AssertionError("spawn")))
        self.assertEqual(report["status"], "cancelled")
        self.assertEqual(report["attempts"], [])

    def test_cancellation_during_retry_does_not_spawn_another_client(self):
        cancel = threading.Event()
        timer = threading.Timer(0.15, cancel.set)
        timer.start()
        self.addCleanup(timer.cancel)
        stopped = "import os; os.write(2,b'error: The domain is not running\\n'); raise SystemExit(1)"
        report = self.run_observer([stopped], retry_interval=1, cancelled=cancel.is_set)
        self.assertEqual(report["status"], "cancelled")
        self.assertEqual(len(report["attempts"]), 1)

    def test_success_also_closes_both_private_pty_descriptors(self):
        fds = []
        real_openpty = observer.pty.openpty

        def opened():
            pair = real_openpty()
            fds.extend(pair)
            return pair

        with mock.patch.object(observer.pty, "openpty", side_effect=opened):
            report = self.run_observer([f"import os; os.write(1,{result_line()!r})"])
        self.assertEqual(report["status"], "observed")
        for fd in fds:
            with self.assertRaises(OSError):
                os.fstat(fd)

    def test_unreapable_client_has_bounded_cleanup_and_explicit_failure(self):
        # Entirely synthetic wait seam: no real child is left running here.
        child = mock.Mock()
        child.poll.return_value = None
        child.wait.side_effect = subprocess.TimeoutExpired("synthetic client", 0.01)
        self.assertFalse(observer._cleanup(child, time.monotonic() + 0.01))
        child.terminate.assert_called_once_with()
        child.kill.assert_called_once_with()
        self.assertEqual(child.wait.call_count, 2)
        for call in child.wait.call_args_list:
            self.assertGreaterEqual(call.kwargs["timeout"], 0)
            self.assertLessEqual(call.kwargs["timeout"], 0.01)

    def test_spawn_failure_closes_private_pty(self):
        fds = []
        real_openpty = observer.pty.openpty

        def opened():
            pair = real_openpty()
            fds.extend(pair)
            return pair

        with mock.patch.object(observer.pty, "openpty", side_effect=opened):
            report = observer.observe(VM_UUID, "qemu:///system", "SEEDED", self.log,
                                      popen=mock.Mock(side_effect=FileNotFoundError(2, "missing virsh")))
        self.assertEqual(report["status"], "io_error")
        for fd in fds:
            with self.assertRaises(OSError):
                os.fstat(fd)

    def test_terminal_setup_error_closes_pty_without_spawning(self):
        fds = []
        openpty = observer.pty.openpty

        def opened():
            pair = openpty()
            fds.extend(pair)
            return pair

        with mock.patch.object(observer.pty, "openpty", side_effect=opened), \
                mock.patch.object(observer.tty, "setraw", side_effect=OSError(errno.EIO, "synthetic PTY failure")):
            report = observer.observe(VM_UUID, "qemu:///system", "SEEDED", self.log,
                                      popen=mock.Mock(side_effect=AssertionError("spawn")))
        self.assertEqual(report["status"], "io_error")
        self.assertEqual(report["log_bytes"], 0)
        for fd in fds:
            with self.assertRaises(OSError):
                os.fstat(fd)

    def test_selector_setup_error_reaps_spawned_client(self):
        with mock.patch.object(observer.os, "set_blocking", side_effect=OSError(errno.EIO, "synthetic pipe failure")):
            report = self.run_observer(["import time; time.sleep(10)"])
        self.assertEqual(report["status"], "io_error")
        self.assertEqual(report["log_bytes"], 0)

    def test_partial_log_writes_are_completed(self):
        original = observer.os.fdopen

        class PartialLog:
            def __init__(self, *args, **kwargs):
                self.file = original(*args, **kwargs)

            def __enter__(self):
                return self

            def __exit__(self, *args):
                self.file.close()

            def write(self, chunk):
                return self.file.write(chunk[:3])

        with mock.patch.object(observer.os, "fdopen", PartialLog):
            report = self.run_observer([f"import os; os.write(1,{result_line()!r})"])
        self.assertEqual(report["status"], "observed")
        self.assertEqual(self.log.read_bytes(), result_line())

    def test_log_write_failure_retains_only_written_prefix_and_reaps_client(self):
        original = observer.os.fdopen

        class FailingLog:
            def __init__(self, *args, **kwargs):
                self.file = original(*args, **kwargs)
                self.writes = 0

            def __enter__(self):
                return self

            def __exit__(self, *args):
                self.file.close()

            def write(self, chunk):
                self.writes += 1
                if self.writes > 1:
                    raise OSError(errno.ENOSPC, "synthetic full evidence filesystem")
                return self.file.write(chunk[:7])

        with mock.patch.object(observer.os, "fdopen", FailingLog):
            report = self.run_observer([f"import os,time; os.write(1,{result_line()!r}); time.sleep(10)"])
        self.assertEqual(report["status"], "io_error")
        self.assertEqual(report["detail"], "OS error " + str(errno.ENOSPC))
        self.assertEqual(self.log.read_bytes(), result_line()[:7])
        self.assertIsNone(report["result"])

    def test_zero_length_log_write_is_failure_not_a_busy_loop(self):
        original = observer.os.fdopen

        class ZeroLog:
            def __init__(self, *args, **kwargs):
                self.file = original(*args, **kwargs)

            def __enter__(self):
                return self

            def __exit__(self, *args):
                self.file.close()

            def write(self, _chunk):
                return 0

        with mock.patch.object(observer.os, "fdopen", ZeroLog):
            report = self.run_observer([f"import os,time; os.write(1,{result_line()!r}); time.sleep(10)"])
        self.assertEqual(report["status"], "io_error")
        self.assertEqual(report["detail"], "OS error " + str(errno.EIO))
        self.assertEqual(self.log.read_bytes(), b"")

    def test_existing_log_and_symlink_are_never_overwritten(self):
        sentinel = b"keep existing evidence"
        self.log.write_bytes(sentinel)
        for path in (self.log, Path(self.temp.name) / "symlink"):
            if path != self.log:
                path.symlink_to(self.log)
            with self.subTest(path=path), self.assertRaises(FileExistsError):
                observer.observe(VM_UUID, "qemu:///system", "SEEDED", path,
                                 popen=mock.Mock(side_effect=AssertionError("spawn")))
        self.assertEqual(self.log.read_bytes(), sentinel)

    def test_input_validation_before_open_or_spawn(self):
        cases = [{"vm_uuid": bad} for bad in ("name", "1", VM_UUID.upper(), " " + VM_UUID,
                 VM_UUID + "; start other", "00000000-0000-0000-0000-000000000000", "{ " + VM_UUID + " }")]
        cases += [{"uri": bad} for bad in ("", "qemu+ssh://host/system", "qemu:///system?socket=/tmp/other")]
        cases += [{"timeout": bad} for bad in (float("nan"), float("inf"), 0, 121)]
        cases += [{"retry_interval": bad} for bad in (float("nan"), float("inf"), 0, 2)]
        cases += [{"max_attempts": bad} for bad in (0, 101, 1.5, True)]
        cases += [{"expected": "FAIL"}]
        for overrides in cases:
            args = {"vm_uuid": VM_UUID, "uri": "qemu:///system", "expected": "SEEDED", "log_path": self.log}
            args.update(overrides)
            with self.subTest(overrides=overrides), self.assertRaises(ValueError):
                observer.observe(**args, popen=mock.Mock(side_effect=AssertionError("spawn")))
            self.assertFalse(self.log.exists())

    def test_real_or_effective_root_is_refused(self):
        for real, effective in ((0, 0), (0, 1000), (1000, 0)):
            with mock.patch.multiple(observer.os, getuid=lambda: real, geteuid=lambda: effective):
                with self.assertRaisesRegex(ValueError, "ordinary user"):
                    observer.observe(VM_UUID, "qemu:///system", "SEEDED", self.log,
                                     popen=mock.Mock(side_effect=AssertionError("spawn")))
            self.assertFalse(self.log.exists())

    def test_main_json_does_not_render_untrusted_terminal_data(self):
        output = io.StringIO()
        fake = {"status": "console_error", "detail": "\x1b]52;c;ZXZpbA==\x07\n", "observation_only": True}
        with mock.patch.object(observer, "observe", return_value=fake), contextlib.redirect_stdout(output):
            code = observer.main(["--uuid", VM_UUID, "--uri", "qemu:///system", "--expect", "SEEDED", "--log", str(self.log)])
        self.assertEqual(code, 1)
        self.assertNotIn("\x1b", output.getvalue())
        self.assertEqual(json.loads(output.getvalue()), fake)

    def test_main_cancellation_handler_reports_130_and_restores_handlers(self):
        before = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
        observe = observer.observe

        def cancelled_at_launch(_kwargs):
            # Deliver through the installed callback seam: no real signal is
            # sent to the test runner, another console, or an unrelated process.
            handler = signal.getsignal(signal.SIGTERM)
            self.assertTrue(callable(handler))
            handler(signal.SIGTERM, None)

        def observing(*args, **kwargs):
            kwargs["popen"] = self.spawn_scripts(["import time; time.sleep(10)"], hook=cancelled_at_launch)
            return observe(*args, **kwargs)

        output = io.StringIO()
        with mock.patch.object(observer, "observe", side_effect=observing), contextlib.redirect_stdout(output):
            code = observer.main(["--uuid", VM_UUID, "--uri", "qemu:///system", "--expect", "SEEDED", "--log", str(self.log)])
        self.assertEqual(code, 130)
        self.assertEqual(json.loads(output.getvalue())["status"], "cancelled")
        for child in self.children:
            self.assertIsNotNone(child.poll())
            self.assertTrue(child.stdout.closed)
            self.assertTrue(child.stderr.closed)
        self.assertEqual({sig: signal.getsignal(sig) for sig in before}, before)

    def test_help_matches_documented_fixture_options_without_native_calls(self):
        output = io.StringIO()
        with mock.patch.object(observer, "observe", side_effect=AssertionError("observe")), contextlib.redirect_stdout(output):
            with self.assertRaises(SystemExit) as exited:
                observer.main(["--help"])
        self.assertEqual(exited.exception.code, 0)
        readme = (SOURCE / "README.md").read_text()
        for option in ("--uuid", "--uri", "--expect", "--log", "--timeout", "--retry-interval", "--attempts"):
            self.assertIn(option, output.getvalue())
            self.assertIn(option, readme)


if __name__ == "__main__":
    unittest.main()
