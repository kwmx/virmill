# Auxiliary authorization regression tests

`internal/helper/auxiliary_authorization_test.go` adds ten focused common-contract tests for the parent-owned `state.auxiliary` authorization path. They use generated Ed25519 keys and metadata entirely in memory. No key file, policy file, privileged endpoint, native VM, TPM/NVRAM file or captured bytes are accessed.

The scope contributes SEC-01 authorization prerequisites, JOB-02 fresh recovery-authentication prerequisites and SNAP-01 auxiliary-capture prerequisites. Successful authorization is not capture, a native inventory proof, durable delivery, restore or acceptance evidence.

## Contract tested

The reviewed parent changes are `internal/helper/auxiliary.go` and the `Request`, `Policy`, `SignedBytes` and `Authorize` integration in `internal/helper/authorization.go`. The final test run observed HEAD `60fed576a00088bf15af753592a256fb6ebabb37` with parent implementation work in progress. Exact reviewed working-file SHA256 values were:

```text
0d3d0c4dc6ee68e4ec8c64c9831a801eadc098cd531406fd97ca9ae704782ae6  internal/helper/authorization.go
6e8b5b5eb8f0c61f488e8d24c4934128b16e08cb3218990b14a84d49066292ff  internal/helper/auxiliary.go
```

The tests distinguish three assertions that could otherwise be confused: an unsigned field substitution invalidates the signature; a freshly signed disallowed request still fails policy/contract checks; and a valid legacy signature continues to authorize its original legacy action. The legacy payload-family tests start from a demonstrably valid legacy request before adding and freshly signing an auxiliary payload, so an unrelated UUID/signature failure cannot masquerade as the required denial.

| Test group | Observable assertion |
|---|---|
| Explicit policy and modes | Existing actors/keys/roots with nil or empty auxiliary permission deny inspect, capture and observe. A single explicit permission allows the supported modes; `AllowCapture:false` permits inspect only. |
| Freshly signed policy violations | Actor/key/VM/root mismatch, wildcard selectors, zero actor, revoked global actor/key/root, invalid public key, duplicate matching permission, permission-count overflow, byte/member budgets, invalid state UID/GID, noncanonical root and changed root mapping are refused. |
| Exact bounds | A matching permission plus 1,023 unrelated entries, exactly 128 directory records, and exact payload-byte/member limits remain representable at the authorization layer. Native directory contents remain executor-validated. |
| Stable identities | A valid non-v4 native VM UUID is supported; absent, wildcard, zero, uppercase and path-like native IDs are refused even when consistently substituted across request, inventory and policy. Virmill job IDs retain their existing v4 requirement. |
| Payload totals | Freshly signed capture and observe requests with underdeclared, zero-declared, overdeclared, over-policy or uint64-overflowing member totals are refused. |
| TPM control metadata | Optional `TPMLock` metadata is excluded from the payload byte/member budget. Its absence is representable; required producer-guard enforcement remains an executor precondition. |
| Payload/inventory confusion | Mixed ACL/auxiliary payloads, unsupported mode/version, malformed fingerprint, inspect carrying Expected, capture/observe missing Expected, wrong native identity/root/fingerprint and nil/empty/oversized collections are refused. |
| Nested signature binding | Actor/action/key/resource/root/plan/job/expiry/mode, native layout, secret references, root generation, directory metadata, member order and every member identity/access-metadata field change the signed bytes and cannot reuse the original signature. TPM control path/generation/presence are included too. |
| Fresh authentication and revocation | Wrong kernel peers fail. Grants expire exactly at expiry and reject durations above 15 minutes. Later observation requires a new signature and current capture permission. |
| Legacy signing compatibility | A local pre-auxiliary Request shape produces identical canonical signed bytes for directory creation and ACL grant/revoke, including cleared signature and omitted auxiliary field. Those legacy signatures still authorize the original requests; adding auxiliary payload then fails. |

## Identified issue and parent correction

Source review found that the initial authorizer compared `Expected.TotalBytes` with policy without checking the sum of `Members[].State.Size`. For example, a declaration of zero with 1,024- and 2,048-byte member claims bypassed that aggregate consistency condition. The finding was reported to the parent before any production fix by this agent.

The parent added subtraction-based overflow-safe accumulation bounded by `permission.MaxBytes`, followed by exact equality with the declared total. `TestAuxiliaryAuthorizationRejectsInconsistentPayloadTotals` covers both capture and observe, including a wrapping `MaxUint64 + 1` claim. The parent also added optional metadata-only `TPMLock`; the tests preserve its exclusion from payload totals while proving that its metadata remains signature-bound.

The initial aggregate behavior was identified from code, not retained as a pre-fix failing execution: the parent changed the production file before the new total regressions ran. The post-fix regressions passed. No further blocking common-authorization gap was reproduced in this scoped matrix.

## Checks run and limits

Initial authorization tests passed, then the matrix was expanded for the parent's aggregate/control-inode contract and additional identity/payload-family boundaries. The final command was:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race ./internal/helper \
  -run '^(TestAuxiliaryAuthorization|TestGrantBindsPeerActionResourcesAndPlan|TestAccessAuthorityBindsEveryReviewedField|TestAccessPathRejectsAliasesAndUnrelatedFiles|TestAccessBindingAllowsOnlyFreshAuthentication)' \
  -count=1
```

Result: `ok virmill.local/core/internal/helper 1.422s`. All ten new tests and the four selected existing authorization/binding tests passed; none of these cases require IPC or skip on unavailable native capabilities. The new file was formatted using the pinned toolchain and whitespace checks passed.

This matrix deliberately does not assert deep native member/path/owner/ACL/label/generation validity merely because `Authorize` succeeds. The executor must independently derive and compare the complete inventory, enforce producer guards, observe current stopped state, and capture only bound files. Signature coverage of an Expected field authenticates the submitted claim; it does not prove that claim true. Freshly authenticated observe requests still need immutable job/receipt binding and crash reconciliation in the executor. No runtime dispatch, sealed FD transport, root policy loading, durable journal, installed build or capture was exercised here.

Only the new test file and this review were edited by this agent. Production contracts, runtime integration, evidence ledger and release tracking remain parent-owned.
