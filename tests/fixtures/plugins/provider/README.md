# Simulated provider contract fixture

From the source repository, run `scripts/build-provider-fixture.sh`. It uses the
pinned offline toolchain and writes a development manifest containing the actual
executable digest into `build/provider-conformance`.

For the complete opt-in Go/Python action and provider test suite, first run
`scripts/build-conformance-fixtures.sh`. This also builds the SDK summary action
required by `VIRMILL_TEST_CONFORMANCE=1`; a fresh checkout needs no preexisting
untracked fixture source. Development manifests must be ordinary files no larger
than 1 MiB; symlinks, directories and FIFOs are refused before execution.

With the development coordinator running, use:

```sh
virmill plugin validate build/provider-conformance
virmill plugin test build/provider-conformance --output json --timeout 90s
```

In the TUI, open **Plugins**, select **plugin test**, and enter the absolute
`build/provider-conformance` path. Both interfaces invoke the same confined
conformance service. The service creates a private disposable workspace, exercises
fake JSON resources, and removes that workspace after reporting its observations.
Failures return an error with completed checks; a partial provider receipt remains
explicitly partial after reconciliation. This command does not install a provider,
manage a remote host, or qualify a real VM. Other provider implementations are
outside this exact reference-fixture test profile.

This SDK executable changes only generated JSON records in its private workspace.
It has no libvirt, guest, network, USB, image, remote-service or credential access.
Its provider ID is `fixture`; its single connection is `fixture:///default`.
Every resource contains that namespace, a stable external ID and the explicit
capability model. A successful fixture effect is **simulated-contract** evidence.

