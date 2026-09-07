# Persisted reconciliation integrity: compatibility review

The new common `Engine.Reconcile` checks preserve recovery of previously accepted
recipes while refusing inconsistent plan/input digests before handler dispatch.
Review found one additional boundary defect: a negative persisted job step caused
an index panic. The parent fixed the existing step check to reject both negative
and out-of-range values. The focused reproducer now passes. No other production
change was made for this review.

## Reviewed boundary and compatibility

The shared `operation.reconcile` service dispatches to the operation engine.
The engine loads an interrupted or recovery-required job and its stored plan,
recomputes the plan digest and canonical raw-JSON input digest, selects the
operation handler, bounds the current step and binds the accepted job ID into
the observation context. It calls the handler's `Reconcile`, not `Execute`.
Handlers still have to establish the operation-specific completion predicate.
An inconclusive observation does not rewrite the job or release resources.

Expiry applies to new authorization. A previously accepted uncertain job may
need reconciliation long after the original plan expired; the integrity check
does not impose a new expiry requirement. JSON whitespace and equivalent escape
spelling do not change canonical input identity. The stored plan and input remain
unchanged during these checks; no schema migration or recipe upgrade is added.

Recovery children retain their own accepted plan and operation ID. Their recipes
may reference an original plan, but the engine does not replace the child with
that original or recursively invoke its handler. The creation resume adapter
separately loads the original through `originalReceipt`, checks original plan and
input digests, strictly decodes its recipe, and validates retained receipt
identity. Cleanup and retained-definition acceptance also use that original
receipt boundary. These adapter responsibilities remain separate from common
engine integrity. The compatibility fixture verifies dispatch and accessible
original references; it does not reimplement or certify a creation lifecycle.

An exact idempotency-key retry still returns its previously accepted operation,
including an uncertain operation whose reconciliation was refused. That read is
not new execution authority and does not turn corrupted input into an applicable
recipe. A changed request with the same key remains an idempotency conflict.
The unresolved operation's resource locks continue to reject another reviewed
contender.

## Regressions and existing coverage

[The existing integrity tests](../../internal/operations/reconcile_integrity_test.go)
already cover unchanged expired direct recipes, changed plan/input content,
duplicate input keys and malformed input. Existing operation tests also cover
actual local fixture-process interruption after a file effect, and the creating
package owns resume/receipt/lifecycle tests. Those scenarios were not duplicated.

[The new compatibility tests](../../internal/operations/reconcile_compatibility_test.go)
add:

- Canonical-equivalent raw input and indented plan bodies preserve the accepted
  digest and reach observation with the original raw input.
- An expired accepted recovery child retains its own context identity even when
  the caller supplies the parent operation in an outer context. Original-plan
  references remain intact, the parent stays partial, and the child retains
  inherited locks and its persisted cancellation request.
- Missing/malformed job or plan reads, unavailable handlers and invalid step
  indices fail without invoking an observer. Raw plan, job, event, lock, dedup
  and metadata rows remain identical after refusal.
- After integrity refusal, exact accepted retries preserve operation identity,
  changed retries fail, and a separately reviewed contender remains resource
  busy. No additional job, event or effect is introduced.

The new observer always returns an explicit inconclusive error. Its execution
method refuses effects and is asserted unreachable. These tests do not use a
generic successful observer to certify native recovery.

The negative-step regression initially failed with
`runtime error: index out of range [-1]`. The parent retained that pre-fix result
as evidence entry `nvram-reconcile-negative-step-001`, source digest
`23e1943245b6cf54f69431d9507d16a1873869cfb96d7d0a783dc2b46e433a5f`, with the
[original failing log](../evidence/logs/nvram-reconcile-negative-step-001.log).
The fix rejects the invalid step before indexing, leaves all journal rows
unchanged and calls no handler.

## Executed validation

The tested working tree is based on
`4c5817674e8c036081715237ad1e768869f377c5` and includes parent-owned integrity and
negative-step changes. File SHA-256 values at validation:

| File | SHA-256 |
| --- | --- |
| `internal/operations/engine.go` | `53cac28152fbfe75962d3545603c3eac05b76e4b66ed19cfa91160945a77d56b` |
| `internal/operations/reconcile_integrity_test.go` | `30beb24bbdd56f897730882d7cc2c6e3087f41d9937fcd3838b2491d0d11ca80` |
| `internal/operations/reconcile_compatibility_test.go` | `2a82594bd69c7dc462d792d8be474ee717c4dc85232f8c2f5895c3818a93d8bc` |

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor ./internal/operations -run '^TestReconcileCompatibility' -count=1
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race ./internal/operations ./internal/store -count=1
GOPROXY=off GOSUMDB=off ./scripts/go vet -mod=vendor ./internal/operations ./internal/store
```

All passed: focused compatibility package run 0.016 s; full race run operations
1.195 s and store 1.031 s; vet exit 0. The common operations test package also
compiled for `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0` using offline vendor
dependencies; `file` identified a Mach-O arm64 test executable. That binary was
not run. The compile checks source separation only: the foreign store rejects
coordinator operation, and the CGO-disabled SQLite build cannot establish runtime
database support. Temporary compile output was removed.

The existing user command remains:

```sh
virmill operation reconcile OPERATION_ID --connection qemu:///session --output json --non-interactive
```

Its ID is an accepted operation, not a new plan. In the TUI this is
**Jobs → operation reconcile**. The command and section match the
[generated help](../cli-reference.md) and shared action registry. Errors retain
uncertainty; operators must inspect the result rather than treat an expired
recipe or a returned idempotency match as proof of completion.

## Evidence limits

This review contributes local regression prerequisites to JOB-02 (observe
uncertain effects without replay), JOB-03 (accepted retry identity), JOB-04
(preserved resource ownership and contention refusal), and REL-03 (documented
reconciliation semantics). It adds no acceptance promotion. Real multi-client,
external-writer, crash/reboot, native/libvirt and guest recovery qualification
remain separate. Digests check consistency with the stored recipe; they do not
independently authenticate a journal rewritten together with its hashes.
