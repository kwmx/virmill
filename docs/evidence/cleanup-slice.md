# Failed-creation cleanup evidence

Source revision: `c54b191a62be7b13b188821f7c80e70fe7e944cc`. All checks below used source digest
`c8252deb28125d64fbb2c2ea3095c48e7e2d1bcd450175c8fa073c85a9962b9d`.
This revision is embedded in the current unsigned 0.0.0 development artifacts.
No release or installation on the host was performed.

| Check | Recorded result | Scope |
|---|---|---|
| [cleanup-core-001](logs/cleanup-core-001.log) | passed, with explicit skips | Full Go race suite; synthetic cleanup ordering, retention, partial deletion, cancellation, stale identities/graphs/manifests, old unresolved references, journal reopen, future-record refusal and schema-1/2 migrations |
| [cleanup-confinement-001](logs/cleanup-confinement-001.log) | passed in stated scope | Real generated image metadata/conversion and single-file sandbox probes; private IPC; two-language plugins and simulated provider restart; native test-driver inventory/XML plus installed read-only executable/firmware metadata |
| [cleanup-static-portable-001](logs/cleanup-static-portable-001.log) | passed | Go vet, SDK race tests and common-package/example cross-builds for Darwin arm64 and Windows amd64; no non-Linux hypervisor claim |
| [cleanup-build-001](logs/cleanup-build-001.log) | passed | Two offline builds produce identical three binaries and four unsigned development RPM/DEB packages |
| [cleanup-artifacts-001](logs/cleanup-artifacts-001.log) | passed | Staged archive/installer/uninstaller checks; private daemon and real generated OVA preparation; cleanup rejects an unrelated operation before host backend access |
| [cleanup-release-gate-001](logs/cleanup-release-gate-001.log) | blocked as required | All 71 mandatory acceptance records and the complete checklist still lack full release evidence |

The append-only ledger records exact commands, revision, fixture hashes, environment
reference, enabled test switches and log hashes. Skipped tests in the outer sandbox
are not passing evidence for those paths. The separate explicitly enabled run
covers the listed namespace, image and IPC paths. Its native network test still
reports that libvirt's test driver lacks inactive-network XML; no persistent
network behavior is fabricated.

The filesystem/graph test uses real generated raw/qcow2 files with statx identities
and confined QEMU metadata. Libvirt inventory in that test is its process-local
simulated driver. Explicit fixture removal and simulated inventory deletion do
not exercise the qemu storage driver's native deletion API. The test protects
backing references (including references between selected candidates), unlisted
files, changed metadata and same-path replacements. Coordinator tests use synthetic
volume effects and ordinary fixture bytes. None of these proves actual disk
streaming, native deletion, labels, fsync durability, cross-tool host races, VM
boot, NVRAM/TPM restoration, USB reconnection, routing or isolation.

The [operator workflow](../creation-cleanup.md) and
[ADR 0007](../adr/0007-creation-disposition-and-file-generation.md) describe current
behavior and limits. Active guests, inaccessible/unsupported graph portions,
uninspectable partial images and ambiguous generations block deletion. Explicit
retention pins candidates and closes the original recipe. Complete live storage
graph adapters, retained-pin release, template/base GC, other required creation
entry paths and the rest of the mandatory suite remain work. Actual host effect
qualification still requires an explicitly authorized disposable environment.
