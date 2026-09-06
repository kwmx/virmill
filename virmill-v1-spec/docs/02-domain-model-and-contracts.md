# 02 — Domain model and service contracts

## Identity and ownership

A resource key is `(providerID, connectionID, kind, resourceUUID)`. Names are labels, not identity. Renaming does not change references. Store backend UUIDs independently of application job/template IDs. Use stable UUIDs for application objects and cryptographically random identifiers; never derive authority from a guess-resistant name.

Classify ownership as `external`, `adopted`, or `managed`. Discovery grants no deletion rights. Adoption records the existing disks, firmware files and ownership without silently moving them. An external/adopted disk is preserved on VM removal unless the operator explicitly selects it in a separate deletion plan.

Track references, not just a single mutable `refcount`: disk backing edges, VM attachments, template versions, snapshots, backup manifests and in-flight leases. Counts may be cached but must be reconcilable from edges and backend observations.

## Core entities

| Entity | Required information |
|---|---|
| HostCapabilities | platform/architecture, acceleration, backend versions, firmware, supported device models, service/network manager, API capabilities and reason codes |
| VM | ID, name, origin/ownership, guest profile and architecture, resources, firmware, disks, NICs, channels, display, tags, effective/persistent state, opaque backend extension preservation |
| Network | ID/type, managed/external ownership, bridge binding, address families, CIDRs, DHCP/DNS, host access and egress policies, routing relationships, attached NICs |
| NIC | stable ID/MAC, network reference, model, link state, live/persistent settings, guest address intent, default-route intent and observed addresses |
| Volume | ID/pool, format, virtual/allocated size, backing reference, mutability, owner, path locator, integrity metadata, leases |
| TemplateVersion | immutable version/digest, guest preparation status, disk set, config, guest setup defaults, dependencies and origin/license metadata |
| RestorePoint | VM or lab scope, disk/config/state members, consistency classification, capture time window, dependency graph, compatibility requirements |
| BackupSet | manifest version, complete member list, capture/consistency information, firmware/TPM/secrets handling, repository object IDs, verification history |
| Lab | definition/digest, namespace, resource map, ownership, dependency DAG, readiness gates and deployed-state fingerprint |
| Job | actor, plan hash, operation, inputs, locks, grants, state/steps, events, artifacts, attempts, cancellation/recovery information |
| PluginInstallation | ID/version/digest, publisher/trust, API versions, supported platforms, declared/granted permissions, installed path and enabled state |

A display name may be Unicode; backend-safe generated names are separate. Normalize Unicode consistently, reject terminal control characters, and retain a reversible relationship to the display label. Do not allow file-path traversal in any resource name.

## Configuration layers

`requested` is user intent. `persistent` is next-boot backend state. `live` is the actual active VM. `observedGuest` is best-effort information from DHCP/guest-agent/SSH. Never collapse these into a single ambiguous field.

Each field or operation exposes a support result with:

```json
{
  "supported": true,
  "applyModes": ["persistent"],
  "requiresShutdown": true,
  "reasonCode": "RESTART_REQUIRED",
  "reason": "This VM's configured device requires a powered-off edit.",
  "evidence": {"source": "backend-capability-probe"}
}
```

Use `now`, `next-boot`, or `both` at the interface. Map them to backend flags only after checking support. When applying both, represent partial live/persistent failure explicitly. No automatic hard shutdown to make a change possible.

## Plan contract

A plan is an immutable, expiring artifact. It includes:

- API/schema version, plan ID, creation/expiry, actor UID and connection.
- Desired operation and resolved resource IDs; source digests and input manifest digest.
- Before-state fingerprints for domain XML, relevant network state, volumes and plugin version.
- Required capabilities, operation/resource-scoped authorizations, risks and acknowledgements.
- Ordered steps with preconditions, idempotency classification, compensation and reconciliation rules.
- Disk-space estimate, downtime, irreversible effects, and completion predicates.

