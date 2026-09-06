# 11 — Plugin-development guide

## What a plugin is

A Virmill plugin is an independently versioned executable with a manifest. It communicates with the application through the protocol in `12-plugin-protocol.md`, not by importing internal packages or loading into the core process. It can be written in Go, Rust or another language that implements the contract.

This specification includes a small dependency-free Go example under `examples/plugins/vm-summary`. It demonstrates protocol mechanics using inventory supplied in its request; it does not connect to libvirt or implement the VM application. The production Go SDK, generator and full conformance harness are v1 deliverables for the implementation agent, not already completed SDK binaries in this package.

## Extension types

| Type | Example | Required contract |
|---|---|---|
| Action | Generate a selected-VM inventory report | Describe input/output, plan effects, execute, cancellation/reconciliation where needed |
| Catalog source (`catalog-source`) | Offer approved guest image metadata | Search/list/resolve, provenance/digests, no arbitrary executable installation |
| Guest recipe (`guest-recipe`) | Configure a supported guest package | Versioned recipe stages, privilege/transport declarations, check/apply/verify |
| Export/repository adapter (`repository-adapter`) | Add an export target or backup transport | Complete artifact contract, integrity, retries and safe finalization |
| Provider | Future remote libvirt or Proxmox management | Provider identity, capabilities, inventory, planning, apply/status/reconcile and namespaced resources |
| Presentation | Additional status card or validated form | Declarative UI schema only; no raw TUI objects or terminal code |

Remote providers are not bundled core features. The **extension contract and mock-provider conformance fixture ship in v1**, so remote support can be added without redesigning the core. A mock provider is a test artifact, not a working remote product.

## 1. Define the smallest permission set

An action that summarizes the selected VMs requests `vm.read` scoped to `selection`; it does not need filesystem, network, guest execution, VM mutation or secrets. A catalog plugin may need brokered HTTPS to an approved source, while a remote provider may need credentials and network access to an explicitly selected private endpoint.

Permission requests are declarations, not grants. Installation and an expanded scope on upgrade trigger owner approval. Effective permissions are the intersection of declared, installed user-approved, invocation-scoped and still-valid grants.

Plugin permissions never confer root-helper access. A plugin that wants to create a local network submits typed core operations through the host API, which still plans, validates and authorizes them. There is no plugin endpoint for privileged shell commands.

## 2. Write the manifest

The manifest includes an ID, semantic version, supported protocol range, platform entrypoints, extension types, permissions, optional configuration schema and UI contributions. Use a reverse-domain ID under a namespace you control. The reference plugin uses `example.virmill.vm-summary`, a documentation-only example namespace, not a published registry identity. Real third-party plugins must replace it with their own stable ID; the manifest, handshake and package index must agree.

```json
{
  "manifestVersion": "1",
  "id": "example.virmill.vm-summary",
  "name": "VM Summary Example",
  "version": "0.1.0",
  "protocol": {"minVersion": "1.0", "maxVersion": "1.0", "transport": "stdio-jsonrpc"},
  "entrypoints": {"linux/amd64": {"path": "vm-summary"}},
  "extensionTypes": ["action"],
  "permissions": [{"name": "vm.read", "scope": "selection"}],
  "network": "none"
}
```

This is a **development manifest**. Distribution packaging must add the actual artifact digest for every executable and any runtime file. Do not paste a dummy checksum or reuse a checksum from a different build. The manifest schema permits a missing hash only so a development workspace can be validated; the distribution installer must reject unsigned/unhashed release artifacts unless the user explicitly chooses the documented trust override.

Entrypoints are package-relative paths; no absolute paths, traversal, symlink escape or shell command strings. No installation hooks run as part of unpacking.

## 3. Implement lifecycle and actions

Read UTF-8 newline-delimited JSON from stdin. Write only protocol JSON to stdout; logs go to stderr. Respond to `initialize`, then `describe`. The host must reject calls before successful negotiation.

For a read-only summary action, `action.plan` returns no effects and no mutation grants. `action.execute` consumes the same allowed input/context and returns structured result data. For a mutating action, planning lists every intended effect and execution uses only the approved operation-scoped grants. A plugin's claim that an action is read-only does not excuse side effects.

