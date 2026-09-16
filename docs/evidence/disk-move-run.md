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
