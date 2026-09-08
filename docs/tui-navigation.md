# TUI navigation

Start with `virmill tui`, or bare `virmill` in an interactive terminal. Use Tab /
Shift-Tab to choose a section, arrows to select an action, Enter to open an input or
submit a read request, Esc to cancel/back, ? for help and PgUp/PgDn for result details.
Ctrl-C detaches from the interface; q also detaches outside search and input views.
Detaching never terminates a VM or daemon job.

Press / in the action menu to search the current section's commands and summaries.
Type words in any order; every word must occur in the command or summary. Matching
uses Unicode simple case folding. It does not remove accents, normalize differently
encoded characters, or perform fuzzy matching. The query accepts up to 128 Unicode
code points; oversized pastes and control characters are refused as a whole.
Backspace removes one code point. A long query shows its trailing text with an
ellipsis; the stored query remains complete.

While searching, Up/Down selects a result, and Tab/Shift-Tab changes section while
keeping the query and search focus. Enter leaves search with the filtered action
selected; it does not open a form or execute the action. Press Enter again to use
the selected action. The visible Search/Filter label identifies the current mode;
the selected row has a > marker, and an empty result explicitly says no actions
match. Esc clears the filter and leaves search. Clearing a filter retains an
explicitly selected result, or restores the earlier action if filtering temporarily
hid it. Each section remembers its selected command during the current TUI session.

Search stays separate from action forms and plan approval: /, q and other printable
keys remain input there. Esc first cancels the active form or approval dialog;
from a filtered action menu it clears search without discarding the current result
or plan. A further Esc performs the usual page cancellation.

The action menu scrolls to keep its selected row visible after resizing. Text width
accounts for wide Unicode characters, and compact result pages advance by their
visible height so PgDn does not skip details. Very small windows show a resize
notice and retain selection/input. Use at least 80x24 for reviewing full forms and
approval details.

Inputs use stable VM UUIDs or explicit local file paths. For fixed CPU/RAM edits enter
`{"id":"VM_UUID","input":{"vcpus":4,"memoryMiB":4096,"applyMode":"next-boot"}}`;
see the [configuration guide](vm-configuration.md) for preservation checks and
required acknowledgements. Autostart uses `enabled: true`.
The current editor is a basic input view, not the full required creation/hardware
wizard. Sections without completed workflows say so explicitly.

**VMs → vm create** accepts
`{"id":"PREPARATION_OPERATION_ID","input":{...creation parameters...}}`.
The [creation guide](vm-creation.md) explains disk/NIC identity and firmware choices.
**vm creation result** and **vm creation resume** each accept an operation ID.
Forms accept up to 128 KiB and display a bounded tail; oversized pastes are refused
without partially appending them. CLI and TUI use the same creation/recovery plans.
**backup policy preview** accepts
`{"path":"/absolute/backup-policy.json","input":{"after":"2026-09-08T00:00:00Z","count":5}}`;
**backup policy validate** accepts a policy file path.

A plan is rendered from the same immutable service object used by CLI. Press a to
review its acknowledgement IDs, then type its full digest to apply. Esc cancels
that dialog without submitting any mutation. Jobs and errors use text labels;
terminal controls and bidi formatting from untrusted output are removed.

Automated model tests cover search/filter focus, selection across sections, Unicode
input, empty results, bounded pastes, compact resizing/paging, service dispatch,
detachment and canceled approval. These are component tests. Supported-terminal
qualification, screen-reader review, normal workflow parity, complete resource
search/wizards and fresh-user walkthroughs remain release blockers.
