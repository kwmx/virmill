# ADR 0016: Active creation results are progress observations

Status: accepted implementation correction; all mandatory creation/recovery scope
remains unchanged. No persisted schema or operation version changes.

The first native creation tests exposed U009: a result query during validation
returned RECOVERY_REQUIRED because no receipt existed yet; later active queries
also recommended recovery solely because the job was not succeeded. The durable
job contract distinguishes active work from uncertain effects, so this guidance
was incorrect.

An optional receipt read now distinguishes absent metadata from corrupt or
unreadable metadata. Existing recovery callers still require a receipt. Result
observations for queued, validating, running and verifying states return the
actual job plus additive `receiptAvailable` and `complete` fields. Missing receipt
is null and never a fabricated zero-valued receipt. `complete` is false for every
active result; next actions point to operation watch/show. A snapshot can lag a
concurrent state transition, so clients continue observing the operation.

A succeeded result requires a present receipt with both verified volumes and a
confirmed definition. Guest boot/setup/connectivity flags remain separate and
false in this adapter. Unknown receipt fields/versions, unreadable metadata and
terminal uncertainty still fail closed. Terminal incomplete/error behavior is
unchanged; this correction does not authorize accepting, replaying or disposing
of a failed creation.

The shared result service feeds CLI and TUI. Result reads never allocate storage,
define a VM, change locks, cancel work or invoke reconciliation. A caller's canceled
observation does not cancel durable work. Existing actor checks run before receipt
access. Tests cover every active state with/without a receipt, actual synthetic
blocked-upload progress through completion, wrong actors, invalid receipts and
inconsistent success. These are coordinator tests, not native upload or hardware
qualification. Prior evidence retains its original results and runtime revision.

Cleanup operations exposed through the same result command use the same active
state rule. Their optional `cleanupProof` and `disposition` keep their existing
names, with additive availability flags and `complete: false` while active.
Present proofs and dispositions must match their version, plan and operation;
malformed records cannot masquerade as normal progress. Successful cleanup needs
both records, and deletion needs every recorded volume absence confirmed.
`complete` then means the cleanup/disposition completed; `vmCreated` remains false.
No read invokes cleanup or releases inherited recovery locks. Synthetic tests
cover every active cleanup state and missing, future or incomplete evidence.
