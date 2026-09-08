# Reboot, policy preview and TUI search

All 71 acceptance scenarios remain required. This work adds functioning commands
and navigation through the existing shared service and durable operation engine.

| Owner | Delivered implementation | Acceptance mapping |
| --- | --- | --- |
| Beauvoir | Native selected-domain event reboot adapter; eight focused race-tested cases | CORE-02, JOB-01 |
| Russell | BackupPolicy semantic validation and bounded timezone/DST occurrence preview; ten focused race-tested functions | BAK-05, UX-01, UX-03 |
| Nash | TUI action search, remembered selection, Unicode and compact paging; 12 new tests plus six Unicode subtests | UX-02 |
| Parent | Shared contracts/service, durable reboot receipt/recovery, CLI/TUI parity, bounded declaration reads, integration and release evidence | CORE-02, BAK-05, JOB-01, UX-01, UX-03, REL-03 |

The parent reviewed each contribution and executed the shared service/UI tests.
Reboot tests include actual SQLite reopen and receipt recovery, duplicate request
suppression, stale plans, missing acknowledgement, invalid or lost event receipts
and retained uncertainty locks. Provider seams in these tests do not exercise a
real guest. Review corrected the public plan idempotency enum and added a final
reconciliation cancellation check. An initial test fixture omitted the required
private journal directory mode; it was corrected before the passing runs.

`lifecycle-policy-search-core-001` ran the full offline race suite with required
private IPC, confined plugins and generated disk tooling. The new feature tests
passed, but the suite failed two QEMU fixture paths with `io_uring` memory
allocation errors. A resource-limited rerun, `lifecycle-policy-search-core-002`,
also failed generated QEMU paths. These are retained failures, not passing full
suite evidence. `lifecycle-policy-search-vet-001` passed whole-repository static
analysis. New code did not change the image adapters; the local native fixture
environment remains an unresolved qualification limitation.

The native reboot recipe uses a new disk copy and isolated coordinator on the
authorized disposable host. Its actual execution is recorded below when done.
Persistent backup scheduling/capture, native reboot qualification and the full
terminal/lifecycle matrices are not inferred from component tests.
