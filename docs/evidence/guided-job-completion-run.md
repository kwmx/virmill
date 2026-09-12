# Guided job completion and guest-tools TUI

This UI-only slice connects verified completed VM jobs to fresh VM details,
checks guest state before SSH setup fields, and keeps prose words readable.
CLI/shared services and durable recipes are unchanged. See ADR 0047.

`guided-job-core-001` passed the integrated Go race suite with required IPC
fixtures enabled. Unchanged cached packages are regression evidence, not fresh
native qualifications. Vet passed. Independent review found and corrected late
navigation replies and keyboard focus shifts while result buttons arrive.
`guided-job-ui-002` records the final TUI race regression run after those fixes.
`guided-job-fixture-unit-001` passed three pure tests for the native fixture's
review/result predicates; these do not certify guest behavior.

The native fixture is scoped to the retained owned Fedora guest. It verifies an
idempotent repeat through the actual 80×24 TUI, then opens its exact VM from the
completed job. It must preserve original media, guests, jobs, network state and
credential references; only its own reviewed start/graceful stop are allowed.
The prior successful package installation remains separate evidence. Other guest
profiles and full 1.0 qualification remain open; all 71 scenarios are required.
