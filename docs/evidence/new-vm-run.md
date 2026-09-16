# New VM: from a file to a running VM with one confirmation

Probe: `tests/fixtures/release/new_vm_walkthrough_probe.py` ([ADR 0065](../adr/0065-new-vm-flow.md)).
It runs on the authorized Fedora 44 test VM through the installed packages, in a
private `qemu:///session` that starts with no storage pools, networks or VMs,
under a private virtual display. It drives the real TUI and saves every screen.

## Result

`new-vm-walkthrough-native-005` **passed** on build `9a2f6dd`, installed over
the beta.5 packages with guests, networks, source-media metadata and the helper
policy preserved. Each case chose a generated file, then pressed only the keys
counted below.

| Case | Keys after choosing the file | Confirmations | Raw identifiers on screen | Progress | VM | Viewer |
| --- | --- | --- | --- | --- | --- | --- |
| qcow2 disk image, no storage pool yet | 3: `n`, Create VM, Confirm | 1, including setting up the pool | none | same screen | running | started |
| ISO installer | 3 | 1 | none | same screen | running | started |
| OVA appliance | 3 | 1 | none | same screen | running | started |

The baseline before this change (`new-vm-baseline-native-001`) needed 72
keypresses, two reviews and ten checkboxes for the disk image case, and never
offered the display.

Also observed:

- The first confirmation began "First, Virmill creates storage pool default in
  …" and the pool was active in the private data folder afterwards.
- Jobs: exactly one `storage.pool.create`, one each of `import.prepare-disks`,
  `import.prepare-install` and `import.prepare`, and three each of
  `vm.create.devices-v1`, `vm.start` and `import.discard`, all succeeded. The
  three `vm.hard-stop` jobs are the probe's cleanup.
- Every prepared copy was removed; the VMs kept their own disks.
- The host's own pools, networks and VMs on both connections were unchanged.
- The ISO settings page suggested a 24 GiB disk with a note, because the host
  had about 31 GiB free (below).

## The system connection, and what the viewer shows

`new-vm-system-native-002` walked the host's own `qemu:///system` on build
`c5a5290`, through the installed user coordinator, with a virtual display.
That host has several active storage pools, none named `default`, and libvirt's
default NAT network.

| Case | Keys after choosing the file | Confirmations | Raw identifiers | Storage | Network | VM | Viewer window |
| --- | --- | --- | --- | --- | --- | --- | --- |
| qcow2 disk image | 3 | 1 | none | the one pool that starts with the host | default NAT, connected | running | open, titled with the VM's name |
| ISO installer | 3 | 1 | none | same | same | running | open |
| OVA appliance | 3 | 1 | none | same | none (the appliance has no adapter) | running | open |

The probe waited for each viewer's mapped window and saved a screenshot of the
display. Each shows a Virt Viewer window titled with the VM's name, showing
that VM's live firmware screen: SeaBIOS, then "Booting from DVD/CD" for the
installer, and "No bootable device" because the generated images hold no
operating system. So the display does not just start a process: it attaches
and draws the guest.

Run `001` of this probe stopped before starting, because listing volumes in a
pool this user cannot read failed; the probe now records such a pool as
unreadable.

Build `c5a5290` made two changes for this host. New VM preselected no pool when
several were usable and none was named `default`, which sent the user to
Advanced settings. It now preselects the only pool that starts with the host, or
else the one with the most free space, and names it on the page. And `v` opens
the selected running VM's display from the VM list or its details, with the
footer saying so.

**Cleanup found a separate gap.** Removing each VM together with its disks was
refused with `RECOVERY_REQUIRED: disk source outside reconciled storage pools`.
Before deleting a volume, removal proves that no VM on the host refers to it,
and other guests on this host use disk files that are not in any pool. The
three VMs were removed through reviewed plans that keep disks. Their four
volumes, and nothing else, were then deleted with `virsh vol-delete`, and the
connection was back to its original 23 VMs.

Build `3657212` compares those out-of-pool files by device and inode instead
([disk removal beside files outside pools](disk-removal-outside-pools-run.md)).
Removal with disks passed natively in a private session. On this host's system
connection it still stops, now because the ordinary user cannot read root-only
pool images.

## Runs that did not pass

- `001`: the probe asked for a 120x40 terminal, which its terminal helper does
  not support. Nothing ran.
- `002`: the probe kept the file-name filter but never selected the file.
- `003`: the settings page, Create VM and the confirmation all worked, and the
  pool was being set up; the probe mistook the confirmation, still shown while
  its jobs were accepted, for a second one.
- `004`: the disk image case passed in three keypresses. The ISO case found two
  product faults. Preparing an installer reserves the disk's full size plus a
  margin (about 40 GiB for the usual 32 GiB disk), the host had 31 GiB, and the
  refusal was written to a field the New VM page did not show, so nothing
  appeared to happen. Build `9a2f6dd` shows the refusal in plain words and
  defaults the installer disk to the largest common size that fits, with a note.

## Not covered

- The private session has no virtual networks, so every case ran with no
  network. An appliance whose adapters need a network, on a host with no active
  network, still needs Advanced settings.
- A host with no storage pool on the system connection; that case ran on the
  session connection only.
- A real desktop session. The viewer ran on a virtual display; the screenshots
  show the window and the guest's screen, not how a desktop places it.
- Deleting a New VM's disks on a system connection whose pool images only root
  can read (above).
