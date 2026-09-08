# Short menus and file explorer — 8 September 2026

Installed production source: `d8766f4f87a377844c5a40275797e186102f4d0d`.
The owner requested fewer upfront options, less path typing and concise useful
explanations. Common tasks now appear in a short menu. A visible Advanced tools
entry retains specialist tasks, and All tools retains every implemented action.
Import starts with three source choices and opens a local read-only explorer.
Forms use one row per input with help for the focused field only. Choosing a
file changes one field without planning or applying an operation.

The picker agent implemented asynchronous bounded directory observation and
navigation. The forms agent implemented the compact renderer and reviewed modal
integration; root fixed a rejected-filename cancellation trap found in review.
The probe agent updated the actual-terminal fixture. Root owned integration,
shared contract preservation, packaging, all remote execution and evidence.

## Observed checks

- `tui-explorer-race-001`: 741 passing UI/CLI test records, 189 top-level tests,
  no failures or skips. Includes all-action request parity, common/advanced
  access, file selection into exactly one field, canceled/stale directory replies,
  permission failures, symlink/special-file refusal, focused input and terminal
  bounds. These temporary-filesystem tests do not prove hardware behavior.
- `tui-explorer-artifacts-001`: three package/private-IPC/installer checks passed,
  including an actual local 80×24 terminal against the private coordinator.
- `tui-explorer-upgrade-001`: replaced only the core RPM on the authorized VM;
  coordinator PID/start time/executable, guest inventory and jobs were preserved.
- `tui-explorer-native-001`: fixture failed before starting the TUI because it
  assumed a child directory in ~/images. All supplied media were direct files.
  VM/job/binary checks passed; the fixture's incomplete media-baseline reporting
  could not establish preservation and is retained as failed evidence. The probe
  was corrected to navigate home → images without creating any directories and
  to identify failed preservation fields accurately. Production binaries unchanged.
- `tui-explorer-native-002`: folder navigation and cancellation worked, but the
  title assertion confused a form hint with the explorer title. Captured screens
  confirmed the form had returned. Native VM/jobs/media checks passed.
- `tui-explorer-native-003`: the complete 80×24 walkthrough passed. The wide
  layout's sidebar required a title matcher adjustment. Preservation checks passed.
- `tui-explorer-native-004`: the unchanged installed CLI passed both 80×24 and
  120×36 walks with resizing, common/advanced navigation, compact CPU/RAM forms,
  plan review/cancellation, actual home/images folder browsing, and selection of
  the existing DFIR-Win11.ova into the OVA source field. No import or guest operation
  was submitted; VM inventory, job states, binary and metadata of all six source
  files stayed identical. Preview metadata was intentionally recorded by the
  coordinator. These checks prove interaction/preservation, not OVA content validity.
- Nine final local probe self-tests (including both title layouts) and scoped UI
  vet passed. All three failed native fixtures remain recorded; production code
  and package were unchanged throughout the native walkthroughs.

The original frozen implementation-tree digest is
`a6452bc0a9e01f0946a27ef0b18e61ac2e1c53dc9c2bdc6640331d7f30322f52`.
The revised probe's digest is recorded separately in the native ledger entry.
No dependency versions changed; existing exact build/environment locks apply.

## Installed artifacts

| Artifact | SHA-256 |
| --- | --- |
| Core RPM | `37e8e473d7dc747edf8f9c26b290763fb61791c22d58385e3dac04425583b435` |
| Core DEB, built but not installed on Fedora | `9b12a074a6cef944e700fc210daf59c8922c050602dcfb1743649389952da7dc` |
| Installed CLI | `dfcaca8b7417a9c56289a122949d1bbf6e574329585607c7de2d2462f382b785` |
| Daemon on disk | `0a4906b684d8372ccd5e9f885b17aad7b1d3482e67c3ce964dd96d03493cb93d` |

Packages are in dist, and remote evidence is retained under
~/virmill-tests/tui-explorer-d8766f4. The current running coordinator is unchanged
from the prior beta until normal restart; the helper was not replaced/restarted.
Existing TUI processes load the new interface only after Ctrl-C then virmill.

Advanced mappings still need settings files; this is not the complete import
wizard. The explorer neither extracts archives nor validates guest media contents.
All 71 acceptance scenarios remain mandatory and their statuses are unchanged.
