# CLI job event following

The CLI can follow one durable operation through the shared coordinator service.
This implements a portion of UX-03's machine-output contract; the full mandatory
CLI workflows and all 71 acceptance scenarios remain required for 1.0.

```sh
virmill operation watch OPERATION_ID --follow --output ndjson \
  --non-interactive --timeout 2m

# Resume after the last complete event line your consumer processed.
virmill operation watch OPERATION_ID --follow --after 17 --output ndjson \
  --non-interactive --timeout 2m

# Submit one already reviewed plan, then follow its returned operation.
virmill plan apply PLAN_ID --digest REVIEWED_DIGEST \
  --idempotency-key REQUEST_KEY --ack REQUIRED_ACKNOWLEDGEMENT \
  --wait --output ndjson --non-interactive --timeout 2m
```

Use the actual acknowledgements from the reviewed plan. Following never supplies
acknowledgements, applies a second request, reconciles a job, or requests job
cancellation. Every service request retains the selected `--connection`.

## Output and compatibility

Each NDJSON line is the existing response envelope: `apiVersion`, `data`,
`warnings`, and `error`. Following emits an envelope for each event, followed by
an envelope containing the observed terminal job. Event data contains
`operationID`, `sequence`, `timestamp`, `phase`, `severity`, and `message`; job
data contains `operationID`, `planID`, `state`, and the other existing Job fields.
A `plan apply --wait --output ndjson` stream first emits the accepted Job response.
Accepted or running observations do not assert completion of the operation.

Machine output uses JSON escaping for terminal control characters and embedded
newlines. Observed event and job timestamps in the follow stream are formatted in
UTC. `--quiet` suppresses human output only; machine response lines remain visible.
No password, approval, or input prompt is introduced by either wait form.

Without `--follow`, `operation watch` still returns one response whose data is an
event array. `--follow` requires NDJSON. `plan apply --wait --output json` and the
human output form keep one final response; they poll job status without streaming
events. Applying without `--wait`, including explicit `--detach`, still returns
one submission response. Existing command input and approval requirements apply.

## Cursor, bounds, and failures

The cursor is the per-operation event `sequence`. `--after N` requests sequences
strictly greater than N; zero starts with the first event. The client validates
an entire batch before emitting any of it: matching version and operation ID,
nonzero time, and strictly contiguous sequences are required. Wrong job identity,
a changed plan/creation-time/recovery-parent binding, malformed data, gaps,
duplicates, and reordered events stop observation with a typed error. An accepted
apply response must name the submitted plan before following can begin.

The shared store returns at most 1,000 events per request. The client checks that
bound and an 8 MiB encoded data bound, keeps one batch, and immediately drains full
batches. When caught up, it polls at 200 ms intervals. After observing a terminal
state it drains once more, including further full batches, because a job transition
and its event are committed together. The terminal line is a recorded observation;
a later recovery operation may add history after this observer exits.

`--timeout` is a positive overall client deadline (30 seconds by default), not a
new timeout for every poll. Ctrl-C and cancellation of the command context detach
the follower. Signal registration is scoped to the wait and stopped on return.
Timeout returns `WAIT_TIMEOUT` (exit 7); client interruption returns
`CLIENT_INTERRUPTED` (exit 130). The server job can continue after either result.
The final error envelope includes the last observed Job when available and
`error.details` containing `operationID`, `cursor`, and `detached: true`.
Transport errors also retain this cursor and are not automatically retried.
If an apply acknowledgement is lost, the operation ID can be unknown; the client
does not infer that submission failed and does not submit again.

A terminal failed/canceled operation returns nonzero and preserves an existing
typed failure when present. Partial and recovery-required states always select
`PARTIAL_APPLY` or `RECOVERY_REQUIRED` in the response envelope and return exit 6,
including when the stored failure has a generic code. Every terminal error
envelope includes the operation ID, final cursor, and `detached: false`; its nested
`cause` preserves the original stored error. The Job data and durable job record
remain unchanged. A terminal canceled job is an operation result, distinct from
interruption of this client.

Within a live client each validated event is emitted at most once. The cursor
advances only after a complete envelope write succeeds. A short write or broken
pipe stops immediately: no frame retry, additional error frame, service mutation,
or continued event read follows. This is not exactly-once delivery to a consumer;
use the last complete event your consumer actually processed when resuming. An
arbitrary output writer that blocks forever cannot be interrupted through the Go
`io.Writer` interface; the client deadline bounds service polling and cooperative
calls, not an unresponsive stdout consumer.

## Executed test scope

`internal/ui/cli/events_stream_test.go` exercises the real command tree and shared
service interface. A generated SQLite journal test calls the actual application
service's job/event reads and checks unchanged durable records. Other tests use
bounded in-process response and writer seams to exercise terminal-event ordering,
full batches, explicit cursor resume, invalid batch and identity refusal, timeout,
cancellation, typed terminal failures, exact apply binding, clean output, broken
writes, and absence of stdin reads. These tests do not execute a VM operation,
start a daemon, or certify native/PTY/IPC behavior.

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor ./internal/ui/cli -count=1
GOPROXY=off GOSUMDB=off ./scripts/go test -race -mod=vendor ./internal/ui/cli -count=1
```

The CLI implementation and this note use the existing service response, Job and
Event contracts. No persisted journal schema or event wire format changes.
