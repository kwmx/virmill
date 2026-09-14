# Local API virmill/v1

Transport: JSON-RPC 2.0, one UTF-8 JSON object per newline, private Unix socket,
string request IDs, maximum 8 MiB, no duplicate keys or batches. Both peers verify
kernel UID. No TCP listener. The private protocol currently handles one request at
a time per connection, with at most 32 concurrent clients.

Result is `{apiVersion,data,warnings,error}`. Application errors contain code,
message, resource, operationID, retryable and safeNextActions. Protocol errors use
JSON-RPC error codes. Successful submission is queued/accepted, not completed.

| Methods | Request params | Response data |
|---|---|---|
| version | none | build and API metadata |
| host.inspect, host.doctor | none | read-only prerequisite checks |
| host.capabilities | connection | native connection capabilities |
| inventory.list | connection | observed VMs; managed ownership requires the local catalog and matching native identity |
| inventory.get | connection, id | VM live/persistent XML and fingerprint |
| storage.pool.list, storage.pool.get | connection / connection, id (pool UUID) | observed pools / one pool; unknown capacity is null |
| network.list, network.get | connection / connection, id (network UUID) | live/persistent network XML; isolationVerification=not-run |
| vm.plan | connection, id, action, input | immutable operation plan |
| vm.create | connection, id (successful preparation operation), input.identityMode/hardware | immutable define-last creation plan |
| vm.creation.result | id (creation/recovery operation) | operation and durable volume/definition receipt; guest verification remains false |
| vm.creation.resume | connection, id (uncertain creation/recovery operation) | fresh reviewed definition-only recovery plan |
| plan.show | id | the immutable plan |
| operation.apply | apply: planID, planDigest, idempotencyKey, acknowledgements | durable Job |
| operation.list | none | durable jobs, newest first, each with its plan's `operation`, `resourceIDs` and `targetName` (VM name) when known |
| operation.get | id | Job with state/error, plus `operation`, `resourceIDs` and `targetName` |
| operation.cancel, reconcile | id | Job with state/error |
| operation.watch | id, after | ordered events after cursor |
| import.inspect | path | bounded OVA inspection report |
| import.prepare | path, input.destination/systemID/disks | immutable all-disk preparation plan |
| import.result | id (operation ID) | durable PreparedImport receipt and directory |
| import.verify | path | PreparedImport receipt after declared file-hash checks |
| lab.validate, document.validate | path | schema/semantic validation report |
| backup.verify-manifest | path, input.root (optional) | manifest-checked result |
| plugin.validate | path | development manifest, distributionVerified=false |
| plugin.test | path | confined synthetic conformance report |
| plugin.develop | action=new/pack, path, input | immutable source/package creation plan |
| plugin.list, plugin.show | none / id | installations / retained immutable versions |
| plugin.plan | action, id/path, input | installation, enable, update, rollback, removal or scope plan |
| plugin.permissions | id | declared and installed permission scopes |
| plugin.call | connection, id, input.action/parameters/vmIDs | reviewed confined action plan |
| plugin.result | id (operation ID) | durable result bound to plan/package/input |

`vm.plan` currently supports start, stop, hard-stop, pause, resume, save,
restore-saved, autostart (`input.enabled`) and powered-off vCPU set (`input.vcpus`).
Other public workflow methods remain unimplemented. The mutable input is stored
separately from the immutable plan and checked against inputDigest. PlanDigest uses
RFC 8785 canonical JSON after excluding only `planDigest`; actor, connection,
resources, preconditions and acknowledgements remain in its digest.
The optional `review` object exposes handler-selected effect details, paths and
permissions; it is also included in the digest. Private signing-key bytes and
opaque VM XML are not copied into this public review object.

Plans expire after 15 minutes. Repeating the same apply request/key returns the
same operation; changed input is rejected. Retention does not currently remove old
keys/events. Uncertain steps retain resource locks and require observation, not
replay. The current watch API returns a bounded event page; full push notifications,
retention resynchronization and all method-specific generated schemas remain work.

Import preparation inputs and receipts use the bundled
`import-preparation-input` and `prepared-import` schemas. Preparation uses the
same apply/cancel/reconcile endpoints as other mutations. Its review binds every
disk mapping, source/tool digest, output location and space limit. A receipt
records file conversion checks with `vmDefined: false` and `guestBootVerified:
false`. Unconfirmed jobs expose `publicationStatus: unconfirmed`, not a false
assertion that publication never occurred. See [the workflow](import-preparation.md)
for cancellation and retained staging behavior.

Creation input uses the bundled `vm-creation-input` schema. The application
generates UUID/MAC identities during preview and records them in its immutable
input/review. The [creation workflow](vm-creation.md) documents explicit mapping,
retained volumes and separate registration/boot/setup/connectivity stages.
`vm.creation.resume` requires a complete verified set and transfers every original
resource lock transactionally. Jobs have additive optional `recoveryOf` and
`recoveryOperationID` links. The original job stays partial; canceled or failed
recovery retains inherited locks. Schema 2 prevents older database readers from
applying the pre-recovery cancellation rules. Reconciliation never replays effects.
