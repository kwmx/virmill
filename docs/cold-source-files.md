# Guarded cold source files

`internal/backend/coldfiles` is a Linux/amd64 filesystem primitive contributing
to SNAP-01, BAK-01, SEC-01 and SEC-05 prerequisites. It does not authorize a root,
map VM artifacts, establish VM shutdown, expose a helper operation or establish
a successful capture. Callers must supply those checks and an exact previously
observed `fileidentity.Identity`.

## Open and identity boundary

`Open(ctx, absoluteRoot, canonicalRelativePath, expected)` requires canonical
paths and a complete expected single-link ordinary-file identity. Empty files
are allowed when their complete expected identity explicitly describes them.
Zero or partially omitted expected identities never request a fresh observation.

The root is pinned with `openat2`, `O_PATH`, `O_DIRECTORY` and `O_CLOEXEC`, with
all symlink and magic-link components refused. The selected member is resolved
relative to that root with `RESOLVE_BENEATH`, `RESOLVE_NO_SYMLINKS`,
`RESOLVE_NO_MAGICLINKS` and `RESOLVE_NO_XDEV`. The last flag rejects mount crossings
below the chosen root, including bind mounts; the approved root itself may be a
mount point. There is no fallback if the required kernel behavior is unavailable.
These resolution controls follow the [Linux openat2 contract](https://man7.org/linux/man-pages/man2/openat2.2.html).

The member is initially opened with `O_PATH` and checked by `fileidentity` before
any readable open. A FIFO, device, directory, hard link or different expected
generation/metadata is rejected at this point. After validation, a deliberate
`/proc/self/fd/<held-pin>` reopen obtains a read-only, nonblocking, close-on-exec
descriptor for the held ordinary inode. This generated descriptor path is the
only initial byte-open path; user paths are never followed again for bytes.
Missing proc access fails the operation. The descriptor is checked again, then
passed to the existing `image.AcquireReadGuard`; the QEMU permission-byte protocol
is not reimplemented here. A final recheck is required before `Open` returns.

The source retains the root, inode pin, readable descriptor and independent
guard. `Identity()` returns the immutable expected identity even after close.
`Recheck(ctx)` compares every held file's complete identity and the member's
current `O_PATH` binding beneath the held root. It also compares both the held
root and its current absolute-path binding with the complete initial root
identity. An unrelated sibling creation or other directory metadata change can
therefore invalidate this deliberately conservative capture boundary.

## Copying and transfer

`ReadAt` reads only the guarded descriptor and preserves ordinary `io.ReaderAt`
offset/EOF behavior. Replacing the selected pathname cannot redirect its bytes.
It does not implicitly recheck paths on every read. Callers must check
cancellation between bounded copy chunks, call `Recheck` before and after the
copy, verify expected sizes and hashes, and recheck before committing a capture.
A failed check invalidates the candidate capture. A regular-file kernel read
already in progress cannot be canceled through this API; it does not spawn a
background reader or promise a hard storage-I/O deadline.

`DupForTransfer` rechecks first, then uses `F_DUPFD_CLOEXEC` on the **guarded**
read-only descriptor. It does not reopen an unguarded file description. The
returned descriptor shares the guard's open file description, file offset and
OFD locks. Its access mode remains read-only. The receiving code owns it and must
close it on every success, failure and cancellation path. If sent with SCM_RIGHTS,
the receiver must separately use `MSG_CMSG_CLOEXEC` when receiving: sender-side
close-on-exec is not a promise about the receiving process's descriptor flags.
See the [Linux recvmsg flags](https://man7.org/linux/man-pages/man2/recvmsg.2.html).
The duplication and lock lifetimes follow
[F_DUPFD_CLOEXEC](https://man7.org/linux/man-pages/man2/F_DUPFD.2const.html) and
[Linux OFD locking](https://man7.org/linux/man-pages/man2/F_GETLK.2const.html).

`Close` is concurrent-safe and idempotent. It closes the source's own references
and reports close failures; it never explicitly unlocks the shared OFD. A
transferred duplicate therefore retains the guard until its last receiver
reference closes. Reads, rechecks and new transfers after close return visible
`os.ErrClosed` errors. Close waits for this source's active operations; it does
not revoke an already transferred descriptor.

The existing guard is cooperative QEMU locking. It does not stop arbitrary
noncooperative writers, tools using `locking=off`, or a descriptor holder that
deliberately changes the shared locks. Caller authorization, approved-root
ownership and VM/emulator shutdown checks remain necessary. Read-only descriptors
do not replace receiver confinement, narrowly scoped transfer authorization or
the complete artifact inventory.

## Local verification and remaining evidence

Run the focused tests with the pinned toolchain and vendored dependencies:

```sh
./scripts/go test -mod=vendor -race ./internal/backend/coldfiles -count=1 -v
./scripts/go vet -mod=vendor ./internal/backend/coldfiles
```

The tests create temporary files and FIFOs, exercise actual `openat2`, `statx`,
inotify and OFD operations, and verify expected-identity refusal, path/root drift,
read-only descriptor transfer, concurrent/idempotent close and lock lifetime.
The FIFO test observes no `IN_OPEN` event for a refused source and confirms that
a deliberate nonblocking readable open does produce an event. A whole-file
kernel lock contender verifies transferred lock retention without duplicating
QEMU's permission-byte protocol. Descriptor observations check cleanup after a
failed open and normal close.

No device nodes or mount points are created, no host devices are opened, and no
VM or QEMU tool runs in this test package. Device-node substitution and actual
mount-crossing exercises remain pending a separately authorized capability
fixture. These generated-file kernel tests do not qualify native backup/restore,
firmware/TPM consistency, helper authorization or any complete acceptance scenario.

## Opt-in QEMU software/file qualification

The preceding no-QEMU statement describes the default, ungated tests. The
separate `TestRealQEMUSourceTransferRetainsReadGuard` test runs only when
`VIRMILL_TEST_DISK_TOOLS=1`:

```sh
VIRMILL_TEST_DISK_TOOLS=1 ./scripts/go test -mod=vendor -race \
  ./internal/backend/coldfiles \
  -run '^TestRealQEMUSourceTransferRetainsReadGuard$' -count=1 -v
```

It invokes only installed `/usr/bin/qemu-img` and `/usr/bin/qemu-io`, through
argument arrays, on fresh `t.TempDir` raw and qcow2 files with 1 MiB virtual
capacity. Each command has a five-second context deadline and a bounded process
wait. Verbose output includes both tools' complete `--version` output. An enabled
run fails if a tool is missing, denied, times out or fails its success controls;
there is no installation, escalation or fallback.

For each format, a writable `qemu-io -f <format> -c info <generated-file>` open
must succeed before the guard. The test then observes the source identity, calls
`coldfiles.Open` and `DupForTransfer`, and requires QEMU's specific
`Failed to get "write" lock` diagnostic plus its matching image-path hint. This
refusal must persist after `Source.Close` leaves only the transferred descriptor.
After the last transferred descriptor closes, the same QEMU command must succeed.
An unrelated subprocess failure never counts as lock-refusal evidence. The
diagnostic comes from QEMU's
[file permission lock checks](https://raw.githubusercontent.com/qemu/qemu/master/block/file-posix.c).

The gated race test passed locally on 2026-09-07 for both raw and qcow2, with no
skips, using `qemu-img version 10.2.2 (qemu-10.2.2-1.fc44)` and
`qemu-io version 10.2.2 (qemu-10.2.2-1.fc44)`. This qualifies the Source/transfer
lock lifetime against those native software tools and generated files. It adds
SNAP-01/BAK-01 prerequisite evidence only; no VM, hardware, firmware/TPM, namespace,
IPC, mount, device or full recovery qualification is performed by this test.
