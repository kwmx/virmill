# 07 — Storage, restore points and backups

## Storage ownership and lifecycle

Directory-backed libvirt pools with qcow2/raw volumes are the certified baseline. Existing other pool types may be inventoried and used only for explicitly tested operations. A detected volume does not imply safe support for thin-pool management, external SAN snapshots or arbitrary storage provisioning.

Track virtual size, allocated size and free space separately. Plan staging overhead, backing-chain space and worst-case sparse growth. Preserve filesystem permission and SELinux/AppArmor constraints; never fix access with world-writable directories or by disabling confinement.

Expand is supported; shrinking virtual disks is not a normal v1 operation. Expanding a block device does not prove the guest filesystem expanded. Relocation requires either a stopped VM or a specifically tested live migration/copy workflow; v1 may use the fully supported shutdown-required path. Source deletion happens only after verified target publication and no remaining references.

Offline qemu-img operations must never modify an active disk. QEMU explicitly warns that this may destroy the image, and even inspection of a concurrently changing image can be inconsistent. Use live backend APIs where required. [S20]

## Backing graph invariant

Every mutable leaf points only to known, immutable parents. Store relative/managed-resolvable backing references where suitable and explicitly name formats. No template base, snapshot ancestor or cloned disk is deleted while referenced by any VM, snapshot, template, export or in-flight operation.

A cached reference count is not enough: reconcile the graph with backend XML, image metadata inspected safely, and persisted manifests before deletion. Unknown/ambiguous references block garbage collection. `gc plan` is read-only; `gc apply` revalidates all candidates. Shared parent disks are never live-committed into.

## Restore point taxonomy

| Class | Contains | Correct label |
|---|---|---|
| Disk-only crash-consistent | Consistent point-in-time disk set, configuration metadata | Equivalent protection level to an abrupt power loss; not application-consistent |
| Filesystem-quiesced | Disk set captured during coordinated filesystem freeze | Filesystem-quiesced, not automatically database/application-consistent |
| Application-quiesced | Verified guest/application hooks plus filesystem/disk capture | Application-quiesced only if hooks actually succeeded |
| Cold complete | VM stopped; disks, config, firmware variables and necessary auxiliary state | Complete stopped-VM recovery point |
| Memory-inclusive | Compatible memory/device state plus matching disks and auxiliary state | Resumable only under the certified compatibility conditions |

Libvirt distinguishes disk, memory and full-state snapshots; memory restore is not safe against unrelated changed disks. Keep those semantics visible. [S21]

## Snapshot workflows

External disk snapshots are the primary managed live-disk workflow where supported. A restore point includes the entire required disk set and configuration fingerprint. Do not create independent snapshots of multiple disks and call them a single atomic VM capture unless the backend guarantees the required cut.

For a normal disk revert: stop the VM with approval, preserve the current branch, create fresh writable overlays from the selected immutable point, restore matching configuration/auxiliary state, then boot if requested. A revert changes active state; it does not inherently delete subsequent history. Destructive branch pruning is a separate operation.

Snapshot removal must choose dependency-safe merge/flatten operations, validate the resulting backing graph and retain source data until completion is confirmed. Never translate “delete snapshot” into simply unlinking the snapshot's file. If a backend cannot perform a safe merge online, provide the shutdown-required path.

Memory snapshots and managed save have host/machine/CPU/device compatibility requirements. A managed save is not an independent backup. Disable unsupported memory capture combinations before starting; do not call a disk-only snapshot memory-inclusive.

### Firmware and TPM

The recovery point must account for UEFI NVRAM, TPM emulator state and encryption/secret dependencies. Do not copy live swtpm state or changing NVRAM and call it consistent. For TPM-backed or other unsupported auxiliary-state combinations, the baseline complete capture is a **cold** capture. More advanced live full-state capture requires separate proven backend support and tests.

A missing TPM/NVRAM member is a hard completeness failure when the VM depends on it. A disk-only live backup may be offered with explicit limitations, but not mislabeled as complete VM recovery. New-clone identity rules do not apply to disaster recovery: preserving the original TPM/firmware identity may be essential.

## Backup design: core capture + restic repository

Use restic as a packaged backup repository integration rather than inventing cryptography/deduplication. Restic documents encrypted repository storage and changed-data efficiency. [S22]

The core owns safe VM capture, manifests, schedules and restore orchestration. Restic owns repository-format operations. A repository backend may later be supplied by a plugin; v1 must include local disk/external mounted-storage repositories, without requiring a plugin. Do not automatically mount network shares using secrets from a manifest.

### Cold complete backup

