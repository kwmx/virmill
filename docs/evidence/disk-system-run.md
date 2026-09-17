# Disk add, move and closing an unfinished addition on the system connection

Probes: `tests/fixtures/release/disk_add_native.py`,
`disk_move_native.py` and `disk_add_disposition_native.py`
([ADR 0062](../adr/0062-disk-grow-add-move.md)). Every run used the authorized
Fedora 44 test VM's `qemu:///system`, the installed packages and the stopped
Ubuntu 24.04 cloud-image VM `noble-server-cloudimg-amd64`, whose disks are qcow2
volumes in an active directory pool. Pool images on that host are owned by root
or qemu and readable by nobody else.

## Result

| Operation | Evidence | Build | Result |
| --- | --- | --- | --- |
| Add an empty 1 GiB disk | `disk-add-system-native-002` | 1.0.0-beta.7 (`4f2b31e`) | **passed**: `sdd` added, volume exactly 1 GiB, definition gained exactly that disk, guest booted with it and shut down gracefully |
| Move a disk to another pool and delete the original | `disk-move-system-native-001` | 1.0.0-beta.7 (`4f2b31e`) | **passed**: `sdd` copied and verified into another pool, definition pointed at the copy at the same target, the original pool volume deleted, guest booted with the moved disk and shut down gracefully |
| Close an addition interrupted before its volume existed | `disk-add-disposition-system-native-001`, `-008` | `ee442ce`, `f7dff36` | **passed**: closed with nothing to delete, two close acknowledgements only |
| Close an addition interrupted after its disk was defined (accept) | `-009`, `-010`, `-011`, `-015` | `f7dff36` | **passed** |
| Close an addition interrupted with a written, unnamed volume (keep) | `-012`, `-013`, `-014` | `f7dff36` | **passed**: delete refused with `permission denied`, keep closed it and named the unused volume |

Other VMs, prior jobs and source media were unchanged in every passing run. In
every disposition run the addition then read `partial` and the VM started and
shut down, so its locks were released.

The disposition probe interrupts a real addition: it applies the addition
detached, kills the user coordinator with SIGKILL after a chosen delay, starts
it again, reads whether the definition names the disk and whether its volume
exists, and chooses the disposition that matches. A whole addition took under
400 ms here, so delays of 0 to 150 ms reached the three states.

## What the runs found

Two product defects, both fixed before the passing runs:

- **An addition interrupted before its volume existed could not be closed.**
  Accept needs the disk in the definition, delete refused an absent volume, and
  reconciliation cannot prove an effect that never happened, so the job held the
  VM and its pool for good. Build `ee442ce` lets delete close such an addition
  with nothing to delete, asking for no deletion acknowledgement, and refuses it
  as stale if the volume appears before it runs.
- **On this host, an addition with a written but unnamed volume could not be
  closed either.** `disk-add-disposition-system-native-004` reached that state
  at 60 ms. Deleting the volume first proves no other VM or image uses it, and
  that proof reads image headers as the coordinator's user; root-only pool images
  make it fail with `permission denied`. No other disposition applied, so the
  VM stayed locked and runs `005` and `006` were refused before doing anything.
  Build `f7dff36` adds **keep**, which closes the addition without deleting
  anything and names the unused volume, and builds the dependency graph only for
  a deletion, so accept, keep and the close-only case never read image files.
  Run `007` closed that stuck addition with keep. Delete keeps its full guard and
  is still refused on this host.

Runs that did not pass for other reasons:

- `disk-add-system-native-001` added `sdc` correctly (exactly 1 GiB, the
  reviewed disk in the definition); the probe then compared the started VM with
  the definition from before the addition. The session runs never reached that
  step. The VM was shut down gracefully by hand and the probe fixed.
- `disk-add-disposition-system-native-002` and `-003` (400 ms and 1.2 s): the
  addition finished before the kill, as recorded.
- `-007` passed every product check and failed the probe's own comparison of
  earlier jobs, whose baseline still held the addition it closed.

A build installed between `ee442ce` and `f7dff36` was made from uncommitted
changes by mistake and labelled with the previous revision; nothing was
recorded against it, and it was replaced by a clean build before any run.

After the runs the four volumes keep left were confirmed unnamed by any
definition on the connection and deleted with `virsh vol-delete`. The accepted
disks stay attached to the test VM.

## Moving the guest's own boot disk

The same Ubuntu guest boots from `sda`, a 3.5 GiB qcow2 volume at boot order 1.
Move requires the VM to be stopped; these runs move the disk the operating system
boots from, then boot it.

| Evidence | Build | Move | Result |
| --- | --- | --- | --- |
| `disk-move-boot-system-native-001` | 1.0.0-beta.8 (`86171b5`) | `sda` to the other pool, original deleted | **passed**: copy verified, definition names the copy at `sda` with boot order 1, the VM started from it and, 90 seconds later, shut down gracefully in 2.2 s |
| `disk-move-boot-system-native-002` | 1.0.0-beta.8 (`86171b5`) | `sda` back to its first pool, reusing the volume name the first move freed, original deleted | **passed**, and while the VM ran libvirt counted 236,462,592 bytes read and 4,959,744 bytes written on the moved disk |

A graceful shutdown already shows an operating system was running: a guest
without one ignores the request, and the stop fails after 60 seconds. The write
counter in run 002 shows it directly, because firmware only reads a disk and a
booted operating system also writes to its root filesystem. The probe reads the
counter with `virsh domblkstat` while the guest runs and requires both to be
non-zero (`--require-guest-writes`).

Other VMs, prior jobs and source media were unchanged in both runs.

## Not covered

- Deleting an unused volume on a connection whose images the user cannot read.
  That needs a privileged, read-only way to prove the volume is unused, which is
  a separate design decision shared with VM removal.
- Moving a disk while its VM runs. Move requires a stopped VM.
