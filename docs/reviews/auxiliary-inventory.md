# Native-bound auxiliary metadata inventory

Implemented and tested with generated ordinary-user files on 2026-09-08. This is
prerequisite work for SNAP-01 and SEC-01. It does not establish a complete capture,
writer exclusion, sealed transfer, durable publication, restore or guest boot.
The privileged boundary follows [specification 10](../../virmill-v1-spec/docs/10-jobs-security-and-recovery.md)
and [ADR 0020](../adr/0020-cold-recovery-boundary.md).

The Linux API in
[`auxiliary_inventory_linux.go`](../../internal/helper/auxiliary_inventory_linux.go)
is `AuxiliaryExecutor{Backend: domain.ColdStateInspector}.Inspect(ctx, request, policy)`.
It returns `AuxiliaryInventory` or the zero inventory and an error. Its private
`rootOwnerUID` test override defaults to zero and cannot be selected through a
request or policy. The production helper supplies the official native backend;
tests supply an explicitly synthetic observation.

The authenticated entry point must first call `Authorize` with the actual kernel
peer credentials and signed grant. The executor itself requires the exact
`state.auxiliary` operation, API versions and `authorizeAuxiliary` structure and
permission checks. Inspect mode cannot provide a source inventory. Capture and
observe modes require the complete expected inventory, but calling this method
still performs metadata observation only. After fresh observation it compares
the entire expected inventory, including the optional `TPMLock`.

Source paths come exclusively from three fresh `InspectColdState` calls for the
authorized UUID on `qemu:///system`. Every result must have the exact resource and
fingerprint, `Persistent=true`, stopped state, no managed save, no autostart, a
matching layout VM UUID, and a non-nil persistent `Source` whose `State` equals
the layout. Native layout changes cause refusal even if the backend returns the
same fingerprint. Freezing the layout cannot repair an invalid UTF-8 string into
a different path.

Only explicitly located NVRAM files and TPM 1.2/2.0 file or directory sources are
observed. Missing or implicit TPM paths are refused; distribution paths are never
invented. Missing NVRAM format remains unknown; explicit formats are limited to
raw and qcow2, and the adapter never opens either format for parsing. Firmware
code/template paths and secret UUID references remain layout metadata, without
reading code, templates or resolving secret values. A declared TPM directory
must contain at least one state member; a zero-size regular TPM member counts.
NVRAM must be nonempty.

Filesystem selection is anchored to the exact `policy.Roots[request.RootID]`.
The root is opened with `O_PATH` and no symlink or magic-link resolution; it must
be administrator-owned without group/other write. Below it, `openat2` also uses
`RESOLVE_BENEATH` and `RESOLVE_NO_XDEV`, including bind-mount crossings. All state
files require the grant's exact UID and GID, no world write and no special mode
bits. Relevant directories may have that state UID/GID or be administrator-owned
without group/other write; world-writable directories are refused. These checks
do not interpret an observed ACL as proof that no external writer exists.

Every regular file, control file and relevant directory is pinned. The inventory
records the approved root, all intermediate directories on the selected native
paths, and all directories encountered in the TPM subtree. It rejects symlinks,
hardlinked files, special files, overlapping source identities, case aliases,
unclean/control-character paths and unsupported control paths. Directory entry
reads use a separately checked read-only directory descriptor with `O_NOATIME`.
Regular state and lock files are never opened for reading bytes.

Metadata uses `statx` with required device/mount/inode/birth identity, ownership,
mode, link count, size, mtime and ctime fields. The generation string also retains
filesystem attribute mask/value. Unsupported or incomplete birth/mount metadata
fails; no inode/path fallback is used. `ACL` and `SELinux` preserve raw xattr bytes
as lowercase hex; empty means `ENODATA`, not an ignored permission or unsupported
filesystem error. Access ACLs must pass the existing canonical Linux ACL parser.
Because `fgetxattr` does not operate on `O_PATH`, xattr observation intentionally
uses only `/proc/self/fd/<held-descriptor>`, with before/after statx checks. It never
substitutes a caller pathname for that held identity.

For a directory TPM source, only its immediate `.lock` is recognized as a
producer control file. It must be a single-link, empty regular file with the
configured state UID/GID. It appears separately as `TPMLock` with ID and kind
`tpm-lock`, outside payload `Members` and `TotalBytes`. Missing `.lock` remains
nil. Nested `.lock`, nonempty `.lock`, or an explicit payload path containing a
`.lock` component is unsupported. Other dot names remain enumerated state
members. Inspection never creates, truncates, opens for content, or acquires the
control lock; a missing lock does not qualify a later capture.

