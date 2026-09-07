# Recovery manifest verification

`backup verify-manifest` accepts the unchanged legacy declaration below and the
versioned [`ColdRecoveryPoint` declaration](cold-capture-manifest.md). An explicit
`kind` selects the versioned contract; an unknown kind or version fails without
falling back. Required fields cannot be inferred from missing booleans or null
arrays. The versioned response also names `manifestKind` and `manifestVersion`.
Both formats retain the same limits on what an integrity check can prove.

The legacy `protection.Manifest` verifier checks the declaration and the bytes of
**every declared member**. It does not establish that the manifest describes a
complete VM capture, that disks were captured at one coordinated instant, or that
the resulting VM can be restored or booted.

The JSON fields are unchanged. The `requiredMembers` list, `requiresNVRAM`,
`requiresTPM`, consistency and independent-recovery fields remain declarations
made by the document. Comparing that list to `members` proves only internal
agreement. Omitting a disk from both lists is not detectable by this legacy
format. This verifier does not derive a complete inventory from arbitrary XML,
inspect disk backing chains, verify encryption/repository durability, establish
guest shutdown or quiesce hooks, or obtain missing secrets. A member labeled
`persistent-xml` is integrity-checked as an artifact; that label does not certify
its contents or completeness.

## Entry points

Inspect a declaration, then optionally verify every declared member:

```sh
virmill backup verify-manifest /absolute/path/to/manifest.json --output json
virmill backup verify-manifest /absolute/path/to/manifest.json \
  --input '{"root":"/absolute/path/to/artifacts"}' --output json
```

In the TUI, open **Protection → backup verify-manifest** and enter
`{"path":"/absolute/path/to/manifest.json","input":{"root":"/absolute/path/to/artifacts"}}`.
Omit `input` for declaration-only checks. Both interfaces resolve canonical
relative paths against the terminal's working directory before contacting the
same shared service. Unknown parameters and non-string/empty roots are errors.

Successful responses report `verification: "manifest-checked"` and whether
`membersChecked` is true. `completeCaptureVerified`, `independentRecoveryVerified`
and `bootTested` remain false. A failed or canceled check returns no successful
verification payload. Verification is read-only and creates no durable job.

- `ReadManifestContext(ctx, path)` reads a stable, nonempty ordinary file of at
  most 8 MiB, applies strict JSON decoding, and validates the legacy declaration.
  Duplicate keys, escaped duplicate keys, unknown fields, invalid UTF-8,
  malformed/trailing JSON and unsupported declarations fail. An error returns
  the zero manifest, never a partially decoded document.
- `Manifest.Validate()` checks declaration structure and consistency rules. It
  does not open any member files.
- `Manifest.VerifyContext(ctx, root)` validates the declaration, checks all
  declared member bytes beneath the selected directory and rejects observed
  changes during verification. It returns only an error or successful completion
  of those checks; it creates no reservation, grant, capture or recovery job.
- `Manifest.Verify(root)` remains as a compatibility wrapper using
  `context.Background()`. Service callers should use `VerifyContext`.

Paths supplied for the manifest file or artifact root must be canonical absolute
or relative paths. Path forms that would hide components during lexical cleanup,
such as `alias/../manifest.json`, are rejected. Symbolic links in any component
are refused, including the root path. Select the actual directory/file path.

## Declaration limits

| Field | Rule |
|---|---|
| API | Exactly `virmill/v1` |
| Identities/references | Nonempty valid UTF-8, at most 256 bytes, no control/format characters or surrounding whitespace |
| Members/required IDs | At most 256 each; unique IDs, matching declared inventory |
| External secret references | At most 256, unique and valid |
| Member kinds | `disk`, `persistent-xml`, `live-xml`, `nvram`, `tpm`, `secret` |
| Member size | Positive and at most 16 TiB |
| Aggregate member size | At most 64 TiB, using overflow-safe arithmetic |
| Digest | Exactly 64 lowercase hexadecimal characters representing SHA-256 |
| Member paths | Canonical relative files under the existing archive path policy; at most 1,024 bytes and fewer than 32 separators |

