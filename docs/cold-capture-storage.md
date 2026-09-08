# Durable cold-capture set storage

`internal/coldstore` publishes one immutable private artifact set from a validated
`protection.CaptureManifest` and held `io.ReaderAt` sources. It contributes storage
prerequisites to SNAP-01, BAK-01, JOB-02 and SEC-03. It does not capture a VM,
authenticate native observations, flatten a backing chain, encrypt a repository,
restore firmware/TPM identity or prove a guest boot.

```go
type Source struct {
    Member protection.CaptureMember
    Reader io.ReaderAt
}

func Publish(ctx context.Context, root string,
    manifest protection.CaptureManifest, sources []Source) (Receipt, error)
func Inspect(ctx context.Context, root, snapshotID string) (Receipt, error)
```

Both functions return a zero receipt on every error, including close, sync and
cancellation failures. The caller owns and closes the source readers. Storage
uses positional reads and never consumes their shared file offsets or opens a
source path from the manifest. Sources must already have the required authority,
source-generation checks, stopped-state checks and cooperative exclusion held
for the complete operation. A matching digest authenticates bytes against this
declaration; it cannot prove the declaration's provenance or a consistent native
capture.

## Catalog and immutable set format

The caller approves and creates the catalog separately. Its path must be
canonical, absolute, non-root and free of control characters or symlink
components. The process must be an ordinary user, and the held catalog directory
must belong to its effective UID with exact mode 0700. The package never creates
the catalog, changes its permissions or infers approval from a path it finds.
Root processes and changed catalog generation/path bindings are refused.

For snapshot UUID `ID`, publication uses only these new names:

```text
CATALOG/.ID.partial/       private staging; retained on pre-publication failure
CATALOG/ID/                final immutable set after atomic rename
  manifest.json
  receipt.json
  <exact member paths declared by the manifest>
```

Every source must match exactly one manifest member, including its ID, kind,
path, size and SHA-256. Missing, duplicate, extra, nil or mismatched sources fail
before staging. The shared manifest validator enforces canonical identities,
relative paths, member roles, size limits and complete declaration structure.
Storage additionally reserves the top-level names `manifest.json` and
`receipt.json`, including descendants and case aliases, for its own metadata.

Member files start at mode 0600 and directories at 0700. After writing, member
and metadata files become 0400; set and nested directories become 0500. The
catalog remains 0700. Publication never restores write permission, overwrites,
unlinks or cleans up a previous object. These permissions prevent accidental
modification through ordinary writes. The owning user or root can still change
permissions; this is an API immutability contract, not a filesystem immutable
flag or cryptographic defense against the same user.

`manifest.json` is the exact JSON serialization of the validated manifest.
`receipt.json` is exact canonical serialization of this storage-owned shape:

```text
version: 1
snapshotID: manifest.snapshotID
operationID: manifest.operationID
manifestSHA256: SHA-256 of the exact manifest.json bytes
manifest: the complete validated CaptureManifest
```

The JSON receipt is stored inside the final set, so storage inspection needs no
application database. It grants no mutation authority or independent-recovery
claim. The coordinator must separately bind the returned receipt to its durable
operation and native capture evidence.

## Publication and uncertain completion

Publication first refuses any existing final or `.ID.partial` entry, including a
symlink or special file. Staging uses an exclusive new directory. Every member
uses exclusive creation beneath the held staging directory with Linux
`openat2` beneath/no-symlink/no-magic-link/no-cross-mount resolution.

Copying uses at most 256 KiB per chunk and reads exactly the declared size plus
one EOF probe. A zero-byte TPM member still requires exact EOF and the empty
content digest. Short data, extra data, invalid reader counts, I/O errors,
checksum mismatch and cancellation fail. No extra byte is written to storage.
Files are sealed and fsynced individually, followed by metadata files and nested
directories from children to parents. The complete sealed staging tree is
reopened and verified before publication.

The final boundary is `renameat2(RENAME_NOREPLACE)` within the held catalog,
followed by catalog fsync and a complete final inspection. There is no ordinary
rename fallback and no replacement of an existing destination, even if another
writer creates it after the initial check. Publication compares the inspected
manifest digest, operation identity and set directory generation with its
original request. Every successful receipt follows member, metadata, set and
catalog durability checks.

Before rename, failure retains staging and inspection of the final ID refuses
completion. After rename, an error or lost acknowledgement can leave a final set
without a returned receipt. Recovery calls `Inspect` on that ID; it never calls
`Publish` to recopy it. A fresh Publish also refuses existing partial state, even
if the supplied bytes would match. Recovery, retention and removal require
separate coordinator decisions; this package provides no cleanup API.

## Inspection and platform limits

Inspection validates the exact manifest and receipt encoding, IDs, digest and
complete membership. Each directory must contain exactly the expected entries;
even an empty undeclared directory fails. Every file must be a single-link,
ordinary, privately owned 0400 file and every set directory must be 0500.
Symlinks, hard links, FIFOs, devices, changed modes and missing or extra members
are refused. O_PATH type/identity checks precede readable opens, so replacement
special files are never opened for their contents. Readable descriptors refer
only to already-held ordinary inodes through `/proc/self/fd`.

All members stay pinned through verification. After checksum verification,
inspection fsyncs all held files and directories and the catalog, then rechecks
held identities and their current paths. Thus recovery can refresh durability
after a lost final sync acknowledgement without rewriting or repairing data.
Inspection returns no complete receipt while sync, identity or membership checks
fail. Fsync does not itself authenticate content, and these tests cannot certify
a filesystem or device that lies about durable flushes.

The shared manifest bounds remain 256 members, 16 TiB per member and 64 TiB total;
manifest and receipt documents are each bounded to 8 MiB. Member paths retain the
shared depth/length limits. Data is copied densely: free-space reservation,
sparse-image conversion and capacity planning are caller responsibilities.
Descriptor exhaustion and unsupported filesystem capabilities fail visibly.
The reader checks cancellation between bounded operations; an already blocked
filesystem syscall or arbitrary ReaderAt cannot be forcibly canceled without
changing that reader's contract. No abandoned reader goroutine hides this limit.

The implementation requires Linux amd64, openat2, NOREPLACE rename, procfs,
complete statx birth identities and supported directory/file fsync. Other
platforms compile explicit `UNSUPPORTED_CAPABILITY` stubs. Mount/device/ownership
mutation tests need separately authorized capabilities and are not inferred from
temporary-file tests. Rechecks do not provide atomic exclusion against a
malicious same-UID process or root changing the catalog concurrently.

## Executable verification

```sh
./scripts/go test -mod=vendor -race ./internal/coldstore -count=1 -v
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 ./scripts/go build -mod=vendor ./internal/coldstore
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 ./scripts/go build -mod=vendor ./internal/coldstore
```

Tests use generated temporary files and synthetic manifest/source declarations.
They cover positional offset preservation, zero TPM bytes, exact-set inspection,
pre-staging input refusal, partial copy/hash/cancellation failures, every sync and
rename fault boundary, destination collision, lost acknowledgement, malformed
metadata, special-file/path substitutions and changed catalog binding. Child
processes exit without defers after a member fsync and immediately after rename;
the parent proves partial refusal or inspection-only recovery without replay.
These are software/filesystem and process-loss tests, not native VM, encrypted
backup, device power-loss, independent restore or hardware acceptance evidence.
