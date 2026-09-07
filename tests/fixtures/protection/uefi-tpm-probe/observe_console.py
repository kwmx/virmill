#!/usr/bin/python3
"""Bounded external observer for the disposable UEFI/TPM probe's serial text.

This fixture opens only a console. It never starts, resumes or stops a VM and
never writes guest input. A matching public marker is an observation, not proof
of complete capture, identity, backup recovery or Virmill console support.
"""
import argparse
import base64
import errno
import hashlib
import json
import math
import os
from pathlib import Path
import pty
import re
import selectors
import signal
import subprocess
import sys
import threading
import time
import tty
import uuid

OUTPUT_LIMIT = 4096
DIAGNOSTIC_LIMIT = 16384
MAX_ATTEMPTS = 100
CLEANUP_BUDGET = 0.25
PUBLIC_MARKER = b"VIRMILL-COLD-TPM-NV-PROBE-V1-001"
INDEX = b"0x0000000001564d50"
PREFIX = b"VIRMILL_PROBE "
RESULTS = ("SEEDED", "PRESERVED", "FAIL", "FAIL_AUXILIARY_STATE_MISMATCH")
FAILURE_STAGES = frozenset((
    b"LOCATE_TCG2", b"TCG2_SUBMIT", b"TPM_RESPONSE_BOUNDS", b"NV_READ_PUBLIC",
    b"NV_PUBLIC_OR_UNWRITTEN_INDEX", b"NV_READ", b"NV_MARKER_OR_RESPONSE",
    b"GET_VARIABLE", b"NVRAM_MARKER_OR_ATTRIBUTES", b"NV_DEFINE",
    b"NV_DEFINE_RESPONSE", b"NV_WRITE", b"NV_WRITE_RESPONSE", b"SET_VARIABLE",
    b"SEED_READBACK",
))
RETRYABLE_DIAGNOSTICS = frozenset((
    b"error: The domain is not running\n",
    b"error: Requested operation is not valid: domain is not running\n",
    b"error: operation failed: PTY device is not yet assigned\n",
))


class MarkerError(ValueError):
    """The bounded text does not match this exact probe protocol."""


def parse_markers(raw):
    """Parse complete LF/CRLF lines, without stripping terminal escapes/noise.

    Optional early observations may be missing when attaching late. Duplicate
    identical lines from EFI console routing plus the COM1 mirror are valid.
    Contradictory lines, partial probe lines and changed protocol fields fail.
    """
    if len(raw) > OUTPUT_LIMIT:
        raise MarkerError("output exceeds parser limit")
    seen = {}
    result_lines = 0

    def record(key, value):
        if key in seen and seen[key] != value:
            raise MarkerError("conflicting " + key + " lines")
        seen[key] = value

    lines = raw.split(b"\n")
    if lines[-1]:
        raise MarkerError("unterminated console line")
    for line in lines[:-1]:
        if line.endswith(b"\r"):
            line = line[:-1]
        if b"VIRMILL_PROBE" not in line:
            continue
        if not line.startswith(PREFIX):
            raise MarkerError("probe line has a prefix or terminal control bytes")
        if line == PREFIX + b"VERSION=1 DISPOSABLE_GUEST_ONLY":
            record("version", 1)
        elif line in (PREFIX + b"SERIAL=COM1_115200_8N1_GUEST_ONLY",
                       PREFIX + b"SERIAL=UNAVAILABLE EFI_CONSOLE_ONLY"):
            record("serial", line[len(PREFIX + b"SERIAL="):].decode("ascii"))
        elif match := re.fullmatch(
                rb"VIRMILL_PROBE INITIAL_TPM=(PRESENT|ABSENT) INITIAL_NVRAM=(PRESENT|ABSENT)", line):
            record("initial", [part.decode("ascii") for part in match.groups()])
        elif match := re.fullmatch(
                rb"VIRMILL_PROBE FAILURE_STAGE=([A-Z0-9_]+) STATUS=(0x[0-9a-f]{16})", line):
            if match[1] not in FAILURE_STAGES:
                raise MarkerError("unknown failure stage")
            record("failure", {"stage": match[1].decode("ascii"),
                               "status": match[2].decode("ascii")})
        elif line == PREFIX + b"RESULT=REFUSED_GUEST_GUARD NO_TPM_OR_NVRAM_ACCESS":
            record("result", "REFUSED_GUEST_GUARD")
            result_lines += 1
        else:
            for result in RESULTS:
                exact = (PREFIX + b"RESULT=" + result.encode("ascii") + b" INDEX=" +
                         INDEX + b" MARKER=" + PUBLIC_MARKER)
                if line == exact:
                    record("result", result)
                    result_lines += 1
                    break
            else:
                raise MarkerError("unknown or malformed probe line")
    result = seen.get("result")
    if "failure" in seen and result not in (None, "FAIL"):
        raise MarkerError("failure stage contradicts result")
    initial = seen.get("initial")
    if initial is not None:
        if ((result == "SEEDED" and initial != ["ABSENT", "ABSENT"]) or
                (result == "PRESERVED" and initial != ["PRESENT", "PRESENT"]) or
                (result == "FAIL_AUXILIARY_STATE_MISMATCH" and initial[0] == initial[1])):
            raise MarkerError("initial state contradicts result")
    return {"result": result, "result_lines": result_lines,
            "failure": seen.get("failure"), "initial": initial,
            "serial": seen.get("serial"), "version": seen.get("version")}


