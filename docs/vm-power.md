# VM power actions

Every power action is a reviewed plan applied as a job. In the TUI, select a VM
and open its tasks: **Start VM**, **Shut down VM**, **Restart VM**, **Pause VM**
and **Resume paused VM** are in the short list when the VM's state allows them.
**Save VM state and stop**, **Resume saved VM** and **Force off VM** are under
Advanced. The CLI commands are `virmill vm start|stop|reboot|pause|resume|save|restore-saved UUID`
and `virmill vm stop UUID --hard`. Each returns a plan; apply it with
`virmill plan apply` and the acknowledgements it lists.

| Action | From | Leaves the VM |
| --- | --- | --- |
| Start | stopped | running; a saved state is resumed |
| Shut down | running | stopped, if the guest shuts down within 60 seconds |
| Force off | running or paused | stopped at once; asks for `data-loss-hard-stop` |
| Restart | running | running, after one graceful reboot ([details](vm-reboot.md)) |
| Pause and resume | running, paused | paused in memory, then running |
| Save | running | stopped, with its memory saved by libvirt (not a backup) |
| Resume saved | stopped with saved state | running where it was saved |

To save a paused VM, resume it first.

**When the guest ignores Shut down.** A shutdown is a request to the guest
through ACPI. A guest with no operating system, or one that ignores the power
button, keeps running. After 60 seconds the job fails with `WAIT_TIMEOUT` and
nothing is forced. The VM is free right away: shut it down from inside the
guest, try again, or choose **Force off VM**. If the guest stopped or changed
state in some other way meanwhile, the job is left for review under **Jobs**
instead.

**Plans are checked again before they run.** A plan records the VM as it was
reviewed. If the VM changed since, for example because another plan paused it,
applying the old plan is refused and nothing happens. Make a new plan.
