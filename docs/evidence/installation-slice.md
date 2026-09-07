# Installation artifact and read-only media evidence

Source revision: `1f0bf96ebb10b41e7e52ec63631b699bec2f53f0`.
Source-tree digest:
`8a0277155377fa1eaef16bf752efb91c77e28ba79daf92cb6985474306b955ce`.
This is development evidence. All 71 full acceptance scenarios and the release
checklist remain unaccepted; this slice does not establish an MVP or 1.0 release.

The shared `import prepare-install` service now copies one selected ISO and creates
all explicitly requested blank disks. It validates source identity/change time,
digest, cooperative writer exclusion, publication parent, tool and free space.
The output distinguishes raw read-only media from empty qcow2 disks and records
bounded volume recognition separately from guest readiness. Publication uses a
private durable receipt before no-replace rename. Cancellation joins workers;
uncertainty keeps staging and locks; journal reopen reconciliation never repeats
copy or disk creation. CLI/TUI access and operator examples are included.

Creation maps every medium, disk and boot candidate explicitly, positively checks
CD-ROM/bus capability and includes media in allocation/upload/readback receipts.
The XML uses a raw read-only SATA/SCSI CD-ROM. Definition waits for every volume.
A media transfer failure leaves a partial set and refuses definition recovery;
a fully verified set can use reviewed recovery without reallocating or reuploading.
Documented native ISO storage metadata is handled as raw media while preserving
its original graph proof. See [ADR 0009](../adr/0009-installation-artifacts-and-readonly-media.md).

| Evidence | Observed result and boundary |
|---|---|
| [installation-core-001](logs/installation-core-001.log) | Full Go race suite passes. Synthetic tests cover source drift, strict schemas, phase failures, cancellation, media mapping, journal reopen, incomplete media refusal and complete-set recovery. Default-sandbox hardware/confinement/IPC limitations remain explicit. |
| [installation-confinement-001](logs/installation-confinement-001.log) | Actual bubblewrap/QEMU generated-file create/check/zero-map and independent zero-file comparison pass, including 512-byte non-cluster-aligned capacity. Actual generated nonbootable ISO copy and two-disk preparation pass. Existing conversion, OFD locks, selected-file isolation, private IPC and two-language plugin confinement pass. Native libvirt test-driver XML/cleanup inventory is simulation; inactive network XML is still explicitly blocked. |
| [installation-static-portable-001](logs/installation-static-portable-001.log) | Vet, common-package Darwin arm64/Windows amd64 cross-builds and SDK race test pass. Exact genisoimage 1.1.11 package/executable hash recorded; no additional runtime dependency or non-Linux virtualization support is claimed. |
| [installation-build-001](logs/installation-build-001.log) | Two offline builds produce identical three binaries and four unsigned development RPM/DEB packages. |
| [installation-artifacts-001](logs/installation-artifacts-001.log) | Three staged package/private-daemon integration tests pass. Actual CLI prepares OVA, selected-disk and ISO artifacts, then source validation reaches production creation's explicit test-URI refusal. No host libvirt connection, volume request or guest execution occurs. |
| [installation-release-gate-001](logs/installation-release-gate-001.log) | Expected exit 1: all 71 acceptance scenarios and SHIP-CHECKLIST remain blocked. This failed release check is retained as evidence, not promoted to a passing result. |

The development CLI reports `0.0.0-dev`, the source revision above and
`releaseQualified: false`. CLI SHA-256:
`402c21143eab1471c247204f26978b24a386b5ec7fe2bc8bf55227273de7c3ef`;
daemon SHA-256:
`b48049ce364e1d7b7e36e6ab71605aa9588ef10fad172ab70a14da7299772006`.
The build log records all binary/package hashes. Packages were installed and
uninstalled only into temporary staging roots, never onto the host or published.
All six entries bind the exact source revision/digest. The complete ledger has
68 entries and all 66 recorded log hashes verified; two legacy intake records
retain their original shape.

Progress contributes to IMP-01/04/05/06/07, JOB-01/02/04, UX-01/03 and REL-01/02/03;
none of those full scenarios is accepted by these partial tests. No KVM guest,
actual pool allocation/upload/refresh, firmware/TPM initialization, ISO installer,
USB device, host networking or capture/restore ran. Generated nonbootable media
must not be presented as a real guest installation fixture.

The next dependency-ready creation work is cloud/NoCloud provisioning and the
installer console/media-ejection/boot-configuration lifecycle, followed by actual
host/guest qualification when an explicitly authorized disposable environment is
provided. Other mandatory configuration, networking, USB, protection, plugin,
retained-reference disposition and polished guided TUI workflows remain in the
requirements matrix. No scope reduction or host/publication approval is inferred.

Operator steps: [ISO preparation](../installation-preparation.md),
[VM creation](../vm-creation.md). Reproducible generated-file recipes:
[fixture documentation](../../tests/fixtures/import/README.md).
Current status: [requirements-to-evidence matrix](../requirements-to-evidence.md).
