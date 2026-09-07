# ADR 0022: authenticated auxiliary metadata inventory

Status: accepted for implementation; capture and restore qualification remain open.

The mandatory cold recovery workflow needs every declared NVRAM and TPM member,
its native mapping, generation and access metadata. The ordinary coordinator
cannot assume permission to inspect protected state directories. The existing
managed-volume ACL grant does not authorize confidential firmware or TPM access.
ADRs 0018 and 0020 and the specification's security and packaging contracts take
precedence over convenience or distribution pathname conventions.

The parent integration owner defines a separate `state.auxiliary` helper request.
It binds the authenticated ordinary actor, signing key, exact native VM UUID,
administrator root ID, request digest and nonce, observed configuration fingerprint
and versioned payload. Existing signed request bytes remain unchanged when this
optional payload is absent. No legacy policy authorizes auxiliary inspection.
Policy requires one exact actor/key/VM/root permission, configured state UID/GID,
whole-set member and byte bounds, and a separate capture flag. Overlapping matching
permissions are denied. Revocation and kernel-peer/signature checks run on every
connection before native or filesystem access.

The first connected mode is metadata-only `inspect`. The ordinary shared service
selects a persistent stopped system VM without managed save or autostart. The
helper independently observes native state three times, enumerates twice, and
rechecks held objects and their rooted paths before return. Source paths come
only from its native observer. Unresolved TPM sources are refused; installed
distribution conventions are not authority to guess a state path. This read
creates no durable operation or helper intent. Its wire `jobID` is a fresh request
correlation nonce, not a persisted job.

The root is held with `O_PATH` and must be administrator-owned without group or
other write permission. Children use `openat2` beneath that root with no symlinks,
magic links or mount crossing. Directory enumeration alone uses a readable
descriptor, with `O_NOATIME`; state members never do. Files require the exact
policy UID/GID and a single regular-file link. Birth, mount, inode, attributes,
timestamps, size, ownership, permissions, POSIX ACL and SELinux bytes are bound as
metadata. Missing identity fields, unsupported metadata or unstable membership
fail the entire observation. ACL and SELinux attributes are bounded lossless hex.

The supported inventory includes explicit regular NVRAM files and explicit TPM
file/directory sources, including empty ordinary TPM members. A direct TPM root
`.lock` is separate control metadata, never payload or a state-completeness proof.
It must be one empty regular file when present. Nested/control-source ambiguity,
aliases, special files, empty NVRAM, empty TPM sets, excessive depth or bounds are
refused. Limits are 128 payload members, 128 directories, depth 16, 255 MiB declared
payload and a 96 KiB encoded inventory. Administrator bounds may be smaller.

The dedicated client receives ancillary descriptors safely even for a metadata
reply, then rejects and closes any descriptor. It validates the response contract,
request binding, native identity, root mapping and layout. The previous sealed
snapshot receiver still requires one immutable descriptor. Successful inventory
cannot claim capture, independent recovery or guest boot. CLI and TUI call this
same service, and both return failures without a usable inventory proof.

Capture/observe wire shapes reserve exact expected inventories and capture-only
authority; their server dispatch currently returns `UNSUPPORTED_CAPABILITY`.
They cannot fall through into legacy storage/ACL execution. No archive writer,
source-content reader, state ACL grant, helper capture journal, coordinator
staging publication or capture command is enabled by this decision. The proposed
bounded auxiliary USTAR set is a future capture artifact, not a recovery point.
Implementing that mode still requires durable intent, source/producer exclusion,
sealed delivery, independent coordinator verification and publication recovery.
Complete VM capture additionally requires every disk, configuration and dependency,
restic integration, original-independent restore and guest verification.

The [swtpm probe](../reviews/swtpm-cold-writer-exclusion.md) establishes a narrow
local fact: OFD read locks conflict with the directory backend's cooperating
POSIX writer lock. BSD `flock` does not; closing an arbitrary duplicate of a
POSIX-locked file can release the process lock. Lock-inode replacement and
lock-disabled producers bypass that exclusion. The
[libvirt source review](../reviews/libvirt-swtpm-lock-selection.md) establishes
that the native `,lock` selection depends on a private feature bitmap, not a
public XML or domain-capability field. Neither observation proves safe capture.
File/block/shared-storage capture and implicit TPM path resolution remain open.

This adds no persistent coordinator schema and requires no data migration. The
optional policy extension fails closed on older policy files; old software must
be upgraded together with its helper before an administrator adds new fields.
There is no new dependency, publication destination or product scope exclusion.
All 71 acceptance scenarios remain required and unaccepted.
