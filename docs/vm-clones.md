# Clone a VM

Make a new, independent VM from a stopped one ([ADR 0066](adr/0066-full-clones.md)).
In the TUI, select the stopped VM, open **More → Advanced tools → Clone VM**, and
give the clone a name. From the CLI:

```sh
virmill vm clone VM_UUID --connection qemu:///system \
  --input '{"name":"web-2"}' --plan
```

Add `"pool":"archive"` to put every copy in one storage pool; by default each copy
goes in the same pool as the disk it copies. Apply the plan with `virmill plan
apply` and the acknowledgements it lists: `host-mutation`,
`exclusive-storage-writer`, `exclusive-configuration-writer`,
`copy-managed-volumes` and `new-vm-identity`, plus `new-firmware-state` for a
UEFI VM and `pool-overcommit` when a pool reports less free space than its
copies need.

**What happens.** Every writable disk is copied into a new volume through
libvirt, hashed as it is copied and read back against that digest. Only then is
the clone defined, stopped, with a new UUID, the name you gave it and new MAC
addresses on the same networks. The original is not changed. Read-only media,
such as an installer or cloud-init seed ISO, are shared with the original rather
than copied.

**Inside the guest.** The disks are copied byte for byte, so the clone has the
original's hostname, machine ID, SSH host keys, accounts and licences. Change
them in the clone before running both VMs on one network. A UEFI VM's firmware
variables are not copied: the clone starts with fresh ones from their template,
so its boot entries are reset. Guests that boot through the standard fallback
path, as most Linux installs and Windows do, still boot.

**What can be cloned.** A stopped VM with no saved state, snapshots or
checkpoints, whose writable disks are raw or qcow2 volumes in active file-based
storage pools, without backing files, used by no other VM. VMs with a plain file
disk outside a pool (move it into a pool first), passthrough or shared host
devices, an emulated TPM, or explicit socket or file paths on a device are
refused, and the refusal says why.

**Space.** The clone needs room for a full copy of every writable disk. The
review gives the total per pool.

**If the job is interrupted.** Until the clone is defined, a failure has written
at most new copies that nothing uses: the job fails, frees the original and names
each copy so you can delete it. If the job stops at or after defining the clone,
**Jobs → Check interrupted job outcome** observes whether the clone exists with
its copies; nothing is copied or defined again.

Linked clones, which share a base image, are not available.
