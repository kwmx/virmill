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
