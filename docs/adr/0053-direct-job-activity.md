# ADR 0053: Read job activity without a parameter form

Status: implemented; native evidence recorded separately.

The Jobs Events button opened a generic command form asking for a job ID and
starting sequence even though the user had already selected a job. Specification
04 requires readable recorded progress and errors, with keyboard navigation at
80×24. The existing operation.watch service already supplies ordered pages.

Activity now opens the selected job directly. It shows complete wrapped messages,
UTC timestamps, phases and severities, with scrolling and labeled Refresh,
Older events, Newer events and Back to job buttons. Active jobs refresh the
current incomplete page every three seconds. Reading errors stop automatic
refresh and appear at the top with a manual retry; earlier events remain visible.
The header shows when the last successful read finished, making a refresh
visible even when events are unchanged. Page history changes only after a valid response. Global sequence gaps are valid.

The workspace binds each request to its connection, job, cursor and request token.
It rejects mismatched or unordered responses and ignores replies after closing.
Background job completion cannot navigate away while Activity is open. The view
owns presentation only; CLI and TUI use the same existing service and SQLite
journal. No API, dependency, persistence schema or execution behavior changes.

Unit tests cover paging, full errors, retries, stale replies and preservation.
The native fixture compares actual recorded messages with the CLI and verifies
read-only navigation and resource preservation. This supports JOB-02/03 and
UX-01/02 at the recorded level; it does not qualify crash recovery or all 71
mandatory scenarios.
