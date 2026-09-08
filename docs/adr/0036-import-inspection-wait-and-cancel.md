# ADR 0036: Explicit import checking and cancellation

Status: accepted correction, 9 September 2026.

The owner encountered a 30-second timeout while inspecting a 35 GB OVA. The
screen subsequently showed both a generic missing-inspection error and a stale
timeout claiming jobs continued. Inspection is read-only. The prior transport
used coordinator-lifetime context for every request, so an abandoned inspection
continued hashing and a retry could start another scan.

Continue now performs OVA inspection as part of the source step. A waiting panel
shows the filename, actual elapsed time and explicit cancellation. Single-system
archives advance automatically; collections retain explicit system selection.
The import modal hides unrelated navigation, keeps the primary button visible
and renders one contextual error. This implements the owner's UX correction
without removing source-integrity checks or changing mandatory import scope.

Import inspection/previews and plan submission have a 20-minute default client
wait, with explicit CLI overrides honored. Heavy read-only import requests also
have a 20-minute server limit and one coordinator-wide slot. Disconnect detection
polls a held duplicate Unix-socket descriptor for hangup without consuming framed
input. Canceling the client closes its socket and cancels that read's server
context. The importer retains cancellation errors and checks trailing-padding
reads. A second concurrent import read receives an actionable busy response.

Apply retains coordinator-lifetime acceptance semantics. Accepted durable jobs
retain their independent engine context and are not canceled by detachment.
Timeout text distinguishes read-only checks from uncertain apply acceptance. No
wire method, persisted schema, plugin contract or dependency changed.

Inspection still computes complete archive/member digests and verifies manifests.
The full immutable inspection cache remains separate mandatory work; this change
does not bypass hashing or certify an archive merely because it is selectable.
Tests cover real local socket cancellation, frame preservation, bounded waits,
serialized reads, durable-job survival, importer cancellation, and UI errors and
navigation. Native evidence records the actual owner file separately from small
fixtures and does not infer boot or hardware support from UI checks.
