# Disposable UEFI/TPM probe console observation

This directory contains the original, public-marker EFI fixture in `probe.c`
and its bounded TPM codec in `wire.c`. The guest tests one UEFI variable and one
emulated TPM NV index. Its guest guard, COM1 mirror, seed/preserve checks and
shutdown behavior remain implemented in those sources. `observe_console.py` is
a separate ordinary-user **external test observer**, not a Virmill command or a
replacement for the native console service.

The observer never builds or executes the EFI image itself. It invokes only
`virsh console` for the supplied UUID and sends no guest input. It does not
start, resume, stop, define or otherwise configure a VM; use of this fixture
does not authorize any lifecycle change. No `sudo`, SSH, remote URI, `--force`
or `--resume` is supported. It refuses execution with either real or effective
UID zero. The owner remains the sole operator of the explicitly authorized
disposable host and probe VM.

## Operator example

Run this on the authorized disposable host as its already authorized ordinary
user, from this directory. Choose an existing private evidence directory and a
new log filename for every observation. This example's UUID is the original
probe UUID; the explicit argument must identify the reviewed target for the
particular run. The fixture does not discover a target by name or choose one.

```sh
python3 observe_console.py \
  --uuid f78674f3-bf3a-43e5-81f9-4283e2472024 \
  --uri qemu:///system \
  --expect SEEDED \
  --log /path/to/existing-private-evidence/seed-console.raw \
  --timeout 30 --retry-interval 0.1 --attempts 100
```

Use `--expect PRESERVED` for a separately reviewed preservation observation and
a different raw log. The observer can be launched before the operator's
separately authorized start. It retries an empty-output attempt only when
`virsh` exits unsuccessfully with one of these complete C-locale stderr values,
including exactly one final LF byte:

```text
error: The domain is not running
error: Requested operation is not valid: domain is not running
error: operation failed: PTY device is not yet assigned
```

The last diagnostic was observed while the native guest was starting. Any
changed text, extra whitespace, CRLF, tracing or additional lines fail visibly,
as do busy-console, authentication and unsupported-safe-handling errors. No
diagnostic content is stripped before matching. It never evicts another
session. Once any stdout bytes arrive, it does not reconnect and accidentally
combine runs.

The fixed subprocess argument array is:

```text
virsh --quiet --no-pkttyagent --connect URI console UUID --safe
```

