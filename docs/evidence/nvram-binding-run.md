# Creation NVRAM declaration binding evidence

This implements [ADR 0021](../adr/0021-creation-nvram-declaration-binding.md)
as a prerequisite for IMP-07, SNAP-01 and JOB-02. Shared CLI/TUI result and review
coverage also contributes to UX-03 and REL-03. No acceptance scenario is promoted.

New UEFI recipes require a versioned, durable declaration binding before marking
definition complete. The record ties the original plan, input, operation and VM
identity to the reviewed firmware digest and observed NVRAM mapping. Repeated
observations cannot replace it. Legacy fieldless recipes retain their historical
semantics and visibly report `legacy-unbound`. Declaration binding opens no
firmware/TPM state file and does not verify initialization, freshness or recovery.
The [operator guide](../creation-nvram-binding.md) documents these stages.

## Integrated checks

`nvram-binding-core-001` passed the complete Go race suite with private IPC and
generated disk tools explicitly required. Its recorded source digest is
`92fe1cfeed2f61dddad9872fbec73cd84eaec69b155ed012fecd6989c0de8cae`;
HEAD was `4c5817674e8c036081715237ad1e768869f377c5` with the binding changes
in the working tree. The log records its actual revision and input digest.
Native VM creation and firmware execution were not part of this local check.

The lifecycle tests exercise the real creation service, engine and private SQLite
journal with injected native observations. They cover definition acknowledgement
loss, reopen/reconciliation, first binding, identical repeats, changed mapping,
publication failure, cancellation, legacy recipes and missing required records.
The validator tests cover complete exact-case JSON shape, identity and digest
bindings, firmware mappings and canonical paths. CLI/TUI tests use the same
application service and journal; mutating backend fixture calls refuse execution.
They cover stored review reuse, required acknowledgements, canceled approval,
progress/error/legacy results, discarded stale success and machine output.

Two issues found during integration were corrected: a persisted cancellation
request could be overlooked during inspection, and recovery/result reads did not
recheck immutable plan/input digests before interpreting the recipe version.
An active canceled inspection now remains uncertain. A later explicit reconcile
may observe an unchanged accepted recipe even after its original expiry, retaining
the cancellation history. Changed plan or recipe bytes fail before observation.

The subsequent compatibility review reproduced a negative journal-step panic in
`nvram-reconcile-negative-step-001`. That failed evidence is retained. The parent
added the missing lower-bound check so malformed steps return an error before a
handler call. Post-fix compatibility and final integration results are recorded
separately; the earlier passing full suite is not evidence for this later fix.

`nvram-result-child-integrity-001` reproduced a separate result-read gap: a
successful recovery child could still return completion after its own plan or
recipe changed, because only the referenced original was checked. Result reads
now check both. `nvram-result-child-integrity-003` passed both race regressions,
including unchanged native call counts, binding bytes, child job and locks, and
successful reads after restoring the exact original journal bytes. The intervening
`-002` failed in the test's restoration step: the test serialized its caller-owned
plan after Apply had sorted the shared acknowledgement slice. The fixture now
restores the raw saved body; no production workaround was added for that test bug.

`nvram-recovery-integration-001` then passed the combined creation binding,
validator, child-result and common recovery race regressions, including the final
recovery-review fields. `nvram-binding-vet-001` passed static analysis of all core
packages. The [package-input audit](../reviews/repository-hygiene-20260908.md)
also led to tracked-file-only source selection, no-follow file handling and exact
output inventories. `nvram-package-inputs-001` passed all 20 temporary Git/worktree
and generated package-orchestration tests. Its RPM subprocess is simulated;
actual RPM/DEB rebuild and installation evidence remains separate.

## Native and release limits

The existing six disposable guests remain stopped and preserved. The installed
4c58176 build predates this binding implementation. Its prior native two-boot
TPM/NVRAM marker results are described in
[the native firmware run](cold-fixes-native-run.md); they cannot verify the new
binding code. A separate new, never-booted guest is planned for deterministic
receipt-publication failure and declaration-change/reconcile checks. Injected
SQLite failure is distinct from a process crash or host power loss.

Complete auxiliary capture, durable complete-set publication, encrypted backup,
independent restore, guest encryption recovery and the remaining native matrices
are still required. All 71 acceptance scenarios remain unaccepted.
