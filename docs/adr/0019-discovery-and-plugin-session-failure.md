# ADR 0019 — Read-only discovery and terminal plugin failures

Status: implemented in development; acceptance qualification remains incomplete.

NET-06 needs real LAN/VPN/defined-network observations before allocation can be
safe. DEV-03 needs real PCI/IOMMU inventory before device decisions. Add bounded
read-only adapters behind neutral domain contracts, then expose them through the
shared service, CLI and TUI. Remote-host inventory is not inferred from a local
route dump; only explicit local qemu connections are accepted. PCI discovery uses
the official native libvirt API with a read-only connection. No sysfs scraping,
driver rebinding or network/helper mutation is part of these observations.

The precedence rules keep every mandatory networking and passthrough workflow in
scope. These observations are prerequisites, not substitutes for packet/isolation
verification or safe assignment/restoration. CIDR results never reserve space.
Retain all routing tables and both IP families; exclude only route `/0` defaults,
not assigned/defined/planned `/0` ranges. Fail incomplete dumps and ambiguous XML.
The separate dumps are explicitly non-atomic. No new persisted contract is added.

Protocol fault fixtures exposed that returning an error alone left some plugin
workers alive and sessions reusable. Protocol 1.0 requires oversized frames to
terminate and unknown response IDs to protocol-fail; host API calls are prohibited
before initialization succeeds. Permanently retire a session on terminal framing,
envelope, correlation, initialization-version, write and deadline failures. Kill
the supervised worker and close stdin while retaining final cleanup ownership.
Normal correlated application errors remain usable exchanges. This follows the
existing normative protocol and changes no permission grants or public methods.

Tests with an ordinary Python peer prove transport behavior only. Separate tests
through the production confined launcher prove the actually exercised kernel
boundaries, and cannot qualify other kernels, architectures or MAC policies.

A further process regression exposed `Close` waiting behind a hung call's mutex.
Close now cancels the worker before acquiring that mutex; active calls also
observe the worker stop channel. A once-only close joins final cleanup and makes
concurrent Close calls wait for the same completion. This does not promise safe
rollback of a plugin's already-performed external effect; durable operation
reconciliation still owns uncertain effects.
