# Moving a VM's disk to another storage pool

Probe: `tests/fixtures/release/disk_move_native.py` ([ADR 0062](../adr/0062-disk-grow-add-move.md)).
Every change is a reviewed CLI plan applied as a detached job. After the job the
probe reads the copy's capacity from libvirt, read-only, checks the saved
definition names the copy at the same target with the same bus and drive port,
checks the original is gone unless it was kept, and starts and stops the VM.

## Runs

| Run | Outcome | What happened |
| --- | --- | --- |
| `disk-move-session-native-001` | failed in the copy, with nothing stranded | Moving `sdd` of a session VM from one pool to another. The copy volume was created in the destination pool and the copy itself failed. Everything around the failure behaved as designed: the job read **`failed`** rather than `recovery-required`, so the VM's locks were released; `sdd` still pointed at the original; the VM stayed stopped; and the refusal named the unused copy as the thing to delete. Only the message was useless. |
| `disk-move-session-native-002` | failed in the same place, but said why | With every stream call carrying its own refusal, the job reported `copying the disk failed: reading the disk stopped early` with `native: EOF` in its details. That named a defect in the copy loop itself, described below. Nothing was stranded again, and the second unused copy was deleted. |

| `disk-move-session-native-003` | the move passed; the probe's later power step was refused | The move itself completed and was confirmed on the host: job `d512e3db` read **`succeeded`**, `sdd` pointed at the copy `…-disk-000.qcow2` in `virmill-pool-62e06457` at the same target and drive port `3`, the copy held the disk's 1 GiB capacity, and the original `…-disk-003.qcow2` was gone from `virmill-pool-62e06457n`. The probe then failed starting the VM, for a reason unrelated to disks: see below. |

The third run is what qualifies the move. Its reviewed plan named both pools,
both volume names, the 1 GiB capacity, the destination's free space, and
`diskDeletion: true`, and asked for `data-loss-delete-old-copy` beside the three
writer acknowledgements. Everything the probe checks after the job passed: the
copy's capacity, the definition naming the copy at the same target with the same
bus and port, the original absent from its pool, no other volume in the source
pool touched, and the VM still stopped.

| `disk-move-session-native-004` | the move passed; a probe check was wrong | Moving `sdd` back to `virmill-pool-62e06457n`. Job `e675cce8` read **`succeeded`** and the host was correct: `sdd` took the name freed by run 003 (`…disk-003.qcow2`) and the original `…disk-000.qcow2` was deleted from the other pool. The probe then refused its own result: it tested the original's bare volume name as a substring of the definition, and `…disk-000.qcow2` still appears there as **`sda`'s** volume in the other pool. A volume name is not unique across pools, so the check now compares pool and volume together. |
| `disk-move-session-native-005` | **passed** | Moving `sdd` out to `virmill-pool-62e06457` again, with the corrected check. The copy was written and verified, the definition named it at the same target with the same bus and drive port, the original was deleted from its pool with no other volume there touched, and the VM then **started and was forced off with the moved disk** (`bootedWithMovedDisk`). Other VMs, prior jobs and source media were unchanged. |

This qualifies Move on the session connection, in both directions, including
reclaiming a name a previous move had freed.

The step that failed in run 003 was `vm start`, refused with
`UNSUPPORTED_CAPABILITY: nothing has started libvirt for your own user yet`.
That is the per-user libvirt refusal working as designed, and it fired because
the install restarts the coordinator while no per-user `virtqemud` was running:
the daemon runs with `--timeout=120` and had already exited. It says something
about the host, not about the move.

It also shows the refusal is impractical as it stands on this kind of host.
Fedora ships no per-user `virtqemud.socket` unit, and the daemon exits two
minutes after it is last used, so the coordinator is nearly always the process
that would fork it — which means a session VM cannot be started until someone
runs a libvirt client by hand. The advice is accurate but asks the user to do
that repeatedly. Making the coordinator start the daemon outside its own service
cgroup, so the hardening stays and no manual step is needed, is recorded as
outstanding.

## Why the copy failed

The Go libvirt binding reports the **normal** end of a stream as `io.EOF` from
`Recv`, and the copy loop treated any non-nil error as a failure. So the copy
failed at the end of every disk, however well it had gone: the bytes were read,
hashed and written correctly, and then the successful end of the stream was
reported as a broken read. Creation's own upload never hit this because it uses
the callback forms, which absorb the end of a stream internally.

Reading the binding also showed two further conventions the loop had wrong. A
stream with nothing ready yet answers `-2`, and one positioned inside a sparse
hole answers `-3`, both with **no error**; the loop would have treated those as
neither progress nor failure and spun. `Send` uses the same conventions, so a
would-block during a write was treated as the destination refusing the copy.

The loops now break only on `io.EOF`, retry on `-2` and `-3`, and fail only on a
real error. The streams are opened blocking, so the would-block cases should not
arise; handling them is the difference between a hang and correct behaviour if
they ever do.

## What the first run exposed

The job's error read `OPERATION_FAILED: the disk could not be copied`, and
nothing else. That was a defect in this code, not in the host: a raw libvirt
error was flattened into a single fallback sentence, so the one copy of the real
cause was discarded. It is the same mistake that made the first four disk
additions unreadable, repeated in a new place.

Two fixes followed, and a third that would have bitten a perfect copy:

- Every stream call now carries its own refusal — opening either stream,
  opening the disk for reading, opening the copy for writing, reading, writing,
  and finishing each — and keeps libvirt's own text in the failure details. A
  cause that is not already a refusal keeps its message rather than being
  replaced.
- The read-back compared the whole destination volume against the bytes the
  copy wrote. A copy is made in a volume sized for the whole disk, so reading it
  to the end returns the volume's length, not the copy's: a correct copy would
  have been reported as differing. It now reads back exactly the bytes that were
  written.
- The volume made for a copy is sized with the same overhead creation already
  allows between an image's virtual size and its file, so a densely written
  qcow2 cannot overflow the volume made for it.

The unreferenced copy the first run left behind was deleted by hand, which is
what its own refusal recommended.
