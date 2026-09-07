# VM resources, boot order and installation media

`vm set` provides preservation-checked CPU/RAM edits for a powered-off persistent
VM. Use its stable UUID and explicit local connection. The CLI and TUI share the
same plan, permission, execution and reconciliation services. This development
adapter has synthetic, libvirt in-memory and one narrow native test: a Q35/BIOS
Kali guest booted after a combined 3-CPU/3072-MiB, boot-order and media edit. See
the [native run](evidence/boot-media-run.md) for exact evidence and limits. General
guest sizing, live modes and all configuration profiles remain unqualified.

```sh
virmill vm set VM_UUID --connection qemu:///session \
  --input '{"vcpus":4,"memoryMiB":4096,"applyMode":"next-boot"}' \
  --plan --output json --non-interactive
```

The [example input](../examples/configuration/fixed-resources.json) can be copied
and edited. Omit either resource to leave it unchanged. `vcpus` accepts 1–512 and
also checks the selected backend's reported maximum. `memoryMiB` accepts whole
MiB from 1 through 1,048,576. These are parser/adapter limits, not a claim that the
host can allocate that much RAM or that a guest can boot with the requested size.

`applyMode` supports `next-boot` only in this adapter; an omitted mode is reported
as `next-boot` for compatibility with the old count-only input. `now` and `both`
are explicitly refused. The guest must already be stopped. A shutdown, managed-save
restore or saved-state disposition is a separate approved workflow.

In the TUI choose **VMs → vm set**, then enter:

```json
{"id":"VM_UUID","input":{"vcpus":4,"memoryMiB":4096,"applyMode":"next-boot"}}
```

Review the requested values, stable identity, required shutdown and risks. Plans
require `host-mutation` and `exclusive-configuration-writer`. The latter means
coordinating other administrators and VM managers so they do not edit/start/save
the VM during this operation. Virmill's lock serializes its own clients. Libvirt
has no atomic compare-and-swap for full definition, so immediate rechecks cannot
eliminate every external race; that support claim remains unqualified.

Apply the exact reviewed plan and digest using `plan apply`, explicitly providing
both acknowledgements. Closing either client leaves accepted work with `virmilld`.
The durable operation name is `vm.configure-resources`; the public command remains
`vm set`. Use **Jobs/Operations**, `operation show` and `operation watch` to inspect
its state. No host operation is performed by this documentation or its fixtures.

The edit changes only the selected text spans in the original persistent XML.
Unknown namespaces, metadata, attributes, device configuration and comments retain
their original bytes in the proposed definition. Memory and currentMemory change
together only if their old byte values agree; an absent currentMemory stays absent.
Existing units remain intact. Inexact conversion, overflow and ambiguous/structured
fields are refused rather than rounded or reconstructed.

This fixed-resource adapter refuses dependent configurations: CPU topology,
per-vCPU allocation/current limits, pinning/tuning, automatic placement, NUMA,
separate current/maximum balloon policy, hotplug maximum memory, memory backing
policy and memory devices. Their dedicated editing workflows remain mandatory
work. Do not remove such settings merely to make this adapter accept a VM.

The native adapter privately compares ordinary and secure inactive XML to detect
omitted settings such as display passwords. A difference is refused before
redefinition; lack of secure-read permission is also a refusal. That read requires
a writable libvirt connection handle but does not mutate the VM. Secure XML and
opaque ordinary XML values are not persisted in new resource plans. Plans retain
the requested counts, before fingerprint and expected XML hash. Returned schema
and native definition errors withhold input/XML values.

Execution reconstructs the span edit from freshly checked XML. It verifies both
ordinary and secure readback and keeps uncertainty if they differ from the exact
expected result. Unsupported backend normalization can therefore leave an
operation requiring attention after a definition; this is not permission to retry
blindly. It also does not certify guest boot, live RAM, host available memory or
application readiness.

If acknowledgement is lost or the daemon restarts, inspect the operation and use
`operation reconcile OPERATION_ID`. Reconciliation observes the exact result and
never redefines it. A mismatched or absent result keeps uncertainty and its resource
lock; reviewed recovery/disposition remains required. Cancellation before a safe
step boundary can avoid the call. Once the native call starts, cancellation cannot
undo it; a verified completed edit is reported as succeeded with cancellation
recorded. No automatic rollback, disk deletion or guest shutdown occurs.

Older binaries do not know the new durable operation and cannot execute it.
Legacy `vm.set` plans need a fresh preview, and legacy uncertain operations need
explicit disposition. SQLite schema 3 and its existing migration backups remain
unchanged. See [ADR 0011](adr/0011-fixed-resource-configuration-preservation.md).
Advanced CPU/RAM, live modes and remaining configuration workflows stay in the
full 1.0 matrix. The boot/media extension below adds a separate versioned operation.

## Boot order and installation-media ejection

Inspect the exact existing targets before editing:

```sh
virmill vm boot show VM_UUID --connection qemu:///system --output json
```

The result has separate `persistent` and `live` boot views, along with VM state and
managed-save presence. `mode` distinguishes firmware defaults, legacy device-class
order, per-device order and direct boot. A disk selector uses its observed target
(for example `vda`); an interface selector uses its observed MAC. An order of zero
means the device has no explicit per-device boot rank. This is observed intent,
not a boot or network readiness test.

To finish an installation, select the installed disk and eject its installer:

```sh
virmill vm set VM_UUID --connection qemu:///system \
  --input '{"bootOrder":[{"kind":"disk","id":"vda"}],"ejectMedia":"sdb","applyMode":"next-boot"}' \
  --plan --output json --non-interactive
```

Replace `vda` and `sdb` with targets from `vm boot show`; do not assume these names.
The [example](../examples/configuration/finish-installation.json) is a template.
`bootOrder` is the complete desired order: omitted devices lose their explicit
rank. It may include existing interface MACs using `kind: interface`; disconnected
interfaces and empty media are refused. Legacy firmware-class order is replaced
explicitly. Direct/conflicting boot, ambiguous identities or advanced boot options
are refused with an explanation and require their dedicated workflow.

`ejectMedia` accepts one existing explicitly read-only CD-ROM. Ejection retains
its backing file/volume and the empty drive. An empty block/volume drive is shown
as `type: file` in the reviewed result. Ejecting a boot candidate requires a
replacement `bootOrder`. Supplying the ejected target as a new boot candidate is
refused. The command does not complete an installer, change guest credentials or
remove installation files. Authentication, structured source policy and backing
graphs require dedicated adapters. Inspect ordinary VM inventory for the retained
source before the edit; opaque source paths are not copied into the new plan.

In **VMs → vm set**, enter the same input wrapped as
`{"id":"VM_UUID","input":{...}}`. **VMs → vm boot show** takes the VM UUID.
Review `beforeBoot`, `afterBoot`, requested resource changes and the required
acknowledgements. Use `plan apply` with the exact digest and all listed `--ack`
values, or press `a` in the TUI and type the displayed digest. Boot replacement
adds `replace-boot-order`; ejection adds `eject-retain-media`. Fixed CPU/RAM values
may be combined with these fields in one reviewed definition.

This workflow requires an already stopped persistent VM without managed-save
state. It uses `vm.configure-hardware` internally and never changes an older
resource plan's meaning. A known formatting change can verify semantically;
unrecognized changes, unavailable secure readback or lost acknowledgement retain
uncertainty. Inspect `operation show/watch` and explicitly reconcile by observation.
Do not replay, erase locks or manually remove a medium to make a failed edit appear
successful. See [ADR 0014](adr/0014-boot-order-and-retained-media-edits.md).
