# ADR 0013: Reviewed chipset devices and creation compatibility

Status: accepted implementation decision, 2026-09-07. This corrects the creation
adapter under the configuration-preservation and no-silent-hardware-change rules;
it does not narrow the complete local 1.0 scope.

The first disposable QEMU-driver run uploaded and verified a disk, then refused
completion because libvirt added devices absent from the reviewed recipe. The
captured definition includes Q35 controllers, PS/2 inputs, disabled audio, an ISA
serial target, a balloon and a reset watchdog. The normative no-silent-change rule
takes precedence over accepting whatever a backend happens to add. The original
uncertain job and its resource locks must retain their original meaning.

New creation plans use the durable operation `vm.create.devices-v1` and persist a
version-1 `hardware.devicePolicy`. If omitted from input, the shared service puts
conservative choices into the plan: USB and balloon disabled, watchdog action
`none`, PS/2 keyboard/mouse, disabled host audio, ISA serial, the selected Q35 or
i440fx chipset, and bounded automatic PCI placement. The plan requires the
`creation-device-policy` acknowledgement. Selecting a Q35 reset watchdog also
requires `watchdog-reset`. The same input, review, acknowledgements and failures
are available through CLI and TUI. USB-controller selection does not attach a
physical device or certify reconnection.

The generator explicitly requests these devices. Q35 includes its SATA and PCIe
root controllers; i440fx includes IDE and PCI root controllers. Libvirt documents
[implicit chipset controllers and automatic PCI bridges](https://libvirt.org/formatdomain.html#controllers),
[the Q35 iTCO watchdog and its default reset action](https://libvirt.org/formatdomain.html#watchdog-devices),
and disabling [USB](https://libvirt.org/formatdomain.html#controllers) and
[memory ballooning](https://libvirt.org/formatdomain.html#memory-balloon-device)
with model `none`. These API descriptions are not hardware qualification.

Automatic PCI placement permits only the bounded known root-port, PCIe-to-PCI
bridge and PCI bridge forms. Verification checks complete, unique controller
indices and addresses, existing parent buses, acyclic hierarchy, bounded numerical
fields and unique root-port chassis/ports. Unrecognized controllers, attributes,
ROMs, drivers, sources and devices still fail confirmation. Older Q35 bridge forms
and any future normalization require their own implementation and evidence.

A missing policy in a persisted `vm.create` recipe retains the old strict matcher;
it is never populated during execution or reconciliation. New operation types
prevent an older binary from executing the new recipe. Closed JSON decoding also
rejects fields it does not understand. No SQLite migration is needed: existing
plans, receipts and lock ownership are unchanged. Tests cover legacy execution,
lost-acknowledgement reconciliation, new-version refusal and inherited recovery.

This does not adopt or redefine the already retained uncertain VM. Accepting its
observed extra devices will require a separate reviewed recovery operation with
original identity/receipt binding, stopped-state checks, retained-volume readback
and atomic inheritance of all parent locks. That workflow remains outstanding;
the old job must not be made successful by weakening its matcher. Native tests of
new plans use separately identified disposable resources and preserve this case.
