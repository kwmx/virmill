# Removing the VMs Virmill creates, and a UEFI VM

Build `858e4c0` (1.0.0-beta.4), installed from its RPMs on the owner-authorized
test VM. Probe: `tests/fixtures/release/removal_created_native.py`
([ADR 0063](../adr/0063-remove-created-and-uefi-vms.md)). Each removal was a
reviewed CLI plan applied as one detached job. No disk deletion was requested.

| Run | VM | Review | After the job |
| --- | --- | --- | --- |
| `removal-created-native-001` | `qemu:///session`, `onestep-f7dc8e7e`, one pool-volume disk | acknowledgements `host-mutation`, `remove-vm-definition`, `exclusive-lifecycle-writer`; the disk listed as retained by its resolved pool path; no disk deletion, no backup deletion | the VM is gone; libvirt still registers the volume; other VMs, prior jobs and source media unchanged |
| `removal-uefi-native-001` | `qemu:///system`, `virmill-hardware--76354da`, UEFI with two pool-volume disks | both disks listed as retained by their resolved paths, plus the firmware settings file as kept; TPM state reported as not applicable | the VM is gone; libvirt still registers both volumes; other VMs, prior jobs and source media unchanged |

Before `858e4c0`, both removals were refused: the disks are pool volumes and the
second VM is UEFI. Both VMs were leftovers from earlier test runs; their disks
were kept and remain in their pools.

Limits:

- The firmware settings file lives in libvirt's root-only NVRAM directory, so
  the probe could not read it back as the ordinary test user. Its retention
  rests on libvirt's keep-NVRAM flag and on the review naming it; a native
  read-back needs privileges this test does not take.
- Keeping an emulated TPM's state is covered by software tests only: no
  disposable TPM VM was available, and the two TPM fixtures on the test host
  belong to other evidence.
- Deleting selected pool-volume disks is covered by software tests; the native
  runs kept every disk.
