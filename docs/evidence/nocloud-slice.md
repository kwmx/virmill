# NoCloud seed and creation evidence

Source revision: `d25d493f406605935edec259f3fe0c212c4dd218`.
Source-tree digest:
`60e15e22f53e1ea693427a593d0c7c0bfff9f0dbb7447888c746fa78adec8958`.
This is development evidence. All 71 acceptance scenarios and the complete release
checklist remain unaccepted. Successful seed construction is not guest provisioning.

The shared `vm create` input now accepts explicit typed NoCloud provisioning for
a declared compatible prepared disk source. The selected original source digest,
new VM UUID, new MACs, non-root user/public keys and complete guest NIC intent bind
the seed to this creation plan. Preview builds, reads back and removes a private
seed; execution must reproduce its exact bytes before allocating any managed volume.
The seed is an explicitly mapped read-only, nonbooting CD-ROM and participates in
the existing durable upload/verification/definition and recovery receipt.

The initial declared profile requires Netplan/networkd and disabled IPv6. Rendering
allows one normal IPv4 default route, suppresses protected DHCP routes/DNS and
requests disabled DHCPv6/RA/link-local configuration. It requests fresh SSH host
keys and disables implicit filesystem growth, package upgrades and reboot. These
are generated policy requests; guest routing, forwarding, isolation, image
generalization and cloud-init completion are not verified.

Two implementation failures were found and fixed before this source revision.
The schema validator echoed a rejected private-key value in its formatted error;
creation now returns a value-free schema error, with credential-refusal tests
covering error text, journal metadata and the seed worker. Actual xorriso extraction
restored a read-only root directory and prevented scratch cleanup; the generated
directory is now made writable through an opened no-follow descriptor before
cleanup. See [ADR 0010](../adr/0010-identity-bound-nocloud-seeds.md).

| Evidence | Observed result and boundary |
|---|---|
| [nocloud-core-001](logs/nocloud-core-001.log) | Full Go race suite passes. Synthetic creation tests cover exact seed replay, distinct clone identity, source/tool/cache drift, no allocation on generation failure, joined cancellation, secret refusal and complete-volume definition recovery without generator/cache. Default-sandbox native/confinement/IPC skips remain explicit. Existing schema 1/2 to 3 migration tests still pass; this slice does not change SQLite schema 3. |
| [nocloud-confinement-001](logs/nocloud-confinement-001.log) | Actual confined xorriso creates a deterministic CIDATA ISO and reads back exactly three matching members. The actual generator also runs through the synthetic creation coordinator. Generated QEMU image/ISO preparation, OFD guards, private IPC and two-language plugin confinement pass. Libvirt test-driver XML is simulation; inactive network XML remains explicitly blocked. |
| [nocloud-key-fuzz-001](logs/nocloud-key-fuzz-001.log) | A bounded 15-second, two-worker public-key parser fuzz run completes 397,951 executions without a failure. This is parser evidence, not a complete credential/security assessment. |
| [nocloud-static-portable-001](logs/nocloud-static-portable-001.log) | Vet, common-package Darwin arm64/Windows amd64 cross-builds including the renderer, and SDK race tests pass. Exact xorriso and prlimit package ownership is observed; cloud-init executable is absent. |
| [nocloud-build-001](logs/nocloud-build-001.log) | Two offline builds produce identical three binaries and four unsigned development RPM/DEB packages. Packages now declare xorriso and existing bubblewrap/util-linux worker dependencies. |
| [nocloud-artifacts-001](logs/nocloud-artifacts-001.log) | Three package/private-daemon tests pass. The packaged CLI prepares generated OVA, selected-disk and ISO sources. NoCloud preview runs the actual confined generator and cleans its private stage, then reaches production creation's explicit test-URI refusal without a host connection or mutation. Staged install/uninstall preservation passes. |
| [nocloud-release-gate-001](logs/nocloud-release-gate-001.log) | Expected exit 1: all 71 acceptance scenarios and SHIP-CHECKLIST remain blocked. The failed release result remains in the ledger. |

Observed generator: xorriso `1.5.8.pl02`, Fedora package
`xorriso-1.5.8-2.fc44.x86_64`, executable SHA-256
`47455ea176c7dcc1afb2006e0c774e2d37db7e9be0b88e27383b29eea2c1e22b`.
The fixed three-file fixture produces ISO SHA-256
`e313bc6ce8573b49ed67b64046c96c891f046b08c30275a081d1d8ebc1f4374a`
in two distinct private workspaces. Per-creation seed hashes differ with generated
clone identity, as required. No guest cloud-init version is claimed.

The development CLI reports `0.0.0-dev` and `releaseQualified: false`.
CLI SHA-256:
`5579d36cb93742d27f8b082ca398e18826aff4c49803a52df146f05861ab349d`;
daemon SHA-256:
`dbe35556e152194ad2b87d2ee4bf4b177fc823a93d8d0589ae33d358159488af`.
The build log records all seven binary/package hashes. All seven new ledger
entries bind the revision and source digest above. The complete ledger contains
75 entries, with all 73 recorded log hashes verified; two legacy intake records
retain their original shape. Packages were staged only in temporary roots, never
installed onto the host or published.

Progress contributes to IMP-01/04/05/06/07, NET-02/05, GUEST-01, JOB-01/02/04,
SEC-02/03, UX-01/03 and REL-01/02/03. No KVM guest, native pool upload/refresh,
guest transport, cloud-init runtime, packet isolation, firmware/TPM, USB or
capture/restore was executed. Guest readiness flags remain false. Private seed
stages may survive a daemon crash and need explicit cache disposition; safe
post-provision seed detachment/removal remains required lifecycle work.

Next dependency-ready work includes configuration preservation and installer
console/media-ejection/boot configuration, followed by actual host/guest checks
when an explicitly authorized disposable environment is available. All mandatory
network, protection, device, recipe, plugin and guided-interface work remains in
the [requirements matrix](../requirements-to-evidence.md). No scope reduction or
host/publication authorization is inferred.

Operator guide: [cloud provisioning](../cloud-provisioning.md).
Reproducible fixture recipes: [creation fixtures](../../tests/fixtures/creation/README.md).
