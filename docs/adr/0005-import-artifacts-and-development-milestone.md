# ADR 0005 — Import preparation and the development milestone

Status: accepted implementation decisions; full 1.0 scope remains locked.

The owner's follow-up asks to continue until a minimum viable product is available.
Under the supplied precedence rules, this identifies an intermediate development
milestone. It does not remove any mandatory acceptance scenario or authorize a
smaller 1.0 release. A usable VM workflow still needs creation, configuration,
lifecycle and verified failure/recovery behavior; menus and synthetic backend
success are insufficient. The complete release checklist remains the final gate.

Documents 05, 07, 11, 12 and 14 require us to prove multi-disk import and unsafe
image handling early. Preparation is therefore an independently usable durable
operation before VM registration. `import prepare` consumes one explicitly selected
OVF system, requires a format and upper size bound for every disk, and publishes
independent qcow2 copies as one atomic directory. It retains the original OVF,
inventory, disk/controller ordering, original archive hash and converter identity.
Its `PreparedImport` receipt explicitly records `vmDefined: false` and
`guestBootVerified: false`. This is no claim that an arbitrary appliance will boot.

The fixed system `qemu-img` executable is hashed during preview and rechecked during
apply and before publication. The observed development executable is QEMU 10.2.2,
Fedora package qemu-10.2.2-1.fc44; exact environment and test hashes remain in the
evidence ledger. The adapter follows the documented [QEMU block graph contract](https://www.qemu.org/docs/master/interop/qemu-qmp-ref.html#object-BlockGraphInfo)
and [image-tool operations](https://www.qemu.org/docs/master/tools/qemu-img.html),
verified against that executable. No runtime download or version claim is inferred
from the online documentation.

Image parsing, conversion, checking and comparison execute as the ordinary user
in a private bubblewrap namespace, with no network syscalls, host devices, service
sockets or home access. Only imported source members are bound read-only; writes
go to that disk's private output workspace. Missing confinement fails closed.
All inspected block nodes, backing paths and VMDK extents must resolve to approved
members. Encrypted, dirty, corrupt, oversized or unresolved graphs are refused.
External qcow2 data-file layouts require a later adapter. Conversion uses explicit
formats and writethrough cache mode. It does not salvage, repair or modify sources.

Disk workers have two concurrent slots, 2 GiB address space, 256 processes, 1,024
file descriptors, 1,800 CPU seconds and a 30-minute wall limit per tool invocation.
The reviewed virtual size determines a per-file output bound. Plugin limits stay
at 60 CPU seconds, 64 file descriptors and 64 MiB output. These limits bound work;
they do not certify all images up to the maximum configurable size.

The publication parent must already exist, belong to the user, have no symlink
components and deny group/other writes. It is pinned by an open directory FD.
Output is flushed before a SQLite receipt is committed and `RENAME_NOREPLACE`
publishes the complete artifact. Recovery matches the published hashes to that
durable receipt. Partial files are never reused automatically. Cancellation kills
and joins the confined worker before removing only its unpublished staging tree;
uncertain failures retain staging. Successful preparation also retains source
staging until dependency-aware cleanup is implemented.

Two additive bundled schemas define preparation input and version 1 receipts.
The existing SQLite schema is unchanged. Unknown receipt fields/API versions are
refused. Native inode/device IDs and nanosecond timestamps are encoded as decimal
strings: RFC 8785 canonical numbers cannot preserve arbitrary 64-bit identities.
Compatibility tests cover that round trip and reject newer receipts.

Current evidence covers generated image files, real QEMU conversion and private
CLI/daemon operation. It does not cover VMware guest adaptation, libvirt domain
registration, NIC mapping, guest boot, firmware/TPM migration or hardware safety.
Those remain required. No disposable host has been authorized.
