# ADR 0057: One approval to import, create and start a VM

Status: accepted; implementation in progress.

Importing a disk image needed two or three separate approvals: image
preparation, VM creation and starting. The owner asked for one. A single
server-side operation was considered and deferred. Creation's plan binds the
prepared disks' sizes and SHA-256 digests, which exist only after preparation;
`Engine.Plan` validates immediately; and creation's recovery, cleanup, acceptance
and result paths all decode its own plan input. Binding those facts at run time
would reopen every creation recovery path for no visible difference to users.

The durable operations stay as they are: `import.prepare*` (ADR 0005), `vm.create`
(ADR 0006) and `vm.start`, each with its own plan, journal, receipt and recovery.
The TUI shows one combined review when VM settings accompany an import. It shows
the real preparation plan, the VM settings creation will use (name, CPU, memory,
pool, firmware, disks and buses, networks and cables), whether the VM starts
afterwards, and every consequence the later steps will ask for. One approval with
all consequences checked authorizes, in order:

1. applying the reviewed preparation plan;
2. after it succeeds, applying a creation plan requested with exactly the
   approved settings for that preparation;
3. if chosen, applying a start plan for the VM that creation verifiably defined.

A chained plan is applied only if its operation and connection are the expected
ones, it references the approved preparation or created VM, and every
acknowledgement it asks for was shown and approved. Anything else, such as a new
consequence, a changed pool or network, or a refused plan, stops the chain and
shows that step's ordinary review for a separate decision. Start remains a
separate, reported stage; guest boot is not claimed.

The approval exists only in the running TUI session. It is never stored on disk
or in the coordinator, and no other client can use it. Closing the TUI stops the
chain after the current job; the jobs themselves continue. Create VM later offers
the prepared images with the saved settings and the ordinary creation review.
The CLI keeps separate plans, which scripts can chain explicitly.

Preparation keeps its prepared copy for reuse, so a disk is still copied twice.
Reclaiming that space is a separate decision. A server-side combined operation
remains possible later if single-copy import is required. This amends the
two-review handoff in ADR 0037 and contributes to IMP-01, IMP-07 and UX-01
without claiming them.
