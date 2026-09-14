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

## Not covered

- OVA and ISO sources, the `default` NAT network adapter (the session connection
  has no networks) and Create or Start pool inside VM setup.
- Closing the TUI mid-chain (the design stops the chain; not exercised natively).
- Guest boot, which a blank disk cannot show.

The software tests had used a preparation plan with a libvirt connection. They
now use the real host-local value.
