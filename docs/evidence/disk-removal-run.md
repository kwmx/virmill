# Explicit VM and selected-disk deletion

Scope: CORE-02, STO-03, JOB-02, SEC-01 and UX-01/02 partial workflow evidence.
All 71 acceptance scenarios remain mandatory. No status is promoted.

Local regression coverage includes explicit target lists and acknowledgements,
per-effect durable receipts, first/second deletion failures, lost acknowledgements,
non-replaying reconciliation, preserved remaining files/locks, metadata reference
refusal, exact own allocation history, graph and generation changes, default-off
TUI toggles and CLI flag/JSON ambiguity. Native deletion is qualified separately.

Three agents began CLI, native and fixture work, then hit an account usage limit.
Root completed integration, remaining TUI/backend tests and the native fixture.
Root owns all remote actions. No dependency version or SQLite schema changed.

The native fixture requires an initially empty ordinary-user libvirt session on
the authorized disposable VM. It creates two never-booted BIOS guests and three
small generated disks, proves a shared-reference refusal, removes that fixture
reference through an exact marked-definition update, then performs reviewed CLI
and TUI selected deletion. Source files and all pre-existing system resources
are retained. It does not bypass system-session graph limitations, change helper
policy, boot guests or delete supplied images. Results follow after execution.

`disk-removal-ui-service-001` passed full app, libvirt, creation, TUI and CLI
race suites (6.961s, 12.689s, 18.451s, 9.687s and 4.176s).
`disk-removal-fixture-001` passed two offline fixture declaration/plan-comparison
checks. These tests do not validate native file deletion.

The initial native run (`disk-removal-native-001`, runtime `5cde08e`) stopped
before any apply: libvirt's ordinary qcow2 `<clusterSize unit='B'>65536</clusterSize>`
metadata was rejected as unknown. No deletion job was submitted. Both generated
VMs and all three disks remained; the failed evidence is retained. Runtime
`3e6a7db` accepts validated positive byte cluster sizes, with malformed/unknown
metadata still refused. The backend regression passed. A subsequent read-only
plan correctly refused the shared disk with `RESOURCE_BUSY`.

`disk-removal-beta-regression-001` passed the complete Go package suite.
Go vet and the separate Go plugin SDK suite also passed. Package validation
(`disk-removal-packages-002`) passed all three tests, including staged
installation/uninstallation and private IPC. Installation and idle coordinator
activation (`disk-removal-upgrade-native-002`, `disk-removal-restart-native-002`)
preserved existing guests, all 32 historical jobs and supplied media observations.
The installed runtime is `3e6a7dbb7e82756b20a13850dbcf9085c8d0b294`.

`disk-removal-native-002` verified the shared-reference refusal, default keep
behavior and successful native CLI deletion of the secondary fixture plus its
one selected disk (`1cb55b38-965d-433c-9fcd-dbdab160dbe5`). Both target disks kept
their original full hashes. Actual 80×24 TUI cancellation, explicit two-disk
selection and CLI/TUI plan parity passed, but the run stopped at confirmation:
long approval identifiers were split across terminal rows. No TUI apply occurred.
The failed overall result remains in the ledger. Runtime `f15de52` separates
readable consequence text from the exact approval identifier; the full TUI suite
passes (`disk-removal-confirmation-003`). Earlier failing test iterations are
retained as `disk-removal-confirmation-001/002`.
