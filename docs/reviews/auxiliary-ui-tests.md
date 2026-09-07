# Auxiliary inspection CLI and TUI regression evidence

These tests exercise the shared `auxiliary.Service` through the CLI and TUI for
`vm.recovery.auxiliary.inspect`. They provide prerequisites for UX-01 (parity),
UX-02 (navigation, cancellation, and errors), UX-03 (JSON/NDJSON), SEC-01, and
SNAP-01. They do not accept or promote any acceptance scenario.

The owned regression files are
`internal/ui/cli/auxiliary_test.go` and
`internal/ui/tui/auxiliary_test.go`. Each registers the real auxiliary service on
an `app.Service` with no mutation engine. The exported native and helper
interfaces supply a bounded synthetic stopped-domain observation and a single
4 KiB NVRAM member's metadata. The fixture paths are strings only; the tests do
not open those paths or read auxiliary state bytes.

## Accessible user paths

The CLI tests execute the normal Cobra command:

```text
vm recovery auxiliary inspect 12345678-1234-4234-8234-123456789abc --input '{"rootID":"state"}' --connection qemu:///system --non-interactive --output json
```

They repeat it with `--output ndjson`. Each response is one clean JSON record,
with no diagnostic contamination, and matches the actual shared-service
response. Both results preserve the resource identity and generation metadata.
Repeated inspections use distinct read-correlation IDs. The helper request's
digest is recomputed and checked; it supplies inspection mode without signed
capture authority, an expected capture inventory, or access permissions.

The TUI tests select the Protection action through `ui.Actions`, enter the JSON
form using key messages, submit it, and execute the resulting command through
the same registered service:

```json
{"id":"12345678-1234-4234-8234-123456789abc","input":{"rootID":"state"}}
```

The resulting output matches the shared envelope. At the normal 80 by 24 model
size, the test renders the current `Model.View`, advances using Page Down key
messages, and verifies that every wrapped result line is accessible without
another service call. This is model rendering and navigation evidence, not a
PTY or terminal-emulator run.

Both paths preserve the `inspected` stage, no artifact, and false
`captureVerified`, `independentRestoreVerified`, and `guestBootVerified` fields.
The TUI has no plan or confirmation state after inspection, and the apply key
does not dispatch a mutation. The synthetic native observation remains unchanged.

## Refusal and cancellation coverage

Both user paths exercise unknown input fields, missing and unapproved root IDs,
a session connection, a root caller, an unavailable native adapter, helper policy
denial, a changed native layout in the helper response, cancellation before
observation, cancellation after helper observation, and a transport failure.
Errors retain their machine code and have no successful metadata payload. The
tests check the expected native/helper call counts, including zero observer
calls for early refusals. TUI failures clear prior output, plan, confirmation,
and scroll offset; the current view displays the error and cannot authorize an
apply operation.

Strict JSON tests reject duplicate fields, malformed JSON, unsupported input
shapes, and concatenated CLI objects before dispatch. The TUI also rejects an
outer `apply` field and plain-text ID input for this JSON form. Escape cancels an
unsubmitted form without invoking either observer. Cancellation during a
submitted TUI call is injected through the client test seam; these tests do not
claim a new in-flight keyboard-cancellation facility.

## Verification and limits

On 2026-09-08, the repository-pinned offline toolchain completed:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test ./internal/ui/cli ./internal/ui/tui -run '^TestAuxiliary' -count=1 -json
GOPROXY=off GOSUMDB=off ./scripts/go test -race ./internal/ui/cli ./internal/ui/tui -run '^TestAuxiliary' -count=1 -json
GOPROXY=off GOSUMDB=off ./scripts/go vet ./internal/ui/cli ./internal/ui/tui
```

Both test runs passed all 6 top-level tests and 44 subtests: CLI 3 top-level plus
28 subtests; TUI 3 top-level plus 16 subtests. There were zero failures and zero
skips. Vet completed successfully. No production changes were needed for these
regressions.

The service, schema validation, error envelope, Cobra parsing, and Bubble Tea
model are real. Native inspection and helper metadata are synthetic. These
tests establish neither helper authentication or filesystem exclusion on a
native host, nor TPM/NVRAM completeness, writer exclusion, capture, restore,
boot, or hardware readiness. They perform no SSH, libvirt operations, private
IPC, host mutation, or source-state reads.
