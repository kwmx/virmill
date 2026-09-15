# ADR 0061: Change next-boot CPU and memory while a VM runs

Status: accepted. Software tests cover the service, the libvirt in-memory driver
and the TUI; native evidence is pending.

## Context

CPU and memory edits change only the VM's saved (persistent) definition; they
take effect the next time the VM starts. Today they also require a stopped VM.
Someone who wants to give a running VM more memory must shut it down, edit,
then start it: three trips for one change, and the shutdown comes first even
though the edit does not need it.

libvirt can redefine the saved definition of a running domain. The running
guest keeps its current configuration until it is powered off; a reboot inside
the guest does not apply the saved definition.

A running VM's full fingerprint covers its live XML, which changes on its own:
balloon memory and guest-agent channel state move as the guest runs. A plan
checked against that fingerprint would often be refused as stale for reasons
unrelated to the edit.

## Decision

- **Allowed states.** Next-boot CPU and memory edits are offered for running,
  paused and stopped VMs with a saved definition and no managed save state. Boot
  order, media ejection and the guest-agent channel still require a stopped VM.
- **Precondition.** For a VM that is not stopped, the plan records
  `editPrecondition: "persistent-xml-v1"` and the SHA-256 of the saved
  definition. Apply checks that digest, the absence of managed save state and
  the reviewed result, not the full fingerprint. A VM that stopped or started in
  between is still edited, because its saved definition is what was reviewed.
  Plans without the field keep the stopped-VM rule unchanged.
- **Effect and readback.** The same preservation-aware define is used. Readback
  checks the saved definition as before; for an active VM it also checks that
  the live definition's CPU and memory values did not change.
- **Review.** The review shows the running values and the next-boot values and
  says that the change applies after the VM is shut down and started again, not
  after a restart inside the guest. The TUI edits in place and offers Shut down
  afterwards rather than before.

## Consequences

- One fewer shutdown for the common "more memory" change, and the VM keeps
  running until the user chooses to stop it.
- Live (hot-plug) CPU and memory changes are still not offered. Virmill-created
  VMs have no headroom (current equals maximum), so they would add little; a
  later ADR can add them with maximum values set at creation.
- Another writer that changes only the live definition does not invalidate the
  plan. One that changes the saved definition does.
