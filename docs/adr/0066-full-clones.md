# ADR 0066: Full clones

Status: accepted and implemented. `vm-clone-system-native-003` cloned an Ubuntu
guest with nine disks on the system connection and booted the clone
([run record](../evidence/vm-clone-run.md)). Linked clones remain open.

## Context

Plan item 2b.4 and STO-02 ask for a full clone: a new VM that is independent of
the one it was copied from. Users otherwise make one by hand with another tool,
copying disks and editing XML, which the owner asked to avoid.

Move (ADR 0062) already copies a pool volume through libvirt streams, hashing the
bytes as they pass and reading the copy back against that digest, and points a
definition at the copy. It is qualified on both connections, including a guest
booting from its moved boot disk. A clone is the same copy for every writable
disk, followed by defining a second VM instead of editing the first.

STO-02's acceptance also names linked clones. Those share a base image and need a
dependency model this ADR does not add; whether they stay in the 1.0 scope is the
owner's decision and is not made here.

## Decision

`vm clone` (`vm.clone-v1`, through `vm.plan` action `clone`) makes one new,
stopped VM from a stopped VM. The input is the new name, and optionally one
storage pool to put every copy in; by default each copy goes in the same pool as
the disk it copies.

**What is copied, shared or refused.**

- Every writable disk (device `disk`, not read-only, not empty) is copied. It
  must be a raw or qcow2 volume in an active file-based storage pool with no
  backing chain, used by no other VM and no other disk of this one.
- Read-only removable media (an installer or seed ISO in a pool) and empty
  drives are kept as they are: the clone refers to the same read-only media.
- Refused before anything is planned: a running or paused VM, saved state,
  snapshots or checkpoints; a writable disk outside a storage pool, layered,
  shared, encrypted or with external state; passthrough or shared host devices
  (`hostdev`, `filesystem`, `shmem`); an emulated TPM, whose state cannot be
  copied by an unprivileged coordinator; firmware variables without a template;
  explicit socket, file or port settings on any device; and any other external
  dependency the cold-source inspection reports. Each refusal names what to
  change.

**Identity.** The clone gets a new UUID, chosen at review so its volume names
are known, and the reviewed name, which must differ from every VM on the
connection. Every network adapter keeps its network, model and link state, and
loses its MAC address so libvirt assigns a new one. Virmill's creation metadata
is removed: the clone is not the original's creation, and ownership is bound to
that record. Other metadata and every device and setting Virmill does not model
are kept exactly as they are.

A UEFI VM's firmware variables file is not copied, because on the system
connection it is readable only by root. The clone's firmware entry keeps its
template and loses its path, so libvirt creates fresh variables from the
template. The review says boot entries start fresh and asks for
`new-firmware-state`; guests that boot through the removable-media fallback
path, as most Linux installs and Windows do, still boot.

What is inside the disks is copied byte for byte: hostname, machine ID, SSH host
keys, accounts and licences are the original's. The review says so, and that
both VMs on one network may conflict until the clone's are changed.

**Execution.** One job with a receipt, as Move does:

1. Recheck the source is unchanged and still stopped, and every copy name absent.
2. For each writable disk in definition order: allocate the reviewed volume
   name, copy the bytes through one connection with the streaming digest, and
   read the copy back against it. The receipt records each verified copy.
3. Define the clone with validation, then read the stored definition back: new
   UUID and name, stopped, every copied disk naming its copy and no copied disk
   naming its original, shared media unchanged, new MAC addresses, and the rest
   equal to the reviewed definition apart from libvirt's formatting and the MAC
   addresses it assigned.

The source VM is never changed. Until the clone is defined a failure has written
at most new, unreferenced volumes, so the job fails plainly, releases its locks
and names each copy it made so the space can be reclaimed. A failure at or after
the define leaves the job needing recovery; reconciliation observes whether the
clone exists with the reviewed disks and never copies or defines again.

**Acknowledgements.** `host-mutation`, `exclusive-storage-writer`,
`exclusive-configuration-writer`, `copy-managed-volumes` and `new-vm-identity`,
plus `new-firmware-state` for UEFI and `pool-overcommit` when a destination pool
reports less free space than its copies need.

**Locks.** The source VM, the new VM's identity, every copied source volume and
every copy's path.

**Surfaces.** CLI `virmill vm clone VM --input '{"name":"…"}'`, and in the TUI
**More → Clone VM** with the name (suggested from the original's) and the pool.

## Consequences

- A clone of a VM Virmill created is listed as an ordinary VM, not a Virmill
  creation; it can be managed, grown, moved and removed like any other.
- Peak space is the sum of the copies; the review gives it per pool.
- Cloning a running VM, and copying UEFI variables or TPM state, need privileged
  or live adapters and are out of scope.
- Linked clones remain open pending the owner's scope decision on STO-02.
