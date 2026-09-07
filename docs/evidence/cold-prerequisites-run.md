# Cold recovery prerequisites, revision 24858d4

The three agent assignments were integrated under one owner. The XML agent
delivered strict native firmware/TPM extraction, then adversarial sealed-FD tests.
The manifest agent delivered held-file verification, then shared-service/CLI/TUI
regressions and a read-only creation review. The fixture agent delivered an
original reproducible EFI/TPM marker probe and its compiler regression. The parent
owns the neutral contracts, service/UI integration, architecture, ledger and all
disposable-host operations. All 71 release scenarios remain required and unaccepted.

The immutable source is `24858d4`, with source digest
`22f2e5391a9dc725848f48219c2bf997b6b371a99b4e43c3e1940967830a6d8f`.
Tests/builds run from `build/cold-24858d4`, an archive of that commit using the
pinned offline Go toolchain and vendor sources. The ledger records command logs.
[Local tool observations](environments/local-cold-24858d4.json) supplement the
initial native-package inventory: parent `python3` resolves to Homebrew 3.14.6,
while `/usr/bin/python3` is Fedora 3.14.7. No dependency version was inferred from
the package name. The probe build manifests record the actual interpreter used.

The SDK evidence initially used the invalid `PLG-01` label. Its append-only
correction maps that executed check to `EXT-01`; the original record is retained.
This bookkeeping correction does not establish full extension acceptance.

## What executed locally

- Full sandboxed Go race tests, vet, portable builds and Go SDK tests passed.
- Four XML fixtures passed installed libvirt 12.0 domain-schema validation.
- Cold-layout fuzzing passed 64,926 executions; manifest fuzzing passed 85,953
  executions. These are parser tests, not hardware tests.
- Two clean builds produced identical three binaries and four unsigned
  `0.0.0-dev` RPM/DEB packages. Staged installation/uninstallation preserved a
  synthetic owner artifact; the private daemon integration test was skipped.
- Two clean EFI probe builds and synthetic TPM wire checks passed. The 9,728-byte
  executable SHA-256 is
  `b50a5ef0a95feb2ee6824b1d5f353b41a4b2ed189c6974c76293592fa3109b33`;
  the 33,554,432-byte FAT image SHA-256 is
  `1945000ea87359d92f6fcb3413549fe477884012d631c66a058c71a1dda9e483`.
  The clean parent build manifest hash is
  `275abd707259b46a31d24fd7e5614b5e655319b7f6815e7a45a240b99a0f82ac`.

The initial EFI build was reproducible but contained an unsafe compiler/linker
call convention: `-fno-plt` generated ELF GOT calls not correctly represented by
the PE link. Review found this before guest execution. The corrected recipe
rejects GOT relocations and RIP-relative indirect branches; a freshly compiled
old-flag object fails the executable regression. Fixed, bounded COM1 mirroring
starts only after the fixture's QEMU/KVM guest guard. No EFI binary was run locally.

Agent transport testing found three defects: cancellation after the final source
read could return success, multiline metadata could violate framing, and a sealed
write-only descriptor could be accepted. Parent fixes address all three. The agent
reported required-IPC tests and 25 race repetitions passing, with red regressions
confirmed using a temporary overlay of the prior checks. Final clean-source
required-IPC verification remains pending: automatic approval review rejected
that command before execution because its review service reported a usage limit.
Sandboxed tests visibly skip descriptor IPC when `getsockopt` is denied. They
must not be substituted for the blocked IPC run.

## What executed on the disposable VM

The installed Virmill runtime remains `def0b10`, not `24858d4`. Read-only preflight
confirmed the installed binary hashes, 24 terminal jobs, zero resource locks,
stopped helper and all four existing guests stopped with their expected XML hashes.
OVMF descriptors, TPM/KVM capabilities and exact compiler/firmware/swtpm packages
were observed. The first capability query failed on libvirt's read-only-handle
restriction; the corrected pure query followed ADR 0012. A later preflight SQL
column mistake also failed and was corrected. Both failures remain in the ledger.

The parent created only the fresh `virmill-cold-probe-v1` directory pool, UUID
`95b94843-0db0-46ac-9bbf-a2fa3c183818`, with autostart disabled. Its setup is an
external disposable-fixture operation, not proof of Virmill pool provisioning.
No UEFI/TPM guest was defined, started, captured or restored. Source media and
existing guest disks were not targets of any mutation in this increment. Further
SSH work awaits review-service availability; the owner's authorization persists.

## Claim limits and next native boundary

`vm recovery inspect` observes configuration through the shared CLI/TUI service.
`backup verify-manifest` verifies declared metadata and optional member bytes;
complete capture, independent recovery and boot flags remain false. The sealed-FD
utility is not connected to an authorized helper capture method. Typed source
resolution, grants, complete publication, encrypted repositories and independent
restore still require implementation and qualification.

Creation review found possible schema-valid UEFI/TPM normalization that the strict
matcher may refuse. This is not evidence that QEMU actually emits those fields.
Any native mismatch must retain the exact definition and allocated resources;
BIOS-only retained-definition acceptance cannot approve a TPM guest. Existing
matching of an assigned absolute NVRAM path is not freshness or identity proof.
Actual cold recovery requires a separate bound auxiliary-state inventory.

The next native check is a fresh, disconnected probe guest: observe `SEEDED`, then
`PRESERVED` on a second boot, and later test complete capture/restore plus omitted
member controls. Even a passing marker would establish only the tested auxiliary
bytes, not encrypted-OS recovery or a complete SNAP-01/BAK-01 acceptance.