Two independently opened scans must produce identical inventories. Native state
is checked before each scan and after their comparison. Final checks compare all
held objects against newly resolved rooted paths; files precede directories,
with deepest directories first and the root last. Root, directory membership,
member generation and `.lock` replacements therefore invalidate the observation
when detected at these boundaries. All owned descriptors close on success,
failure or cancellation. Repetition does not make observation atomic against
arbitrary writers, and context checks cannot interrupt a blocked kernel or native
library call. A separately qualified producer guard and capture transaction are
still required.

The enforced bounds are:

| Dimension | Bound |
| --- | --- |
| Payload members and declared bytes | Exact administrator caps, at most 128 members and 255 MiB total |
| Relevant directories | 128, excluding the approved root |
| Relative depth and path bytes | 16 components and 4,096 bytes |
| Single TPM directory enumeration | At most configured member cap + 128 directories + one control entry; global counts also checked |
| Access ACL / SELinux xattr | 1,028 / 4,096 raw bytes per object |
| Native layout / complete inventory JSON | 32 / 96 KiB |

Payload members are sorted by relative path and assigned `members/000`,
`members/001`, etc. Directory order is also deterministic. An oversized response
fails instead of returning a truncated set; the 96 KiB inventory leaves room for
the surrounding bounded helper response.

The generated-data suite in
[`auxiliary_inventory_linux_test.go`](../../internal/helper/auxiliary_inventory_linux_test.go)
contains these ten test functions, comprising 116 leaf cases:

- `TestAuxiliaryInventoryMetadataOnlyDeterministicAndPinned`
- `TestAuxiliaryInventoryAuthorityRefusesBeforeNativeObservation`
- `TestAuxiliaryInventoryRequiresFreshExactStoppedPersistentNativeState`
- `TestAuxiliaryInventoryRefusesUnsafeOrIncompleteFilesystemSources`
- `TestAuxiliaryInventoryRefusesChangedRootMembershipAndLockGeneration`
- `TestAuxiliaryInventoryTPMFileMissingLockAndExactBounds`
- `TestAuxiliaryInventoryGlobalCountsAndMetadataEnvelopeBound`
- `TestAuxiliaryInventoryPreservesNativePathsWithoutRepairOrFallback`
- `TestAuxiliaryInventoryAccessMetadataAndXattrBounds`
- `TestAuxiliaryInventoryCancellationAndFailureCloseOwnDescriptors`

They exercise unreadable NVRAM with unchanged timestamps, zero-size TPM state,
explicit file and directory sources, missing lock, exact expected-lock binding,
invalid authority before native calls, native refusal at all three observation
points, metadata and set substitutions at middle/final observations, count and
envelope bounds, actual ordinary-file ACL and existing SELinux metadata reads,
and descriptor cleanup across repeated cancellation. The ACL fixture uses a
mapped current-user named entry, so no unrelated user's files or permissions are
involved. Mount-crossing and special-device branches are enforced in code but
were not exercised through privileged mount/device creation.

Validation used `./scripts/go`, `GOPROXY=off`, `GOSUMDB=off` and vendor mode:

- Focused inventory suite: passed, 0.136 s.
- `go test -mod=vendor -race ./internal/helper ./internal/backend/fileaccess ./internal/backend/fileidentity -count=1`:
  passed, 2.043 / 1.021 / 1.009 s. Every new inventory case ran without a skip.
  Existing private-IPC cases skip in the outer sandbox; existing privileged ACL
  and root-access integration fixtures remain opt-in skipped. No escalation was
  attempted and those skips are not new IPC or privileged evidence.
- `go vet -mod=vendor` for those three packages: passed.
- Final targeted race checks after strengthening existing-label equality and
  first-scan failure cleanup assertions: passed, 1.017 and 1.099 s.
- `FuzzAuxiliaryNativeRelativePath`, two workers for three seconds: passed 41,708
  executions, with eight initial seeds. This fuzzer performs no filesystem I/O.
- Linux arm64, `CGO_ENABLED=0`, helper package test binary: compiled, not executed.

No native guest/backend call, privileged host change, SSH, confidential capture,
ledger edit or acceptance promotion was performed by this assignment.
