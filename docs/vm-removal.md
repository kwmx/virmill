# Remove a VM, with optional disk deletion

Open **VMs**, select the VM, then **More → Remove VM**. For a running VM
it is under **Advanced tools**, and the form explains that the VM must be stopped.
Disks start unchecked: they will be kept. Select a disk only if you want to
permanently delete it. Type the displayed VM name exactly, select **Preview**,
and review the exact files before applying.
Esc from review returns to your choices; Esc from the form leaves the VM unchanged.

Default removal takes the VM out of libvirt's list. Its disk files and existing backups
stay where they are. **No disk space is freed, and no configuration backup is
created.** Make a recovery point first if you need the complete supported VM
configuration for restoration. To use retained disks again, create or restore a
VM definition; disk files alone do not preserve all VM settings.

The VM must be stopped with automatic startup off and no saved runtime state.
This beta supports x86 BIOS and UEFI VMs whose disks are files or storage-pool
volumes, including the VMs Virmill creates ([ADR 0063](adr/0063-remove-created-and-uefi-vms.md)).
A UEFI VM's firmware settings file (NVRAM) and an emulated TPM's state are kept
and listed in the review beside the disks; nothing deletes them yet. Snapshots,
checkpoints, protected settings, a passthrough TPM or unsupported
device/storage layouts block removal before any change. The issue explains the refusal; F1 opens its
full text. Resolve it through the relevant workflow or keep the VM defined.
Do not delete auxiliary files manually to bypass the check.

CLI equivalent:

```sh
virmill vm remove VM_UUID --plan
```

Apply through the usual [plan approval workflow](cli-reference.md). There is no
implicit disk deletion, forced shutdown or setting-file input.

To delete specific disks, use their guest target names shown in VM details:

```sh
virmill vm remove VM_UUID --delete-disk vda --delete-disk vdb --plan
```

The flag also accepts `--delete-disk vda,vdb`. There is no implicit “all disks”
selection. JSON callers may use `{"deleteDisks":["vda","vdb"]}`. The generated
plan requires `data-loss-delete-disks` as well as its host and exclusive-writer
acknowledgements; `--yes` alone is insufficient. The TUI offers the same exact
review and explicit acknowledgements. Deletion cannot be undone. Unselected
disks, installer media, backing parents and backups remain.

Selected deletion requires registered raw/qcow2 files or pool volumes and a complete,
readable native storage graph. Every guest on the connection must be stopped
until live graph inspection is qualified. Shared disks, referenced parents,
source/preparation records, snapshot/backup references, unknown files in pool
directories, inaccessible images or changed generations block it. Virmill explains
the reason before removing the VM; F1 reads the full issue. Coordinate other
storage/VM editors during deletion. Do not bypass these checks by unlinking files.

After a partial or uncertain deletion, some files may already be gone. Remaining
files and resource locks are kept. Read Jobs → Activity and preserve remaining
files; reconciliation observes receipts and absence without repeating deletion.

After submission, check **Jobs**. If the result is uncertain, leave retained files
and any VM with that UUID intact and inspect the operation. A missing VM alone
cannot prove that this job completed. Reconciliation reads the durable receipt
and observes the UUID; it never repeats removal. A changed definition invalidates
its preview, so return and review fresh state. Coordinate other libvirt editors
while applying the operation.