def valid_uuid(value):
    try:
        parsed = uuid.UUID(value)
    except (ValueError, AttributeError):
        raise ValueError("uuid must be a canonical nonzero lowercase UUID") from None
    if str(parsed) != value or parsed.int == 0:
        raise ValueError("uuid must be a canonical nonzero lowercase UUID")
    return value


def console_argv(vm_uuid, uri):
    valid_uuid(vm_uuid)
    if uri not in ("qemu:///system", "qemu:///session"):
        raise ValueError("only an explicit local qemu:///system or qemu:///session URI is supported")
    return ["virsh", "--quiet", "--no-pkttyagent", "--connect", uri,
            "console", vm_uuid, "--safe"]


def _cleanup(child, deadline):
    """Detach only our client, with a bounded reap; never signal a VM/process group."""
    if child.poll() is not None:
        return True
    child.terminate()
    try:
        child.wait(timeout=max(0, min(0.1, deadline - time.monotonic())))
        return True
    except subprocess.TimeoutExpired:
        child.kill()
        try:
            child.wait(timeout=max(0, deadline - time.monotonic()))
            return True
        except subprocess.TimeoutExpired:
            return False


def observe(vm_uuid, uri, expected, log_path, timeout=30.0, retry_interval=0.1,
            max_attempts=MAX_ATTEMPTS, *, popen=subprocess.Popen, cancelled=lambda: False):
    """Observe one supplied UUID; `popen` is a synthetic-test seam, not a CLI option."""
    argv = console_argv(vm_uuid, uri)
    if expected not in ("SEEDED", "PRESERVED"):
        raise ValueError("expect must be SEEDED or PRESERVED")
    if not math.isfinite(timeout) or not 1 <= timeout <= 120:
        raise ValueError("timeout must be finite and between 1 and 120 seconds")
    if not math.isfinite(retry_interval) or not 0.05 <= retry_interval <= 1:
        raise ValueError("retry interval must be finite and between 0.05 and 1 seconds")
    if isinstance(max_attempts, bool) or not isinstance(max_attempts, int) or not 1 <= max_attempts <= MAX_ATTEMPTS:
        raise ValueError("attempts must be between 1 and 100")
    if os.getuid() == 0 or os.geteuid() == 0:
        raise ValueError("run this fixture as the authorized ordinary user, never root")

    # O_EXCL refuses existing files/symlinks; the operator supplies an existing
    # artifact directory. Never create parent directories or truncate a log.
    log_fd = os.open(log_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    started = time.monotonic()
    deadline = started + timeout
    read_deadline = deadline - CLEANUP_BUDGET
    raw = bytearray()
    diagnostics = 0
    attempts = []
    parsed = parse_markers(b"")
    status, detail = "no_result", None
    # An explicit local URI and disabled pkttyagent prohibit a hidden privilege
    # prompt. Keep user libvirt authentication/config available; do not alter it.
    env = os.environ.copy()
    env.update({"LC_ALL": "C", "TERM": "dumb"})
    # Level 0 enables virsh option tracing; absence disables ambient tracing.
    env.pop("VIRSH_DEBUG", None)
    env.pop("VIRSH_LOG_FILE", None)

    try:
        with os.fdopen(log_fd, "wb", buffering=0) as log:
            for number in range(1, max_attempts + 1):
                if cancelled():
                    status = "cancelled"
                    break
                if time.monotonic() >= read_deadline:
                    status = "timeout"
                    break
                attempt = {"number": number, "log_start": len(raw), "log_end": len(raw),
                           "returncode": None, "stderr_base64": ""}
                attempts.append(attempt)
                child, master, slave = None, None, None
                error_raw = bytearray()
                status = "no_result"
                try:
                    master, slave = pty.openpty()
                    tty.setraw(slave)
                    # Only stdin needs to be a terminal. Separate pipes preserve
                    # exact stdout bytes and keep virsh diagnostics out of parsing.
                    # The parent never writes the PTY master (not even an escape).
                    child = popen(argv, stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  close_fds=True, start_new_session=True, env=env)
                    os.close(slave)
                    slave = None
                    with selectors.DefaultSelector() as selector:
                        for stream, kind in ((child.stdout, "output"), (child.stderr, "diagnostic")):
                            os.set_blocking(stream.fileno(), False)
                            selector.register(stream, selectors.EVENT_READ, kind)
                        while selector.get_map() or child.poll() is None:
                            if cancelled():
                                status = "cancelled"
                                break
                            remaining = read_deadline - time.monotonic()
                            if remaining <= 0:
                                status = "timeout"
                                break
                            if selector.get_map():
                                events = selector.select(min(0.05, remaining))
                            else:
                                # Closed output alone is not child completion.
                                time.sleep(min(0.01, remaining))
                                events = ()
                            for key, _ in events:
                                capacity = (OUTPUT_LIMIT - len(raw) if key.data == "output"
                                            else DIAGNOSTIC_LIMIT - diagnostics)
                                try:
                                    chunk = os.read(key.fd, min(1024, capacity))
                                except BlockingIOError:
                                    continue
                                if not chunk:
                                    selector.unregister(key.fileobj)
                                    continue
                                if key.data == "output":
                                    # Unbuffered writes may be partial: retain an
                                    # exact raw prefix before processing any marker.
                                    view = memoryview(chunk)
                                    while view:
                                        written = log.write(view)
                                        if not written:
                                            raise OSError(errno.EIO, "short log write")
                                        raw.extend(view[:written])
                                        view = view[written:]
                                    if len(raw) == OUTPUT_LIMIT:
                                        status = "output_limit"
                                else:
                                    error_raw.extend(chunk)
                                    diagnostics += len(chunk)
                                    if diagnostics == DIAGNOSTIC_LIMIT:
                                        status = "diagnostic_limit"
                                if status != "no_result":
                                    break
                            if status != "no_result":
                                break
                except KeyboardInterrupt:
                    status = "cancelled"
                except OSError as error:
                    # Do not echo an arbitrary executable/path/terminal payload.
                    status, detail = "io_error", "OS error " + str(error.errno)
                finally:
                    if child is not None:
                        if not _cleanup(child, min(deadline, time.monotonic() + CLEANUP_BUDGET)):
                            status, detail = "cleanup_failed", "client could not be reaped within the deadline"
                            attempt["client_pid"] = child.pid
                        attempt["returncode"] = child.poll()
                        for stream in (child.stdout, child.stderr):
                            if stream is not None:
                                stream.close()
                    for fd in (slave, master):
                        if fd is not None:
                            os.close(fd)
                    attempt["log_end"] = len(raw)
                    attempt["stderr_base64"] = base64.b64encode(error_raw).decode("ascii")

                # Never splice an unterminated marker across console attempts.
                attempt_raw = bytes(raw[attempt["log_start"]:attempt["log_end"]])
                try:
                    parsed = parse_markers(attempt_raw)
                except MarkerError as error:
                    if status == "no_result":
                        status, detail = "malformed_marker", str(error)
                if status != "no_result":
                    break
                if cancelled():
                    status = "cancelled"
                    break
                if time.monotonic() >= read_deadline:
                    status = "timeout"
                    break
                if parsed["result"] is not None:
                    if parsed["result"] not in ("SEEDED", "PRESERVED"):
                        status = "probe_failed"
                    elif attempt["returncode"] != 0:
                        status = "console_error"
                    elif parsed["result"] != expected:
                        status = "unexpected_result"
                    else:
                        status = "observed"
                    break
                retryable = (not attempt_raw and attempt["returncode"] != 0 and
                             bytes(error_raw) in RETRYABLE_DIAGNOSTICS)
                if not retryable:
                    status = "console_error" if attempt["returncode"] != 0 else "no_result"
                    break
                if number == max_attempts:
                    status = "attempt_limit"
                    break
                retry_at = min(time.monotonic() + retry_interval, read_deadline)
                while time.monotonic() < retry_at and not cancelled():
                    time.sleep(min(0.01, max(0, retry_at - time.monotonic())))
    except KeyboardInterrupt:
        status = "cancelled"
    return {"observation_only": True, "status": status, "detail": detail,
            "uuid": vm_uuid, "uri": uri, "expected": expected, **parsed,
            "log_path": str(Path(log_path).absolute()), "log_bytes": len(raw),
            "log_sha256": hashlib.sha256(raw).hexdigest(), "diagnostic_bytes": diagnostics,
            "attempts": attempts, "elapsed_seconds": round(time.monotonic() - started, 6)}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--uuid", required=True, help="exact canonical lowercase UUID of the authorized probe VM")
    parser.add_argument("--uri", required=True, choices=("qemu:///system", "qemu:///session"),
                        help="local libvirt connection on the authorized disposable host")
    parser.add_argument("--expect", required=True, choices=("SEEDED", "PRESERVED"), help="expected observed marker")
    parser.add_argument("--log", required=True, type=Path, help="new raw log in an existing private artifact directory")
    parser.add_argument("--timeout", type=float, default=30, help="overall seconds including cleanup, 1..120 (default 30)")
    parser.add_argument("--retry-interval", type=float, default=0.1, help="seconds between console-not-ready retries, 0.05..1")
    parser.add_argument("--attempts", type=int, default=MAX_ATTEMPTS, help="maximum console attempts, 1..100")
    args = parser.parse_args(argv)
    cancel = threading.Event()
    previous = {}
    try:
        for sig in (signal.SIGINT, signal.SIGTERM):
            previous[sig] = signal.signal(sig, lambda _sig, _frame: cancel.set())
        try:
            result = observe(args.uuid, args.uri, args.expect, args.log, args.timeout,
                             args.retry_interval, args.attempts, cancelled=cancel.is_set)
        except (ValueError, OSError) as error:
            result = {"observation_only": True, "status": "setup_error",
                      "detail": str(error) if isinstance(error, ValueError) else "OS error " + str(error.errno)}
        print(json.dumps(result, ensure_ascii=True, sort_keys=True), flush=True)
        return 0 if result["status"] == "observed" else (130 if result["status"] == "cancelled" else 1)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


if __name__ == "__main__":
    sys.exit(main())
