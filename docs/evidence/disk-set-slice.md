# Selected disk-set preparation evidence

Source revision: `8f266f062fe5f9a9f83599cf093137decd1f8e86`.
Source-tree digest:
`5ea4b999c366f089106f6e1632886ec3a0a73b1516873eb154dc969fc6f6c648`.
These are development results. All 71 full acceptance scenarios and the complete
release checklist remain blocked; no MVP or 1.0 completion is claimed.

The shared `import prepare-disks` workflow now selects and pins ordinary source
files, obtains cooperative QEMU OFD read guards, exposes only held selected files
to the confined converter, and publishes a complete independent disk set through
the durable journal. It covers nested relative backing and split extents. Every
root disk passes size inspection, check and content comparison. CLI/TUI access,
source drift, cancellation, failure staging, receipt authority and reopen recovery
are implemented. Approved creation accepts the successful operation with explicit
hardware assumptions. Preparation and cleanup use common source-file path locks;
older deletion plans missing those locks require a fresh preview.

| Evidence | Actual result and scope |
|---|---|
| [disk-set-core-001](logs/disk-set-core-001.log) | Full Go race suite passes. Synthetic coordinator tests cover source mutation, future fields, cancellation, independent receipt authority, SQLite reopen and cleanup contention. Default-sandbox native/IPC/confinement skips are explicit. |
| [disk-set-confinement-001](logs/disk-set-confinement-001.log) | Actual bubblewrap selected-file isolation, QEMU conversion/check/compare, raw/qcow2 OFD writer exclusion in both acquisition orders, private IPC and confined plugin fixtures pass. Native libvirt test-driver observations are simulation. Inactive network XML remains explicitly blocked. |
| [disk-set-static-portable-001](logs/disk-set-static-portable-001.log) | Vet, common-package Darwin arm64/Windows amd64 builds and SDK race test pass. Records exact qemu-io version, package and executable SHA-256. These cross-builds do not provide a non-Linux virtualization backend. |
| [disk-set-build-001](logs/disk-set-build-001.log) | Two offline builds produce identical three binaries and four unsigned development packages. |
| [disk-set-artifacts-001](logs/disk-set-artifacts-001.log) | Three package/staged-preservation/private-daemon tests pass. Actual CLI prepares OVA and selected-disk fixtures, then both reach the native test-URI refusal through approved-source creation validation. No native storage request or VM definition occurs. |
| [disk-set-release-gate-001](logs/disk-set-release-gate-001.log) | Expected exit 1: all 71 scenarios and SHIP-CHECKLIST remain unaccepted. This is a blocked release, not passing release evidence. |

The lock spike exposed an unsafe assumption: plain `qemu-img info` can inspect a
writer-held raw file without `-U`. The recorded regression reproduces that behavior
on a generated file and verifies that the production guard refuses it. The guard
also prevents a later QEMU writer and releases its locks when its independent
descriptor closes. A synthetic noncooperating writer test verifies that final
source drift refuses publication; advisory locks do not constrain arbitrary
programs or root. The request therefore requires an explicit offline-source
assertion. See [ADR 0008](../adr/0008-selected-disk-files-and-offline-guards.md).

The rebuilt `virmill` reports `0.0.0-dev`, source revision above and
`releaseQualified: false`. Its SHA-256 is
`bee1c85a0e46a381e47b2a3d2d3c896c1ce67f4ff9227c05d2f55646b7786646`;
`virmilld` is
`5253b7a693f4b6a2d6aa728a27df4c7b785448cd516e2282aacaa1d8c730f584`.
The build log records all binary/RPM/DEB hashes. Packages were staged into temporary
directories for preservation tests, never installed on the host or published.

No KVM guest, native libvirt storage mutation, host network, USB attachment,
firmware/TPM initialization or capture/restore was executed. There is still no
explicitly authorized disposable guest/host matrix. ISO/cloud/NoCloud creation,
full configuration/lifecycle/forms, guest adaptation, retained-pin release,
protection and other mandatory workflows remain implementation work. The next
creation source work is ISO/cloud provisioning, followed by their actual guest
qualification when an authorized environment is available.

Operator steps are in [existing-disk preparation](../existing-disk-preparation.md)
and [VM creation](../vm-creation.md); generated-file recipes are in
[the fixture documentation](../../tests/fixtures/import/README.md). The current
[requirements-to-evidence matrix](../requirements-to-evidence.md) distinguishes
implementation progress from release acceptance.
