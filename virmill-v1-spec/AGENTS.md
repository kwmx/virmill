# Implementation-agent contract

You are implementing Virmill, not producing a shell wrapper prototype. Read the entire specification before changing an architectural decision.

## Non-negotiable delivery rules

1. Ship every mandatory local v1 workflow. Do not substitute an MVP, a GUI-only implementation, or an application requiring cloud access. Remote providers remain optional plugins.
2. The CLI and TUI call the same application services. Neither view owns virtualization, firewall, disk, or provisioning logic.
3. Use Go for the core. Use the official libvirt Go module in a Linux-specific adapter. Native Go `plugin` loading is prohibited; external plugins use the documented process protocol.
4. All mutations use plan → authorize → execute → reconcile. Apply must repeat state and capability checks. Persist intent before every externally observable change.
5. Never run the TUI, arbitrary plugins, or an untrusted image parser as root. Privileged host work goes through the bounded, authenticated helper.
6. Do not overwrite originals on import, operate on live disk images with offline tools, delete a referenced backing file, or call a partial capture a successful backup.
7. No silent networking, firmware, storage-controller, identity, credential, or isolation changes. Surface tradeoffs and obtain the required approvals.
8. Respect existing libvirt configurations. Preserve unknown XML semantically; refuse a lossy edit. Do not reconstruct an adopted VM solely from a simplified struct.
9. No invented success messages, dummy percentages, swallowed errors, success-returning stubs, or runtime mock providers in a release build.
10. Unit tests do not certify real virtualization, USB, firmware, routing, backup recovery, or host safety. Record physical/integration evidence separately. An unavailable test environment is a release blocker for that support claim, not a passed test.

## Working method

Build a requirements tracker from the acceptance IDs. Implement vertical slices through contracts, backend, jobs, CLI, TUI, tests, and documentation. Start with the difficult feasibility fixtures before filling out menus. Keep an evidence ledger containing the exact code revision, host/runtime versions, fixture hashes, test result, and logs.

Each completed slice must include error cases, cancellation/recovery behavior, help/reference documentation, schema compatibility tests, and a migration test if persisted state changed. Run package lint/test/build checks before reporting completion. Use deterministic fakes for fast tests and real disposable libvirt hosts for release evidence.

No production-host mutation is authorized by this document. Use explicitly designated disposable test hosts and images. Never upload a user's images, logs, secrets, or disk contents to outside services. Never download proprietary guest media or assume redistribution rights.

## Architecture guardrails

Keep `internal/domain` free of libvirt, systemd, filesystem-layout, CLI, and TUI types. Providers return capabilities, observed state, and typed execution results. Extensions may add namespaced fields and actions; they cannot replace authorization, mutate core schemas, or insert privileged shell commands.

A framework/library update is not an excuse to drift from the public contract. Pin tested versions and use primary documentation. Do not invent dependency versions from examples in this package. Record native-library and guest-image versions too.

## Reporting

At each gate report: implemented requirement IDs; tests actually executed and their results; failures and unresolved risks; changed ADRs; and the next dependency-ready slice. Distinguish implemented, tested, verified on hardware, and released. Do not mark v1 complete until `SHIP-CHECKLIST.md` and the release evidence policy are satisfied.
