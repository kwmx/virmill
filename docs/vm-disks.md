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

## Add a disk

Give a stopped VM one more empty disk in its own storage pool. In the TUI,
choose **Advanced → Add VM disk**, type a size in GiB and leave the connection
on automatic to follow the VM's existing disks. From the CLI:

```sh
virmill vm disk add VM_UUID --connection qemu:///system \
  --input '{"sizeGiB":20}' --plan
```

Virmill picks the next free disk name, target and port in the VM's own pool,
builds the empty image privately, uploads it and verifies it by reading it
back, and only then names it in the saved definition. The plan asks for
`host-mutation`, `exclusive-storage-writer` and `exclusive-configuration-writer`,
plus `pool-overcommit` when the disk's full size is larger than the pool's free
space — its file grows only as the guest writes. The new disk is empty:
partition and format it inside the guest.

## Close an unfinished disk addition

If an addition stops before it finishes, its job keeps this VM and its pool
locked so nothing else can change them. Close it from **Jobs**: select the
unfinished addition and choose **Close disk addition**, or from the CLI:

```sh
virmill vm disk add dispose OPERATION_UUID \
  --input '{"disposition":"accept"}' --plan
```

- **accept** keeps the new disk. It is offered only when the saved definition
  already names it, which means the disk and its verified volume are there.
- **delete** removes the new volume, and only while no definition names it. A
  disk the VM still uses is never deleted this way; remove the disk first if
  that is what you want.

Either way the locks are released and the addition is closed for good: it never
becomes a successful operation, and a new disk needs a new review.

Moving a disk to another pool is planned in the same ADR.
