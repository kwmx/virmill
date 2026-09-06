# 12 — External plugin protocol 1.0

## Transport

Use JSON-RPC 2.0 semantics over newline-delimited UTF-8 frames. Framing is an application convention, not a change to JSON-RPC's method/result/error rules. No batches in protocol 1.0. One object per physical line, no BOM, maximum 8 MiB per frame, no duplicate keys, no NaN/infinity and no ambiguous numeric identifiers. [S29]

Request IDs are strings. Host-origin requests use `h-` and plugin-origin host-API requests use `p-`; responses echo IDs exactly. Notifications omit `id` and receive no response. Malformed JSON gets a parse error with `id: null` when a response can be sent; an oversized frame terminates the connection. Ignore no unsolicited response: log/protocol-fail unknown IDs.

Stdout is protocol only. Stderr is bounded untrusted log content, redacted and sanitized by the host. Binary/large image data is never base64-embedded in JSON frames; use host-issued scoped artifact handles or approved file streams.

Default limits: initialization 10 s; description 5 s; planning 30 s; active execution deadline supplied by host; heartbeat every 10 s during long execution; ping after 30 s without activity and fail after an additional 5 s; cancellation grace 5 s unless the approved operation declares a safe-boundary wait. These are configurable engineering limits, not performance guarantees.

## Session lifecycle

1. Verify package/trust/grants and create confined workspace.
2. Spawn plugin with protocol pipes and minimal environment.
3. Host sends `initialize`; plugin returns one mutually supported protocol version and its identity.
4. Host sends `describe`; validate advertised extension schemas against manifest/grants.
5. Perform read-only calls or reviewed plan/apply/reconciliation calls.
6. Host sends `shutdown`, waits for response, closes stdin and waits for process exit.

A plugin may not call host APIs before initialization succeeds. Version mismatch is fatal. An unknown extension type or unsupported required permission disables that extension rather than being silently treated as trusted core functionality.

## Initialization example

```json
{"jsonrpc":"2.0","id":"h-1","method":"initialize","params":{"protocolVersions":["1.0"],"host":{"version":"1.0.0","os":"linux","arch":"amd64"},"sessionID":"example-session","permissions":[{"name":"vm.read","scope":"selection"}],"limits":{"maxMessageBytes":8388608}}}
```

```json
{"jsonrpc":"2.0","id":"h-1","result":{"protocolVersion":"1.0","plugin":{"id":"example.virmill.vm-summary","version":"0.1.0"},"extensionTypes":["action"]}}
```

The host verifies the returned identity against the installed manifest. A protocol minor version adds optional capabilities; breaking semantic/schema changes require a new major version. Version 1.0 hosts only accept 1.0 until a newer version is actually implemented and negotiated.

## Method registry

| Direction/method | Purpose and required behavior |
|---|---|
| Host → `initialize` | Negotiate version/identity, limits and effective grant summary |
| Host → `describe` | Return declared actions/provider capabilities and input/output schemas |
| Host → `ping` | Bounded liveness check; no side effects |
| Host → `action.plan` | Input/context → summary, effects, required scopes and opaque plan token |
| Host → `action.execute` | Approved plan token/input/context/grant IDs → structured final result |
| Host → `action.reconcile` | Resolve an interrupted operation from stable effect/resource identifiers |
| Host → `request.cancel` | Notification naming an active request ID; plugin acknowledges through eventual operation result |
| Host → `shutdown` | Stop accepting work and prepare clean exit |
| Plugin → `plugin.progress` | Notification with request/operation IDs, sequence, phase and actual progress |
| Plugin → `plugin.heartbeat` | Notification indicating active request remains responsive |
| Plugin → `host.inventory.get` | Scoped, redacted selected-resource facts |
| Plugin → `host.operation.plan` | Request a typed core operation; no execution authority |
| Plugin → `host.operation.apply` | Apply the matching approved typed plan using a valid grant |
| Plugin → `host.artifact.open` | Obtain an approved artifact handle, not arbitrary filesystem access |
| Plugin → `host.http.request` | Optional brokered network request within effective endpoint grant |
| Plugin → `host.secret.use` | Scoped use/retrieval according to permission; never list all secrets |