Paths may not be duplicated under the existing lowercase comparison, and one
member cannot also be another member's parent directory. Directory entries and
zero-length lock/socket placeholders are not recovery members.

`cold` requires the `cold-complete` consistency declaration. `live-disk` accepts
`crash-consistent`, `filesystem-quiesced` or `application-quiesced`; these are
observed-consistency vocabulary, distinct from backup policy request vocabulary.
Acceptance of the word does not verify that the observation actually occurred.

There must be at least one disk, exactly one persistent XML artifact and at most
one live XML artifact. Required NVRAM needs exactly one NVRAM member; required TPM
needs at least one TPM member. Auxiliary members contradicting a false requirement
flag are rejected. A live-disk declaration with required auxiliary state may not
claim independent recovery. External secret references also prohibit that claim.

## Linux file boundary

The implementation reuses the repository's `fileidentity` adapter. It holds the
root directory and opens members with `openat2`, preventing path escape, symbolic
links, magic links and traversal into mounted subtrees beneath the root. The
selected root itself may be a mounted backup filesystem. An `O_PATH` descriptor
is first checked as an ordinary single-link file, before any readable open. The
verified held inode is then reopened through `/proc/self/fd`, with `O_NONBLOCK`
and `O_NOCTTY`, and its identity is checked again. This refuses a substituted
special file before opening it for bytes. Missing proc access fails closed.
Symlinks, hard links, FIFOs, sockets, directories
and devices are not accepted as members or manifest documents.

All declared member descriptors are held before any member is hashed and remain
held until the complete set has been checked. Filesystem identity includes device,
inode and birth time; relevant metadata includes size, mode, link count, modification
time and change time. Files are read in chunks of at most 256 KiB, consuming no
more than the declared size plus one byte. The actual size and SHA-256 must match.
At the end, the held identities/metadata are checked again, each declared path
must still resolve to the held object, and the selected root must still identify
the same unchanged directory. These checks detect observed replacement, growth,
truncation, same-size writes, hard-link additions and permission changes.

The read-only document path uses the same opening and identity checks, with the
separate 8 MiB limit. No shell, helper, privileged action or host mutation is
involved. There is no weaker fallback when Linux path-resolution protection or
required filesystem identity fields are unavailable. Other OS builds compile and
return `UNSUPPORTED_CAPABILITY` for runtime file reading/verification; declaration
validation remains portable.

Context cancellation is checked before filesystem access, between bounded reads,
after each read and throughout final verification. Cancellation discards the
result. A kernel read or metadata operation stalled in the storage stack cannot
be forcibly interrupted by these context checks; the implementation does not
spawn abandoned reader goroutines to disguise that limit.

## Evidence and remaining scope

The local tests reproduce the previous acceptance of a hardlinked artifact and
show its refusal after the change. Subprocess tests put a FIFO at a previously
regular member path and at the document path; both must fail before a three-second
deadline without a writer. Tests also cover ordinary nested files, all declared
members, corruption/missing artifacts, strict JSON, bounded sizes/counts,
cancellation, root replacement and final identity checks after substitution or
same-size writes with restored modification time.

These are local filesystem/unit security checks contributing to BAK-01, BAK-04
and SNAP-03, with read-boundary relevance to SEC-01/SEC-05. They do not satisfy
those acceptance scenarios by themselves. No encrypted-repository test, independent
restore, retention/pruning check, VM stop/capture, packet test, hardware qualification
or grant/privileged-helper integration test is established by this verifier.

Holding and rechecking files is not an atomic filesystem or hardware snapshot.
Files can change immediately after the last observation. A complete capture and
restore workflow needs its own authoritative inventory, stopped-state/quiesce
evidence, publication rules and recovery tests. This verifier must never turn
self-declared `independentlyRecoverable: true` or `cold-complete` into a verified
capture/recovery/boot claim.

The Linux syscall boundary follows the
[Linux `openat2` documentation](https://man7.org/linux/man-pages/man2/openat2.2.html)
and the [`open` nonblocking behavior](https://man7.org/linux/man-pages/man2/open.2.html).
