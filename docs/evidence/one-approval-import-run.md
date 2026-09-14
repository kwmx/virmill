# Native check of the one-approval import (ADR 0057)

Scope: the roadmap phase 1 import path on the authorized Fedora 44 test VM,
through installed packages and the real TUI on `qemu:///session`. This is not a
release and promotes no status. All 71 acceptance scenarios remain mandatory.

The probe `one_approval_import_probe.py` creates one small blank qcow2 disk in its
private stage and imports it with private client state and data folders. It
approves one combined review and checks that preparation, creation and start run
with no further review. It then hard-stops the new VM through a reviewed plan and
leaves it defined. A blank disk has no operating system, so guest boot is not
claimed.

## Runs

| Evidence | Build | Result |
| --- | --- | --- |
| `one-approval-import-native-001` | `433d964` | failed: probe check. The folder step was skipped correctly, but the TUI shortens the long private save path |
| `one-approval-import-native-002` | `433d964` | failed: probe check. Preparation was applied and succeeded; the probe mistook the still-visible confirmation for a second review and closed the TUI |
| `one-approval-import-native-003` | `433d964` | failed: **product defect**. The chain took its connection from the host-local preparation plan (`local`), so creation stopped at its review as "the connection changed". Fixed in `e6e4b38` |
| `one-approval-import-native-004` | `e6e4b38` | failed: creation ran with no further review and succeeded (VM `onestep-f7dc8e7e`, left stopped). While applying, the TUI showed a checkbox page the user never saw; the probe stopped there, before start. The page was removed in the next build |

| `one-approval-import-native-005` | `dfd87ae` | **passed** |

In run 5 the folder step was skipped, BIOS was suggested, and the probe chose an
existing active session pool. One review listed creation and start; checking its
7 items and applying once ran `import.prepare-disks`, `vm.create.devices-v1` and
`vm.start`, all succeeded, with no further review. The suggested VM name had no
file extension (`onestep-b3215d48`). The VM was then hard-stopped through a
reviewed plan and left defined. No other pool or VM changed.

Installs for these builds passed (`onestep-433d964-install-native-001`,
`onestep-e6e4b38-install-native-001`, `onestep-dfd87ae-install-native-001`),
preserving guests, networks, source-media metadata and the inactive helper policy.

Left for review on the test VM's session connection: stopped VMs
`onestep-f7dc8e7e` (run 4) and `onestep-b3215d48` (run 5) with their volumes, and
the prepared copies inside each run's stage.

## ISO and OVA sources

The same probe then imported an existing installer ISO on `qemu:///system`, whose
`default` NAT network exists, and a generated two-disk BIOS OVA on
`qemu:///session`. The OVA came from `tests/fixtures/import/multidisk-probe/build.py`:
a 16 MiB boot disk and a 3 GiB data disk on a VMware LSI SCSI controller, no
network adapters and no declared disk format. The 36.5 GB owner OVA was not used;
preparing and copying it would need more than the 93 GB free.

| Evidence | Build | Result |
| --- | --- | --- |
| `one-approval-import-iso-native-001` | `c61ffc3` | failed: probe wait. VM settings showed the suggested connected e1000e adapter on `default`. Planning hashes the 4.8 GB ISO, which took longer than the probe's 20 s screen wait. The TUI also kept a stale "Finish the highlighted VM setting" notice |
| `one-approval-import-ova-native-001` | `c61ffc3` | failed: **product defect**. The undeclared disk format was reported on the VM settings page, where it cannot be fixed. The next build checks the import first and keeps its problems on the import page |
| `one-approval-import-iso-native-002` | `254ca33` | **passed** |
| `one-approval-import-ova-native-002` | `254ca33` | failed: the format problem now showed on the import page as intended, but the unset choice was drawn blank (`<  >`), which the probe did not recognise and which does not tell a user a choice is needed. Unset import choices now read "Choose…" |
| `one-approval-import-ova-native-003` | `ef8af6d` | failed: **product defect**. The first Right press on the undeclared format chose `qcow2`, and the choice then disappeared behind Advanced disk options, so it could not be corrected to `vmdk`. Only a declared format now moves behind Advanced, and an undeclared one is suggested from the file name with a label |
| `one-approval-import-ova-native-004` | `d401a67` | **passed** |
| `one-approval-import-ova-native-005` | `38a249e` | stopped by hand: **product defect**. Preparation succeeded, but the suggested VM name was already used by the stopped VM from run 4, so creation was refused and the chain waited. VM settings now suggest the next free name (for example "… 2") with a label |

In the OVA pass both undeclared disk formats were suggested from their file
names (`vmdk`), SATA was suggested in place of the VMware LSI SCSI controller, and
BIOS was preselected. Only the pool needed a choice, since the session
connection has several and none is named `default`. The fixture has no network
adapters. One review of 6 items led to `import.prepare`, `vm.create.devices-v1`
and `vm.start`, exactly the three jobs of this run, with no further review. The
VM was hard-stopped and left defined; the OVA was unchanged.

Left for review: stopped VM `Kali Linux amd64 1` on `qemu:///system`, stopped
VM `Virmill generated BIOS multi-disk probe` on `qemu:///session`, and the
prepared copies and fixture OVAs inside each run's stage.

In the ISO pass, planning took 12 s. VM settings preselected BIOS, SATA and one
connected e1000e adapter on `default`; only the pool needed a choice, since the
system connection has several and none is named `default`. One review of 9
items led to preparation, creation and start in 85 s, with no further review.
The VM was hard-stopped and left defined; the ISO was unchanged. That run's report
lists the preparation as `import.prepare` because the probe then read the whole
job history; the job for an ISO is `import.prepare-install`. The probe now counts
only the jobs its own run creates.

## Not covered

- Create or Start pool inside VM setup.
- Closing the TUI mid-chain (the design stops the chain; not exercised natively).
- Guest boot, which a blank disk cannot show.

The software tests had used a preparation plan with a libvirt connection. They
now use the real host-local value.