The normative contract is [protocol 1.0](../../../../virmill-v1-spec/docs/12-plugin-protocol.md#provider-extension-contract).
This fixture covers the method families required by EXT-02 and contributes
SDK dispatch/error cases to EXT-01/EXT-03. It does not certify a real remote
provider, host performance or the remaining plugin acceptance scenarios. The
separate shared conformance service checks actual confinement and CLI/TUI access
for this exact reference fixture; it does not add a real remote backend.

## Supported methods and local payload shape

These parameter shapes belong to the fixture; they do not change the core
protocol or introduce a remote-provider API contract. `connectionID` may be
omitted to select the single simulated connection; another value is unsupported.
`describe` returns method input/output schemas and capabilities.

| Method | Parameters | Result |
|---|---|---|
| `provider.capabilities` | `{}` | Explicit capabilities and `simulated: true` |
| `provider.inventory` | Optional `limit`, `cursor` | Ordered resources, generation, nullable `nextCursor` |
| `provider.get` | `id` | One namespaced resource and its capabilities |
| `provider.plan` | `operation`, plus `name` for create or `id` for an existing resource | Effects, result state and state-bound `planToken`; no file/resource mutation |
| `provider.apply` | Same effect fields, `operationID`, `idempotencyKey`; optional `planToken`, synthetic `partial` flag | Original operation receipt, or a typed refusal/partial error |
| `provider.status` | `operationID`, `idempotencyKey`, or resource `id` | Persisted receipt; no readiness inference or mutation |
| `provider.reconcile` | Same selectors as status | Exact original receipt plus recorded observation; no lifecycle/resource change |
| `provider.cancel` | Optional operation/resource selector | `UNSUPPORTED_CAPABILITY` |

Multiple status/reconcile selectors must refer to the same receipt/resource.
With only a resource ID they select that resource's latest operation. Deleted
resources are absent from inventory/get; tombstones and operation receipts remain
available through status/reconcile.

Lifecycle operations are `create` → `defined`, `start` → `running`, `stop` →
`stopped`, and `delete` → a retained `deleted` tombstone. Start accepts defined or
stopped resources. Stop requires running state. Deletion requires a stopped or
never-started resource. Unrelated/unsupported operation names cannot reuse those
effects. Configuration edits, networks, devices, snapshots, backups, guest
transports and lifecycle cancellation are explicitly false. Reboot, pause,
restore, USB and backup requests return unsupported capability.

The external ID is `fixture-` followed by the original create operation ID. Names
are display data. Operation IDs and keys are bounded opaque ASCII identifiers;
the existing `op-1` / `key-1` conformance example remains valid. A used operation
ID cannot acquire a new key. A used key cannot change operation, target, name,
operation ID or plan token. Identical retries return the original receipt and
never repeat a lifecycle change.

For compatibility with the existing core provider conformance calls, `apply`
accepts an omitted plan token. Supplied tokens must match the reviewed effect and
resource revision. The token is a predicate, never an authorization grant. The
host remains responsible for plan approval and scoped execution authority. The
synthetic `partial` flag selects a fault after the durable effect; it is not part
of logical effect identity, and changing that flag cannot replay an effect.

## Partial effects and durability

A `partial: true` apply writes its resource and receipt, then returns
`PARTIAL_EFFECT`. The receipt retains `status: partial`. Status and duplicate
apply cannot silently change that history; a new lifecycle step is refused until
explicit reconciliation. Reconcile compares the current resource with the
original receipt and refuses drift. It can persist `reconciled: true` in the
local journal, while preserving the resource, its generation and the original
partial status. This is read-only with respect to simulated provider effects;
it is not a claim that reconciliation writes no local observation record.

The private directory must have mode 0700. State format version 1 is a bounded
mode-0600 JSON document. It validates identity/key/receipt links on restart and
uses a unique temporary file, file sync, rename and directory sync. Any failed
persistence acknowledgement makes that process refuse further work until restart
and reconciliation; it cannot replay from stale in-memory state. The old
unversioned 0.1 fixture state is refused, not silently migrated. Use a fresh
private workspace when switching fixture versions.

This fixture assumes one provider process per private workspace. The SDK
serializes concurrent requests through the fixture mutex. Independent simultaneous
writer processes are unsupported; this fixture does not claim distributed
locking. Process tests kill only their own generated child and reuse its workspace
after it has been reaped.

## Pagination and bounds

Resources sort by stable external ID. Pages default to 25 items and permit 1–100.
The opaque cursor authenticates its connection, resource generation, last ID and
page size using a randomly generated key held in the private fixture state.
Changed/forged cursors fail; any lifecycle change invalidates outstanding cursors.
Receipt-only reconciliation preserves the inventory generation. Callers must
repeat the page size on continuation and restart from the first page after a
stale-cursor error.

The fixture permits at most 1,024 retained resources (including deleted
resources), 2,048 operation receipts and a 4 MiB durable state file. Parameters
are at most 16 KiB, cursors at most 1,024 bytes, names at most 128 UTF-8 bytes,
operation IDs at most 120 ASCII characters, and keys at most 128. The SDK adds
its protocol frame, nesting, concurrent-request and heartbeat bounds. The bounded
history has no implicit garbage collection or identity reuse.

## Run locally, offline

From the repository root:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go -C tests/fixtures/plugins/provider test -mod=mod -count=1 -v ./...
GOPROXY=off GOSUMDB=off ./scripts/go -C tests/fixtures/plugins/provider test -race -mod=mod -count=1 ./...
```

This standalone module has only a repository-local SDK replacement and uses no
third-party downloads. `-mod=mod` selects that replacement; it does not modify
root dependencies. The subprocess tests execute the compiled test binary as the
fixture entry point, using bounded ordinary pipes and temporary generated state.
They require no libvirt host or privilege escalation.

Tests cover the SDK initialize/describe/unknown-method/error dispatch, inventory,
get, capabilities, lifecycle, status, explicit unsupported cancellation, real
child termination/restart after a durable partial effect, stable IDs, deduplication,
stale plans, malformed parameters, corrupt state and lost persistence acknowledgements.
The existing core `TestProviderCrashReconcileFixture` remains a separate confined
host harness, executed by the integration owner.

`TestProviderInventoryMeasurements10_100_500` creates and syncs actual generated
resources, reloads their state, then measures direct handler pagination plus JSON
encoding. It logs elapsed time, page count, response bytes and durable-state bytes.
One local run measured 10/100/500 resources in 94.242 µs / 156.173 µs / 1.40686 ms
with 1/1/5 pages and 11,726 / 115,947 / 579,147 state bytes. These are observed
samples, not thresholds or release budgets. They do not measure TUI latency,
confinement, network transport or real VM capacity.
