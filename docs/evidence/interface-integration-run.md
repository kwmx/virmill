# CLI streaming, terminal forms and cross-build integration

Frozen source `715aa9f1545f567a8d10f0b05fc7b386b1069e7c` has implementation digest
`c3f8857c6676420d05149f87c956aaa4feb83d5b6ed6b45f8bbe3910178d2a99`.
The retained checkout is `build/interface-release`. All checks below used this
source; no remote VM, network, storage, guest or installed service was changed.

The CLI now supports explicit NDJSON operation following and cursor resume.
`plan apply --wait --output ndjson` submits once, reports durable acceptance,
streams ordered events, and returns terminal state. Timeout and interruption
retain the cursor and detach without cancellation or replay. Partial and
recovery-required results return exit 6 while preserving the stored error as
cause and leaving the durable job unchanged. JSON and default event snapshots
retain their single-response behavior. See [CLI streaming](../cli-streaming.md).

TUI forms now preserve physical spaces and literal text resembling key names.
Approval input remains visible while the complete plan, acknowledgements and
digest can be paged. Dialog focus, cancellation, visible validation failures
and offsets after resizing are covered by the [terminal contract](../tui-terminal-contract.md).
The original full-digest prompt wording is retained for existing native recipes.
These fixes do not change the shared apply request or approval requirements.

Development manifest reads reject oversized files, symlinks, directories and
FIFOs before starting a plugin; a canceled test does not read its manifest.
`scripts/build-conformance-fixtures.sh` now builds both required Go fixtures in
a fresh checkout using the exact local SDK and offline Go 1.27.1 toolchain.

| Recorded check | Actual result |
| --- | --- |
| `interface-race-001` | 411 test records passed; no failures or skips. Domain, plugins, CLI and TUI used the race detector, required private IPC and confined generated Go/Python/provider fixtures. |
| `interface-cli-stream-001` | Actual private coordinator and generated SDK scaffold job: one submission, four ordered events, terminal success, identical event replay, resume after cursor 4 with no repeated events, unchanged cancellation flag and clean JSON/NDJSON stdout. |
| `interface-provider-ui-001` | Actual confined reference provider through CLI and 80×24 PTY passed with the updated TUI. This is generated-state conformance, not native virtualization. |
| `interface-cross-001` | All 26 artifacts built for the declared darwin/arm64 and windows/amd64 targets. No foreign executable was run. |
| `interface-cross-orchestration-001` | Eight generated-compiler orchestration checks passed. These are separate from the actual cross build. |
| `interface-repro-001` | Two offline builds in the same native environment produced identical three binaries and four unsigned RPM/DEB packages. |
| `interface-artifacts-001` | Package metadata, staged installer/uninstaller preservation and private coordinator/CLI/TUI checks passed. |
| `interface-documents-001` | Eight checks passed: 227 staged payload files, 128 Markdown documents, 193 resolved local links and zero missing targets. |
| `interface-vet-001` | Scoped domain/plugin/CLI/TUI static checks passed. |

The exact [cross artifact inventory](interface-cross-artifacts.json) records
hashes, sizes and independently inspected Go-archive/Mach-O/PE target headers.
The SDK summary implementation is correctly identified as a library archive;
the standalone action and provider are executable compile artifacts. REL-02
meets its stated cross-build acceptance level. This does not add macOS or Windows
host support. See [cross-build instructions](../cross-builds.md).

During agent integration, tests reproduced a terminal-formatting error: a stored
generic error on a partial/recovery-required job could override the required
exit 6. The fix wraps that cause in the terminal envelope without rewriting the
job; real SQLite regression tests verify its stored bytes remain unchanged.
Agent authoring checks also reproduced six TUI defects before their fixes, as
recorded in the terminal guide. An early repeat cross build stopped during a
concurrent CLI edit; the frozen final matrix above passed independently.

The main `build/bin`, `dist` and package stage were refreshed only after comparing
all artifact hashes with the frozen reproducibility record. Current attribution
is in ignored `build/current-artifacts.json`; prior package artifacts remain
under `build/`. These are development packages. Disposable-host system packages
remain the earlier `490b88c` build; the native network work used isolated
`22846d4` binaries and retains its separate failed overall preservation result.

EXT-02 and REL-02 are accepted at their specified evidence levels. The remaining
69 acceptance scenarios and all other unchecked release requirements remain
mandatory. UX-02 still needs its supported-terminal/manual qualification; UX-03
still spans the complete command/workflow set. Polling deadlines cannot interrupt
a permanently blocked generic stdout writer. Full guest, isolation, physical
USB, complete capture/independent restore, host recovery and distribution
installation matrices are not established by these checks.
