# Full clone of a stopped VM

Probe: `tests/fixtures/release/vm_clone_native.py`
([ADR 0066](../adr/0066-full-clones.md)). Every run used the authorized Fedora 44
test VM's `qemu:///system`, the installed packages and the stopped Ubuntu 24.04
cloud-image VM `noble-server-cloudimg-amd64`. That VM has nine writable qcow2
disks in two storage pools, including its boot disk `sda`, a read-only cloud-init
seed ISO, one network adapter and no UEFI variables.

## Result

`vm-clone-system-native-003` **passed** on build `2389657`:

| Check | Observed |
| --- | --- |
| Review | `vm.clone-v1` with `host-mutation`, `exclusive-storage-writer`, `exclusive-configuration-writer`, `copy-managed-volumes` and `new-vm-identity`; a new UUID and a copy for each of the nine writable disks, each in its disk's own pool; the ISO listed as shared |
| Job | succeeded; every copy verified by read-back before the define, and the stored clone confirmed |
| Original | saved definition byte for byte unchanged, still stopped |
| Clone | `noble-clone-003`, stopped, with the reviewed UUID, a new MAC address, no Virmill creation metadata, every copied disk naming its copy and none naming the original, and the shared ISO unchanged |
| Boot | the clone started from its copied `sda`; while it ran libvirt counted 237,396,480 bytes read and 4,840,960 bytes written on that disk; it then shut down gracefully in 2.2 s |
| Removal | the clone's definition removed through a reviewed plan keeping disks, then its nine copies deleted once no definition named them |
| Everything else | other VMs, prior jobs and source media unchanged |

## Runs that did not pass

- `vm-clone-system-native-001` on `0b9b26a` copied all nine disks, verified each
  copy and defined the clone with a new MAC address, then refused its own
  read-back. Removing Virmill's creation record had left an empty `<metadata>`
  element, which libvirt does not store, so the reviewed and stored digests
  differed. The job went `recovery-required` as designed, holding its locks.
  `vm-clone-system-reconcile-native-001` then reconciled it: recovery observed
  that the clone existed and used every verified copy, and the job succeeded
  without copying or defining again. Build `2389657` removes the element with
  its last child; checked against that run's stored clone, the digests match.
  The clone was removed as in the table above.
- `vm-clone-system-native-002` on `2389657` passed every product check, including
  the read-back, and the probe then refused the clone because the shared
  cloud-init ISO's volume name mentions the original's UUID. The probe now checks
  the clone's `uuid` element. That clone was removed the same way.

## Not covered

- A UEFI VM, whose clone gets fresh firmware variables from their template.
  Software tests cover the rewrite; no native UEFI clone has run.
- A clone into a different pool with `"pool"`, and a raw disk.
- Changing the guest's own identity inside the clone, which Virmill does not do.
- Linked clones, which STO-02 also names and this work does not implement.
