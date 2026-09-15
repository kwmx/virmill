# Next-boot CPU and memory edits on a running VM

`resources-running-native-001`: build `0c686ce` (1.0.0-beta.4) installed from its
RPMs on the owner-authorized test VM, `qemu:///system`, VM
`noble-server-cloudimg-amd64 2` (Ubuntu 24.04 cloud guest, 2 CPUs, 2048 MiB).
Probe: `tests/fixtures/release/resources_running_native.py`. Every change was a
reviewed CLI plan applied as a detached job ([ADR 0061](../adr/0061-next-boot-edits-while-running.md)).

| Step | Result |
| --- | --- |
| Start, then raise next-boot values to 3 CPUs and 2560 MiB while running | job succeeded; the running guest kept 2 CPUs and 2048 MiB; the view said a shutdown is needed |
| Shut down and start | the guest now ran with 3 CPUs and 2560 MiB |
| While running, plan a restore to 2 CPUs and 2048 MiB and a second edit to 4 CPUs | both plans made |
| Shut down, then apply the restore | job succeeded: a plan made while running stayed valid after the VM stopped |
| Apply the second plan | refused with `STALE_PLAN` before a job was made, because the saved definition had changed |

Every edit plan's estimate said the VM keeps running until it is shut down. The
VM ended stopped with 2 CPUs and 2048 MiB and a saved definition byte-for-byte
identical to the original. Other VMs, prior jobs and source media were unchanged.

Limits: the running guest's balloon and guest-agent state did not change during
the run, so tolerance of live-only changes is covered by software tests. The TUI
edit path is covered by software tests, not this run.
