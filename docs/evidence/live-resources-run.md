# CPU and memory changed while a VM runs, and boot edits while running

Probe: `tests/fixtures/release/resources_live_native.py`
([ADR 0068](../adr/0068-live-resources-and-running-boot-edits.md)). Every run
used the authorized Fedora 44 test VM's `qemu:///system` and the installed
packages. Each part of the probe can be skipped, so one VM covers the live
changes and another covers the boot edit.

## Live CPU and memory

`resources-live-native-001` **passed** on build `b8daf1e`, against the stopped
Virmill-created Kali guest `Virmill qualification Kali QCOW2` (2 CPUs,
2048 MiB, a virtio memory balloon chosen at creation).

Virmill does not write spare CPU slots, so the probe asked libvirt for them
first: `virsh setvcpus --config --maximum 4` with the boot count left at 2, which
makes libvirt render the element itself. Everything after that is Virmill's own
reviewed path.

| Check | Observed |
| --- | --- |
| Next-boot CPU edit on a definition with slots | the stored definition became `<vcpu placement='static' current='3'>4</vcpu>` and matched the reviewed bytes, so the spare slots survived the edit; a second edit put the boot count back |
| Refused, with no job made | 4096 MiB: "This VM can use at most 2048 MiB until it is started again. Change its memory for the next boot instead."; 128 MiB: "A running guest keeps at least 256 MiB. Shut it down to give it less."; 5 CPUs: "This VM can run at most 4 CPUs until it is started again."; 2 CPUs: "This VM is already running 2 CPUs." |
| Memory down | reviewed `vm.resources-live-v1` with `guest-resource-pressure`, no downtime; the guest went from 2048 to 1536 MiB |
| Memory back up | 1536 to 2048 MiB, without that acknowledgement |
| One CPU in | the running VM went from 2 to 3 CPUs |
| One CPU out | back to 2, unplugging the CPU that had been plugged in while it ran |
| Saved definition | byte for byte unchanged after every live change |
| Afterwards | the VM stopped, its definition identical to the one it started with; other VMs, prior jobs and source media unchanged |

## Boot order changed while the VM runs

`boot-edit-running-native-001` **passed** on build `b8daf1e`, against the Ubuntu
24.04 cloud guest `noble-server-cloudimg-amd64`, which has no memory balloon and
no spare CPU slots, so the same probe covered the boot edit and the two refusals
that VM earns.

| Check | Observed |
| --- | --- |
| Refused, with no job made | more memory: "This VM has no memory balloon, so its memory can only change at its next boot."; more CPUs: "This VM is running all 2 of its CPUs: it was not started with spare CPU slots." |
| Boot order changed while running | the reviewed plan said the running VM keeps its current boot order; the saved definition became `sda`, `sdb` while the running VM still had `sda` alone, and the VM kept running |
| After a stop and a start | the running VM's boot order was `sda`, `sdb`: the change took effect at the next start |
| Afterwards | the original order restored through a second reviewed plan; the definition ended identical to the one it started with, and other VMs, prior jobs and source media were unchanged |

## Runs that did not pass

- `resources-live-native-001` on `17570bb` refused its own first memory change:
  the guest reached 1536 MiB a moment after the check, so the job went
  `recovery-required` as designed, holding its locks, and
  `resources-live-reconcile-native-001` then reconciled it — recovery observed
  the running size and the job succeeded without asking for anything again. That
  is a bad answer for an ordinary change, and libvirt exposes no balloon target
  to check instead, only the size the guest has acknowledged. Build `b8daf1e`
  asks, then waits up to 20 seconds for the running domain to report the reviewed
  size, and asks for the earlier size back if the guest never answers, so such a
  guest gets a plain failure instead of a recovery decision.

## Not covered

- A guest without a balloon driver, which is what the 20-second wait and the
  return to the earlier size are for; software tests cover that path.
- The session connection, and a VM whose definition was given spare CPU slots by
  something other than libvirt's own API.
- Whether the guest uses a CPU that was plugged in, or what it does inside itself
  with memory it gave back. Virmill reports what the running domain holds.
- Spare CPU slots chosen at creation, which Virmill does not write (ADR 0068).