Only `qemu:///system` and `qemu:///session` are accepted. `--uuid` requires a
canonical, lowercase, nonzero UUID, rather than a domain name, numeric ID or
command expression. `--no-pkttyagent` disables virsh's terminal authentication
agent; existing ordinary-user console authorization must already work. The
child environment removes inherited `VIRSH_DEBUG` and `VIRSH_LOG_FILE` settings.
Setting `VIRSH_DEBUG=0` enables option tracing, so it must be absent rather than
assigned zero. Diagnostic matching remains exact; arbitrary stderr is not
stripped to make a console-not-ready error appear retryable. The
native console API opens a bidirectional stream with `open-device` permission;
this observer sends zero input bytes and does **not** claim a read-only libvirt
connection. Upstream defines `--safe` as requiring exclusive console handling,
and `--force` as evicting an existing session. See the official
[virsh console documentation](https://libvirt.org/manpages/virsh.html#console)
and [console API](https://libvirt.org/html/libvirt-libvirt-domain.html#virDomainOpenConsole).

## Bounds, artifacts and results

- Guest/console stdout across all attempts is limited to **4096 bytes**, including
  any firmware text or virsh banners. Reads never exceed the remaining budget.
  Reaching the ceiling is inconclusive, even if a complete result appeared
  earlier or the stream happens to end at exactly 4096 bytes. No overflow bytes
  are read to guess whether truncation occurred.
- The raw stdout prefix is written as received to a new mode-0600 file, with
  exclusive creation and no symlink following. Existing logs are never
  overwritten. Parent directories must exist. Console control bytes remain in
  this artifact; do not display it directly in an interactive terminal.
- Stderr is excluded from marker parsing and has a separate **16384-byte total**
  ceiling. Raw diagnostics are retained as base64 in the JSON attempt records.
  Reaching this ceiling also fails closed.
- The overall `--timeout` is finite, from 1 to 120 seconds (default 30), including
  a reserved 0.25-second client-cleanup budget. `--retry-interval` is 0.05 to 1
  second (default 0.1). `--attempts` is 1 to 100 (default 100). The earliest
  applicable bound wins, so repeated console-not-ready failures can reach the attempt
  cap before the timeout. Timing bounds concern normal userspace process/I/O
  behavior; the fixture cannot guarantee deadlines against a wedged kernel or
  evidence filesystem.
- SIGINT/SIGTERM cancellation retains the bounded log. Cleanup terminates, then
  if necessary kills and reaps, only the observer's own `virsh` child. It sends
  no console escape, guest input, process-group signal or lifecycle command.
  A child that cannot be reaped within the budget is reported as
  `cleanup_failed`, including its client PID, with no next attempt.
- One escaped JSON object reports status, expected and observed results, exact
  supplied UUID/URI, raw-log path/size/SHA-256, elapsed time and each attempt's
  byte offsets, return code and diagnostic bytes. It always declares
  `observation_only: true`. The raw stdout log contains no added separators;
  byte offsets distinguish attempts, and parsing never joins attempts.

Exit 0 means only `status: observed`: the expected marker was received, all
observed probe lines were consistent, the console stream ended and its client
exited zero within the bounds. It does not mean the VM is stopped, that an
artifact was captured, or that recovery succeeded. Missing markers, partial
lines, changed fields, conflicts, a wrong expected result, failed probe,
transport failure, timeout and limits exit nonzero. Cancelled observation exits
130. A marker can still be reported as a diagnostic fact on an inconclusive
transport outcome; consumers must check `status`, not just `result`.

## Exact probe protocol and limits of the evidence

The result lines are exactly:

```text
VIRMILL_PROBE RESULT=SEEDED INDEX=0x0000000001564d50 MARKER=VIRMILL-COLD-TPM-NV-PROBE-V1-001
VIRMILL_PROBE RESULT=PRESERVED INDEX=0x0000000001564d50 MARKER=VIRMILL-COLD-TPM-NV-PROBE-V1-001
VIRMILL_PROBE RESULT=FAIL INDEX=0x0000000001564d50 MARKER=VIRMILL-COLD-TPM-NV-PROBE-V1-001
VIRMILL_PROBE RESULT=FAIL_AUXILIARY_STATE_MISMATCH INDEX=0x0000000001564d50 MARKER=VIRMILL-COLD-TPM-NV-PROBE-V1-001
```

The payload is 32 public bytes; the index is a lowercase 16-digit hexadecimal
value, following `probe.c`'s `hex()` formatter. Tests compare these constants
with `wire.c`/`wire.h`, all failure stages, and result names in `probe.c`.
Complete LF or CRLF lines are recognized, without trimming spaces, ANSI codes,
extra carriage returns or arbitrary prefixes. A partial final line is refused.
Identical duplicates are allowed because EFI routing and the guest COM1 mirror
may both print a line. Different results, initial-state contradictions,
inconsistent failure stages, unknown probe lines or malformed identity fields
are refused. The exact guest-guard refusal is reported as `probe_failed`.
Optional version, serial and initial-state lines may be absent when attaching
late; their absence is not inferred success. If present they must match this
probe protocol and agree with the result.

The probe requests guest shutdown one second after its result. A console may be
unavailable before the guest starts, and output emitted before attachment may
be lost. Retries reduce this race; they do not guarantee capture. No marker is
inconclusive: it does not establish whether the probe booted, seeded state,
failed, or shut down. A missed observation never authorizes another boot or a
rebuild. If the parent cannot reliably obtain the result with this existing
image/lifecycle, that limitation needs an explicit reviewed change elsewhere.

The marker is public and is not cryptographic attestation of guest identity or
proof that every required TPM/firmware byte survived. Even a real PRESERVED
observation must be combined with exact target/image provenance, independent
stopped-state checks, artifact hashes, disk/configuration/auxiliary completeness,
and the separately reviewed native capture/restore evidence. Synthetic tests
here are only SNAP-01 and BAK-01 prerequisites and a bounded fixture/help
validation contribution to REL-03; they do not satisfy those acceptance
scenarios or prove real Virmill console support.

## Local synthetic checks

The audited v3 runtime is frozen at SHA-256
`dc96fbc1ca5dae19be1d1b6f8f9a6a1101ac95d133442e4227d12804a251c5e6`.
The synthetic suite pins this source before reconstructing either historical
variant, so a future runtime edit requires explicit preservation and review of
the reproduction chain. The patches apply independently to separate copies of
v3, never sequentially and never to the active observer.

The first native stopped-guest preflight exposed a fixture error: setting
`VIRSH_DEBUG=0` enabled option tracing, so the strict diagnostic parser correctly
refused the combined stderr. The observer now removes inherited `VIRSH_DEBUG`
and `VIRSH_LOG_FILE`. `observed-debug-variant.patch`, applied to a scratch copy
of the current observer, reproduces the original observer SHA-256
`ffa5ce3ebaaeb2360fb838d68b8895bc24aa62e84751034fae9ba02f1c2d58f4`.
That historical variant is for failure reproduction only. The native failure
is retained as `cold-probe-console-preflight-001`; it occurred before planning
or starting a guest. The corrected synthetic tests assert inherited tracing is
removed and unrecognized traced stderr remains an error.

The subsequent native first-boot observation, retained as
`cold-probe-first-boot-001`, received zero stdout: one stopped-guest diagnostic
followed by the exact PTY-unassigned diagnostic above. The prior observer
refused that second attempt. This failure remains history; the successful
native start and later shut-off state do not supply a missing SEEDED marker.
The added synthetic regression covers stopped → PTY-unassigned → observed and
refuses changed/traced diagnostics or any stdout before retry. It does not
retroactively establish what the guest printed or authorize a further boot.
`observed-pty-variant.patch` recreates that second historical observer from a
scratch copy of the current source, with SHA-256
`4d2232a9570e5089fb5704db1f7f398ec3702a48027526144f5978603a415261`.

Both reproductions are checked by applying GNU `patch` with `--batch --fuzz=0`
to temporary files, requiring exact hashes and no offset/fuzz/backup/reject
artifacts. Historical files are not imported or executed. The audited patch
tool was GNU patch 2.8; the test requires an already installed compatible GNU
patch and never downloads it. See
[the v3 observer review](../../../../docs/reviews/uefi-console-observer-review.md)
for the audit scope, limits and results.

```sh
python3 -B tests/fixtures/protection/uefi-tpm-probe/test_observe_console.py -v
python3 -B tests/fixtures/protection/uefi-tpm-probe/observe_console.py --help
```

Run these two commands from the repository root. Tests substitute short local
Python subprocesses at the launch seam, using private PTYs and pipes. They do
not invoke virsh, connect to libvirt, use a network, execute the EFI code or
modify a host/VM. No packages are downloaded. They cover exact/split/duplicate
and conflicting markers, source-constant alignment, retry classification,
stderr forgery, partial/limited raw logging, hanging or cancelled clients,
spawn/FD cleanup, UUID/bound/root validation and existing-log preservation.
Additional error-path checks cover cancelled/deadline-expired results after
final EOF, partial evidence-file failure, zero-length writes, terminal/pipe
setup failures, CLI cancellation callbacks and restored signal handlers, and
unrelated-process survival while only the observer's own client PID receives
cleanup signals. These are local synthetic checks, not console hardware or
complete-recovery evidence.
