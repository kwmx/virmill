# Firmware reconciliation and TPM/NVRAM persistence probe

This is scoped development evidence from the owner-authorized disposable Fedora
44 VM on 2026-09-07. All 71 acceptance scenarios remain required and unaccepted.
The parent agent alone performed remote operations. Earlier guests and supplied
media were retained; the two new original probe guests have no network adapters.

The installed runtime was clean revision
`ad608076cd928e6c6c270e6442cbc2d8047e6e0c`, source digest
`1949f2b27ee3389d4e6287d44c1d43d7397acd35f50c8780e253eed3be91d712`.
Its exact native packages are in
[the environment record](environments/disposable-cold-ad60807.json).
Libvirt was 12.0.0-3.fc44, QEMU 10.2.2-1.fc44, edk2-ovmf
20260812-4.fc44 and swtpm 0.10.2-1.fc44. SELinux remained enforcing.
The later working-source NVRAM syntax correction and clearer freshness warning
were **not installed during these native checks**.

## Integrated runtime and interfaces

| Evidence ID | Observed result and boundary |
|---|---|
| cold-fixes-core-001 | Full Go core race command passed with private IPC and generated disk tools required. Other opt-in native/conformance cases were not all selected. |
| cold-fixes-vet-001 | Full core vet passed. |
| cold-fixes-repro-001 | Two offline builds produced identical three binaries and four RPM/DEB artifacts. |
| cold-fixes-packaging-001 | Three selected artifact checks passed without skips: staged install/uninstall preservation, package metadata and ordinary-user daemon/CLI/scaffolder with confined generated OVA/disk/seed/ISO work. No production-host mutation. |
| cold-fixes-upgrade-001 | Exact development RPM upgrade preserved 26 jobs, immutable plans, three unresolved locks, five stopped definitions and the probe disk. |
| cold-fixes-reconcile-001 | Native reconciliation recognized only the reviewed firmware metadata normalization, changed the old creation receipt's `defined` flag, and released its three locks. No allocation, upload or definition replay. Other jobs, plans, definitions and disk bytes stayed unchanged. |
| cold-fixes-estimate-tui-001 | Installed CLI and actual TUI showed the same fresh plan digest, 125829120-byte pool budget and 458752-byte payload note. Preview only; the original immutable plan retained its old estimate. |
| cold-probe-4m-inspection-tui-001 | Installed CLI and actual TUI showed the stopped 4 MiB guest's exact firmware mapping, TPM profile and unresolved source-path warning. All 31 jobs and six stopped definitions were preserved; no plan or apply submitted. |

These are distinct evidence classes. A private socket, generated conversion,
package installation or TUI display does not establish hardware recovery.
Native recipes are retained in
[disposable-recorded-run](../../tests/fixtures/protection/disposable-recorded-run).
The ledger provides individual log and recipe hashes, source revisions, fixture
digests and limitations. The actual TUI used a 180-column, 70-row terminal;
this observation is not the full accessibility/terminal matrix.

After integrating the agents' NVRAM lexical/attribute checks, isolated TPM
capability regressions and observer tests, `cold-native-integration-core-001`
passed the full core race command with IPC and generated disk tools required.
`cold-native-integration-vet-001` passed vet; `cold-observer-tests-004` passed all
44 synthetic observer tests, including exact reconstruction of both historical
variants. Those entries bind the working source digests they actually tested;
they are separate from the installed ad60807 runtime. The complete release gate
`cold-native-release-gate-001` failed for all 71 unaccepted scenarios and the
unfinished SHIP checklist, as required.

The reviewed syntax/observer checkpoint was then frozen at
`4c5817674e8c036081715237ad1e768869f377c5`, source digest
`483440c70932ecaf3d29f893ec24ae6ff59fa8a83ffb43ee282966cad852b0b1`.
`cold-native-packages-repro-001` reproduced all three binaries and four packages;
`cold-native-packaging-001` passed all three packaged-artifact tests without skips.
`cold-native-cross-001` compiled the portable common packages, Go SDK and example
for Darwin/arm64 and Windows/amd64; those foreign binaries were not executed.
`cold-native-upgrade-001` installed the exact RPMs on the disposable host and
preserved all 31 job bodies, plans, metadata, six stopped definitions, the probe
disk and zero locks. It restarted only the private coordinator, with the helper
inactive and SELinux enforcing. No guest was started, defined or reconciled in
that upgrade. Its [environment](environments/disposable-cold-4c58176.json) records
the installed checkpoint; later binding implementation is separate work.

## Original EFI fixture and observation failures

The [original probe](../../tests/fixtures/protection/uefi-tpm-probe) stores a benign
public marker in one TPM NV index and one EFI variable. First initialization
requires both to be absent; a later boot requires both exact values. Partial or
conflicting state fails visibly. Host collectors did not copy NVRAM/TPM bytes.

| Input | SHA-256 |
|---|---|
| Original BOOTX64.EFI, 9728 bytes | `b50a5ef0a95feb2ee6824b1d5f353b41a4b2ed189c6974c76293592fa3109b33` |
| Original FAT boot image, 33554432 bytes | `1945000ea87359d92f6fcb3413549fe477884012d631c66a058c71a1dda9e483` |
| Build manifest, 2361 bytes | `275abd707259b46a31d24fd7e5614b5e655319b7f6815e7a45a240b99a0f82ac` |
| Serial observer v3 | `dc96fbc1ca5dae19be1d1b6f8f9a6a1101ac95d133442e4227d12804a251c5e6` |

