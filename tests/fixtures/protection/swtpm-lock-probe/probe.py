#!/usr/bin/env python3
"""Ordinary-user Linux swtpm 0.10.2 lock probe; generated temporary state only.

No guest, libvirt, TPM device, network socket, existing state, or state-byte read.
The native executable generates original disposable TPM state itself. This is
dependency behavior evidence, not Virmill capture or hardware qualification.
"""

import ctypes
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import resource
import signal
import socket
import stat
import subprocess
import tempfile
import time


SWTPM = "/usr/bin/swtpm"
IOCTL = "/usr/bin/swtpm_ioctl"
TIMEOUT = 8


class Flock(ctypes.Structure):
    _fields_ = [("type", ctypes.c_short), ("whence", ctypes.c_short),
                ("start", ctypes.c_long), ("length", ctypes.c_long),
                ("pid", ctypes.c_int)]


def request(kind):
    return bytes(Flock(kind, os.SEEK_SET, 0, 0, 0))


def conflict(fd):
    value = Flock.from_buffer_copy(fcntl.fcntl(fd, fcntl.F_GETLK,
                                              request(fcntl.F_RDLCK)))
    return {"type": value.type, "start": value.start,
            "length": value.length, "pid": value.pid}


def guard(fd):
    fcntl.fcntl(fd, fcntl.F_OFD_SETLK, request(fcntl.F_RDLCK))


def expect_busy(fd):
    try:
        guard(fd)
    except OSError as exc:
        assert exc.errno in (errno.EAGAIN, errno.EACCES), exc
    else:
        raise AssertionError("OFD reader unexpectedly admitted beside swtpm writer")


def safe_open(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        os.close(fd)
        raise AssertionError("generated fixture ceased to be a single-link regular file")
    return fd


def state_metadata(directory):
    entries = {}
    for path in sorted(directory.iterdir()):
        if path.name == ".lock":
            continue  # The lock's inode is checked separately; swtpm truncates it.
        info = path.lstat()
        assert stat.S_ISREG(info.st_mode) and info.st_nlink == 1
        assert 0 <= info.st_size <= 8 << 20 and len(entries) < 16
        entries[path.name] = (info.st_dev, info.st_ino, info.st_size,
                              info.st_mtime_ns, info.st_ctime_ns, info.st_mode)
    return entries


def limit_child():
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_FSIZE, (8 << 20, 8 << 20))
    resource.setrlimit(resource.RLIMIT_CPU, (12, 12))
    resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))


