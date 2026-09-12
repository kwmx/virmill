# CPU and memory settings

Open **VMs**, select a VM and choose **CPU / RAM**. The page reads the VM again
and shows **Live** and **Next boot** values separately. Requested values start
with the next-boot settings, so you only need to change what you want.

Change CPU count or RAM, then choose **Preview changes**. Review the affected VM
and requested changes before applying. Going back keeps your edits. An unchanged
form does not submit a job. A blank or invalid value explains what to enter.

This beta changes next-boot CPU/RAM settings on stopped persistent VMs. A running
VM needs a separate reviewed graceful shutdown first. Managed-save state must be
restored and then shut down before changing hardware. There is no automatic hard
power-off. Current live values describe the active configuration, not CPU usage
or memory consumption inside the guest.

VMs with CPU topology, NUMA, ballooning or memory hotplug can still show their
observed values. If the basic editor cannot preserve that layout, the affected
control explains the restriction. It does not reset advanced settings or round
an existing byte count to a different amount of RAM. If the existing RAM is not
an exact whole MiB, **Keep current** preserves it; enter a new whole-MiB value
only when you intend to change it. A supported independent field can still be edited.

The same read-only view is available through the CLI:

```sh
virmill vm resources show UUID --output json
virmill vm set UUID --input '{"vcpus":4,"memoryMiB":8192,"applyMode":"next-boot"}' --plan
```

Apply the returned plan through the usual explicit digest and acknowledgement
flow. The service rechecks current state and XML preservation before execution.
Readable settings do not prove enough host memory is available or that the guest
will boot; the plan retains those limitations.
