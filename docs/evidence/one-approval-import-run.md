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
| `one-approval-import-ova-native-006` | `ac1abe4` | **passed**, with guest boot |
| `one-approval-import-iso-native-003` | `ac1abe4` | **passed** |

With build `ac1abe4` each import ends by removing its prepared copy
([ADR 0058](../adr/0058-remove-prepared-copies.md)): one review of 7 (OVA) or 10
(ISO) items ran preparation, `vm.create.devices-v1`, `vm.start` and
`import.discard`, and the private imports folder was then empty. Both runs
suggested a free VM name ("… 2") because the stopped VMs of earlier runs kept
theirs. After the OVA VM started, the probe attached to its serial console,
reset it, and read `VIRMILL SECOND DISK PASS` from the generated boot sector.
That shows the guest booted from its first disk and read the expected marker
from its second, in the reviewed order. The ISO run took 77 s with the connected
NAT adapter; its installer guest was not driven.

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

## Cloud image

The same probe with `--cloud-source` imported Ubuntu 24.04's cloud image
(`noble-server-cloudimg-amd64.img`, 625 MB) on `qemu:///system`. The owner approved
the download to the test VM; its SHA-256 matched Ubuntu's `SHA256SUMS`. The typed
https address is recorded as declared provenance only
([ADR 0059](../adr/0059-guided-cloud-image-setup.md)). The probe generated a
throwaway Ed25519 key for each run and then logged in over SSH with it.

| Evidence | Build | Result |
| --- | --- | --- |
| `cloud-image-native-001` | `f83ef85` | failed: probe check. Clearing the key file field with Ctrl+U on an already empty field did not redraw, and the probe waited for one. The probe no longer waits |
| `cloud-image-native-002` | `f83ef85` | failed: **test host problem**. One approval ran preparation, creation, start and removal of the prepared copy. The guest booted and cloud-init finished from the NoCloud seed, but it got no DHCP address |
| `cloud-image-native-003` | `f83ef85` | stopped: the evidence recorder's default 120 s limit ended the ssh session; the probe was stopped by hand on the test VM |
| `cloud-image-native-004` | `f83ef85` | failed: probe check. Apply was refused with `RESOURCE_BUSY` because run 3's preparation of the same file was still running. The TUI kept the settings and the reviewed plan and said so; the probe did not read error lines and waited out its deadline. It now stops at the first error |
| `cloud-image-native-005` | `302e80d` | **passed**, with SSH login |

Run 2's guest reached its login prompt, and its serial console showed
`DataSourceNoCloud [seed=/dev/sr0]`. Its DHCP requests reached the `default`
network's bridge but got no answer. On the test host that network had started
before firewalld at boot, so libvirt never put `virbr0` in firewalld's `libvirt`
zone, and the default `public` zone dropped DHCP. Restarting the network, with no
guest attached, restored the zone. Virmill made no firewall change. `virmill
doctor` now reports this condition as **Network firewall**, with the
`firewall-cmd` command that fixes it (`network-firewall-native-001` shows it
ready on the test host).

In run 5, VM setup suggested **Cloud image: Yes** from the file name and
preselected BIOS, SATA and one connected e1000e adapter on `default`. Only the pool
needed a choice. The suggested name was `noble-server-cloudimg-amd64 2`, because
run 2's VM kept the first. One review of 13 items, including
`guest-root-provisioning`, `rotate-guest-host-keys` and `guest-passwordless-sudo`,
ran `import.prepare-disks`, `vm.create.devices-v1`, `vm.start` and
`import.discard` with no further review. The VM was running and the private
imports folder was empty 339 s after the review opened. The guest then took a
DHCP lease on `default`. The test key logged in as the reviewed cloud user,
`sudo -n true` succeeded, and `cloud-init status --wait` reported `status: done`.
The VM was hard-stopped through a reviewed plan and left defined. The image, other
pools, networks and VMs were unchanged.

Installs for these builds passed (`cloud-f83ef85-install-native-001`,
`doctor-302e80d-install-native-001`).

Left for review on `qemu:///system`: stopped VMs `noble-server-cloudimg-amd64`
(run 2) and `noble-server-cloudimg-amd64 2` (run 5) with their volumes and seed
media, and the downloaded image on the test VM.

## Not covered

- Create or Start pool inside VM setup, covered separately in the
  [streamlined setup record](streamlined-setup-run.md#pools-inside-vm-setup).
- Closing the TUI mid-chain (the design stops the chain; not exercised natively).
- Guest boot from the blank disk runs, which a blank disk cannot show. The OVA run
  showed guest boot on its serial console; the cloud-image run showed login.
- Cloud images other than Ubuntu 24.04, and cloud images on `qemu:///session`.

The software tests had used a preparation plan with a libvirt connection. They
now use the real host-local value.
