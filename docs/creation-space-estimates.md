# Creation space estimates

New `vm.create` and `vm.create.devices-v1` plans use the same creation estimator.
`plan.estimates.additionalBytes` reports the target-pool free-space budget already
published as `plan.review.requiredFreeBytes`. It is the existing conservative
disk/media headroom policy, not a measurement of initial physical allocation:

```text
pool budget = 64 MiB
            + sum(virtual disk bytes + floor(virtual disk bytes / 4) + 16 MiB)
            + sum(exact read-only ISO bytes)
```

For example, one 32 MiB virtual disk requires a reviewed pool budget of
125,829,120 bytes (120 MiB). A small sparse QCOW2 file can have a much smaller
copied payload while retaining that same virtual capacity and pool budget.
The estimate notes separately report `sum(volume.fileBytes)` as copied payload.
They do not label either figure as measured physical allocation. Filesystem
overhead and firmware/TPM state are not separately measured, and the estimate is
not a reservation or a guarantee against external writers consuming space.
Already prepared source files are retained and excluded from additional bytes.

Provisioned creation counts the generated NoCloud ISO exactly once in both the
pool media budget and the copied payload. The notes also expose the existing
private seed-cache free-space budget:

```text
seed cache budget = maximum ISO bytes + 3 * maximum content bytes + 1 MiB
                  = 16 MiB + 3 * 1 MiB + 1 MiB
                  = 20,971,520 bytes (20 MiB)
```

The cache budget is separate from `additionalBytes`; cache and pool locations
may share a filesystem. The numbers describe the existing separate checks, not
a combined filesystem reservation. The estimator neither creates a new seed
preview nor invokes the seed generator. Normal planning retains its existing
bounded preview and cleanup behavior.

Creation defines a new stopped VM and does not interrupt an existing guest, so
`requiresDowntime` is false. No elapsed-time estimate is supplied. The plan notes
make that limitation explicit; registration does not establish guest boot or
provisioning completion.

Before publishing an estimate, the handler strictly decodes the persisted
recipe, checks its creation/device-policy version, and reconciles the complete
disk/media/seed mapping and sizes against volume intent. Counts and source sizes
remain bounded to the current native creation baseline. Checked arithmetic
refuses overflow, and the recomputed budget must exactly equal `requiredBytes`.
Missing, duplicate, contradictory or excessive accounting returns an error and
a zero estimate. Cancellation also returns no estimate. The calculation makes
no provider, source-loader, filesystem or generator calls.

The operation engine includes estimates in the immutable plan digest and saves
the plan only after successful validation, review and estimation. Previously
saved plans are not rewritten or re-estimated by this change. Allocation,
upload, definition, free-space checks and retained-resource recovery semantics
are unchanged.

The CLI includes the shared budget and notes in table, JSON and NDJSON output
from `vm create`; `plan show PLAN_ID` retrieves the same stored estimates and
plan digest. Table mode currently presents the indented response envelope.
The TUI exposes the same plan through **VMs → vm create** and
**Jobs → plan show**. Use PgUp/PgDn to read the complete estimate notes at an
80×24 terminal size. Neither view recalculates the budget.

Applying a plan requires its exact reviewed digest and acknowledgements. In
the TUI, press `a`, then enter the full digest; Esc cancels confirmation without
submission. Errors and canceled plan reads do not expose a successful estimate
or leave a TUI plan armed for approval. A transport failure also discards any
previous successful response supplied alongside the error.

Tests cover the reported 125,829,120-byte case, ordinary and provisioned plan/
review comparisons, installation media, legacy and devices-v1 recipes,
accounting corruption, overflow, cancellation, plan persistence refusal and
absence of provider mutations. These are STO-01, UX-03 and REL-03 implementation
prerequisites. Synthetic coordinator tests do not qualify actual pool behavior,
permissions/labels, guest data preservation, throughput or firmware state.
The CLI/TUI integration tests exercise production creation planning, estimate
generation, shared-service dispatch and journal persistence with injected source
and backend observations. They verify output, paging and digest reuse, then
refuse apply at preflight so no provider mutation or job is created.
