#!/usr/bin/env python3
"""Generated-only swtpm 0.10.2 FILE lock experiment; --self-test runs no native code.

Never accepts a state pathname. Native mode is an explicit, separately authorized
ordinary-user Linux x86_64 experiment, not a Virmill capture implementation.
"""

import argparse
import base64
import ctypes
from contextlib import ExitStack
import errno
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import re
import resource
import selectors
import shutil
import signal
import socket
import stat
import subprocess
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock


SWTPM = "/usr/bin/swtpm"
PUBLIC_FILES = (SWTPM, "/usr/lib64/swtpm/libswtpm_libtpms.so.0.0.0",
                "/usr/lib64/libtpms.so.0.10.2")
OUTPUT_LIMIT = 8192                 # combined stdout/stderr per child
REPORT_LIMIT = 256 * 1024
STATE_LIMIT = 8 * 1024 * 1024
COMMAND_TIMEOUT = 5.0
OVERALL_TIMEOUT = 75.0
MAX_CHILDREN = 12
CASE_NAMES = ("initialized-file-guard", "explicit-mode-before-lock",
              "short-file-before-lock", "replaced-file-bypasses-old-guard")


class Failure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Failure(message)


class Flock(ctypes.Structure):
    _fields_ = [("type", ctypes.c_short), ("whence", ctypes.c_short),
                ("start", ctypes.c_long), ("length", ctypes.c_long),
                ("pid", ctypes.c_int)]


def lock_request(kind):
    return bytes(Flock(kind, os.SEEK_SET, 0, 0, 0))


def lock_owner(fd, requested=fcntl.F_RDLCK):
    data = fcntl.fcntl(fd, fcntl.F_GETLK, lock_request(requested))
    require(len(data) == ctypes.sizeof(Flock), "malformed kernel lock response")
    value = Flock.from_buffer_copy(data)
    return (value.type, value.whence, value.start, value.length, value.pid)


def guard(fd):
    fcntl.fcntl(fd, fcntl.F_OFD_SETLK, lock_request(fcntl.F_RDLCK))


def expect_busy(fd):
    try:
        guard(fd)
    except OSError as exc:
        require(exc.errno in (errno.EACCES, errno.EAGAIN),
                "OFD failure was not a conflicting lock")
        return
    raise Failure("OFD reader admitted beside producer writer")


def metadata(info):
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1,
            "scratch member is not a single-link regular file")
    require(info.st_uid == os.geteuid() and 0 <= info.st_size <= STATE_LIMIT,
            "scratch member owner/size changed or exceeded bound")
    return {"device": info.st_dev, "inode": info.st_ino, "size": info.st_size,
            "mtimeNS": info.st_mtime_ns, "ctimeNS": info.st_ctime_ns,
            "mode": stat.S_IMODE(info.st_mode), "uid": info.st_uid,
            "gid": info.st_gid, "links": info.st_nlink}


def inode(info):
    return (info["device"], info["inode"])


def open_state(path):
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        require(metadata(os.fstat(fd)) == metadata(path.lstat()),
                "scratch state changed during open")
        return fd
    except BaseException:
        os.close(fd)
        raise


def state_argument(root, path, mode=None):
    require(root.is_absolute() and path.parent == root and
            re.fullmatch(r"/tmp/vml-file-[A-Za-z0-9_-]+", str(root)) and
            re.fullmatch(r"[a-z0-9-]+", path.name), "non-generated source path")
    require(mode in (None, "0600", "0640"), "unexpected generated mode")
    value = "backend-uri=file://" + str(path) + ",lock=true"
    return value if mode is None else value + ",mode=" + mode


def child_environment(root):
    return {"PATH": "/usr/bin:/bin", "LC_ALL": "C", "HOME": str(root)}


def child_limits():
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (STATE_LIMIT, STATE_LIMIT))
    resource.setrlimit(resource.RLIMIT_CPU, (10, 10))
    resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))
    os.umask(0o077)