class Probe:
    def __init__(self, root):
        self.root = root
        self.counter = 0
        self.children = []
        self.env = {"PATH": "/usr/bin:/bin", "LC_ALL": "C", "HOME": str(root)}

    def start(self, state, extra=(), flags="not-need-init,startup-clear"):
        self.counter += 1
        base = self.root / ("p%d" % self.counter)
        base.mkdir(mode=0o700)
        ctrl = base / "c"
        args = [SWTPM, "socket", "--tpm2", "--tpmstate", state,
                "--ctrl", "type=unixio,path=%s,mode=0600" % ctrl,
                "--server", "type=unixio,path=%s,mode=0600" % (base / "s")]
        if flags:
            args += ["--flags", flags]
        args += list(extra)
        # Separate files bound child diagnostics without a PIPE deadlock.
        out = open(base / "stdout", "xb")
        err = open(base / "stderr", "xb")
        try:
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=out,
                                       stderr=err, env=self.env, close_fds=True,
                                       preexec_fn=limit_child)
        finally:
            out.close()
            err.close()
        self.children.append(process)
        process.probe_base = base
        process.probe_ctrl = ctrl
        return process

    def ready(self, process):
        deadline = time.monotonic() + TIMEOUT
        while time.monotonic() < deadline:
            assert process.poll() is None, self.diagnostics(process)
            try:
                with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
                    sock.settimeout(0.2)
                    sock.connect(str(process.probe_ctrl))
                    # CMD_GET_CAPABILITY: fixed query, no state blob or TPM bytes.
                    sock.sendall(b"\x00\x00\x00\x01")
                    response = b""
                    while len(response) < 8:
                        part = sock.recv(8 - len(response))
                        if not part:
                            raise AssertionError("short capability response")
                        response += part
                    assert response[:4] == b"\x00" * 4, response.hex()
                return
            except (FileNotFoundError, ConnectionRefusedError, TimeoutError):
                time.sleep(0.02)
        raise AssertionError("swtpm startup deadline exceeded")

    def diagnostics(self, process):
        values = []
        for name in ("stdout", "stderr"):
            with open(process.probe_base / name, "rb") as stream:
                data = stream.read(8193)
            assert len(data) <= 8192, "diagnostic bound exceeded"
            values.append(data.decode("utf-8", errors="strict"))
        return "".join(values)

    def refused(self, process):
        code = process.wait(timeout=TIMEOUT)
        assert code != 0, "competing swtpm unexpectedly started"
        text = self.diagnostics(process)
        assert "lock" in text.lower(), "failure was not a lock refusal: " + text

    def stop(self, process, crash=False):
        assert process.poll() is None, self.diagnostics(process)
        process.send_signal(signal.SIGKILL if crash else signal.SIGTERM)
        result = process.wait(timeout=TIMEOUT)
        assert result == (-signal.SIGKILL if crash else 0), self.diagnostics(process)

    def cleanup(self):
        for process in reversed(self.children):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=2)

    def locked_backend(self, backend):
        directory = self.root / backend
        directory.mkdir(mode=0o700)
        if backend == "dir":
            state, lock = "dir=" + str(directory), directory / ".lock"
        else:
            lock = directory / "state"
            state = "backend-uri=file://" + str(lock) + ",lock=true"
        process = self.start(state)
        self.ready(process)
        fd = safe_open(lock)
        inode = os.fstat(fd).st_ino
        try:
            found = conflict(fd)
            assert found == {"type": fcntl.F_WRLCK, "start": 0,
                             "length": 0, "pid": process.pid}, found
            expect_busy(fd)
            # Linux BSD flock does not interoperate with POSIX record locks.
            fcntl.flock(fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
            fcntl.flock(fd, fcntl.LOCK_UN)
            before = state_metadata(directory)
            self.refused(self.start(state))
            assert state_metadata(directory) == before
            # STOP terminates the TPM library but retains the producer process.
            result = subprocess.run([IOCTL, "--unix", str(process.probe_ctrl), "--stop"],
                                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, env=self.env, timeout=TIMEOUT)
            assert result.returncode == 0 and not result.stderr, result.stderr
            assert process.poll() is None
            expect_busy(fd)
            self.stop(process)
            assert lock.stat().st_ino == inode
            assert conflict(fd)["type"] == fcntl.F_UNLCK
            guard(fd)
            before = state_metadata(directory)
            self.refused(self.start(state))
            assert state_metadata(directory) == before
            duplicate = os.dup(fd)
            os.close(fd)
            fd = duplicate
            # A shared OFD survives closing the original descriptor.
            self.refused(self.start(state))
            assert state_metadata(directory) == before
        finally:
            os.close(fd)
        restarted = self.start(state)
        self.ready(restarted)
        fd = safe_open(lock)
        try:
            expect_busy(fd)
            self.stop(restarted, crash=True)
            assert conflict(fd)["type"] == fcntl.F_UNLCK
            guard(fd)
            assert lock.stat().st_ino == inode
        finally:
            os.close(fd)
        return {"backend": backend, "producerLock": "POSIX whole-file F_WRLCK",
                "bsdFlockDoesNotExclude": True, "stopRetainsLock": True,
                "exitAndCrashReleaseLock": True, "staleObjectRetained": True,
                "testedRefusalsPreserveStateMemberMetadata": True,
                "ofdReaderExcludesProducerAcrossDupClose": True}

    def unlocked_backend(self, backend):
        directory = self.root / (backend + "-disabled")
        directory.mkdir(mode=0o700)
        if backend == "dir":
            lock = directory / ".lock"
            lock.touch(mode=0o600, exist_ok=False)
            state = "dir=" + str(directory) + ",lock=false"
        else:
            lock = directory / "state"
            state = "backend-uri=file://" + str(lock)
        process = self.start(state)
        self.ready(process)
        fd = safe_open(lock)
        try:
            assert conflict(fd)["type"] == fcntl.F_UNLCK
            guard(fd)
            assert process.poll() is None
            self.stop(process)
        finally:
            os.close(fd)
        return {"backend": backend, "runningWithoutConflictingLock": True}

    def deferred_incoming(self):
        directory = self.root / "incoming"
        directory.mkdir(mode=0o700)
        lock = directory / ".lock"
        lock.touch(mode=0o600, exist_ok=False)
        fd = safe_open(lock)
        try:
            guard(fd)
            process = self.start("dir=" + str(directory), ("--migration", "incoming"),
                                 flags="")
            self.ready(process)
            assert process.poll() is None
            self.stop(process)
        finally:
            os.close(fd)
        return {"incomingProducerAliveWhileGuardHeld": True}

    def replaced_directory_lock(self):
        directory = self.root / "replacement"
        directory.mkdir(mode=0o700)
        lock = directory / ".lock"
        lock.touch(mode=0o600, exist_ok=False)
        fd = safe_open(lock)
        try:
            guard(fd)
            old_inode = os.fstat(fd).st_ino
            lock.rename(directory / "original-lock")
            process = self.start("dir=" + str(directory))
            self.ready(process)
            assert lock.stat().st_ino != old_inode
            assert process.poll() is None
            self.stop(process)
        finally:
            os.close(fd)
        return {"differentLockInodeBypassesOldGuard": True,
                "generationMismatchDetected": True}

    def posix_duplicate_close(self):
        directory = self.root / "posix-close"
        directory.mkdir(mode=0o700)
        lock = directory / ".lock"
        lock.touch(mode=0o600, exist_ok=False)
        fd = safe_open(lock)
        try:
            fcntl.fcntl(fd, fcntl.F_SETLK, request(fcntl.F_RDLCK))
            self.refused(self.start("dir=" + str(directory)))
            duplicate = os.dup(fd)
            os.close(duplicate)
            process = self.start("dir=" + str(directory))
            self.ready(process)
            assert process.poll() is None
            self.stop(process)
        finally:
            os.close(fd)
        return {"closingAnySameInodeFDReleasesProcessLock": True}


def main():
    assert os.geteuid() != 0, "ordinary user required"
    assert platform.system() == "Linux" and platform.machine() == "x86_64"
    assert ctypes.sizeof(Flock) == 32 and Flock.start.offset == 8
    version = subprocess.run([SWTPM, "--version"], check=True, capture_output=True,
                             timeout=TIMEOUT, env={"LC_ALL": "C"}).stdout.decode()
    assert "TPM emulator version 0.10.2," in version, version
    result = {"fixture": "swtpm-lock-probe-v1", "uid": os.geteuid(),
              "kernel": platform.release(), "python": platform.python_version(),
              "swtpmVersion": version.strip(), "checks": [], "failures": [],
              "hardwareVerified": False, "completeCaptureVerified": False}
    for path in (SWTPM, IOCTL, "/usr/lib64/libtpms.so.0.10.2",
                 "/usr/lib64/swtpm/libswtpm_libtpms.so.0.0.0"):
        # Public installed binaries only; no TPM persistent-state bytes are read.
        with open(path, "rb") as stream:
            result.setdefault("binarySHA256", {})[path] = hashlib.file_digest(stream, "sha256").hexdigest()
    with tempfile.TemporaryDirectory(prefix="vml-swtpm-", dir="/tmp") as directory:
        root = Path(directory)
        probe = Probe(root)
        try:
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
                sock.bind(str(root / "ipc-check"))
            for name, call in (
                ("directory default lock lifetime", lambda: probe.locked_backend("dir")),
                ("file explicit lock lifetime", lambda: probe.locked_backend("file")),
                ("directory explicit locking disabled", lambda: probe.unlocked_backend("dir")),
                ("file default locking disabled", lambda: probe.unlocked_backend("file")),
                ("incoming migration defers writer locking", probe.deferred_incoming),
                ("replacing directory lock defeats old inode guard", probe.replaced_directory_lock),
                ("POSIX lock lost on duplicate close", probe.posix_duplicate_close),
            ):
                observation = call()
                result["checks"].append({"name": name, "passed": True, **observation})
        except Exception as exc:
            result["failures"].append(type(exc).__name__ + ": " + str(exc))
        finally:
            probe.cleanup()
        result["allOwnedChildrenReaped"] = all(p.poll() is not None for p in probe.children)
        result["childrenStarted"] = len(probe.children)
    result["generatedTemporaryTreeRemoved"] = not root.exists()
    result["passed"] = not result["failures"] and len(result["checks"]) == 7
    print(json.dumps(result, sort_keys=True, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
