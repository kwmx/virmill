# Creation qualification fixtures

`internal/creating/*_test.go` creates two ordinary temporary files and a synthetic
typed volume backend. These exercise coordinator ordering, independent fixture
copies, stale mappings, cancellation, lost acknowledgements, recovery lock transfer
and managed catalog identity. They do not contain bootable qcow2 data and do not
exercise the native storage driver or create VMs on the host.

`internal/backend/libvirt/creation_linux_test.go` generates BIOS and UEFI/TPM domain
XML and uses libvirt's `test:///default` driver with `DOMAIN_DEFINE_VALIDATE`, then
checks the inactive XML round trip. That driver simulates domains entirely within
the test connection. The runtime Virmill provider refuses test-driver URIs.
No QEMU process, guest, real NVRAM, TPM state or network is created by this test.
Another native test allocates a volume only within that simulated driver's memory
and checks mismatched-connection and read-only data-access refusals. It does not
write a file or exercise a successful storage stream.

`VIRMILL_TEST_DISK_TOOLS=1` enables the installed QEMU device-help metadata test.
Its fixed `-machine none -device MODEL,help` queries exit before machine
initialization. The root-owned executable is hashed. Outer user namespaces that
remap root to nobody correctly fail its ownership preflight; run this read-only
metadata check in the ordinary host namespace when authorized by the test runner.
The same check reads the observed installed OVMF code/template/descriptor files
and records their hashes for secure/nonsecure profiles. It never initializes
firmware state, proves enrolled keys in a running guest, or creates TPM state.

The [import fixture recipe](../import/README.md) creates real untrusted-input test
images for confined preparation, but those disks are not guest boot fixtures.
The example creation JSON assumes two original NICs; the synthetic preparation
OVF has no NICs, so its correct mapping uses `nics: []` or explicit additional
adapters with `sourceIndex: -1`.

Native qualification still requires an owner-designated disposable connection,
active file pool, network UUIDs, complete legitimate guest media and exact source
hashes. Record native allocation/upload/finish/refresh/download, each injected
crash/cancel boundary, permissions/security labels, unchanged originals, new VM
XML, all guest disks, boot order and each NIC separately. Capture guest boot,
guest setup and connectivity as separate evidence. UEFI/Secure Boot/TPM needs
actual enrollment, persistent state and independent recovery checks; definition
XML and metadata probes cannot establish those results. Keep routing/isolation
packet tests separate from network model and source-name checks.
