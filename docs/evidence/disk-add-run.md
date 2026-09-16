# Adding a disk to a stopped VM, and closing an unfinished one

Probe: `tests/fixtures/release/disk_add_native.py` ([ADR 0062](../adr/0062-disk-grow-add-move.md)).
Every change is a reviewed CLI plan applied as a detached job. After the job the
probe reads the new volume's capacity from libvirt, read-only, checks the saved
definition gained exactly the reviewed disk, and starts and stops the VM.

## What the first four attempts found

| Run | Outcome | Cause |
| --- | --- | --- |
| `disk-add-session-native-001` | failed | The volume was written and verified and the disk was defined, then the read-back was refused: it compared a byte-exact digest of Virmill's own XML, and libvirt reindents and requotes the definition it stores. Fixed in `d350ef1` with the normalisation-tolerant hardware digest that cold restore already uses, plus a check that the stored definition names the reviewed pool and volume. |
| `disk-add-session-native-002` | failed | Refused with `RESOURCE_BUSY` before doing anything: run 001's job stayed `recovery-required`, which keeps its VM and pool locks. |
| `disk-add-session-native-003` | failed | The same `RESOURCE_BUSY`; an attempt to clear the job by hand had edited a path that was not the live journal, so nothing was freed. |
| `disk-add-session-native-004` | failed | On the Ubuntu VM, whose VM and pool no job held, so lock contention was ruled out. The disk was defined correctly at `sdc` with the reviewed port, pool and volume, and the read-back was still refused. One shared message covered four checks, so `f9b5dd6` split it and made the digest mismatch report both digests. |

| `disk-add-session-native-005` | failed | With the locks free and the digest tolerant of formatting, the same read-back was refused, now reporting which check and both digests. The definition had gained `sdd` exactly as reviewed. |

Between runs 002 and 005 the additions themselves were correct on the host: the
volume existed and verified, and the definition named the disk. Only Virmill's
own confirmation of that was wrong.

## Why the read-back kept refusing

Reproducing run 005's comparison off the host, against the definition libvirt
stored, showed the two documents hold the same devices in a different order:
libvirt files a new disk among the other disks, while Virmill's insert appends
it to the end of `<devices>`. The hardware digest deliberately treats child
ordering as significant — it admits only attribute order, empty-tag spelling,
indentation at five containers and a `boot` child's position — so a
full-definition digest can never confirm a device *insert*, however the
formatting is normalised.

The confirmation is therefore not a digest of the result at all. The stored
definition is confirmed by *removing* the disk at the reviewed target and
expecting the digest the review bound for the definition before the insert.
That one comparison proves more than a disk list would: the definition holds
the disks it held before plus exactly the reviewed disk, nothing else changed
anywhere in the document, and it holds wherever libvirt filed the disk and
however it reindented, because the indentation left behind sits where the
hardware digest already ignores it. The disk itself must still name the
reviewed pool, volume and qcow2 format, and the VM must still be stopped
without saved state. The plan therefore no longer stores a digest of the
result; that field is gone.

A unit test in the ordinary suite now builds the definition the way libvirt
stores it, with the new disk filed among the other disks and indented, and
checks both halves: the removal gives back the reviewed definition, and the
whole-definition digest of that same correct addition does not match, which is
what refused runs 001 to 005. The host-pair diagnostic is kept as a reproducer,
now needing the definition from before the insert as well as the stored one, so
no host's XML is committed.

## Closing the two unfinished additions

An addition left needing recovery keeps its VM and pool locked, and only
proving the effect or adopting the job in a recovery plan releases them. Disk
addition had neither, so both stuck jobs held their VMs. `operation
dispose-disk-addition` was built for exactly this and closed both, on build
`7f14ab8`:

| Addition | Review | Result |
| --- | --- | --- |
| session, `onestep-b3215d48`, `sdb` | `referenced: true`, `volumeState: present`, `diskDeletion: false`; acknowledgements `inherit-recovery-resources` and `close-disk-addition` | disposition succeeded; the addition now reads `partial`, so its VM and pool locks are released |
| system, `noble-server-cloudimg-amd64 2`, `sdc` | the same, for its own disk and volume | disposition succeeded; the addition now reads `partial` |

The system disposition was refused on the first attempt, before `7f14ab8`:
accepting a disk asked for the host-wide deletion dependency graph, which
refuses whenever any VM on the connection has firmware state outside a storage
pool. The imported UEFI appliance and two auxiliary fixtures on that host have
exactly that. Accepting deletes nothing, so the graph is now built only when the
volume is unreferenced and present, which is the only case that can delete.
Deleting keeps the full guard and would still refuse on that host, which is what
the guard is for.

Both dispositions were taken as recovery actions rather than through the
evidence recorder, so they have no ledger entry; the outcomes above are what the
host reported.
