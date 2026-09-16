# ADR 0063: Remove the VMs Virmill creates, including UEFI ones

Status: accepted for keeping firmware and TPM state, which is implemented;
deleting them on request is proposed.

## Context

VM removal (ADR 0050) and selected-disk deletion (ADR 0054) accept only BIOS x86
definitions whose disks are plain files. The VMs Virmill creates declare their
disks as pool volumes (`<disk type='volume'>` with a pool and volume name), and
imported appliances are often UEFI with NVRAM, sometimes with a TPM. Removal
refuses all of these, so **Remove VM** fails for every VM Virmill creates and
for UEFI VMs, even when all disks are kept. Phase 2 asks for dependable delete.

## Decision

- **Pool-volume disks.** Inspection resolves each `<disk type='volume'>` to its
  registered volume: pool UUID, key, path, format, filesystem generation and
  fingerprint, exactly as file disks are observed today. The retained-sources
  list holds the resolved paths, so the review, the dependency graph and the
  per-disk receipts stay as they are. A volume that is not registered in the
  named pool, or whose path is not the pool entry, is refused.
- **UEFI NVRAM.** Definition removal keeps the VM's NVRAM file by default
  (`VIR_DOMAIN_UNDEFINE_KEEP_NVRAM`) and lists it in the review as kept, beside
  the disks. Deleting it is a separate selection with its own acknowledgement,
  offered only when no other definition names that file.
- **TPM state.** Removal keeps emulated TPM state by default
  (`VIR_DOMAIN_UNDEFINE_KEEP_TPM`) and says so in the review. Secrets sealed to
  that TPM stay recoverable only while the state is kept.
- Everything else is unchanged: stopped VM, no saved state or snapshots, one
  review, durable receipts, and deletion never replayed.

## Consequences

- Remove VM works for the VMs Virmill creates and for UEFI appliances.
- Kept NVRAM and TPM state take a little space until deleted; the review names
  them so nothing is left unaccounted for.
- Native qualification must cover a created pool-volume VM, a UEFI VM and a
  UEFI VM with a TPM, keeping and deleting disks.
