# Everyday power actions on the test VM

Build `a2f470b` (1.0.0-beta.4), installed from its RPMs on the owner-authorized
test VM. Probe: `tests/fixtures/release/power_native_cycle.py`. Every action is
a reviewed CLI plan with its exact acknowledgements, applied as a detached job
and read back with `vm show`. The probe refuses to run unless the VM is stopped,
has no saved state, was created by Virmill and matches the name given. It
compares the other VMs, prior jobs and source media before and after.

## Session connection, a guest with no operating system

`power-session-native-001`, `qemu:///session`, VM `onestep-b3215d48`: a 64 MiB
blank disk left by an earlier import run, so nothing inside answers ACPI.

| Step | Result |
| --- | --- |
| Start, pause, resume | running, paused, running |
| Save, then Resume saved | stopped with saved state, then running with none |
| A pause plan made while running, applied after another plan paused the VM | refused with `STALE_PLAN` before a job was made; VM still paused |
| Force off from paused | stopped |
| Start, then Shut down | job failed with `WAIT_TIMEOUT` after 60 seconds; VM still running, nothing forced |
| Force off right after | stopped |

Before `a2f470b`, the unanswered shutdown left a recovery-required job that kept
the VM locked, so Force off could not run. This run confirms the fix on a real
host. The VM ended stopped with no saved state and its persistent definition
byte-for-byte unchanged; other VMs, prior jobs and source media were unchanged.

## System connection

The first attempt, `power-system-native-001`, stopped at its first apply before
any job or change: the probe built idempotency keys from the shared stage name,
so the start plan reused the session run's key and was refused (exit 5). The
probe now keys each run by its own folder.

`power-system-native-002`, `qemu:///system`, VM `noble-server-cloudimg-amd64 2`:
the Ubuntu 24.04 cloud guest from the cloud-image walkthrough, which answers
ACPI. The probe waited 90 seconds after each boot before a graceful request.

| Step | Result |
| --- | --- |
| Start, pause, resume | running, paused, running |
| Save, then Resume saved | stopped with saved state (1.7 s), then running with none |
| Stale pause plan after another plan paused the VM | refused with `STALE_PLAN` before a job was made |
| Force off from paused; start; force off from running | stopped, running, stopped |
| Start, then Restart after the guest booted | reboot event observed; running |
| Shut down after the guest booted again | stopped by the guest within the 60-second wait |

The VM ended stopped with no saved state and its persistent definition
unchanged; other VMs, prior jobs and source media were unchanged.

Limits: the stale plan was made stale by another Virmill plan, not by an
external XML edit. Guest readiness is not checked. The TUI Force off entry is
covered by software tests, not this run.
