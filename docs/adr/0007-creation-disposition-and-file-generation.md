# ADR 0007 — Creation disposition and file-generation checks

Status: accepted routine implementation decision. Documents 07 and 10 require
reference reconciliation, explicit disposition and durable intent; documents 03,
04 and 14 require shared interface access, failure behavior and scoped evidence.
These rules take precedence over treating a returned libvirt file-volume key as
exclusive ownership. No locked scope or owner review point changes.

A libvirt file-volume key can be a path and that path can be reused. Creation now
captures an ordinary file's Linux statx device/inode/birth-time generation and pins
the pool directory generation in its reviewed target. Complete type, link, inode,
size and timestamp fields are required. Files must have one link and no symlink
components. Upload, verification and definition observation compare the recorded
generation where present. Older receipts without one remain readable, but cannot
authorize cleanup deletion. If allocation succeeds and a later identity read fails,
the coordinator preserves any returned exact key/path without marking it verified.
Filesystems lacking these observations fail closed; helper-mediated access and
real filesystem qualification remain work.

Cleanup is a separate reviewed recovery operation with an explicit `retain` or
`delete` input. It inherits every original lock atomically; deletion adds locks for
the native storage pools and domains in its proof. The original operation stays
partial and links to the child. Retention commits durable pins, including unknown
candidates. Deletion applies only to the entire specifically reviewed new-volume
set, leaving source artifacts, VMs and firmware untouched. A retained volume set is
not implicitly released for later GC. Closing a recipe is durable and prevents
future definition recovery or created-VM catalog reconciliation from that recipe.

Deleting a file with no direct VM attachment is insufficient. The adapter combines
native XML, managed-save/snapshot/checkpoint XML, native pool registrations, pool
directory entries, safely inspected image metadata, persisted records and every
unresolved job. It refuses incomplete graphs and aliases. Every image, including selected candidates,
must be independently inspectable as inactive raw/qcow2 and have declared backing
edges resolved against the graph. Pool capacity changes caused by deleting a
candidate are excluded from the configuration digest; unrelated configuration,
file generation/content metadata or reference changes invalidate review. A selected
candidate referencing another candidate also blocks deletion: failure halfway
through a batch must not orphan a surviving dependent. Corrupt/incompletely
written images whose metadata cannot be safely read require retention, pending
a more specific recovery adapter.

The pinned bubblewrap 0.12.0 supports `--ro-bind-fd`. The single-file inspector
passes a held read-only file to that facility and exposes it as `/source/image`.
No parent directory or backing path is mounted. The fixed system qemu-img runs
`info --output=json -f FORMAT /source/image`, without `--backing-chain` or `-U`.
Dependencies are reported, then resolved by the native adapter. The worker has a
30-second deadline, 60-second CPU bound, 64 descriptors, two-worker admission,
2 GiB address-space limit and bounded output. Existing plugin limits are unchanged.
This follows the installed tool's help and the primary
[QEMU utility documentation](https://www.qemu.org/docs/master/tools/qemu-img.html)
and [bubblewrap implementation](https://github.com/containers/bubblewrap/blob/main/bubblewrap.c).
Generated-file tests prove missing-backing reporting, held-file behavior, read-only
access, closed source descriptors and hidden siblings/sockets on this environment.
They do not prove storage-driver deletion or guest behavior.

This initial graph adapter deliberately refuses all active guests and unsupported
or inaccessible storage graph portions. It does not run offline tools on active
images or assert an absent dependency from an incomplete cache. Qualified live
graph adapters, other mandatory formats/storage workflows, complete template/GC
integration and a later explicit retained-pin disposition remain required. Future
writers must lock every source/destination pool and referenced VM and persist their
actual volume keys/paths in manifests; opaque untracked indirections cannot bypass
cleanup. New pool/alias/lifecycle adapters must join this graph contract before
being advertised alongside destructive collection.

Every deletion intent precedes its native request. Failure retains the remaining
candidates and inherited locks. Reconciliation checks observed absence after
recorded intent, never replaying deletion. A fresh cleanup review is needed when
resources remain. The native API cannot provide exclusive ownership against all
external writers; document 10 explicitly recognizes these races. Rechecking the
critical step and reconciling its result is required, while the actual race/fault
matrix remains blocked on an authorized disposable environment.

SQLite schema 3 supersedes ADR 0006's schema-2 barrier. Opening versions 1 or 2
writes a consistent, private, flushed `journal.db.pre-v3-UUID.db` backup before
upgrading. Earlier binaries must not ignore pins or resume a disposed recipe.
Tests cover both migrations, retention, deletion, partial failure, cancellation,
stale generations/graphs/references, old unresolved jobs, lost acknowledgement
and reopening the journal. Versioned disposition/proof records reject future
semantics. No host pool, VM, network, firmware, service or device was mutated.
