# Generated import fixture recipes

No guest media or proprietary appliance is redistributed. The Go test sources
are executable recipes and are hashed into each evidence entry.

`internal/backend/image/tool_linux_test.go` creates an 8 MiB sparse raw file with
a fixed marker at offset 4096. The installed qemu-img generates each supported
container format. The production confined adapter inspects, converts to qcow2,
checks structure and compares virtual content. Separate fixtures generate split
flat VMDK extents, remove an extent, substitute an outside path, and create a
relative qcow2 backing chain with explicit format. Missing dependencies must fail.

`internal/importing/stage_linux_test.go` creates two distinct 8 MiB marker disks,
converts each into a split sparse VMDK, and packs the descriptors plus every extent
with an OVF selecting controller positions 0 and 1. The actual preparation operation
converts both disks and records archive/output SHA-256 hashes in test output.
Generated VMDK content IDs may vary; evidence records each observed fixture hash.

Other tests in that file deliberately use a synthetic phase adapter for source
drift, conversion failure, cancellation and lost-acknowledgement recovery. Those
tests validate coordinator behavior only, and their bytes are not valid qcow2.
There is no runtime fixture provider in the application.

`internal/backend/image/files_linux_test.go` passes explicitly selected held files
through the real descriptor sandbox: nested relative qcow2 backing, split VMDK
extents, missing selection and incorrect declared format. The raw-writer fixture
demonstrates that an unguarded QEMU metadata probe can succeed on a writable source;
the production OFD guard must refuse it. Raw/qcow2 tests also attempt a writer
after taking the guard and verify release after its independent descriptor closes.
These use `qemu-io 10.2.2 (qemu-10.2.2-1.fc44)` from the recorded qemu-img package.
No VM process is created. Namespace fixtures additionally check read-only selected
files, hidden siblings/original directory/sockets and closed inherited descriptors.

`internal/importing/disks_linux_test.go` publishes a two-root disk set with a
nested qcow2 backing file and split VMDK through the production conversion adapter.
It hashes originals, verifies the new receipt, then removes only its generated
source fixture and verifies that the published copies remain independent. Separate
synthetic phase tests cover source drift (including restored mtime/content),
directory/symlink substitution, failure, safe cancellation, and actual SQLite
reopen after lost publication acknowledgement. Their synthetic output bytes do
not validate an image format. Source-file locks and old cleanup-plan refusal have
coordinator tests in `internal/creating/cleanup_linux_test.go`.

Run file integration on a host supporting ordinary-user bubblewrap namespaces:

```sh
VIRMILL_TEST_DISK_TOOLS=1 ./scripts/go test -count=1 -race \
  -tags libvirt_dlopen -v ./internal/backend/image ./internal/importing ./internal/platform/linux
```

The private daemon integration in `tests/integration/artifacts_test.py` generates
a second two-disk raw OVA using Python's standard library and submits the real CLI
preview, apply, result and verify commands. Its marker contents and zero tar
timestamps are reproducible.

The same private daemon test now prepares a selected raw/qcow2 set with a relative
backing file through CLI plan/apply/result/verify. Both preparation kinds reach the
real creation service's approved-source validation and then the intentional native
`test:///default` refusal. This confirms dispatch and authority without entering
a host libvirt connection, uploading storage or defining a guest.

Build the development binaries/packages first, then run:

```sh
VIRMILL_TEST_REQUIRE_IPC=1 VIRMILL_TEST_DISK_TOOLS=1 \
  python3 tests/integration/artifacts_test.py
```

These commands create only temporary files, a private journal/socket and confined
image workers. They do not open a libvirt domain for mutation, boot a guest, change
host networks/services/devices or prove hardware support. Required guest fixtures
and their explicit disposable target remain in the qualification plan.

## ISO preparation and empty disks

`TestRealISOAndConfinedBlankDiskPreparation` creates a temporary ordinary directory
with a Virmill README, then invokes the exact installed `/usr/bin/genisoimage`
1.1.11 (package `genisoimage-1.1.11-63.fc44.x86_64`) with
`-quiet -V VIRMILL_TEST -o TEMP/fixture.iso TEMP/source`.
The executable SHA-256 is pinned in `contracts/dependencies.lock.json`.
This recipe creates nonbootable ISO9660 media; timestamps make its bytes specific
to each run, so the test logs the actual media digest rather than promising an
invented stable image hash. No proprietary media is downloaded or redistributed.

The actual preparation service copies and verifies that ISO and invokes confined
QEMU 10.2.2 to create 8 MiB and 16 MiB empty qcow2 disks, checking format/capacity,
backing independence, consistency and complete zero maps. The image adapter test
also compares generated blank disks (including a non-cluster-aligned 512-byte size)
against independent temporary sparse raw zero files. Synthetic phase fixtures have
only recognition headers and deliberately non-image worker bytes; they prove
journal semantics only. Native libvirt test-driver fixtures check read-only CD-ROM
XML/boot mapping, never real pool streaming or guest installation.

The staged package integration test additionally prepares an ISO through the real
CLI/private daemon, verifies the approved receipt and reaches production creation's
explicit test-URI refusal. It never contacts a host libvirt connection.
