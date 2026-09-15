# Growing a stopped VM's disk on the test VM

Probe: `tests/fixtures/release/disk_grow_native.py` ([ADR 0062](../adr/0062-disk-grow-add-move.md)).
Every change is a reviewed CLI plan applied as a detached job. After each grow
the probe reads the volume's capacity from libvirt's volume XML, read-only, and
checks that the VM's definition did not change.

## First runs, build `1eae6f1`

- **`disk-grow-session-native-001`:** `qemu:///session`, VM `onestep-b3215d48`, pool-volume disk `sda`. It grew from 64 MiB to 1 GiB. The job succeeded and the volume reported exactly 1 GiB.
- **`disk-grow-system-native-001`:** `qemu:///system`, VM `noble-server-cloudimg-amd64 2`, pool-volume disk `sda`. It grew from 3.5 GiB to 5 GiB. The job succeeded and the volume reported exactly 5 GiB.

Both runs then applied a second plan that was made against the original size.
It was refused before a job was made, as it should be, but with
`SOURCE_CHANGED` instead of `STALE_PLAN`. The probe counts that as a failure.
A change of size on the same volume now makes the plan stale ("review again");
`SOURCE_CHANGED` is kept for a different volume. The runs stopped there, so the
second grow, the same-size refusal and the boot check did not run.

## Passing runs, build `bdffe06`

| Run | VM and disk | Grow | Stale plan | Same size again | Afterwards |
| --- | --- | --- | --- | --- | --- |
| `disk-grow-session-native-002` | `qemu:///session`, `onestep-b3215d48`, `sda` | 1 → 2 → 3 GiB, each confirmed in the volume XML | refused with `STALE_PLAN` before a job | refused before a job | definition unchanged |
| `disk-grow-system-native-002` | `qemu:///system`, `noble-server-cloudimg-amd64 2`, `sda` | 5 → 6 → 7 GiB, each confirmed in the volume XML | refused with `STALE_PLAN` before a job | refused before a job | started, then shut down gracefully |

Both disks are pool volumes, as Virmill creates them. Other VMs, prior jobs and
source media were unchanged. Limits: partitions inside the guest were not
checked, and the TUI form is covered by software tests, not these runs.

An earlier attempt did not start: the second install into the same stage
folder failed, because the installer creates its results folder only once.
Each install now uses a fresh stage.
