# Cold source XML inventory

Run `virmill vm recovery inspect VM_UUID --output json`, or open **Protection →
vm recovery inspect** in the TUI. Both use the shared service and selected local
libvirt connection. The existing `layout` auxiliary-state field is preserved;
the additive `source` field includes that state plus disks and dependencies.
Malformed or unsupported source layouts return an error without a partial
inspection result. Running, managed-save and autostart observations remain
visible warnings; this read does not stop guests or acquire a capture grant.

`InspectColdSourceXML(raw)` returns a `domain.ColdSourceLayout` containing the
existing cold firmware/TPM/secret projection, declared architecture and machine,
every direct disk/media declaration, and unresolved external dependencies. It is
a read-only parser. It never calls libvirt, opens a storage path, resolves a pool,
reads image metadata, detaches a device or authorizes capture. The full original
persistent XML remains authoritative and must be retained separately.

This is prerequisite evidence for SNAP-01 and BAK-01. It does not implement or
qualify cold snapshots, complete backups, recovery or STO-01 storage mutations.
Before capture, a separate adapter must reconcile native state, the effective
and persistent definitions, actual disk formats/backing graphs, held source
generations, concurrent writers and every unresolved dependency.

Disk entries retain target, bus, device class, read-only status, empty removable
media, declared format, and a local file path or pool/volume names. Backing
entries follow the nested XML order. Unsupported block, network, directory,
NVMe, vhost-user, vhost-vDPA, CTL and future source types remain in the disk or
backing list with their type and format, plus an unresolved dependency targeted
to that disk or backing position. Network names, hosts, usernames, block paths,
command arguments and extension contents are not copied into dependencies.

