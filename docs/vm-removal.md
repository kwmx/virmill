# Remove a VM and keep its disks

Open **VMs**, select the VM, then **More → Advanced tools → Remove VM, keep disks**.
Type the displayed VM name exactly, select **Preview**, and review before applying.
Esc from review returns to your choices; Esc from the form leaves the VM unchanged.

Removal takes the VM out of libvirt's list. Its disk files and existing backups
stay where they are. **No disk space is freed, and no configuration backup is
created.** Make a recovery point first if you need the complete supported VM
configuration for restoration. To use retained disks again, create or restore a
VM definition; disk files alone do not preserve all VM settings.

The VM must be stopped with automatic startup off and no saved runtime state.
This beta supports x86 BIOS VMs with file-backed disks. Firmware/TPM state,
snapshots, checkpoints, protected settings or unsupported device/storage layouts
block removal before any change. The issue explains the refusal; F1 opens its
full text. Resolve it through the relevant workflow or keep the VM defined.
Do not delete auxiliary files manually to bypass the check.

CLI equivalent:

```sh
virmill vm remove VM_UUID --plan
```

Apply through the usual [plan approval workflow](cli-reference.md). There is no
implicit disk deletion, forced shutdown or setting-file input. The selected disk
deletion extension is not available in this beta.

After submission, check **Jobs**. If the result is uncertain, leave retained files
and any VM with that UUID intact and inspect the operation. A missing VM alone
cannot prove that this job completed. Reconciliation reads the durable receipt
and observes the UUID; it never repeats removal. A changed definition invalidates
its preview, so return and review fresh state. Coordinate other libvirt editors
while applying the operation.