Compute the plan digest from a documented canonical JSON representation, excluding the digest field itself. Use RFC 8785-compatible [S27] canonicalization and golden tests rather than an ad hoc ordering implementation. Human previews are derived from the same object being applied.

Plans normally expire after 15 minutes; longer-lived scheduled jobs generate fresh plans at execution. Approval binds to the operation and resources, not indefinitely to a stale config. Expiry does not cancel an already accepted running job.

## Service API shape

The versioned local API has read methods plus `plan`, `apply`, `watch` and recovery methods. v1 transport is JSON-RPC over a private Unix socket; streamed events are versioned notifications. Protocol details for the external plugin channel are separate because plugins have different privileges and exposed methods.

```text
host.inspect / host.doctor
inventory.list / inventory.get / inventory.refresh
vm.plan / network.plan / storage.plan / import.plan
snapshot.plan / template.plan / backup.plan / lab.plan / guest.plan
operation.apply / operation.get / operation.list / operation.cancel
operation.retry / operation.reconcile / operation.watch
plugin.list / plugin.inspect / plugin.plan / plugin.call
```

The implementation must publish method-specific request/response schemas. This package supplies declarative-resource, plan and envelope schemas; it does not pretend those are a complete generated implementation RPC specification.

Read methods have no host mutations beyond bounded caching. `plan` may inspect and stage source metadata only with disclosed I/O; it must not install dependencies, alter guests or create host bridges. Downloads are explicit jobs, never an invisible side effect of a supposedly read-only preview.

`apply` accepts a plan ID/hash, idempotency key, and acknowledgements. The coordinator revalidates permissions and state. Repeating an idempotency key with the same canonical request returns the same operation; different inputs with the same key are rejected. Persist deduplication records for at least 30 days and retain them with nonterminal jobs indefinitely.

## Errors and events

Every error has `code`, `message`, `resource`, `operationID`, `retryable`, `safeNextActions` and optional sanitized diagnostic details. Important codes include `UNSUPPORTED_CAPABILITY`, `PERMISSION_REQUIRED`, `STALE_PLAN`, `RESOURCE_BUSY`, `INSUFFICIENT_SPACE`, `SOURCE_CHANGED`, `INCOMPLETE_BACKUP`, `PARTIAL_APPLY`, `CANCEL_PENDING`, `RECOVERY_REQUIRED`, `PLUGIN_PROTOCOL_ERROR` and `GUEST_READINESS_UNKNOWN`.

Events include monotonically increasing per-job sequence numbers, UTC timestamps, phase, severity, progress units and message. Consumers can resume from a cursor. When a cursor falls outside retention, send a resynchronization response rather than silently losing events. Never infer success from an absent in-memory job or a missing backend event.

Progress is `determinate` only with a defensible numerator and denominator. Otherwise publish phase and elapsed time. CLI automation receives stdout data and stderr diagnostics without ANSI by default in JSON mode.

## Persistence

Use SQLite with foreign keys, WAL, transactional job writes and explicit migrations. Disk images are not stored in SQLite. Database durability does not replace flushing the image/manifest directory before publishing a completed artifact.

Store secrets in the credential mechanism, not the job table. Jobs hold secret references and redacted field paths. A native export or backup must carry enough manifest information for reconstruction without this SQLite database. Namespace plugin-owned data; plugins do not receive a database handle.

## Semantic validation beyond JSON Schema

Resolve references; require unique NIC/disk IDs and MACs; disallow multiple default routes unless an explicit supported advanced-routing policy exists; check subnet/DHCP ranges and overlaps; check templates/architecture; detect dependency cycles; enforce deletion ownership; reject unsupported firmware combinations; verify available disk space and enabled capabilities; require every backup member; and reject unknown unnamespaced keys.

Schemas validate syntax and shape. Passing a schema is not evidence a VM will boot, a network is isolated, or a restore point is complete.
