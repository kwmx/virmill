# TUI terminal model contract

This change contributes automated model evidence to **UX-02** in
`virmill-v1-spec/docs/13-testing-and-acceptance.md`, applying the keyboard,
responsive layout, readable errors and terminal sanitization requirements in
`virmill-v1-spec/docs/04-tui-specification.md`. It does not complete the required
manual supported-terminal qualification.

## Reproduced defects and corrections

The initial focused regression run failed six test functions: physical spaces
were omitted from path/parameter input; multi-rune text equal to a key name could
be treated as a control; long approval acknowledgements obscured the input and
digest at 80×24; approval details could not be paged; widening a previously
scrolled narrow result could leave a blank details pane; and Tab did not expose
dialog details focus. The error-content regression already passed.

`internal/ui/tui/model.go` now distinguishes text events from control events,
including physical Space. Text such as `enter`, `esc` and `tab` remains literal
inside an input field. Pasted or multi-rune text does not trigger navigation
shortcuts outside a field. Physical Ctrl+C remains the detach control.

Forms and approval dialogs keep a bounded input area while their complete details
can scroll. Approval review includes the full digest, every acknowledgement,
affected resources and the complete plan as JSON. Long details no longer consume
the fixed prompt/input area. The ordinary path/parameter prompt is unchanged.
Resizing clamps details offsets to the available content and preserves input,
selection and the reviewed plan. Input and errors pass through the existing
terminal-text sanitizer and cell-aware wrapping; state and focus have text labels
and need no color.

## Dialog keys and submission

| Key | Behavior |
| --- | --- |
| Tab / Shift+Tab | Switch between input and details; never change the selected action. |
| PgUp / PgDn | Page details while retaining the input text. |
| Up / Down in details | Scroll one details line. |
| Enter in details | Return focus to input without submitting. |
| Enter in input | Validate and submit the selected form, or require the exact full plan digest for approval. |
| Esc | Close the unsubmitted dialog and clear its input; retain the plan preview and action selection. |
| Ctrl+C | Detach the interface; do not issue a job cancellation request. |

The apply request still uses the existing shared `operation.apply` service with
the exact plan ID, digest and acknowledgements and a new idempotency key. A
trailing space in a digest fails validation; input is not trimmed to manufacture
approval. Invalid JSON and digest mismatches remain visible and do not dispatch.
Navigation while a request is busy cannot submit it a second time. Backend
transport, cancellation and typed domain errors remain textually visible, clear
the actionable plan and preserve surrounding search/selection state.

## Executed checks

Local pinned runtime: `go version go1.27.1 linux/amd64`. No dependencies changed.

```text
./scripts/go test -mod=vendor ./internal/ui/tui -run '^TestTerminal' -count=1
Before corrections: FAIL; six of the initial seven top-level tests failed.
After initial corrections: PASS, 0.040s.

./scripts/go test -race -mod=vendor ./internal/ui/tui -count=1
Final expanded suite: PASS, 2.379s.

git diff --check -- internal/ui/tui/model.go internal/ui/tui/terminal_contract_test.go
PASS (no output).
```

The new `terminal_contract_test.go` contains 11 top-level tests using the actual
Bubble Tea model update/view methods and a deterministic recording client. They
exercise path and parameter forms, physical spaces and literal key names, exact
approval dispatch, complete review paging, input/details focus, cancellation,
typed/transport errors and busy detachment. Resize sequences include 80×24,
120×40, 40×10, 30×9, 20×6, 12×4, 2×2 and 0×0. Tests assert UTF-8 and cell/line
bounds, retained state, absence of terminal escapes and zero unexpected service
calls. Existing search/model/provider tests also ran in the full race suite;
their source files were unchanged by this work.

## Limits

These are automated model tests, without a real terminal, PTY, screen reader,
daemon or virtualization backend. Parent-owned artifact/PTY testing is separate
and must identify the exact built revision. No manual-terminal or completed
UX-02 acceptance claim follows from this suite.

At fewer than 12 columns or four rows, or fewer than six rows with an active
dialog, a bounded resize notice replaces the normal view while preserving state.
Very narrow views clip prompts; complete review content remains pageable after
resizing. The existing 128 KiB input and 128-rune search limits remain. Editing
still appends text and removes one rune on Backspace; this change does not add a
cursor/grapheme editor, command palette or configurable ASCII mode. Job
cancellation/recovery remains a shared-service workflow, separate from closing a
form or detaching the interface. No action registry, authorization rule or
operation semantics changed.