Unknown methods return JSON-RPC method-not-found. Missing/invalid parameters return invalid-params. Host APIs are explicitly allowlisted by extension type; existence in this table is not automatic permission to invoke them.

## Planning semantics

`action.plan` receives `{action, input, context}`. Context includes only selected-resource facts granted for the invocation. It returns `{summary, effects, requires, planToken}`. The token is opaque plugin data, not a security authorization. The host wraps the result in its own canonical approved plan bound to package digest, input, context fingerprints and granted effects.

Read-only actions return an empty effects array. Mutating actions enumerate resources, operation classes, irreversible effects and required grants. Planning cannot call `host.operation.apply`. A plugin with unrestricted direct network access is intrinsically trusted to honor the no-side-effects planning contract; the host cannot prove remote read-only behavior simply from a manifest label.

`action.execute` receives the same action/input/context plus `planToken`, host operation ID and scoped grant references. The plugin must reject stale/mismatched input. Host mediation still enforces grants independently. A plugin token or a `requires` list never grants privileges.

## Provider extension contract

A provider advertises provider ID and resource capability models. Required method families are `provider.capabilities`, `provider.inventory`, `provider.get`, `provider.plan`, `provider.apply`, `provider.status`, `provider.reconcile` and `provider.cancel` where cancellation is supported.

`provider.inventory` is paginated and emits stable external IDs, namespaced by provider/connection. `plan` explains supported effects and maps the neutral operation to provider behavior. `apply` receives a core operation ID and idempotency key; the provider persists enough external identity for status/reconciliation. Each resource declares whether it supports configuration edits, networks, devices, snapshots, backups and guest transports.

Providers may expose capabilities absent from the local backend using namespaced extension data. They may not reinterpret core fields, claim local USB is remote USB, or pretend a provider snapshot includes independent backup storage. The host does not force unsupported provider features into fake local semantics.

V1 conformance must include a simulated provider that creates stable fake IDs, reports partial failure, recovers after restart and explicitly rejects unsupported operations. No remote service credentials are required for that test. Implementations of real remote providers remain separate deliverables.

## Errors

Use standard JSON-RPC codes for parse/invalid-request/method-not-found/invalid-params/internal errors. Application errors use the server-error range with a stable string in `error.data.code`:

```json
{"jsonrpc":"2.0","id":"h-4","error":{"code":-32010,"message":"VM read permission is not granted","data":{"code":"PERMISSION_DENIED","retryable":false,"safeNextActions":["Review this plugin's permissions"]}}}
```

Define application error codes for permission denied, stale plan, unsupported capability, deadline exceeded, canceled, resource busy, partial effect and recovery required. Transport success does not mean operation success. A plugin crash after an external side effect is an uncertain operation, not an automatic failure with no consequences.

## Concurrency and flow control

The host may use multiple request IDs in a process; responses may arrive out of order. The production SDK must support concurrent cancellation/heartbeat while work runs. Serialize writes to stdout. The small example performs immediate read-only work and does not demonstrate a long-running concurrency engine.

Bound outstanding requests and buffered events. Limit event frequency; coalesce progress without dropping final results. Use monotonically increasing sequences for operation events and provide status refresh after lost cursors. Child processes must remain in the plugin's supervised process group/sandbox.

## Contract and security tests

Test version mismatch, identity spoofing, unavailable required scopes, duplicate keys, escaped newlines, invalid UTF-8, unknown responses, large frames, stdout log pollution, hung initialization, canceled execution, unplanned core mutation, missing heartbeat, crash/restart reconciliation, signature mismatch and path escape. Test protocol implementation from at least two languages before claiming language neutrality.