The parser applies three documented native XML defaults: omitted disk `device`
means `disk`, omitted disk/backing storage `type` means `file`, and `cdrom` is
read-only. An explicit `<readonly/>` also sets read-only. Missing bus,
architecture, machine and formats remain empty. No format is inferred from a
filename or storage type. These shapes follow the official
[disk format documentation](https://libvirt.org/formatdomain.html#hard-drives-floppy-disks-cdroms)
and the installed native schema recorded with the fixtures.

An absent or empty source is accepted only for `cdrom` or `floppy`. Empty
file/block media may retain validated source index/startup-policy metadata. An
explicitly empty identity attribute is malformed. Empty data disks, LUNs or backing members
are refused. An empty `<backingStore/>` sets `BackingTerminated`; it records an
XML assertion only. Missing terminators stay false. Neither value proves that
the disk image's actual backing graph is complete or matches the XML.

Every filesystem, hostdev, shared-memory device, interface, memory device,
pstore, lease and emulator is an unresolved runtime dependency. RNG,
serial/console/channel, graphics and unknown devices also remain unresolved.
The cold-state projection accounts for TPM. Reviewed configuration-only devices
are retained in the authoritative XML without creating an external dependency.
The allowlist is deliberately narrower than the native schema:

| Device | Allowed configuration shape |
|---|---|
| `controller` | Types pci, usb, scsi, sata, ide, fdc, ccid, isa, virtio-serial; attributes type/index/model/ports/vectors; model, target, master and driver children |
| `input` | Mouse, tablet or keyboard; attributes type/bus/model; driver child |
| `video` | Driver and model children; known native software models, non-accelerated resolution settings; blob off or absent |
| `memballoon` | Attributes model/autodeflate/freePageReporting; stats and driver children |
| `watchdog` | Attributes model/action |
| `panic` | Attribute model |
| `hub` | USB type |
| `sound` | Attributes model/multichannel/streams; audio ID, codec and driver children |
| `audio` | Empty type=none element with id/type attributes |

Except standalone audio, these shapes allow empty `alias` (name), `acpi` (index)
and device `address` children. Address attributes are limited to
type/domain/bus/slot/function/multifunction/controller/target/unit/port/cssid/ssid/
devno/reg/iobase/irq/base. Driver attributes are limited to
name/queues/cmd_per_lun/max_sectors/iothread/ioeventfd/iommu/ats/packed/page_per_vq/
vgaconf, with driver name absent or qemu. Controller model permits name; target
permits chassisNr/chassis/port/busNr/index/hotplug/memReserve; master permits
startport. Stats permits period; codec permits type/cad; sound audio permits id.
Video model permits type/ram/vgamem/vram64/vram/heads/primary/blob/edid;
acceleration permits accel2d/accel3d absent or no, and resolution permits x/y.
The known video models are vga, cirrus, vmvga, xen, vbox, virtio, gop, none,
bochs, ramfb and qxl. Values in these configuration shapes are bounded scalar
identifiers. Unknown attributes/children, duplicate singleton children, foreign
extensions, sources, paths, external backends, accelerated/blob video and audio
backends other than none all remain dependencies. This classification does not
establish hardware/model compatibility; later native preflight must do so.

Targets such as `devices/device[3]` are one-based XML child locations, avoiding
disclosure of arbitrary backend identifiers. Root QEMU
command-line and other runtime extensions, including extensions nested in known
configuration containers, are unresolved. Direct-boot artifacts are marked as
boot/runtime dependencies. Application `<metadata>` stays opaque and does not
create file or secret lookup authority.

The direct native `domain/features/acpi` marker is configuration-only when it
has no namespace, attributes, children or non-whitespace text, matching the
[native ACPI feature shape](https://libvirt.org/formatdomain.html#hypervisor-features).
This exception does not propagate into nested containers. Attributed ACPI,
table/content-bearing ACPI and foreign extensions remain dependencies, as does
every `os/acpi` declaration. In particular, the
[OS ACPI table files](https://libvirt.org/formatdomain.html#operating-system-booting)
and kernel, initrd and DTB artifacts retain their boot/runtime dependency
locations without exposing their contents. An empty feature marker does not
establish firmware or hardware compatibility.

Disk encryption/authentication, shared/transient disks, mirrors, backend-domain
and private runtime data are explicit unresolved dependencies. Local sources
with encryption, slices, external data stores, FD groups or explicit volume mode
also remain unresolved. A declared file path in such an entry must never be
treated as a complete flat file. Secret UUID references come from
`InspectColdStateXML`; secret usage references and unsupported firmware/TPM
layouts retain that parser's visible refusal behavior. No secret values are
resolved or projected.

The parser refuses duplicate or foreign relevant fields, competing storage
identities, duplicate disk targets (including the `ioemu:` spelling), repeated
explicit source identities or indices in one backing chain, malformed markers,
unsupported ambiguous disk structure and unsafe local paths. Shared sources on
distinct disks are preserved as distinct references. Paths must be canonical
absolute POSIX paths, cannot be `/`, and cannot contain controls or bidirectional
formatting characters. This is lexical validation, not filesystem authorization.
Pool/volume names use a deliberately narrow baseline: at most 256 ASCII letters,
digits, dots, underscores, hyphens or colons, excluding `.` and `..`; other valid
native names require an adapter extension and are visibly refused. Unknown
safe format identifiers are retained, without a support claim.

Bounds are 1 MiB input, XML depth 32, 16,384 elements, 65,536 tokens, 256 disks,
16 backing members per disk, 1,024 total disk/backing entries, 1,024 unresolved
dependencies, 128 bytes per disk target and 4,096 bytes per path/scalar. The
reused state parser additionally bounds secret references. Any error returns an
empty layout. The parser validates the inventory fields it uses; it is not a
replacement for complete native-domain schema validation.

Generated fixtures and schema provenance are in
`tests/fixtures/protection/cold-source-xml/README.md`. Reproduce focused checks
with the pinned toolchain and offline dependencies:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race -tags libvirt_dlopen ./internal/backend/libvirt -run 'TestColdSourceXML|FuzzColdSourceXML' -count=1
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt -run '^$' -fuzz '^FuzzColdSourceXML$' -fuzztime=5s -parallel=2
```

The schema test uses installed `xmllint --nonet` and libvirt's installed
`domain.rng`; missing dependencies are reported as blocked skips. These fixtures
prove extraction and rejection behavior, wire compatibility and native XML
shape. They provide no evidence of safe storage capture, stopped-state control,
independent restore, physical devices or complete guest data.
