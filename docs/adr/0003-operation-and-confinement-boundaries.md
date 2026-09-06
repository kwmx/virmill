# ADR 0003 — Implemented durability and confinement boundaries

Status: accepted engineering decisions; broader gate exit evidence remains open.

The coordinator owns SQLite WAL with FULL synchronization, foreign keys and a
single database connection. A filesystem lock is acquired before migration; the
private socket has a separate singleton lock. Event sequence allocation and state
updates share one transaction. Resource locks and idempotency records persist.
There is currently no event/dedup retention deletion, so accepted keys are retained
beyond the required 30 days. Production retention/migration upgrades remain work.

An externally observable step is preceded by a committed running intent. A process
crash after an effect leaves that step uncertain. Startup never repeats it. Explicit
reconciliation checks observed state; it may close a completed final step, otherwise
it preserves the resource lock and requires a reviewed recovery plan. No provider
supports generic blind retry. A test subprocess exits after fsync of an exclusively
created fixture file and before writing the step acknowledgement; the subsequent
process verifies that it never executes the effect a second time.

The helper validates a signed, expiring operation grant against the actual Unix
peer and a root-installed allowlist. Its implemented action is limited to preparing
one UUID-named directory under a registered storage root. Network checkpointing,
firmware/TPM state transfer, SELinux labeling and independent thaw guardians are
**not implemented**, not hidden as environmental restrictions. The sample policy
grants no actors, roots or keys. No helper or system service was installed or run.

Normal plugin conformance uses bubblewrap plus explicit seccomp and resource
limits. `/usr` is a read-only runtime mount; home, `/run`, host devices, credentials
and database paths are absent. Network/PID/IPC/mount namespaces are isolated and
socket creation is denied by seccomp. Only the selected executable and a private
per-invocation workspace are mapped. There is no unconfined fallback.

Codex's nested sandbox prevented bubblewrap from creating its private network
namespace. The same test was then executed with automatic approval outside that
outer sandbox, successfully. This involved temporary fixtures and private process
namespaces, not host firewall/uplink changes. The ledger distinguishes failed-closed
behavior, successful namespace checks and synthetic plugin protocol behavior.

The public SDK is a separate local module, with concurrent handler cancellation,
serialized protocol writes, scoped host-call correlation and heartbeat emission.
The normal plugin runner and SDK are not yet a complete extension lifecycle: update,
rollback, persistent grants, brokered networking, provider reconciliation and full
UI contributions remain mandatory implementation work.
