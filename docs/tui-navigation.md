# TUI navigation

Start with `virmill tui`, or bare `virmill` in an interactive terminal. Use Tab /
Shift-Tab to choose a section, arrows to select an action, Enter to open an input or
submit a read request, Esc to cancel/back, ? for help and PgUp/PgDn for result details.
Ctrl-C or q detaches from the interface. It never terminates a VM or daemon job.

Inputs use stable VM UUIDs or explicit local file paths. For vCPU edits enter
`{"id":"VM_UUID","input":{"vcpus":4}}`; autostart uses `enabled: true`.
The current editor is a basic input view, not the full required creation/hardware
wizard. Sections without completed workflows say so explicitly.

**VMs → vm create** accepts
`{"id":"PREPARATION_OPERATION_ID","input":{...creation parameters...}}`.
The [creation guide](vm-creation.md) explains disk/NIC identity and firmware choices.
**vm creation result** and **vm creation resume** each accept an operation ID.
Forms accept up to 128 KiB and display a bounded tail; oversized pastes are refused
without partially appending them. CLI and TUI use the same creation/recovery plans.

A plan is rendered from the same immutable service object used by CLI. Press a to
review its acknowledgement IDs, then type its full digest to apply. Esc cancels
that dialog without submitting any mutation. Jobs and errors use text labels;
terminal controls and bidi formatting from untrusted output are removed.

Automated tests cover basic 80x24 navigation, service dispatch, detachment and
canceled approval. Complete resizing, search/focus, screen-reader review, normal
workflow parity and fresh-user walkthroughs remain release blockers.