The first console preflight failed because observer v1 set `VIRSH_DEBUG=0`, which
enabled native option tracing. Its exact diagnostic classifier refused the
combined text before any start. Observer v2 removed inherited debug/log settings,
then correctly observed the stopped-domain refusal. During the first 2 MiB boot,
v2 refused the transient `PTY device is not yet assigned` diagnostic before
receiving guest output. The start job succeeded and the guest stopped itself,
but that marker observation was inconclusive. These failed records remain in
the ledger as `cold-probe-console-preflight-001` and
`cold-probe-first-boot-001`.

V3 adds only the exact zero-output PTY-readiness refusal to the bounded retry set.
It never sends guest input, never retries after output, and only cleans up its
own console child. Two small retained patches reconstruct the exact v1/v2 bytes
from v3. Synthetic observer tests certify this fixture logic only.

## Actual firmware comparison

Both guests used KVM, x86_64, `pc-q35-10.2`, one vCPU, 512 MiB RAM, host-passthrough
CPU, one SATA QCOW2 boot disk, TPM 2.0 CRB emulator with persistent state, disabled
Secure Boot, and zero NICs. Each had a separate UUID, disk and native auxiliary
state. The prepared image and EFI executable were identical. The first guest
was retained when creating the second; its firmware/state was not reset.

| Observation | 2 MiB raw firmware | 4 MiB QCOW2 firmware |
|---|---|---|
| Guest UUID | `f78674f3-bf3a-43e5-81f9-4283e2472024` | `d4c95f21-28bc-428d-9e5f-ceda025d279e` |
| Code | `/usr/share/edk2/ovmf/OVMF_CODE.fd` | `/usr/share/edk2/ovmf/OVMF_CODE_4M.qcow2` |
| Code SHA-256 | `52c0cd0d270dee9dd21f38d247028753a021b674b609c5050e33232645509387` | `2eeb785094b42fc16c41b46b92949405332ce527b62d0f1646fe0c3f42cffbde` |
| Template | `/usr/share/edk2/ovmf/OVMF_VARS.fd` | `/usr/share/edk2/ovmf/OVMF_VARS_4M.qcow2` |
| Template SHA-256 | `6ed987af3a3c155be71665f510eae3e007eda9b8b94afd59d45e91c4a11565cc` | `035317bb2923a13c1dc57373d608521cc3a486f1e3119016727d54baccc6e8bb` |
| First boot | Marker observation inconclusive due to observer v2 | `ABSENT/ABSENT`, then `SEEDED` |
| Second boot | `FAIL LOCATE_TCG2 0x800000000000000e` (`EFI_NOT_FOUND`) | `PRESENT/PRESENT`, then `PRESERVED` |
| Final state | Stopped, retained | Stopped, retained |

The 2 MiB second-boot serial result is recorded in
[cold-probe-second-boot-001](logs/cold-probe-second-boot-001.log). Its marker-read
and marker-write branches were not reached after the failed protocol lookup.
The [ABI review](../reviews/uefi-tcg2-probe-review.md) found no concrete GUID,
calling-convention or table-offset defect; that does not identify the runtime
cause. Equal descriptor feature lists do not prove equal firmware behavior.

The 4 MiB creation, seeding and persistence records are
[cold-probe-4m-create-001](logs/cold-probe-4m-create-001.log),
[cold-probe-4m-boot-001](logs/cold-probe-4m-boot-001.log) and
[cold-probe-4m-second-boot-001](logs/cold-probe-4m-second-boot-001.log).
Their separate successful start jobs are
`06ec52b1-ecce-4a91-9746-161a2f6c0218` and
`018986b7-a217-4045-bab2-5cbe96647d98`. The two public serial streams were 775 and
785 bytes, hashes `147f94086027b300d8d203cf641b89ef7f3c204d4cc3783fff31f1492de4ced9`
and `0703438869201e656646f3bc5a4e51c95097652d491f1d5ccccbaf16c969ca8f`.
Each had two identical result lines because the fixture mirrors its report;
they are not separate boots.

The 4 MiB guest's persistent XML acquired the native `default-v1` TPM profile
after first boot and remained unchanged through the second. Its stopped boot
disk remained `f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51`
through both boots. All five earlier definitions and all prior journal job bodies
were preserved at each step. There are now six stopped guest definitions,
31 jobs and zero locks in the isolated coordinator journal.

## Remaining boundary

This establishes same-guest public-marker persistence across two native boots
for the recorded 4 MiB tuple. It does not qualify all 4 MiB firmware, Secure Boot,
guest encryption, TPM identity/PCR/EK semantics, arbitrary guest readiness,
Virmill console support, a copied auxiliary set or independent restore.

Cold inspection still reports an unspecified TPM state path. No collector guessed
it. A point-in-time absence check before first start and later ordinary-file
metadata do not establish atomic exclusive initialization or durable first-path
provenance. The creation matcher now has a separate local
[path-syntax correction](../creation-nvram-path-validation.md); it does not supply
that missing proof.

The remaining protection work is the independently checked native state resolver,
typed authorized helper capture, durable complete publication, encrypted restic
repository lifecycle, retained-branch revert and independent restore without
original files or the application database. SNAP-01 and BAK-01 remain blocked on
those complete workflows. USB, routing/isolation and every other mandatory
acceptance scenario retain their own required evidence.
