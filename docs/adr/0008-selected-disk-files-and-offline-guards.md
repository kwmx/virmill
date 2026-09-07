# ADR 0008 — Selected disk files and offline read guards

Status: accepted routine implementation decision. The import and security rules
require independent copies, complete dependencies, offline tools and confinement.
The workflow/gate rules require a shared CLI/TUI service and durable recovery.
These take precedence over assuming successful image inspection proves a source
is offline. All creation sources and full 1.0 scope remain required; no host
mutation or publication is authorized by this decision.

Existing disk sets are prepared with `import.prepare-disks`, independently of OVA
extraction. The caller selects every root, backing file and split extent under one
directory and supplies explicit formats and size limits. Openat2 rejects symlink
components and escapes. Held read-only descriptors expose only selected names
through bubblewrap `--ro-bind-fd`; no source-directory grant is made. Contained
relative parent references are normalized for membership/cycle checks without
rewriting the QEMU audit report. Escape followed by reentry is still rejected.

The first actual lock experiment passed for qcow2 and failed for raw: unguarded
`qemu-img info` could inspect a raw file held writable by `qemu-io`. A permanent
regression fixture retains this observation. The production adapter now acquires
independent OFD read guards before hashing, keeping them through all conversions
and final source validation. QEMU's file permission protocol uses separate byte
ranges for acquired and unshared permissions. The guard requests consistent read,
refuses sharing write/write-unchanged/resize, then checks for conflicting owners.
Applying locks before checking closes the cooperating-writer acquisition race.
Each guard owns a separate open description, so closing a nested inspector does
not release another reader's guard. No source bytes are written.

This implementation was checked against exact upstream QEMU 10.2.2
[file-posix.c](https://github.com/qemu/qemu/blob/v10.2.2/block/file-posix.c) and
[permission definitions](https://github.com/qemu/qemu/blob/v10.2.2/include/block/block-common.h),
plus the installed 10.2.2 tools. Source hashes are recorded in the dependency
contract. The [QEMU locking documentation](https://www.qemu.org/docs/master/system/images.html#disk-image-file-locking)
describes cooperative locks and explicitly allows disabling them. Therefore the
request requires an offline-source assertion and exact acknowledgement as well as
the guard. There is no claim of exclusion against noncooperating writers or root.
Missing OFD support fails closed. Toolchain changes require rerunning this contract,
including refusal of already-open writers, prevention of later writers and release
after the last relevant descriptor closes. No VM was started in these tests.

The immutable plan pins each original absolute path, native identity, change time,
SHA-256, optional supplied digest, complete observed chain, output bound, parent
and tool. It also locks source inodes and `local-file|ABSOLUTE_PATH` resources.
Creation cleanup deletion uses the same file-path resources and rejects older
delete plans missing them, requiring a fresh preview. Persisted source proofs
contain actual paths, so reference reconciliation cannot miss them behind an
opaque identifier. Provenance references currently protect originals conservatively;
explicit reference disposition and full GC remain future mandatory work.

All root disks become checked, compared independent qcow2 files. A versioned
`PreparedDiskSet` receipt and source report explicitly declare hardware unknown;
no synthetic OVF descriptor is passed off as original configuration. Publication
uses the existing private-receipt-before-rename pattern. Cancellation joins workers
before removing unpublished staging; uncertainty retains it. Reconciliation after
journal reopen matches the private receipt, plan/input digest and owning operation,
never replaying conversion. Approved creation consumes either this new kind or the
existing `PreparedImport` kind and still requires every hardware choice.

This adds an operation/kind, not a SQLite layout change. Schema 3 remains current.
Older binaries cannot execute the unknown handler or decode the new source kind;
legacy OVA receipts retain their schema. Tests cover journal reopen, legacy/new
schema separation and future-field refusal. A source lock protocol discriminator
also prevents silently interpreting a changed recipe. The native creation,
firmware, guest-driver, ISO/cloud provisioning and hardware support claims remain
blocked pending their implementations and authorized qualification.
