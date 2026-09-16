# Deleting disks beside guests that use files outside pools

Build `3657212` (1.0.0-beta.5 test build), installed from its RPMs on the
owner-authorized test VM (`outside-pools-3657212-install-native-001`). Design:
[ADR 0054, amendment 2026-09-16](../adr/0054-explicit-selected-disk-removal.md#amendment-2026-09-16-files-outside-storage-pools).
Found by `new-vm-system-native-002` ([New VM run](new-vm-run.md)).

Removal with disks used to refuse with `RECOVERY_REQUIRED: disk source outside
reconciled storage pools` whenever any other guest named a plain file outside
the pools. It now compares each such file's device and inode with the selected
volumes, and follows qcow2 backing files.

## Private session: passed

`disk-removal-outside-pools-native-003` **passed**. Probe:
`tests/fixtures/release/disk_removal_outside_pools_probe.py`. It ran in a
private `qemu:///session` that started and ended empty, through the installed
binaries, and never started a guest.

| Guest defined beside the VM being removed | Review of `vm remove --delete-disk vda` |
| --- | --- |
| Disk `alias.qcow2` outside the pool, a symlink to the VM's volume | refused, `RESOURCE_BUSY`: a path outside storage pools reaches a cleanup volume |
| Disk `child.qcow2` outside the pool, a qcow2 image backed by the VM's volume | refused, `RESOURCE_BUSY`: retained image references a cleanup volume |
| UEFI guest with a qcow2 disk outside the pool (backed by another outside file), a missing installer ISO and an NVRAM file that was never created | allowed; `vm.remove-disks-v1` applied and succeeded |

After the job, the VM was gone, its volume file was absent and no longer listed,
and the unrelated guest was still defined. `other.qcow2`, its backing file and
`child.qcow2` kept their SHA-256, and the symlink was left in place. The host's
own pools, networks and VMs did not change.

Runs `001` and `002` stopped before any review: libvirt refused to define the
unrelated UEFI guest without ACPI. The probe now reports libvirt's reason and
declares ACPI.

## System connection: still refused

`outside-pools-review-native-001` repeated a read-only removal review
(`vm remove ... --delete-disk sda`, no plan applied) for an existing stopped
pool-volume VM on the test host's `qemu:///system`. On beta.5 the same review
refused with `disk source outside reconciled storage pools`. On `3657212` that
refusal is gone, and the review stops with `OPERATION_FAILED: permission denied`.

The dependency graph reads every pool image's header as the user running the
coordinator. On this host, libvirt creates pool files as root or qemu with mode
0600, so an ordinary user cannot read them. Several unrelated guests there would
also still refuse under the amendment: their files sit in directories this user
cannot search, or are qcow2 images this user cannot read. So a person still
cannot delete disks through Virmill on this host's system connection. Fixing
that needs a privileged, read-only way to observe these files, which is a
separate design decision.

Limits:

- A native link or backing file created after the last recheck is an external
  writer race, as ADR 0054 already states.
- Bind mounts are covered by the same device and inode comparison in unit tests
  only; the test user cannot create one.