def append_output(output, which, chunk):
    require(sum(map(len, output.values())) + len(chunk) <= OUTPUT_LIMIT,
            "child diagnostic output exceeded bound")
    output[which] += chunk


def lock_refusal(code, output, path):
    require(code is not None and code > 0, "producer did not exit with a refusal")
    prefix = ("swtpm: SWTPM_NVRAM_LinearFile_Lock: Could not lock backend-uri file://" +
              str(path) + ": ").encode("ascii")
    lines = output["stderr"].splitlines()
    require(any(line in (prefix + b"Resource temporarily unavailable",
                         prefix + b"Permission denied") for line in lines),
            "producer failure lacked the exact FILE lock diagnostic")


def version_check(code, output):
    require(code == 0 and not output["stderr"], "version command failed")
    lines = output["stdout"].splitlines()
    require(lines and lines[0].startswith(b"TPM emulator version 0.10.2, "),
            "requires swtpm 0.10.2")
    return output["stdout"].decode("utf-8", errors="strict").strip()


def capability_reply(data):
    require(len(data) == 8 and data[:4] == b"\0" * 4,
            "malformed control capability reply")


def reap_owned(process):
    """Signal/reap this Popen child only; never a process group or foreign PID."""
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=1.0)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=1.0)
    else:
        process.wait(timeout=0)


class Child:
    def __init__(self, process, role, ctrl=None):
        self.process, self.role, self.ctrl = process, role, ctrl
        self.output = {"stdout": b"", "stderr": b""}
        self.streams = [process.stdout, process.stderr]


