# ADR 0014: Boot order and retained installation media

Status: accepted implementation decision. All CORE-03/04/05 and complete local
1.0 requirements remain mandatory. This extends the stopped-VM preservation
adapter; it does not remove live editing, advanced hardware or recovery scope.

`vm set` may now request a complete ordered list of existing disk targets and
interface MACs, optionally ejecting one existing read-only CD-ROM. The CLI and TUI
use the same service. `vm boot show` observes live and persistent layers separately.
An empty medium or explicitly disconnected NIC cannot be selected as bootable;
no success flag claims the selected device actually boots.

The edit selects exact XML spans. It removes old firmware-class boot entries when
replacing them with explicit per-device boot orders, retaining all other settings.
Libvirt's [boot contract](https://libvirt.org/formatdomain.html#hard-drives-floppy-disks-cdroms)
requires per-device and firmware-class boot forms to be mutually exclusive.
Conflicting/direct boot, ambiguous identities and unsupported boot attributes are
refused rather than discarded. Other boot-capable device classes still require
dedicated adapters; they are not excluded from v1.

Ejection removes only the selected CD-ROM source. The file/volume is retained,
as are the read-only drive, target, controller, driver and other configuration.
A current CD boot candidate requires an explicit replacement boot order. The
empty block/volume CD-ROM is explicitly changed to type `file` in the preview,
matching observed native in-memory normalization. Structured/authenticated sources
and nonempty backing graphs require dedicated ejection adapters. No storage
cleanup, disk deletion, live detach or implicit shutdown occurs.

New boot/media plans use durable operation `vm.configure-hardware`, edit version
2. It may combine fixed CPU/RAM edits with boot and media changes in one definition.
Version-1 `vm.configure-resources` plans retain their original exact-XML digest
semantics. Older binaries refuse the unknown operation, and an operation/version
mismatch fails closed. SQLite schema 3 and existing receipts are unchanged.

The version-2 expected digest admits only known representation changes: attribute
order, empty-tag spelling, indentation at five known containers, and placement of
a boot child within a disk/interface. Unknown text, attributes, namespaces,
comments, processing instructions and other child ordering remain significant.
Inherited `xml:space` preservation disables both indentation and boot-position
normalization; `xml:space="default"` explicitly resets inheritance. The byte-span
proposal retains even exterior comments and processing instructions.

Before/after boot views expose the selected changes without persisting opaque XML
or source paths. Requested counts, identities, expected digest and original
fingerprint form the durable recipe. The existing secure/public XML comparison,
stopped/persistent/no-managed-save requirements, repeated state checks and
`exclusive-configuration-writer` acknowledgement still apply. Libvirt provides no
atomic external-writer compare-and-swap. Unknown readback changes remain uncertain
with locks retained; reconciliation observes the result and never redefines it.

Additional acknowledgements are `replace-boot-order` and `eject-retain-media` as
applicable. Cancellation cannot undo a completed native definition. Failed or
ambiguous effects retain uncertainty; blindly retrying a definition is forbidden.
The native in-memory tests and captured/synthetic XML tests are not guest or media
hardware evidence. Actual disposable-host tests must separately record source
preservation, firmware/configuration retention, applied order, empty media, and
subsequent boot. General uncertain-edit acceptance/disposition remains outstanding.
