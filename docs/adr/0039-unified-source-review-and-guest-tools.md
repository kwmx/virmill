# ADR 0039: Unified source review and reviewed guest tools

Status: implemented; native evidence tracked separately.

The owner's metadata-first workflow applies to all supported import sources.
Import opens one file/folder browser at the normal user location. The shared
`import.source.describe` service identifies OVA, ISO and QEMU disk formats
(qcow2, raw, vmdk, vdi, vpc/VHD and vhdx). A selected disk folder explicitly
permits bounded, shallow inspection of dependency files. Parsers run confined
with held read-only descriptors. Archives and loose VM descriptors are rejected
with extraction/source-selection guidance rather than mistaken for raw disks.
Automatic archive extraction and loose OVF/VMX/VBOX reconstruction are not newly
claimed by this slice; the mandatory generic OVF acceptance remains open.

OVA settings use source declarations. Standalone disks and ISO files do not
reliably declare CPU, RAM or guest OS: suggestions are labeled and editable.
Metadata cannot replace verified preparation, durable planning or apply checks.
Source selection, summary, advanced hardware and destination use the same flow.

Guest tools are fixed, versioned recipes through the existing guest recipe
service and durable operation, with CLI/TUI parity. Exact built-in recipe content
is the only recipe allowed to request guest sudo. Arbitrary recipes remain
non-root. Plans bind SSH credentials, verified host keys, guest identity, recipe
content and explicit guest administrator acknowledgement. No host SSH or shell
execution is added to views. Linux recipes support Debian, Ubuntu and Fedora
with systemd, existing SSH access and guest passwordless sudo. Windows receives
manual trusted-media instructions; unsupported guests are not guessed.

New VM creation offers an explicit guest-agent channel toggle and acknowledgement.
The channel has a fixed virtio target and libvirt-owned automatic socket. Native
capability checks and narrow persistent XML comparison reject unexpected channels
or explicit socket paths. Existing guests are never silently retrofitted. Adding
a channel, installing a package and proving a host/guest agent handshake are
separate evidence levels. Desktop package installation does not prove clipboard
or resize support without compatible display configuration and native tests.

These choices preserve the specification's precedence, confinement and truthful
verification requirements. No mandatory scope is removed or acceptance promoted.