class Probe:
    def __init__(self, root, executable_fd, cancelled):
        self.root, self.executable_fd, self.cancelled = root, executable_fd, cancelled
        self.deadline = time.monotonic() + OVERALL_TIMEOUT
        self.children = []
        self.selector = selectors.DefaultSelector()

    def check(self):
        require(not self.cancelled[0], "probe cancelled")
        require(time.monotonic() < self.deadline, "overall probe timeout")

    def start(self, path=None, mode=None):
        self.check()
        require(len(self.children) < MAX_CHILDREN, "child count exceeded bound")
        ctrl = None
        args = [SWTPM, "--version"]
        if path is not None:
            number = len(self.children)
            base = self.root / ("p%d" % number)
            base.mkdir(mode=0o700)
            ctrl = base / "c"
            args = [SWTPM, "socket", "--tpm2", "--tpmstate",
                    state_argument(self.root, path, mode),
                    "--ctrl", "type=unixio,path=%s,mode=0600" % ctrl,
                    "--server", "type=unixio,path=%s,mode=0600" % (base / "s"),
                    "--flags", "not-need-init,startup-clear"]
        process = subprocess.Popen(
            args, executable="/proc/self/fd/%d" % self.executable_fd,
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=child_environment(self.root), close_fds=True,
            pass_fds=(self.executable_fd,), preexec_fn=child_limits)
        child = Child(process, "version" if path is None else path.name, ctrl)
        # Own the process before any further fallible setup.
        self.children.append(child)
        for name in ("stdout", "stderr"):
            stream = getattr(process, name)
            os.set_blocking(stream.fileno(), False)
            self.selector.register(stream, selectors.EVENT_READ, (child, name))
        return child

    def pump(self, delay=0.02):
        self.check()
        for key, _ in self.selector.select(min(delay, max(0, self.deadline - time.monotonic()))):
            child, name = key.data
            chunk = os.read(key.fileobj.fileno(), OUTPUT_LIMIT + 1)
            if chunk:
                append_output(child.output, name, chunk)
            else:
                self.selector.unregister(key.fileobj)
                key.fileobj.close()

    def wait_exit(self, child):
        end = min(self.deadline, time.monotonic() + COMMAND_TIMEOUT)
        while True:
            self.pump()
            code = child.process.poll()
            if code is not None and all(stream.closed for stream in child.streams):
                return code
            require(time.monotonic() < end, "child exit/output timeout")

    def ready(self, child):
        end = min(self.deadline, time.monotonic() + COMMAND_TIMEOUT)
        while time.monotonic() < end:
            self.pump()
            require(child.process.poll() is None, "generated producer exited before readiness")
            try:
                with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
                    sock.settimeout(min(0.2, max(0.001, end - time.monotonic())))
                    sock.connect(str(child.ctrl))
                    sock.sendall(b"\0\0\0\1")  # CMD_GET_CAPABILITY only.
                    data = b""
                    while len(data) < 8:
                        self.check()
                        part = sock.recv(8 - len(data))
                        require(part, "truncated control reply")
                        data += part
                    capability_reply(data)
                self.check()
                return
            except (FileNotFoundError, ConnectionRefusedError, TimeoutError):
                continue
        raise Failure("producer readiness timeout")

    def stop(self, child):
        self.check()
        require(child.process.poll() is None, "owned producer already exited")
        child.process.terminate()
        require(self.wait_exit(child) == 0, "owned producer failed graceful termination")

    def refused(self, path, mode=None):
        child = self.start(path, mode)
        lock_refusal(self.wait_exit(child), child.output, path)

    def seed(self, name):
        path = self.root / name
        require(not path.exists(), "generated seed already exists")
        child = self.start(path)
        self.ready(child)
        fd = open_state(path)
        try:
            require(lock_owner(fd) == (fcntl.F_WRLCK, os.SEEK_SET, 0, 0,
                                       child.process.pid), "wrong FILE producer lock owner/range")
            expect_busy(fd)
            require(not (self.root / ".lock").exists(), "unexpected separate lock file")
        finally:
            os.close(fd)
        self.stop(child)
        require(metadata(path.lstat())["size"] > 0, "seeded state is empty")
        return path

    def initialized_file(self):
        path = self.seed("initialized")
        fd = open_state(path)
        try:
            guard(fd)
            before = metadata(path.lstat())
            self.refused(path)
            after = metadata(path.lstat())
            require(after == before, "initialized file metadata changed on lock refusal")
            return {"before": before, "after": after,
                    "sameInodeCooperatingWriterRefused": True}
        finally:
            os.close(fd)

    def explicit_mode(self):
        path = self.seed("explicit-mode")
        # Deliberately change only this newly generated fixture before observing.
        path.chmod(0o600)
        fd = open_state(path)
        try:
            guard(fd)
            before = metadata(path.lstat())
            self.refused(path, "0640")
            after = metadata(path.lstat())
            require(inode(after) == inode(before) and after["mode"] == 0o640,
                    "expected explicit-mode effect before lock refusal was not observed")
            return {"before": before, "after": after,
                    "modeChangedWhileOFDGuardHeld": True}
        finally:
            os.close(fd)

    def short_file(self):
        path = self.root / "short"
        fd_new = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC, 0o600)
        os.close(fd_new)  # Generated empty file, no supplied TPM bytes.
        fd = open_state(path)
        try:
            guard(fd)
            before = metadata(path.lstat())
            self.refused(path)
            after = metadata(path.lstat())
            require(inode(after) == inode(before) and before["size"] == 0 and after["size"] > 0,
                    "expected short-file growth before lock refusal was not observed")
            return {"before": before, "after": after,
                    "fileGrewWhileOFDGuardHeld": True}
        finally:
            os.close(fd)

    def replaced_file(self):
        path, replacement = self.seed("replace-target"), self.seed("replace-source")
        fd = open_state(path)
        try:
            guard(fd)
            held = metadata(os.fstat(fd))
            path.rename(self.root / "retained-old")
            replacement.rename(path)
            named = metadata(path.lstat())
            require(inode(named) != inode(held), "replacement did not change inode")
            child = self.start(path)
            self.ready(child)
            with ExitStack() as opened:
                new_fd = open_state(path)
                opened.callback(os.close, new_fd)
                old_fd = open_state(self.root / "retained-old")
                opened.callback(os.close, old_fd)
                require(lock_owner(new_fd) == (fcntl.F_WRLCK, os.SEEK_SET, 0, 0,
                                               child.process.pid), "replacement writer lock missing")
                require(lock_owner(old_fd, fcntl.F_WRLCK) == (fcntl.F_RDLCK, os.SEEK_SET, 0, 0, -1),
                        "old OFD guard was lost")
                require(inode(metadata(os.fstat(fd))) == inode(held), "held original changed identity")
                self.stop(child)
            return {"heldOriginal": held, "replacement": named,
                    "newProducerAdmittedOnDifferentInode": True,
                    "namedVersusHeldMismatchDetected": True}
        finally:
            os.close(fd)

    def cleanup(self):
        failures = []
        for child in reversed(self.children):
            try:
                reap_owned(child.process)
            except Exception as exc:
                failures.append(type(exc).__name__ + ": " + str(exc))
            finally:
                for stream in child.streams:
                    if not stream.closed:
                        stream.close()
        self.selector.close()
        return failures

    def diagnostics(self):
        return [{"role": child.role, "pid": child.process.pid,
                 "returncode": child.process.poll(),
                 **{name + "Base64": base64.b64encode(data).decode("ascii")
                    for name, data in child.output.items()}}
                for child in self.children]


