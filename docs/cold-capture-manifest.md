# Cold recovery manifest declaration

`CaptureManifest.Validate` checks the internal consistency of a version 1
`ColdRecoveryPoint` declaration. `DecodeCaptureManifest` first applies the 8 MiB
document bound, strict `wire.Decode`, and the bundled
`schemas/cold-recovery-point.schema.json` shape contract. All serialized fields
are required; arrays must be arrays, including empty arrays. Only the typed
optional source NVRAM and TPM objects may be null. Unknown fields, case aliases,
duplicate keys, malformed Unicode, null scalar substitutes, trailing values and
unsupported versions fail. Decode failure always returns a zero manifest.
Typed validation also rejects nil required collections, so an accepted Go
declaration can serialize to this explicit wire shape.

This is declaration validation. It does not read source files or recovery
members, verify a digest against bytes, parse the captured XML, resolve a native
state directory, flatten an image, authenticate a coordinator receipt, or prove
an actual capture. The `independentlyRecoverable` and per-disk `independent`
fields remain claims supplied by the document. Successful validation must not be
reported as complete capture, independent recovery, firmware/TPM identity
preservation, or guest boot evidence.

The snapshot, operation, VM and secret identities use canonical lowercase,
nonzero UUID strings. Native UUID versions are not restricted to version 4.
The source is one local `libvirt` VM on `qemu:///system` or `qemu:///session`,
whose source-state UUID must match. Start and finish times must be nonzero,
representable UTC timestamps with offset zero and finish no earlier than start;
equal timestamps are allowed. Both observed state declarations must say
`stopped`, managed-save state is forbidden, and the source fingerprint must be
a lowercase SHA-256 digest. Bounded nonempty version strings for libvirt and
QEMU are required, plus swtpm when TPM state is present. These strings are
recorded claims, not observed runtime qualification.

The member inventory is bounded to 256 entries, with unique IDs and canonical
relative paths of at most 1,024 bytes and fewer than 32 slashes. Paths cannot
contain traversal, backslashes, colons, terminal controls or invisible format
characters. Case-insensitive duplicate paths and file/directory prefix
conflicts are rejected. Each member declares a lowercase SHA-256 digest and a
positive size, except TPM members may explicitly contain zero bytes. No member
may exceed 16 TiB, and the total may not exceed 64 TiB. IDs and ordinary scalar
version values are limited to 256 bytes; native version maps to 32 entries.

Every member must have exactly one declared role:

- Each nonempty source disk or removable medium has one unique target mapping
  to an independent raw or qcow2 output, with the matching `disk` or `media`
  member kind. Empty removable devices have no captured disk mapping. Source
  targets cannot duplicate an `ioemu:` alias of another target.
- Exactly one `persistent-xml` member is referenced. Effective XML either
  explicitly shares that reference or names a separate `effective-xml` member.
  This deliberate XML sharing is the sole exception to one reference per member.
- `firmware-code` is required exactly when a loader path exists. `nvram` is
  required exactly when the source has an NVRAM object. TPM state requires a
  nonempty TPM member list; TPM names are canonical relative paths with no case
  or prefix conflicts.
- An `auxiliary-inventory` member is required exactly when NVRAM or TPM state
  exists. Native source paths that were not exposed may remain empty. A separate
  coordinator must independently resolve and verify the auxiliary inventory;
  the member's presence here does not establish completeness or ownership.
- Each source secret UUID has exactly one included `secret` member or an
  explicit external disposition. Source reference duplicates are rejected. A
  TPM encryption UUID may overlap the source reference list and still requires
  only one disposition. Missing, extra, duplicate or contradictory dispositions
  fail validation.

The source projection must retain bounded, unambiguous identities. Architecture
and machine identifiers are required. Nonempty source paths are canonical
absolute paths of at most 4,096 bytes without controls. File sources carry only
their path; volume sources carry only their bounded pool and volume names.
At most 256 source disks, 16 backing entries per disk, and 1,024 aggregate
storage entries are accepted. Repeated file/volume identities within a declared
backing chain fail regardless of differing format labels. A backing terminator
records XML syntax only; it is not an image-graph completeness proof.

Opaque storage types require the corresponding `storage-*` unresolved dependency
at the exact source target or `target/backing[N]` location. Unknown storage types
use `storage-unsupported`. External dependency entries remain visible, bounded
to 1,024 unique kind/target pairs; they cannot be silently omitted from the
declared recovery point. Any external dependency or external secret forbids
`independentlyRecoverable=true`, even though each captured output disk must
still declare itself independent. At most 256 unique secret dispositions are
accepted.

These checks contribute declaration and hostile-input prerequisites for
SNAP-01, BAK-01 and SEC-03. The synthetic shared fixture and unit/fuzz tests do
not exercise native capture, backup encryption, source-unavailable restoration,
or firmware/TPM recovery. Those require separate coordinator, integrity, native
and guest evidence. The legacy `Manifest` remains a separate contract.
