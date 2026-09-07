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

Run file integration on a host supporting ordinary-user bubblewrap namespaces:

```sh
VIRMILL_TEST_DISK_TOOLS=1 ./scripts/go test -count=1 -race \
  -tags libvirt_dlopen -v ./internal/backend/image ./internal/importing
```

The private daemon integration in `tests/integration/artifacts_test.py` generates
a second two-disk raw OVA using Python's standard library and submits the real CLI
preview, apply, result and verify commands. Its marker contents and zero tar
timestamps are reproducible. Build the development binaries/packages first, then:

```sh
VIRMILL_TEST_REQUIRE_IPC=1 VIRMILL_TEST_DISK_TOOLS=1 \
  python3 tests/integration/artifacts_test.py
```

These commands create only temporary files, a private journal/socket and confined
image workers. They do not open a libvirt domain for mutation, boot a guest, change
host networks/services/devices or prove hardware support. Required guest fixtures
and their explicit disposable target remain in the qualification plan.
