# UEFI console observer v3 review

Reviewed 2026-09-07. Scope: the external disposable-probe observer's error,
cancellation, bounded-output and client-cleanup behavior, plus exact historical
source reconstruction. Only local synthetic processes, private PTYs, pipes and
temporary regular files were used. No virsh, libvirt connection, SSH, EFI image,
TPM access, VM action or host configuration change was executed by this review.

**No runtime change was needed in the audited v3 source.** The observer remains
byte-identical to the parent's frozen native fixture. Missing regressions were
added to `tests/fixtures/protection/uefi-tpm-probe/test_observe_console.py`;
the README now records the frozen source and patch-verification contract.
Both historical patches were correct and remain unchanged.

## Frozen files and historical reconstruction

| File/version | SHA-256 |
| --- | --- |
| Current v3 `observe_console.py` | `dc96fbc1ca5dae19be1d1b6f8f9a6a1101ac95d133442e4227d12804a251c5e6` |
| `observed-debug-variant.patch` | `d8c4f7ebcdb3bd630223e1abdab463ebd9f581a032972470e0bebbb7c0babe0e` |
| Reconstructed v1 observer | `ffa5ce3ebaaeb2360fb838d68b8895bc24aa62e84751034fae9ba02f1c2d58f4` |
| `observed-pty-variant.patch` | `b2d4f4513ac353adef1b7a7eb6e4fa17cab70b7c4181b06b3c39f55cad4bbb59` |
| Reconstructed v2 observer | `4d2232a9570e5089fb5704db1f7f398ec3702a48027526144f5978603a415261` |

Using installed **GNU patch 2.8**, each patch was applied independently to a
fresh temporary copy of current v3 with `--batch --fuzz=0
--no-backup-if-mismatch -p0`. Each invocation exited zero, reported only
`patching file observe_console.py`, produced no offset/fuzz warning or extra
file, and reconstructed the exact expected hash. No reconstructed Python file
was executed or imported, and the active source was not replaced.

`HistoricalPatchTests` repeats this verification in the synthetic suite with a
five-second subprocess bound. It first requires the exact v3 hash. A future
runtime change therefore cannot silently make these patches describe a
different historical program: preserve the source chain and review the pin and
patches together. This verifies source provenance; it does not turn either
historical failed native observation into a passed run.

## Behavior reviewed

| Property | Source behavior and regression evidence |
| --- | --- |
| Target and command confinement | Required canonical nonzero lowercase UUID; only the two explicit local QEMU URIs; fixed argument array containing `console` and `--safe`. No shell, force, resume, lifecycle command or guest-input write. Root and malformed arguments are refused before log creation or launch. |
| Guest bytes and framing | Across all attempts, stdout reads request at most the remaining 4096-byte budget. Reaching it fails closed, including exact-ceiling output containing an earlier valid marker. Complete LF/CRLF marker lines are parsed; ambiguous, changed, conflicting or incomplete lines cannot yield an observed result. |
| Diagnostic separation | Stderr cannot supply a guest result. It has a separate 16384-byte total ceiling and is retained as base64. Retry matching accepts only one of the three exact documented LF-terminated readiness diagnostics, with unsuccessful client exit and zero stdout. Tracing, changed text or any stdout blocks retry. |
| Result gates | A matching result requires clean client exit and stream completion within the observation bound. A valid marker cannot override cancellation, timeout, malformed output, I/O failure or an output limit. Tests specifically deliver cancellation or advance the clock after final EOF and still require a non-success status. |
| Evidence-file errors | Exclusive mode-0600 creation refuses existing files and final-component symlinks. Partial writes are completed; ENOSPC after a short write retains only the written prefix with matching byte count and hash. A zero-length write becomes EIO instead of looping. Both failures reap the client and cannot report an observed result. |
| Time and attempts | Finite timeout 1–120 seconds, at most 100 attempts, retry delay 0.05–1 second, selector polls at most 0.05 second, and 0.25 second reserved for cleanup. Closed pipes alone do not imply child completion. Hung or SIGTERM-ignoring synthetic clients reach timeout and are reaped. |
| Own-client cleanup | The runtime calls `terminate`, then bounded `wait`, and if needed `kill` and another bounded `wait` on its own Popen object. A regression intercepts every `os.kill` call, requires the observer's child PID, rejects group signaling and verifies a separately running process survives. Spawn/terminal setup failures close both PTY descriptors; pipe setup errors reap the created client. |
| CLI cancellation and output | The installed SIGTERM callback sets cancellation; the test invokes that callback seam and requires exit 130, a cancelled JSON result, closed client pipes and restoration of both original signal handlers. JSON escapes terminal controls; raw bytes remain in the private artifact rather than being printed. |

The review also retained existing checks for fragmented output, duplicate
identical mirror lines, source marker/index alignment, expected-result
mismatch, failed probe, stderr forgery, debug/log environment removal,
cancelled retries, byte caps, client/PTY cleanup and log preservation. Synthetic
failure to reap is explicitly exercised at the bounded cleanup seam; it is not
represented as a successful native cleanup observation.

## Results and limits

Command executed from the repository root:

```sh
python3 -B tests/fixtures/protection/uefi-tpm-probe/test_observe_console.py -q
```

Result: **44 tests passed in 4.249 seconds**. This includes both historical
reconstructions, all newly added failure-path regressions, and help/README
option correspondence. No runtime or historical-patch edit was necessary.

The timeout bounds userspace observation and ordinary client cleanup. It is not
a guarantee against a wedged kernel, a blocking executable launch or evidence
filesystem, loss of scheduling, or uncatchable termination of the observer.
The documented artifact-directory requirement remains: the operator supplies
an existing private directory; final-component exclusivity is not a general
defense against hostile replacement of ancestor directories. This fixture
trusts its ordinary-user environment and installed virsh executable; it is not
a general sandbox or privilege boundary for arbitrary local code.

The public marker is neither authenticated guest identity nor proof of a full
TPM/firmware capture. The console may become available after important output,
and a missed marker stays inconclusive. Native console permission, attachment
timing, guest shutdown, artifact completeness and independent restore are
outside these tests. The historical native failures remain preserved evidence.

This deliverable contributes synthetic fixture prerequisites to **SNAP-01**
and documentation/provenance validation to **REL-03**. It makes no CON hardware,
real Virmill console, complete capture or recovered-TPM claim. The parent owns
release evidence, native investigation, commits and any later runtime change.
