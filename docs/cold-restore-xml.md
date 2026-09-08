# Cold restore XML transformation

`xmlpatch.ColdRestore` prepares a new VM definition from preserved captured XML.
Its caller supplies a new canonical lowercase, nonzero UUID, a distinct display
name accepted by `validation.DisplayName`, and an exact disk target-to-staged-file
mapping. The primitive returns no XML on error. It performs no filesystem,
libvirt, guest, secret or privileged operation.

```go
type ColdRestoreDisk struct { Target, Path, Format string }
type ColdRestorePatch struct {
    UUID, Name string
    Disks []ColdRestoreDisk
    NVRAMPath, TPMPath string
    DisconnectNICs bool
}
func ColdRestore(raw string, change ColdRestorePatch) (string, error)
```

The caller must retain the complete original XML separately. A successful
transformation proves neither complete capture nor restored byte provenance.
Before definition, the restore service must verify that every supplied file is
an independently staged raw/qcow2 image with its complete original backing data
flattened, and that auxiliary paths contain the captured state with appropriate
metadata and producer exclusion. Source capture separately refuses unresolved
external dependencies. This primitive does not perform those checks.

## Exact edit boundary

The transformer edits byte spans, using the existing positioned XML parser and
replacement primitives. It changes only:

- The scalar domain UUID and name, requiring both to differ from the original.
  Original and restored names may contain spaces and Unicode, are bounded to
  256 UTF-8 bytes, and reject controls/formatting characters and path separators.
  NFC-equivalent names are treated as aliases; provided name bytes are otherwise
  retained and XML-escaped without silent normalization.
- Every nonempty disk or removable-media source and its explicit driver format.
  Supported sources are local files and simple named pool/volume references.
  A pool/volume reference becomes `type='file'` with its pool/volume attributes
  replaced by the staged file path. Startup policy, quote style and unrelated
  attributes remain intact. An omitted file storage type retains the native
  file default. A missing driver or format gains the explicit staged format.
- Obsolete supported local `backingStore` subtrees, which are removed because
  the supplied destination is required to be independent.
- Existing explicit NVRAM and emulator TPM paths, when matched by corresponding
  supplied auxiliary paths.
- Every direct native `interface` node when `DisconnectNICs` is explicitly true.

Disk targets, bus/controller settings, boot orders, read-only markers, serials,
aliases, driver tuning, comments, metadata, unrelated device configuration and
bound opaque extensions remain byte-for-byte unchanged outside those spans.
Empty CD-ROM/floppy declarations remain unchanged and must not receive a disk
mapping. Existing interface definitions remain unchanged unless removal is
requested. Removing interfaces can leave gaps in device boot-order numbers;
the primitive does not silently renumber other devices.

## Refusal rules

Every nonempty target must map exactly once, with no extra, duplicate or empty
media mappings. Missing data-disk sources, repeated top-level source identities,
unsafe original paths, stale mappings, unknown staged formats and overlapping
destination paths fail. Staged paths must be canonical absolute paths, at most
4,096 bytes, without control/formatting characters; they cannot equal or contain
any observed disk/backing, NVRAM, TPM, firmware-code or template path, or be
contained by one. This is lexical alias refusal: symlinks, mounts, hard links,
pool resolution and file freshness remain the caller's responsibility.

Block/network/LUN layouts, source file-descriptor groups, pool source modes,
structured source policies, disk authentication/encryption, mirrors, transient
or shared disks and unsupported backing layouts fail visibly. The supported
original explicit disk/backing formats are raw and qcow2; an unknown original
format is not inferred from a filename. Missing original format remains
acceptable only because the caller supplies and verifies the staged format.
Unknown unrelated XML is retained, so this transformer alone must never be used
as the external-dependency validator.

NVRAM supports scalar paths or a local file source, preserving raw/qcow2 format,
template, template format and pflash loader security metadata. A pinned pflash
loader is required when NVRAM exists. TPM supports an existing emulator backend
and explicit `source type='file'` or `type='dir'`, preserving version, persistent
state, profile, PCR settings and canonical encryption-secret UUID references.
Implicit auxiliary paths, added/omitted auxiliary mappings, external or
passthrough TPM, varstore and pSeries NVRAM are refused. The primitive never
invents a state path, resolves secret contents, resets TPM/NVRAM, or changes
firmware trust settings.

Malformed XML, DTD/directives, unsupported processing instructions, duplicate
attributes, duplicate/foreign relevant elements, unbound/reserved namespace
misuse and ambiguous identity scalars fail. Input uses the existing 16 MiB XML,
65,536-node and 64-level limits; restore additionally limits disk mappings and
observed disks to 256 and backing traversal to 32 nested levels. There is no
context or hard wall-clock execution guarantee in this pure API. Active
namespace bindings are limited to 256.

## Validation scope

Generated tests cover exact whole-document comparison outside edit spans,
multiple disks and removable media, empty media retention, pool-to-file and
missing-driver edits, quoting/escaping, backing removal, firmware and both TPM
source types, secret/profile preservation, explicit complete NIC removal,
stale/partial/extra mappings, alias refusal, malformed XML/namespaces and unsafe
auxiliary layouts. The full xmlpatch race suite is run for integration with the
existing boot/resource patch primitives. These are SNAP-01, BAK-01 and CORE-04
implementation prerequisites, not native restore or backup qualification. All
71 acceptance scenarios remain required.