def public_file(path, expected, check):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and
                not before.st_mode & 0o022 and 0 < before.st_size <= 32 << 20,
                "unqualified public executable/library metadata")
        digest, count = hashlib.sha256(), 0
        while True:
            check()
            data = os.read(fd, 65536)
            if not data:
                break
            count += len(data)
            require(count <= 32 << 20, "public dependency grew beyond hash bound")
            digest.update(data)
        after = os.fstat(fd)
        require(public_identity(before) == public_identity(after) and
                public_identity(os.stat(path, follow_symlinks=False)) == public_identity(after),
                "public dependency changed during fingerprinting")
        require(digest.hexdigest() == expected, "public dependency SHA256 mismatch: " + path)
        return fd
    except BaseException:
        os.close(fd)
        raise


def public_identity(info):
    # Reading a public binary may update atime; that is not content substitution.
    return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink, info.st_uid,
            info.st_gid, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def report_bytes(report):
    data = (json.dumps(report, sort_keys=True, indent=2) + "\n").encode("ascii")
    require(len(data) <= REPORT_LIMIT, "report exceeded output bound")
    return data


def native(args):
    report = {"fixture": "swtpm-file-lock-probe-v2", "nativeProbeExecuted": False,
              "status": "failed", "checks": [], "failures": [],
              "completeCaptureVerified": False, "guestIdentityVerified": False,
              "hardwareVerified": False, "stateBytesReadByProbe": False}
    cancelled, descriptors, probe, root = [False], [], None, None
    previous = {}
    def cancel(_signum, _frame):
        cancelled[0] = True
    try:
        require(os.geteuid() != 0 and platform.system() == "Linux" and
                platform.machine() == "x86_64", "ordinary-user Linux x86_64 required")
        require(ctypes.sizeof(Flock) == 32 and Flock.start.offset == 8 and
                hasattr(fcntl, "F_OFD_SETLK"), "unqualified Linux flock ABI")
        for sig in (signal.SIGINT, signal.SIGTERM):
            previous[sig] = signal.signal(sig, cancel)
        root = Path(tempfile.mkdtemp(prefix="vml-file-", dir="/tmp"))
        root_identity = (root.lstat().st_dev, root.lstat().st_ino)
        try:
            probe = Probe(root, -1, cancelled)
            report.update(uid=os.geteuid(), kernel=platform.release(),
                          python=platform.python_version(), scratchRoot=str(root))
            try:
                expected = (args.swtpm_sha256, args.swtpm_libtpms_sha256, args.libtpms_sha256)
                for path, digest in zip(PUBLIC_FILES, expected):
                    descriptors.append(public_file(path, digest, probe.check))
                probe.executable_fd = descriptors[0]
                report["publicDependencySHA256"] = dict(zip(PUBLIC_FILES, expected))
                child = probe.start()
                report["nativeProbeExecuted"] = True
                report["swtpmVersion"] = version_check(probe.wait_exit(child), child.output)
                with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
                    sock.bind(str(root / "ipc-preflight"))
                for name, call in zip(CASE_NAMES, (probe.initialized_file, probe.explicit_mode,
                                                 probe.short_file, probe.replaced_file)):
                    observation = call()
                    probe.check()
                    report["checks"].append({"name": name, "passed": True, **observation})
            finally:
                report["failures"].extend(probe.cleanup())
                report["diagnostics"] = probe.diagnostics()
                report["childrenStarted"] = len(probe.children)
                report["nativeProbeExecuted"] = bool(probe.children)
                report["allOwnedChildrenReaped"] = all(c.process.poll() is not None for c in probe.children)
                require(report["allOwnedChildrenReaped"], "owned child could not be reaped")
        finally:
            # Keep generated state if cleanup cannot prove every owned writer exited.
            if probe is None or all(c.process.poll() is not None for c in probe.children):
                current = root.lstat()
                require(stat.S_ISDIR(current.st_mode) and current.st_uid == os.geteuid() and
                        (current.st_dev, current.st_ino) == root_identity,
                        "generated root replaced before cleanup")
                shutil.rmtree(root)
            report["generatedScratchRemoved"] = not root.exists()
        probe.check()
        require(len(report["checks"]) == len(CASE_NAMES), "incomplete native case set")
        require(not report["failures"], "cleanup failed")
        report["status"] = "passed"
    except (Exception, KeyboardInterrupt) as exc:
        report["failures"].append(type(exc).__name__ + ": " + str(exc))
    finally:
        for fd in reversed(descriptors):
            os.close(fd)
        for sig, handler in previous.items():
            signal.signal(sig, handler)
    print(report_bytes(report).decode("ascii"), end="")
    return 0 if report["status"] == "passed" else 1


