# Native import options and optional settings export

Installed source: `01a2b949d4e5bab8266bd8639d7b82c3dd0b2a02`.
The initial implementation was `042977b`; rendering and navigation corrections
landed in `dc67aa0` and `6900379`. No dependencies changed: Go 1.27.1 and the
existing exact vendored module set were used.

OVA, ISO and existing disk preparation now have Source, Destination and Disks
pages. The user browses files, edits options and toggles explicit confirmations.
No imported settings file is required. Optional Export saves the same input
object used by the CLI, privately and without overwriting an existing entry.
Plan review retains the draft when canceled. These actions prepare images;
they do not define a guest or certify its installation.

## Executed checks

- `import-options-race-002`: complete CLI/TUI package race tests passed after the
  rendering and back-navigation corrections. Tests cover all three kinds,
  multi-disk OVA completeness, explicit appliance choice, root/backing-file
  distinction, request-schema parity, stale inspection cancellation, export
  refusal and visible editable values. Subsequent changes only affect packaging.
- `import-options-artifacts-003`: all three final package, staged preservation
  and private-daemon/terminal checks passed with IPC required.
- `import-options-native-003`: installed ordinary-user CLI passed at 80×24 and
  120×36. Each walkthrough selected a source through the browser, changed the
  media ID, configured system/data disks at 1024/2048 MiB, checked the initially
  unchecked offline toggle, exported a private JSON file and opened the actual
  shared-service plan. CLI plan readback matched every exported disk and media
  option. Back navigation preserved the draft and returned to the source chooser.
  Output destinations remained absent; no job or VM mutation was submitted.
  Guest inventory, job state, executable and source metadata were preserved.
- `import-options-upgrade-003`: the final core RPM installed successfully. The
  existing coordinator remained running during package replacement; the separate
  idle restart and identity correction are described below.
- `import-options-coordinator-003`: read-only follow-up passed. The running daemon
  matches the final hash, uses host UID/GID maps and remains UID 1000. Complete
  jobs, locks, events, preexisting plans, guest inventory and source metadata
  match the pre-restart baseline. Only the two explicitly identified subsequent
  import previews were excluded from the plans comparison. The complete report
  is [retained here](environments/import-options-coordinator.json).
- Whole CLI/TUI vet, six probe self-tests and diff checks passed locally.

The native source is an 81920-byte reproducible ISO9660 recognition fixture, not
bootable installation media. Its recipe and all final binary/package hashes are
in [import-options-artifacts.json](import-options-artifacts.json). This run adds
UI/input/preview evidence, not new disk-conversion, OS-installation or hardware
qualification. OVA and existing-disk form coverage in this slice is component
evidence. All 71 acceptance scenarios remain required; no status was promoted.

## Failures retained and corrected

`import-options-native-001` exposed incorrect use of the terminal library's
left-truncation API: short focused values disappeared. The correction has a
visible-value regression test. `import-options-native-002` completed options and
export but the real service refused qemu-img ownership.

The latter exposed an installed service defect. PrivateTmp on the user unit
implicitly created a namespace mapping only UID/GID 1000. The coordinator saw
the RPM-verified root-owned qemu-img 10.2.2 executable as UID/GID 65534. Read-only
nsenter confirmed this. [ADR 0035](../adr/0035-coordinator-host-identities.md)
disables that user-unit setting while retaining ownership checks and separate
worker confinement. The helper unit is unchanged.

`import-options-coordinator-001` checked the complete schema-3 journal (zero
jobs), recorded all existing table hashes, guest inventory and source metadata,
and restarted the user coordinator. Its namespace became the host namespace.
The fixture then failed its immediate inventory query because its readiness
probe used the local version command instead of an IPC command. The actual
native terminal/service walkthrough subsequently passed. The failed report is
preserved; a separate read-only verification compares the pre-restart state and
accounts explicitly for the two later authorized preview plans.
The first read-only verifier (`import-options-coordinator-002`) used `id` instead
of the actual `planID` field; it refused the exception and failed without changing
state. The corrected verifier passed as `import-options-coordinator-003`.

`import-options-upgrade-001` installed its expected binary hashes and preserved
services/guests/jobs, but failed a final revision assertion because the invocation
contained a mistyped full commit ID. Later upgrade invocations use Git's actual
revision and passed. None of these failed records was rewritten as a pass.

Remote final evidence remains in `~/virmill-tests/import-options-01a2b94` on the
authorized disposable VM. No release was published.
