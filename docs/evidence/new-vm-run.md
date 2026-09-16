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
- Only the session connection. The owner's system connection uses the same
  flow and its own pool folder; it has not been walked natively yet.
- The viewer ran under Xvfb; that proves the process started and stayed
  attached, not what a person sees in the window.
