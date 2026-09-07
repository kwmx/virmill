# ADR 0018 — Reviewed managed-volume read access

Status: implementation in progress; not release-qualified.

Native acceptance exposed U011: qemu file volumes can be root-owned mode 0600.
Libvirt download authority does not confer filesystem read permission for the
coordinator's cooperative QEMU guards. A temporary external ACL proved the
acceptance path, but fixture setup is not a shipped permission workflow.

Implement an explicit, reversible managed-volume read grant through the shared
service and bounded helper. This is a host mutation with a fresh immutable plan,
actor authorization, native VM/volume identity checks, and a durable helper intent.
The grant adds read access only for the authenticated ordinary-user actor. It does
not make a disk world-readable, change ownership, grant write/execute access,
disable SELinux/AppArmor, or run an image parser as root.

The helper accepts only a Virmill-named volume under an administrator-registered
root, with the exact file generation, current POSIX access ACL and mode, stopped
VM fingerprint, pool and disk identity bound into its signed request. Directory
and file resolution uses held descriptors and rejects symlinks, hard links,
non-regular files and replacements. The helper independently verifies the native
stopped-VM mapping; the coordinator's assertion alone is insufficient.

A named-user read entry may require broadening the ACL mask. Before doing so,
intersect existing group-class entries with the old mask so their effective
permissions remain unchanged. Keep owner/other permissions and prior non-read rights of the target actor.
Derive those rights from the connection-time kernel `SO_PEERCRED` and
`SO_PEERGROUPS` snapshot, including supplementary groups; no PID lookup or NSS
membership guess is used. Disjoint group write/execute rights that cannot be represented without
adding a previously denied non-read request are refused. Unsupported ACL layouts or incomplete observations
fail before mutation. Store the exact original ACL/mode for reviewed revocation.

Revocation refers to the original successful grant and restores only its recorded
original access metadata. It refuses changed file generations, ACLs or mappings;
it cannot be used as an arbitrary chmod/chown/xattr endpoint. A new plan binds
the current ctime; original proof ctime may advance through nested grants that
were subsequently revoked, while the original generation, content-related
metadata, ownership and full granted ACL must still match. Unwind grants in
reverse order and keep the guest stopped until restoration. Grant and revoke
persist helper intent before the metadata change and observe the result afterward.
A lost acknowledgement is reconciled against the helper's root-owned record and
actual metadata, never by blindly repeating the change. Original VM/disk bytes
remain outside all metadata write targets.

The administrator configures approved roots, actors and signing keys. The user
coordinator keeps its private signing key outside logs/plans and validates the
helper's root peer. The shipped default grants no authority. Installing keys or
changing socket/policy access requires explicit host setup; the disposable-host
fixture may configure these under the owner's existing authorization. Publication
and production-host installation remain separate review points.

Read permission remains until explicit revocation; a granted reader can retain a
copy of data it was authorized to read. This is not revocable secret disclosure or
protection against a malicious host administrator. Other required helper families,
complete cold firmware/TPM capture/restore and the full permission/distro/fault
matrix remain in scope. No existing creation/acceptance recipe gains implicit
permission changes or new authority.

Primary interface references: [Linux POSIX ACL semantics](https://man7.org/linux/man-pages/man5/acl.5.html)
and the installed Linux UAPI `posix_acl_xattr.h` version-2 little-endian encoding.
The local nested test namespace maps only UID 1000; kernel ACL tests using another
UID need an ordinary-user temporary-file run outside that namespace or the
authorized disposable host. A skip there does not qualify native permissions.

The engine now supplies the original durable job ID during explicit reconciliation,
matching ordinary execution. A lost-helper-acknowledgement regression test exposed
the missing context; no old plan or persisted receipt is rewritten. Access receipts
use a new version-1 metadata namespace and new allowlisted operation names.