1. Resolve all disks, configuration, UEFI/TPM state, required secrets, template ancestors and external dependencies.
2. Obtain graceful shutdown approval; wait for confirmed stopped state. Do not hard-stop automatically.
3. Freeze the dependency graph through operation leases and record fingerprints; ensure another writer has not started the VM.
4. Capture independent disk images or an explicitly complete backing set, plus configuration and auxiliary state through approved adapters.
5. Create a versioned manifest; checksum members; commit the immutable staging set.
6. Store the set in the encrypted repository, confirm restic completion and verify member inventory.
7. Mark the backup committed only after all mandatory members exist. Restore the VM's previous running state only when the approved plan requested it.

If the VM cannot safely stop, the job fails or awaits an explicitly approved alternate capture policy. No hidden consistency downgrade.

### Live disk backup

Use `virDomainBackupBegin` and supported push-mode full capture for the baseline instead of implementing an overlay-delete fallback casually. Libvirt recommends the backup API over an older overlay approach because erroneous deletion can lose data. [S23] [S24]

Quiesce only for the cut establishment, not the entire data-copy duration. Run an independent thaw guardian with a hard deadline, always thaw on every error path, and record whether the coordinated cut was actually established. Default maximum freeze budget is 15 seconds; timeout fails the consistency requirement. Application hooks have their own bounds and cleanup hooks.

Observe actual backend completion events/results. A disappearing job is not proof of success. A guest-initiated shutdown can interrupt capture; newer backend support may preserve the process for backup completion, but feature-detect and test it. Otherwise mark the capture incomplete rather than store a successful backup with truncated data. [S23]

Require all participating disks. Auxiliary state that cannot be captured consistently makes complete live recovery unavailable; choose cold mode or an explicitly limited disk-only capture. Never silently skip disks, TPM or encrypted-volume dependencies.

v1 provides complete logical full backups with repository deduplication. It does **not** claim libvirt dirty-bitmap incremental capture simply because restic stores only changed chunks. Dirty-bitmap capture/chain maintenance is a separate future optimization, not necessary for a complete backup product.

## Manifest and independent recovery

The manifest records API version, backup ID, source VM identity, capture timestamps, guest/architecture, effective and persistent XML, machine/firmware versions, disks and formats, flattened/backing relationships, NVRAM/TPM files, secret inclusion/reference status, NIC policy, consistency class, completeness exclusions, repository objects and hashes.

Store confidential auxiliary state inside the encrypted backup. Credential references alone are not recoverable secrets: a backup depending on an unavailable external secret must explicitly report that dependency and cannot be called independently recoverable. The user chooses whether to include exportable secrets encrypted or retain external dependency requirements.

Provide a documented recovery path that uses the backup manifest and repository credentials without the original application database. Never place the only repository recovery password inside that same repository.

## Restore and restore testing

Default restore creates a new name/UUID, disconnected or connected only to an isolated test network. Preserve identity only for an explicitly selected recovery replacement, with duplicate-MAC/IP/VM conflicts checked. Different identity semantics for TPM-protected guests require a reviewed supported path; “restore as clone” can require guest preparation or recovery keys.

Restore every member into staging, verify integrity, map firmware/CPU/network capabilities, and define only after validation. Existing VMs are never overwritten by default. Replacement requires shutdown and a safety backup/checkpoint before swapping references.

Verification levels are `manifest-checked`, `repository-data-checked`, and `restore-boot-tested`. A checksum test is not a boot test. A restore test can report partial readiness for a guest without an agent, but must state what was actually verified. It must not expose a duplicate restored guest to the LAN.

## Scheduling and retention

Policies select VMs by stable IDs/tags and specify timezone, capture mode, downtime permission, consistency, repository, retention and verification. Generate a fresh plan for each occurrence; reuse only appropriately scoped standing authorization. If credentials are unavailable after logout/reboot, fail visibly or pause; never fall back to plaintext secrets.

Missed schedules default to one coalesced run, not replaying every missed occurrence. Laptop sleep/power loss produces an overdue record. Avoid overlapping capture jobs on the same VM/repository. A stopped user service does not magically run scheduled work.

Retention operates on committed backup sets. Preserve the last known-good set unless explicitly overridden, do not prune before a replacement succeeds, and serialize repository prune/check/write operations as required. Report removable-drive absence and distinguish backup failure from a temporarily unavailable destination.

## Core release tests

Require successful recovery after original disks and application DB are removed from the test host; include multi-disk, linked-template and UEFI/TPM fixtures. Test interruption during capture/upload/restore/prune, full staging/repository disks, locked credentials, partial restic failure, source VM shutdown mid-backup and recovery of a frozen guest after coordinator death.