class SelfTests(unittest.TestCase):
    def test_argument_fixed_file_backend(self):
        root = Path("/tmp/vml-file-synthetic")
        self.assertEqual(state_argument(root, root / "state"),
                         "backend-uri=file:///tmp/vml-file-synthetic/state,lock=true")
        self.assertTrue(state_argument(root, root / "state", "0640").endswith(",mode=0640"))

    def test_argument_refuses_outside_and_injection(self):
        root = Path("/tmp/vml-file-synthetic")
        for path in (Path("/existing/tpm"), root / "../state", root / "x,lock=false",
                     root / "x\nflag", root / "x\0bad", root / ".lock"):
            with self.subTest(path=str(path)), self.assertRaises(Failure):
                state_argument(root, path)
        with self.assertRaises(Failure):
            state_argument(root, root / "state", "0777")

    def test_environment_ignores_ambient(self):
        with mock.patch.dict(os.environ, {"TPM_PATH": "/existing", "LD_PRELOAD": "/bad",
                                          "TMPDIR": "/existing", "SWTPM_DEBUG": "1"}):
            self.assertEqual(child_environment(Path("/tmp/vml-file-test")),
                             {"PATH": "/usr/bin:/bin", "LC_ALL": "C",
                              "HOME": "/tmp/vml-file-test"})

    def test_output_exact_bound_and_overflow(self):
        output = {"stdout": b"a" * (OUTPUT_LIMIT - 1), "stderr": b""}
        append_output(output, "stderr", b"b")
        with self.assertRaises(Failure):
            append_output(output, "stderr", b"c")
        self.assertEqual(len(output["stderr"]), 1)

    def test_refusal_requires_exact_path_and_reason(self):
        path = Path("/tmp/vml-file-test/state")
        good = ("swtpm: SWTPM_NVRAM_LinearFile_Lock: Could not lock backend-uri file://" +
                str(path) + ": Resource temporarily unavailable\n").encode()
        lock_refusal(1, {"stdout": b"", "stderr": good}, path)
        for code, stderr in ((0, good), (-9, good), (1, b"some lock failed"),
                             (1, good.replace(b"/state", b"/other")),
                             (1, good.replace(b"Resource temporarily unavailable", b"I/O error"))):
            with self.subTest(code=code, stderr=stderr), self.assertRaises(Failure):
                lock_refusal(code, {"stdout": b"", "stderr": stderr}, path)

    def test_native002_prefixed_diagnostic_and_changed_line_refusals(self):
        # Exact decoded stderr of child 3076470 from the preserved native002 log.
        path = Path("/tmp/vml-file-hin1kge5/initialized")
        observed = (b"swtpm: SWTPM_NVRAM_LinearFile_Lock: Could not lock backend-uri "
                    b"file:///tmp/vml-file-hin1kge5/initialized: Resource temporarily unavailable\n")
        lock_refusal(1, {"stdout": b"", "stderr": observed}, path)
        variants = (
            observed.removeprefix(b"swtpm: "),
            observed.replace(b"swtpm: ", b"other: ", 1),
            b"trace: " + observed,
            observed.replace(b"/initialized:", b"/initialized-other:"),
            observed.replace(b"hin1kge5", b"other-root"),
            observed.replace(b"Resource temporarily unavailable", b"Resource temporarily unavailable noise"),
            observed.replace(b"Resource temporarily unavailable", b"Input/output error"),
            observed.replace(b"Resource temporarily unavailable", b"Resource temporarily unavailable\x00"),
        )
        for changed in variants:
            with self.subTest(stderr=changed), self.assertRaises(Failure):
                lock_refusal(1, {"stdout": b"", "stderr": changed}, path)

    def test_version_is_exact(self):
        good = {"stdout": b"TPM emulator version 0.10.2, Copyright\n", "stderr": b""}
        version_check(0, good)
        for stdout in (b"0.10.2", b"TPM emulator version 0.10.20, Copyright\n",
                       b"TPM emulator version 0.11.0, Copyright\n"):
            with self.subTest(stdout=stdout), self.assertRaises(Failure):
                version_check(0, {"stdout": stdout, "stderr": b""})
        with self.assertRaises(Failure):
            version_check(0, {"stdout": good["stdout"], "stderr": b"warning"})

    def test_capability_exact_length_and_status(self):
        capability_reply(b"\0" * 8)
        for data in (b"", b"\0" * 7, b"\0" * 9, b"\0\0\0\1" + b"\0" * 4):
            with self.subTest(data=data), self.assertRaises(Failure):
                capability_reply(data)

    def test_busy_does_not_accept_unsupported(self):
        for value in (errno.EAGAIN, errno.EACCES):
            with mock.patch(__name__ + ".guard", side_effect=OSError(value, "synthetic")):
                expect_busy(42)
        with mock.patch(__name__ + ".guard", side_effect=OSError(errno.EINVAL, "synthetic")):
            with self.assertRaises(Failure):
                expect_busy(42)
        with mock.patch(__name__ + ".guard"):
            with self.assertRaises(Failure):
                expect_busy(42)

    def test_cleanup_exited_child_never_signals(self):
        child = mock.Mock()
        child.poll.return_value = 1
        reap_owned(child)
        child.terminate.assert_not_called()
        child.kill.assert_not_called()
        child.wait.assert_called_once_with(timeout=0)

    def test_cleanup_terminates_then_reaps(self):
        child = mock.Mock()
        child.poll.return_value = None
        reap_owned(child)
        child.terminate.assert_called_once_with()
        child.kill.assert_not_called()
        child.wait.assert_called_once_with(timeout=1.0)

    def test_cleanup_kills_only_owned_stuck_child(self):
        child = mock.Mock()
        child.poll.return_value = None
        child.wait.side_effect = [subprocess.TimeoutExpired("synthetic", 1), 0]
        reap_owned(child)
        child.terminate.assert_called_once_with()
        child.kill.assert_called_once_with()
        self.assertEqual(child.wait.call_count, 2)

    def test_cancellation_blocks_start(self):
        probe = Probe(Path("/tmp/vml-file-test"), 42, [True])
        try:
            with mock.patch("subprocess.Popen") as popen, self.assertRaises(Failure):
                probe.start()
            popen.assert_not_called()
        finally:
            probe.cleanup()

    def test_deadline_and_child_cap_block_start(self):
        probe = Probe(Path("/tmp/vml-file-test"), 42, [False])
        try:
            probe.deadline = -1
            with mock.patch("subprocess.Popen") as popen, self.assertRaises(Failure):
                probe.start()
            popen.assert_not_called()
            probe.deadline = time.monotonic() + 1
            probe.children = [None] * MAX_CHILDREN
            with mock.patch("subprocess.Popen") as popen, self.assertRaises(Failure):
                probe.start()
            popen.assert_not_called()
        finally:
            probe.children = []
            probe.cleanup()

    def test_state_open_failure_closes_fd(self):
        with (mock.patch("os.open", return_value=42), mock.patch("os.fstat") as info,
              mock.patch("os.close") as close):
            info.side_effect = OSError(errno.EIO, "synthetic")
            with self.assertRaises(OSError):
                open_state(Path("/tmp/vml-file-test/state"))
            close.assert_called_once_with(42)

    def test_open_rejects_foreign_special_and_hardlinked_files(self):
        for changes in ({"st_mode": stat.S_IFIFO | 0o600}, {"st_nlink": 2},
                        {"st_uid": os.geteuid() + 1}, {"st_size": STATE_LIMIT + 1}):
            info = SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_nlink=1,
                                   st_uid=os.geteuid(), st_size=0)
            for name, value in changes.items():
                setattr(info, name, value)
            with (self.subTest(changes=changes), mock.patch("os.open", return_value=42),
                  mock.patch("os.fstat", return_value=info), mock.patch("os.close") as close):
                with self.assertRaises(Failure):
                    open_state(Path("/tmp/vml-file-test/state"))
                close.assert_called_once_with(42)

    def test_public_fingerprint_ignores_only_atime(self):
        fields = dict(st_dev=1, st_ino=2, st_mode=stat.S_IFREG | 0o755, st_nlink=1,
                      st_uid=0, st_gid=0, st_size=10, st_mtime_ns=12, st_ctime_ns=13)
        before = SimpleNamespace(**fields, st_atime_ns=1)
        after = SimpleNamespace(**fields, st_atime_ns=2)
        self.assertEqual(public_identity(before), public_identity(after))
        after.st_ctime_ns += 1
        self.assertNotEqual(public_identity(before), public_identity(after))

    def test_failed_pipe_setup_still_owns_and_reaps_child(self):
        child = mock.Mock()
        child.poll.return_value = None
        child.stdout.closed = child.stderr.closed = False
        probe = Probe(Path("/tmp/vml-file-test"), 42, [False])
        with (mock.patch("subprocess.Popen", return_value=child) as popen,
              mock.patch("os.set_blocking", side_effect=OSError(errno.EIO, "synthetic"))):
            with self.assertRaises(OSError):
                probe.start()
            self.assertEqual(len(probe.children), 1)
            self.assertEqual(popen.call_args.kwargs["executable"], "/proc/self/fd/42")
            self.assertEqual(popen.call_args.kwargs["pass_fds"], (42,))
            self.assertTrue(popen.call_args.kwargs["close_fds"])
            self.assertEqual(probe.cleanup(), [])
        child.terminate.assert_called_once_with()
        child.stdout.close.assert_called_once_with()
        child.stderr.close.assert_called_once_with()

    def test_cleanup_reports_unreaped_child(self):
        child = mock.Mock()
        child.poll.return_value = None
        child.wait.side_effect = subprocess.TimeoutExpired("synthetic", 1)
        with self.assertRaises(subprocess.TimeoutExpired):
            reap_owned(child)
        child.terminate.assert_called_once_with()
        child.kill.assert_called_once_with()

    def test_old_read_guard_requires_write_conflict_query(self):
        answer = lock_request(fcntl.F_UNLCK)
        with mock.patch("fcntl.fcntl", return_value=answer) as call:
            lock_owner(42, fcntl.F_WRLCK)
        query = Flock.from_buffer_copy(call.call_args.args[2])
        self.assertEqual(query.type, fcntl.F_WRLCK)
        self.assertEqual((query.whence, query.start, query.length), (os.SEEK_SET, 0, 0))

    def test_cli_cannot_select_existing_source_or_unpinned_native(self):
        for args in ([], ["--run-generated-probe"],
                     ["--run-generated-probe", "--source", "/existing/state"],
                     ["--self-test", "--run-generated-probe"],
                     ["--run-generated-probe", "--swtpm-sha256", "A" * 64,
                      "--swtpm-libtpms-sha256", "b" * 64, "--libtpms-sha256", "c" * 64]):
            with (self.subTest(args=args), mock.patch(__name__ + ".native") as execute,
                  mock.patch("sys.stderr", new=io.StringIO()), self.assertRaises(SystemExit)):
                main(args)
            execute.assert_not_called()

    def test_partial_pipe_output_and_eof(self):
        stream = mock.Mock()
        child = Child(mock.Mock(), "synthetic")
        key = SimpleNamespace(fileobj=stream, data=(child, "stderr"))
        selector = mock.Mock()
        selector.select.return_value = [(key, selectors.EVENT_READ)]
        with mock.patch("selectors.DefaultSelector", return_value=selector):
            probe = Probe(Path("/tmp/vml-file-test"), 42, [False])
        try:
            with mock.patch("os.read", side_effect=[b"first", b" second", b""]):
                probe.pump(0)
                probe.pump(0)
                self.assertEqual(child.output["stderr"], b"first second")
                stream.close.assert_not_called()
                probe.pump(0)
                stream.close.assert_called_once_with()
                selector.unregister.assert_called_once_with(stream)
        finally:
            probe.cleanup()

    def test_report_has_hard_bound(self):
        self.assertIn(b'"nativeProbeExecuted": false', report_bytes({"nativeProbeExecuted": False}))
        with self.assertRaises(Failure):
            report_bytes({"x": "a" * REPORT_LIMIT})


