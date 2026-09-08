# ADR 0025: Reboot event receipts and offline policy previews

Status: accepted routine implementation within the locked v1 scope.

`vm reboot` uses one native graceful request, a selected-domain event subscription
established before that request, and a durable event receipt bound to the operation
and immutable plan. Its operation step is non-repeatable. A running state cannot
reconcile a missing receipt; uncertainty retains the resource lock. An observed
event proves a transition, not guest readiness. Libvirt supplies no request ID in
that event, so the plan requires coordination with other lifecycle writers.
Registration of the native event implementation precedes connections; one event
loop starts lazily. The event wait is bounded, while synchronous native calls
retain their libvirt transport limits. No reset or force-stop fallback is allowed.

BackupPolicy declarations now receive semantic validation in the existing shared
service. A new preview returns future UTC instants for the declared named timezone
using a documented five-field cron dialect. The specification fixes five fields
but does not define the complete dialect; numeric/named lists, ranges and positive
steps are supported without adding a dependency. Preview is bounded to 32 results
within eight years and observes cancellation. Daylight-saving gaps are skipped;
both occurrences of a repeated matching wall minute remain explicit. This does
not install a schedule, evaluate missed runs, grant standing authorization, access
credentials, capture state or weaken a consistency policy.

Both workflows use the shared CLI/TUI action registry. Schema IDs and the database
schema are unchanged; the reboot receipt uses a new versioned metadata namespace.
Old binaries have no reboot operation handler and cannot replay its jobs. Full
scheduled execution and the complete lifecycle/hardware matrices remain required.
