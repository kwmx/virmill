# Installed TUI workspace update — 8 September 2026

The owner requested an application-style TUI with action buttons, the complete
implemented action set, and bare `virmill` as the terminal default. The installed
core package now uses source `b73e2834f88310bd8f64b3db5d811d50b9e8d3c8`.
See [navigation](../tui-navigation.md) and [decision](../adr/0031-resource-workspace-and-visible-actions.md).

The runtime default is the resource workspace. It provides real inventory rows,
full resource details, keyboard-focusable buttons, an 81-action catalog derived
from the shared registry, labeled common forms, structured parameter-file inputs
for advanced mappings, and explicit immutable-plan acknowledgement/apply controls.
No backend, operation engine, database schema or daemon service logic changed.

## Verification actually run

- `tui-workspace-frozen-race-001`: 637 passing UI/CLI test records, 166 top-level
  tests, zero failures/skips. Includes request parity for every registered action,
  focus/input handling, cancellation, stale replies, lost/ambiguous selection,
  exact digest/acknowledgement binding, hidden-dialog refusal and terminal bounds.
- `tui-workspace-reproducibility-001`: two identical builds of three executables
  and four RPM/DEB packages at the frozen revision.
- `tui-workspace-artifacts-002`: three passing package/installer/private-IPC tests,
  including a real 80×24 terminal action-catalog navigation check. The earlier
  `artifacts-001` fixture incorrectly waited for an unchanged Overview header to
  be redrawn; the corrected fixture waits for the changed resource content. The
  tested binaries/packages were unchanged and the revised fixture digest is saved.
- `tui-workspace-native-002`: actual authorized-VM inventory and terminal
  navigation at 80×24 and 120×36, with bidirectional resizing. Its first fixture
  run used the wrong UUID wire-key name and failed before terminal interaction;
  that failed evidence remains recorded.
- `tui-workspace-upgrade-native-001`: replaced only the core RPM, preserving the
  running coordinator's PID, start time and executable hash, VM observations and
  jobs. The helper was not replaced or restarted.
- `tui-workspace-installed-native-001`: the **installed `/usr/bin/virmill`**, with
  no subcommand, opened the new UI at both sizes. The probe selected real VMs,
  searched names, opened full-ID details, opened/canceled the CPU/RAM form,
  focused/activated action buttons, opened the full action catalog, previewed a
  real start plan, opened its confirmation and canceled without applying. Guest
  observations and job states stayed identical; no operation was created. Plan
  preview metadata was intentionally persisted by the coordinator.

The three agents supplied guided/action forms, readable detail/plan formatting
and a state-machine review, and the native terminal probe. Root integrated the
runtime default and visible controls, fixed the reviewed async/identity issues,
ran integration and exclusively coordinated remote changes.

## Exact installed artifacts

| Artifact | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.1.x86_64.rpm` | `b2db013306c644c648d708a534c805c46dc2338a855fe60d87bf14d8bbd5da90` |
| `virmill_1.0.0~beta.1_amd64.deb` | `392ab75b3e333db4a610026236efc88300df5a5903a3ca4eb3ee324450f0dc8a` |
| `/usr/bin/virmill` | `25b939e7cea299331c0fdf9bd0d470f633a553ce8346c63f53da6a58b00bfa3f` |
| `/usr/bin/virmilld` on disk | `a5057a91399e110d5808b12a9f4a4c90eab28b65e0949b1713b0811c9076d02a` |

Local artifacts are under `build/tui-workspace-release/dist`. Remote update and
terminal evidence are retained under `~/virmill-tests/tui-update-b73e283`.
The running coordinator deliberately retains its preceding executable
`99ad4f7bae1861af2287def6cc106ad14365a181b7e6ed5755fc20839611a1c4` until its next
normal restart. This is compatible because the update changed only UI code.
Existing open TUI processes also retain their prior executable: exit with Ctrl-C
and run `virmill` again to load the new interface.

All 71 full-v1 acceptance scenarios remain required. These results establish
scoped software interaction and preservation, not complete workflow usability or
hardware support. Advanced import/create mappings still use parameter files;
complete multi-step wizards and missing backend workflows remain open. Overview
pool availability aggregates pool-reported values; pools can share physical
storage, so that number is not a unique physical free-space budget. The service's
operation-specific capacity checks remain authoritative.
