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
| inventory.list | connection | observed VMs, external ownership |
| inventory.get | connection, id | VM live/persistent XML and fingerprint |
| vm.plan | connection, id, action, input | immutable operation plan |
| plan.show | id | the immutable plan |
| operation.apply | apply: planID, planDigest, idempotencyKey, acknowledgements | durable Job |
| operation.list | none | durable jobs |
| operation.get, cancel, reconcile | id | Job with state/error |
| operation.watch | id, after | ordered events after cursor |
| import.inspect | path | bounded OVA inspection report |
| lab.validate, document.validate | path | schema/semantic validation report |
| backup.verify-manifest | path, input.root (optional) | manifest-checked result |
| plugin.validate | path | development manifest, distributionVerified=false |
| plugin.test | path | confined synthetic conformance report |

`vm.plan` currently supports start, stop, hard-stop, pause, resume, save,
restore-saved, autostart (`input.enabled`) and powered-off vCPU set (`input.vcpus`).
Other public workflow methods remain unimplemented. The mutable input is stored
separately from the immutable plan and checked against inputDigest. PlanDigest uses
RFC 8785 canonical JSON after excluding only `planDigest`; actor, connection,
resources, preconditions and acknowledgements remain in its digest.

Plans expire after 15 minutes. Repeating the same apply request/key returns the
same operation; changed input is rejected. Retention does not currently remove old
keys/events. Uncertain steps retain resource locks and require observation, not
replay. The current watch API returns a bounded event page; full push notifications,
retention resynchronization and all method-specific generated schemas remain work.
