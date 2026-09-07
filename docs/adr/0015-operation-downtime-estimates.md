# ADR 0015: Operation-specific downtime estimates

Status: accepted implementation correction; no v1 scope or wire/schema change.

The native boot/media run exposed the engine's zero-valued estimates on VM
lifecycle plans. In particular, stop/hard-stop plans carried
`requiresDowntime: false` despite their explicit interruption warnings. Structured
estimates must agree with the reviewed action. This corrects implementation
behavior against the plan/review contract; the locked scope is unchanged.

An optional read-only `PlanEstimator` supplies operation-specific estimates before
the engine computes the immutable digest and writes a plan. Estimation failure or
cancellation prevents plan storage; it cannot start an effect. VM stop, hard-stop,
pause and managed save report downtime. Stopped-only resource/boot/media edits
also report downtime and explain that the VM must already be off. Start, saved-state
restore, resume and autostart do not introduce a new guest interruption. A future
unrecognized VM action fails closed until it has an estimate policy.

Notes explicitly distinguish this indication from downtime duration, guest
readiness and storage sizing. These operations allocate no new managed disk
volumes, but guest writes and managed-save bytes are not estimated. The zero
additionalBytes field must not be interpreted as a prediction of runtime writes.
Exact capacity estimation for all other handlers remains mandatory separate work.

CLI and TUI render the same immutable estimate. Reopening/reviewing an old plan
never adds new estimates or changes its digest, and apply/reconcile do not rerun
planning. Existing accepted work retains its original execution intent. There is
no SQLite schema migration, operation version change, implicit shutdown, retry or
new host mutation. The earlier native evidence retains the values it observed.
