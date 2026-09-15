# Grow a VM's disk

Make one disk of a stopped VM larger ([ADR 0062](adr/0062-disk-grow-add-move.md)).
In the TUI, select the VM, open its tasks, choose **Advanced → Grow VM disk**,
pick the disk and type its new size in GiB. From the CLI:

```sh
virmill vm disk grow VM_UUID --connection qemu:///system \
  --input '{"target":"vda","sizeGiB":40}' --plan
```

Apply the plan with `virmill plan apply` and the acknowledgements it lists:
`host-mutation` and `exclusive-storage-writer`, plus `pool-overcommit` when the
added size is more than the pool's free space. Host space is used only as the
guest writes, so an overcommitted pool can fill up later.

**What can be grown.** The VM must be stopped, with no saved state and no
snapshots or checkpoints. The disk must be a writable raw or qcow2 file in a
storage pool, without a backing file, and no other VM or disk image may use it.
The new size is a whole number of GiB larger than now; disks are never shrunk.
On a system connection libvirt runs `qemu-img resize` as root on the VM's own
image; imported or untrusted images are never resized.

**Inside the guest.** Only the virtual disk grows. Extend the partition and
filesystem inside the guest afterwards, for example with `growpart` and
`resize2fs` on Linux or Disk Management on Windows. Ubuntu cloud images extend
their root partition at boot through cloud-init.

**If the job is interrupted.** The job records its intent before the resize.
Afterwards, **Jobs → Check interrupted job outcome** reads the volume's size and
never resizes again. A resize that visibly did not happen fails plainly and
leaves the disk free for another try.

Adding a disk and moving one to another pool are planned in the same ADR.