Do not retain broader inventory than the invocation requires. Treat missing permissions, empty selection and changed inventory as normal errors. Never read the application's database directly.

## 4. Build and exercise the included example

From `examples/plugins/vm-summary`:

```sh
go build -o vm-summary .
python3 smoke_test.py ./vm-summary
```

The smoke test performs handshake, description, planning and a read-only execution with two synthetic VM records; it also checks error handling. It neither installs the application nor changes real VMs. The supplied module uses only Go's standard library so this demonstration does not need libvirt.

In the finished application, the intended workflow is:

```sh
virmill plugin new --language go --type action --id dev.example.vm-report
virmill plugin validate ./vm-report
virmill plugin test ./vm-report
virmill plugin install ./vm-report --development
virmill plugin permissions show dev.example.vm-report
virmill plugin enable dev.example.vm-report
virmill plugin call dev.example.vm-report report --vm workstation
```

These `virmill` commands are part of the product contract to implement. The plugin generator must produce buildable source, manifest, tests and README, not placeholder imports pointing to a nonexistent SDK.

## 5. Use the Go SDK without coupling to internals

The production SDK must expose typed protocol models, server registration, context/deadline/cancellation propagation, a scoped host client, structured errors, progress helpers, redaction helpers and a test harness. It must not import `internal/` packages or require libvirt merely to build an action plugin.

The SDK module is versioned independently from the application. Provide generated JSON schemas and a language-neutral protocol guide so Rust authors can implement the protocol directly. A working non-Go conformance fixture is required to verify language independence; a full second-language SDK is not mandatory.

Public API stability concerns the wire contract, manifest and documented SDK APIs. Go struct memory layout is never a cross-process ABI.

## 6. Add a UI action

Declare an action ID, title, placement, input schema and output schema. The host can render forms/tables/cards from approved widgets. Inputs reference selected resource IDs rather than duplicating opaque connection secrets in forms.

The core owns the confirmation dialog, progress screen and permission prompt. Plugins cannot remove warnings, relabel their own identity as core, or override VM deletion. Outputs are bounded and sanitized before rendering; ANSI sequences do not make a valid UI extension.

## 7. Long operations and recovery

Use a deadline-aware execution context. Publish real progress and periodic heartbeat while active. Honor cancellation at safe boundaries. Return `recovery-required` when a side effect may have occurred and cannot be identified reliably.

A plugin operating on an external provider must supply idempotency/reconciliation semantics. Lost process state cannot justify repeating “create VM” and producing duplicates. Record stable external resource identifiers in the operation result and make status lookup independent of a single process lifetime.

Do not implement your own daemon that remains hidden after the plugin is disabled. If an external service is required, declare it and let the user configure it explicitly.

## 8. Package and distribute

An archive contains the manifest, executable/runtime files, license and optional README/schema. All members are bounded and package-relative. The packer generates a file inventory with SHA-256 hashes. Distribution uses a detached signature over the canonical package inventory; the implementation must publish the exact signed-byte format and trust-store procedure. The release contract selects Ed25519 for this package-signature use, using a reviewed standard library, not custom cryptography.

The owner may explicitly install an unsigned local plugin after an unmistakable trust warning. “Checksum valid” proves integrity against the supplied inventory, not author identity. Public registry search/install is not required in v1.

Install atomically into a versioned directory. Verify hashes/signatures before launch. Update permissions are reviewed before enabling a new version. Running jobs stay pinned to their original plugin version until they finish or safely stop.

## 9. Upgrade, disable and remove

Disabling stops new invocations and asks how to handle active jobs; it does not blindly kill a remote operation. Crashes must not take down the core UI/coordinator. Repeated protocol violations quarantine the installation with diagnostic reasons.

Keep the prior version for rollback. Snapshot plugin-owned configuration before migration; refuse a claimed rollback if the old version cannot read the new data. Removing a plugin does not delete VMs, backups, labs or external resources it created. Such resources remain visible with an unavailable-provider explanation and exportable metadata.

## Developer checklist

Validate schema; negotiate protocol; keep stdout clean; request minimum permissions; test empty/denied/stale inputs; implement cancellation/deadlines; prevent unplanned mutation; sanitize output; test oversized/malformed frames; prove recovery semantics; package actual hashes; document credentials/endpoints; and run the full host conformance suite before claiming compatibility.
