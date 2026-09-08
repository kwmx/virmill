# Task-oriented TUI menu update — 8 September 2026

Superseded for installed hashes and controls by the
[file-explorer update](tui-explorer-run.md). Evidence below is historical.

The owner found the action catalog confusing. Source
`421c03e6f5d951667500414b278088ae2e0d4498` replaces CLI-style labels with explicit
task names and descriptions, groups tasks by purpose, and shows the selected VM.
Common tasks use resource toolbar buttons. VM power controls reflect observed
state, and selected-VM power tasks go directly to the existing plan review.
All 81 registered actions remain available through All tools and section filters.
Forms use the same task language. Advanced mappings still need parameter files.

The label agent supplied all 81 explicit labels and coverage tests. The probe
agent updated the real-terminal walkthrough and reviewed integration. Root owned
navigation, request integration, packaging, remote changes and release tracking.
Review fixes cover correct group headings while scrolling and ignored late
responses after canceling a read-only form.

## Recorded checks

- `tui-task-menus-race-001`: 726 passing test records, 174 top-level UI/CLI tests,
  no failures or skips. Includes all-action request/label coverage, selected-VM
  preview identity, task search/back behavior, every selected menu row and focused
  button visible at 60×18, 80×24 and 120×36, and canceled read response suppression.
- `tui-task-menus-artifacts-001`: all three package/private-IPC/staged installer
  checks passed, including actual 80×24 terminal menu interaction.
- `tui-task-menus-upgrade-001`: installed the core RPM on the authorized VM,
  preserving the coordinator PID/start time/executable, guest inventory and jobs.
  No helper package or service restart. Backend and durable service code unchanged.
- `tui-task-menus-native-001`: the installed CLI passed actual PTY walks at
  80×24 and 120×36 with resizing both ways. Walks exercised visible More button
  focus, selected VM context, Power grouping, plain CPU search, search/menu Back,
  forms, start plan review and canceled confirmation. Native inventory and job
  states stayed identical; no operation was created. Plan preview metadata was
  persisted intentionally. Root inspected the captured actual 80-column screens.
- Seven local probe self-tests and focused UI vet passed.

Implementation-tree digest:
`9c8732dafa888fe97c6f03d867be5b37ba2105011c58fbb06ea5c5b2ec68b028`.
Dependencies unchanged; exact versions remain in the dependency lock.

## Installed artifacts

| Artifact | SHA-256 |
| --- | --- |
| Core RPM | `094854ecda77780d3f159788cf89027cf36c71c5a49b0d4831847620080ac3f4` |
| Core DEB (built, not installed on Fedora) | `f4c98c230eef89813096e08a8852028325c55ce44c67c8e2cba9329fb487283f` |
| Installed CLI | `43cae53c1751b8d0c5dd1998d6c1ba7beb434ef9c7cfc406c842db28a8bda26d` |
| Daemon on disk | `2f82bc92ef7a07605633fa980476892397de035c443095825603d3f994241eea` |

Packages are in `dist`; remote evidence is under
`~/virmill-tests/tui-task-menus-421c03e`. The running coordinator retains its
previous executable until normal restart, as documented in the previous UI
handoff. Reopen the TUI with Ctrl-C then bare `virmill` to load the update.

This is scoped interaction evidence, not proof that complete-v1 usability or
hardware requirements pass. All 71 acceptance scenarios remain required and
existing acceptance statuses are unchanged.
