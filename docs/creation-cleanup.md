# Disposing failed-creation resources

`vm creation cleanup` closes an unresolved creation recipe with an explicit
resource disposition. It does not create, start, stop or undefine a VM. A successful
cleanup leaves the original creation operation `partial`, linked to its recovery
operation. Inspect that recovery ID with `vm creation result` and `operation show`.
This is development functionality: actual qemu storage-driver deletion, filesystem
labels, host faults and cross-tool races still require an authorized disposable
environment. The release checklist remains open.

If the VM definition might already exist, first use `operation reconcile`.
Cleanup requires the original UUID and name to be absent. It refuses a receipt
whose definition was already observed. For a complete verified copied disk set,
[definition recovery](vm-creation.md) remains an alternative to abandoning creation.

Preview either disposition with the original actor and connection:

```sh
virmill vm creation cleanup FAILED_OPERATION_ID \
  --connection qemu:///session --input '{"disposition":"retain"}' \
  --plan --output json --non-interactive
```

`retain` records durable pins for every reserved candidate, including volumes
with missing generation identity or uncertain ownership. It preserves all files,
closes the original recipe and releases operation locks after the pin record
commits. Pins are not automatic collection candidates. Later removal of pinned
resources needs a separate explicit disposition workflow; this initial adapter
does not silently turn retention into deletion. The selected pool must still be
observable and its directory accessible; unreachable pool/helper recovery remains
separate work.

For a deliberate deletion preview, change the input to
`{"disposition":"delete"}`. Review the exact volume keys, paths, filesystem
generations, dependency digest, preservation list and risks. A reserved name,
matching path, content hash or matching VM label is insufficient deletion authority.
Original appliance media and prepared files are retained. An allocation that
returned a key/path but failed its generation check stays `unknown` and cannot
be deleted by this adapter. Missing volumes are distinguished from unidentified
ones; native inventory absence alone is not filesystem absence.

Apply the returned plan using `plan apply PLAN_ID`, its exact `--digest`, a new
`--idempotency-key`, and each listed `--ack`. Both dispositions require
`inherit-recovery-resources` and `abandon-creation`. Retention also requires
`retain-partial-volumes`; deletion requires `host-mutation` and
`delete-new-volumes`. No deletion or lock transfer occurs during preview.
Host application tests must use explicitly authorized disposable resources.

The TUI exposes **VMs → vm creation cleanup**. Enter
`{"id":"FAILED_OPERATION_ID","input":{"disposition":"retain"}}` or the
explicit `delete` choice. It calls the same service and displays the same plan.
Press `a`, review the acknowledgements and enter the full plan digest to apply.
Esc discards the form or approval; detaching does not cancel an accepted operation.
Full guided forms remain part of the mandatory UI work.

Deletion checks every visible native pool and inactive domain, managed-save XML,
snapshot/checkpoint XML, persisted application records and unresolved jobs,
including jobs older than the recent-history display limit. It holds all original
locks plus the observed storage-pool/domain locks. It reconciles registered files
with pool directory entries and independently reads retained raw/qcow2 metadata
inside an unprivileged sandbox. The worker receives only one held read-only file;
it cannot open sibling/backing files and never ignores writer locks. Each declared
backing edge is resolved separately against known objects. Any image, including
another selected candidate, referencing a selected candidate blocks deletion.
Uninspectable partial/corrupt copies require retention until a dedicated recovery
adapter can prove their dependencies.

The current deletion proof refuses active guests, inactive/inaccessible/non-file
pools, unlisted entries, ambiguous aliases, missing backing objects, unsupported
formats, dirty/encrypted/corrupt images, and auxiliary storage outside reconciled
pools. It is bounded to 128 pools and 4096 volumes, with at most 4096 entries per
pool. Live block-graph reconciliation, other storage adapters, full template/GC
workflows and explicit release of retained pins remain required development work.
These restrictions are not evidence that the full storage acceptance scope passes.

Every delete intent is durable before the single native request. The adapter
rechecks file generation and dependencies immediately before that request, then
observes absence. A failed request, lost acknowledgement or cancellation retains
the remaining resources and all inherited locks. `operation reconcile` observes
only; it never repeats deletion. If volumes remain, review a fresh cleanup plan
using the failed cleanup operation ID. It can dispose the remaining candidates or
retain them. Already absent volumes are not deleted again. A committed disposition
prevents the old creation recipe from being resumed or cataloged as a created VM.
External writers can still race the last observation; native fault/race tests are
blocked until a disposable host is designated.

Opening an older schema-1 or schema-2 journal creates and flushes a private
`journal.db.pre-v3-UUID.db` backup, then upgrades to schema 3. Version 3 requires
retention pins and closed-recipe semantics as well as inherited recovery locks.
Earlier binaries must refuse it. Keep the backup for deliberate operator recovery;
restoring it can discard newer effects and is not an automatic downgrade procedure.

## Observe cleanup progress

Use `vm creation result CLEANUP_OPERATION_ID` or **VMs → vm creation result**.
Queued, validating, running and verifying cleanup jobs return a normal progress
observation with `complete: false`. `cleanupProofAvailable` and
`dispositionAvailable` distinguish records that exist from null values. Follow
`operation watch` while work continues; an active result does not ask you to
reconcile an operation that is still executing.

Only a succeeded cleanup with valid, matching proof and disposition can report
`complete: true`; deletion also requires every recorded absence confirmation.
This means the cleanup finished, with `vmCreated: false`. Invalid or uncertain
results remain errors. Reading progress never deletes a volume, releases locks
or retries cleanup. See [ADR 0016](adr/0016-active-creation-result-observations.md).
