# ADR 0068: Change CPU and memory while a VM runs, and boot edits while running

Status: accepted and implemented. Native evidence: `resources-live-native-002`
and `boot-edit-running-native-001`
([run record](../evidence/live-resources-run.md)). Plan item 2b.5.

## Context

ADR 0061 lets CPU and memory edits be reviewed and applied while a VM runs, but
they change only the saved definition: the guest keeps its CPU count and memory
until it is next started. Boot order and media ejection, which are also
next-boot changes, still require a stopped VM for no reason other than that the
older rule was never revisited.

Nothing changes a running guest's CPU count or memory today. libvirt can, within
what the running domain was started with:

- `virDomainSetVcpusFlags` with `VIR_DOMAIN_VCPU_LIVE` sets how many of the
  domain's vCPUs are plugged in, from one up to the maximum the domain was
  started with (`<vcpu>`). A domain started with no spare slots — current equals
  maximum, which is what Virmill defines — can only lose CPUs, and QEMU cannot
  unplug a CPU the domain booted with.
- `virDomainSetMemoryFlags` with `VIR_DOMAIN_MEM_LIVE` sets the balloon target,
  from a floor up to the domain's memory maximum. It needs a virtio memory
  balloon in the running domain, and it is a *request*: the guest gives the
  memory back only if it has the balloon driver. Going above the maximum needs
  memory hotplug, which Virmill does not model.

So the operation is real, but most of this item is refusals, and the refusals
are only honest if a Virmill VM can be given room to change in the first place.
Today it cannot: creation writes `<vcpu>N</vcpu>` with no spare slots and
`<memballoon model="none"/>`, and the next-boot editor refuses a `<vcpu>` element
that carries a `current` attribute at all.

## Decision

**1. Boot edits while a VM runs.** Boot order and media ejection are offered for
running, paused and stopped VMs on ADR 0061's terms: the plan records
`editPrecondition: "persistent-xml-v1"` with the SHA-256 of the saved
definition, apply checks that digest and the absence of managed-save state, and
the readback also checks that the *live* definition's boot order and media did
not change. The review says the change applies the next time the VM starts, not
after a restart inside the guest. The guest-agent channel stays stopped-only: it
adds a device, and offering it on a running VM invites the belief that the
channel can be used now.

**2. Room to change.** Creation's memory balloon default becomes `virtio`, which
is libvirt's own default; `none` stays an advanced choice. That is all memory
needs: the balloon moves memory below the maximum and back, so a VM defined the
way Virmill defines it can give memory to the host and take it back.

CPU slots are not invented. A VM has spare slots only if its definition already
carries them (`<vcpu current="n">max</vcpu>`), which Virmill does not write:
the next-boot edit binds the exact bytes of the definition it produces, and an
attribute libvirt renders in its own way cannot be added under that rule without
its own renderer and native proof. What the next-boot edit does now is *keep*
the slots a VM already has instead of refusing to edit it at all: the boot count
moves within the maximum, and a request at or above the maximum sets both, which
are the only two shapes libvirt itself writes. Choosing slots at creation is a
later slice; until then live CPU changes serve VMs defined with slots elsewhere,
and the refusal says so.

**3. The live operation.** `vm.resources-live-v1`, through `vm.plan` action
`set` with `applyMode: "now"`, changes the running VM only. It never touches the
saved definition, so a VM's next boot is exactly what was last reviewed for it.

- **Requires** a running VM (not paused: a paused guest cannot service a balloon
  or plug in a CPU), no managed-save state, and a live definition Virmill can
  read.
- **CPU.** The requested count must be between one and the maximum the domain is
  running with, and different from the current count. Raising plugs CPUs in;
  lowering unplugs them, which only works for CPUs that were plugged in after
  boot and only with a cooperating guest. Refused with what to change: a VM
  running all the CPUs it has ("this VM is running all N of its CPUs; it was not
  started with spare CPU slots"), or a request above the maximum.
- **Memory.** The requested size must be between 256 MiB and the maximum the
  domain is running with, and different from the current size. Refused when the
  running domain has no virtio memory balloon ("this VM has no memory balloon,
  so its memory can only change at the next boot"), and when the request is
  above the maximum, naming the next-boot edit.
- **Acknowledgements.** `host-mutation`, `exclusive-configuration-writer`, and
  `guest-resource-pressure` whenever CPUs or memory are taken away.
- **Review.** The running values and the requested values, that the saved
  definition is unchanged and the next boot is unaffected, and, for memory, that
  the change goes through the guest's balloon driver, which Virmill waits for.
- **Effect and reconciliation.** One step: set the live value. The completion
  predicate is what the running domain reports, because that is all libvirt
  exposes: a balloon target is not readable, only the size the guest has
  acknowledged. A memory change therefore asks, then waits up to 20 seconds for
  the guest to answer. A guest that does not answer gets its earlier size asked
  for again, and once it is running that again the job fails plainly, saying the
  guest did not answer and its balloon driver may not be running. A refusal
  before any effect also ends the job failed, releasing its locks
  (`operations.NotDone`). An uncertain effect — a partially plugged CPU set, a
  balloon that answers neither request, a lost connection — leaves the job
  needing recovery; reconciliation reads the live definition and reports whether
  the reviewed values are in place, and nothing is replayed.

## Consequences

- A user can hand memory back to a host that needs it, and take it back, without
  stopping the guest. CPUs can be added and removed live on a VM that was
  started with spare slots; on one that was not, the refusal says so rather than
  failing inside libvirt.
- New VMs get a virtio balloon. A guest without the driver is unaffected; the
  device is one more thing on the PCI bus, and `none` remains available.
- A memory change is only reported as done once the running domain reports the
  new size, which is the guest acknowledging it. What the guest then does with
  that memory inside itself is its own business and is not claimed. CPU and
  memory maximums of a *running* domain cannot be raised at all, and memory
  hotplug (`maxMemory`, DIMM devices) stays out of scope.
- Live changes are not recorded in the saved definition, so a reboot returns the
  VM to its reviewed configuration. That is deliberate: one place decides what a
  VM boots with.