def self_test():
    output = io.StringIO()
    with mock.patch("subprocess.Popen", side_effect=AssertionError("native subprocess in self-test")), \
            mock.patch("socket.socket", side_effect=AssertionError("IPC in self-test")), \
            mock.patch("fcntl.fcntl", side_effect=AssertionError("kernel lock in self-test")):
        result = unittest.TextTestRunner(stream=output, verbosity=2).run(
            unittest.defaultTestLoader.loadTestsFromTestCase(SelfTests))
    print(report_bytes({"fixture": "swtpm-file-lock-probe-v2", "mode": "self-test",
                        "testsRun": result.testsRun, "passed": result.wasSuccessful(),
                        "nativeProbeExecuted": False, "testOutput": output.getvalue()}).decode(), end="")
    return 0 if result.wasSuccessful() else 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--self-test", action="store_true")
    mode.add_argument("--run-generated-probe", action="store_true")
    for name in ("swtpm", "swtpm-libtpms", "libtpms"):
        parser.add_argument("--" + name + "-sha256")
    args = parser.parse_args(argv)
    if args.self_test:
        require(all(value is None for value in (args.swtpm_sha256, args.swtpm_libtpms_sha256,
                                                args.libtpms_sha256)),
                "dependency hashes apply only to native mode")
        return self_test()
    if not all(value is not None and re.fullmatch(r"[0-9a-f]{64}", value)
               for value in (args.swtpm_sha256, args.swtpm_libtpms_sha256, args.libtpms_sha256)):
        parser.error("native mode requires three reviewed lowercase SHA256 fingerprints")
    return native(args)


if __name__ == "__main__":
    raise SystemExit(main())
