# Clear, cancellable appliance inspection

Installed CLI and running coordinator source:
`4945d75471bc343f2486a6953688cbafa04645c7`. Exact binary and package hashes are in
[import-flow-artifacts.json](import-flow-artifacts.json). Go 1.27.1 and the existing
vendored dependency set are unchanged.

The owner's screenshot showed a 30-second inspection timeout and a second error
asking for inspection before Next. The generic transport message incorrectly
claimed jobs continued; the server actually continued the read-only scan after
the client abandoned it. This release slice corrects both the workflow and
cancellation, without skipping archive integrity verification.

Continue now performs inspection and advances a single-system appliance. The
import modal hides unrelated navigation, keeps the primary button visible, shows
elapsed checking time and a working Cancel button, and displays one contextual
message. Collections retain explicit system selection. The default inspection
and preview wait is 20 minutes with a matching server bound and one concurrent
heavy read. Plan submission has a 20-minute default client wait; accepted jobs
retain independent lifetime. CLI overrides remain explicit.

## Executed evidence

- `import-flow-race-001`: UI, CLI, local transport, importer, import preparation
  and durable-operation race suites passed. Includes actual private Unix-socket
  cancellation, scan-slot release, pipelined-frame preservation, timeout wording,
  accepted-job survival, visible UI cancellation and single-system continuation.
- `import-flow-artifacts-001`: all three package/installer/private-IPC and
  terminal integration checks passed. CLI reference was regenerated.
- `import-flow-upgrade-001` and `import-flow-restart-001`: final RPM installation
  and idle user-coordinator restart passed. The full schema-3 journal had zero
  jobs. All preexisting jobs/locks/events/plans, guest inventory and source-media
  metadata matched after restart; the running daemon hash matched the package.
  The generic updater's historical scope label says UI-only; this run also
  updates transport/parser behavior, activated by the separately guarded restart.
- `import-flow-native-001`: installed ISO options/export/actual unapplied preview
  passed at 80×24 and 120×36. The actual owner DFIR-Win11.ova was selected through
  the browser; Continue showed checking and Cancel, Escape canceled, and a new
  ISO draft remained usable without a stale error during three seconds of native
  observation. That case makes no complete-inspection claim. Guests, job state,
  executable and both source files' metadata were preserved.
- `import-flow-owner-ova-001`: complete read-only inspection of the owner's
  36551735808-byte DFIR-Win11.ova passed in **118.480 seconds**, confirming why the
  previous 30-second client wait failed. The archive has four members and one
  system, DFIR-Win11, with attached disk vmdisk1. Its supplied checksums verified;
  publisher authentication remains unverified. Full source SHA-256 was
  `dadaa2344caf0d9b170207d497e2ab3958d5fd5971b36dcd7b030dabe573efe7`.
  Source metadata, guest inventory and jobs remained unchanged. This proves
  archive/descriptor inspection, not conversion, Windows boot or installation.

Native fixture: `tests/fixtures/release/import_options_probe.py`, with the
`tui_workspace_probe.py` helper alongside it. Its ISO is the existing reproducible
81920-byte recognition fixture, not bootable media. The OVA is owner supplied and
is not redistributed. Final remote records remain under
`~/virmill-tests/import-flow-4945d75` on the authorized disposable VM.
The exact executed one-off restart and owner-OVA scripts are retained as
`tests/fixtures/release/restart_idle_coordinator.py` and `inspect_owner_ova.py`.
Their ledger paths refer to the identical build-directory copies used for the
run. They are explicitly scoped disposable-host fixtures, not product defaults.

The three agents delivered form changes, transport/parser cancellation and a
native UI fixture in separate owned files. Root integrated the changes, handled
all remote mutations and maintained architecture and release evidence. All 71
acceptance scenarios remain required. Complete installation/boot, immutable
inspection caching, full supported-terminal and full-v1 qualification remain open.
