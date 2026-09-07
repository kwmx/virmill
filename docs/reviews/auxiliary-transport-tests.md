# Auxiliary inspection transport regression tests

`internal/helper/auxiliary_transport_linux_test.go` adds ten Linux/amd64 tests for the typed metadata inspection decoder. The fixtures use private AF_UNIX socket pairs, generated request metadata and synthetic sealed/unsealed memfds. They exercise the production `receiveAuxiliaryInspection`, `ValidateAuxiliaryInspection` and descriptor-aware framing code through the existing test-only connection seam after peer authentication. They do not impersonate an authenticated root endpoint or read native TPM/NVRAM files.

This is local transport/contract evidence contributing SEC-01 helper-boundary, JOB-02 refusal/recovery-status and SNAP-01 auxiliary prerequisites. It does not prove installed helper authentication, crash reconciliation, native capture, restore or any complete acceptance scenario.

## Observed implementation

The final run on 2026-09-08 observed HEAD `60fed576a00088bf15af753592a256fb6ebabb37` with parent implementation changes in the working tree. The reviewed production file SHA256 values were:

```text
4ef7ba2d0d2cac71e01796cec3053e5d238c3b371a95f713d6852e48f3836d73  internal/helper/auxiliary.go
0ef154fd582c62d269e829d3fb5899e98ae41a3612c4f286125afa619c75062d  internal/helper/auxiliary_client_linux.go
2a08a1eb982b52fb0161ef9b10d531b865f49d7f751755d46253ddda2d9c386c  internal/helper/sealed_linux.go
```

`Client.InspectAuxiliary` still enters through `connectRequest`, which owns request authentication and root-peer verification. The decoder accepts one bounded metadata frame with no descriptor. A received descriptor is always an inspection failure, including a correctly sealed one. The older `ReceiveSealedSnapshot` entry point still requires a sealed descriptor.

## Observable assertions

| Test group | Assertion |
|---|---|
| Valid response and fragmentation | Complete and 1-, 17- and 4,096-byte write chunks produce exactly the expected typed metadata, without changing descriptor ownership. Stream chunk boundaries may coalesce in the kernel. |
| Typed response confusion | Wrong outer/auxiliary/inventory versions, job, binding, stage, artifact, resource provider/connection/kind/UUID, layout UUID, fingerprint and root ID yield an error and the zero auxiliary result. Nil, empty or oversized inventories and inconsistent/overflowing payload totals also fail. |
| Request confusion | A previously valid response cannot be reused after actor, key, plan, job, operation or inspect mode changes, or with ACL payload, absent/future auxiliary request or capture Expected inventory. |
| Root syntax | Relative, aliased and root-only paths, NUL, newline, tab, DEL, Unicode format controls, backslash and a 4,097-byte root path are refused. |
| Response families | Success plus error/code, success plus ACL data including explicit JSON null, failure plus auxiliary/ACL data and an empty refusal cannot yield a proof. |
| Error mapping | PERMISSION_DENIED, SOURCE_CHANGED, STALE_PLAN, UNSUPPORTED_CAPABILITY, INVALID_INPUT, INVALID_STATE, INCOMPLETE_BACKUP and OPERATION_FAILED remain typed refusals. Empty, success-like and unknown codes become OPERATION_FAILED; no refusal returns partial metadata. |
| Framing | Empty/truncated JSON, exact/escaped/nested duplicate keys, invalid UTF-8, unknown fields, wrong field type, two frames, trailing values, EOF without newline and oversized frames are refused. Duplicate-key cases retain all otherwise-valid response fields and repeat the same value, so an unrelated missing field cannot explain rejection. |
| Descriptor rejection and cleanup | Sealed, unsealed, two-descriptor and truncated ancillary data are refused. A descriptor combined with an error, malformed JSON or oversized frame is also refused. Each exchange checks the process FD count returns to its baseline before test cleanup. |
| Cancellation | A partial frame first installs one received sealed descriptor, proven by the FD-count increase. Cancellation interrupts the receive within one second, returns no proof and closes that descriptor. |
| Existing sealed receiver | The exact metadata frame accepted without an FD by inspection fails the old sealed receiver with nil frame and nil FD. The separate existing positive transfer/immutability test still passes with a sealed FD. |

For malformed exchanges the sender must finish delivering its bytes, and a timeout alone cannot satisfy rejection. Deadlines bound the test sender and receiver. Tests run serially because their FD-count assertions observe process-wide ownership; they do not use `t.Parallel`.

## Reproduced root-path defect and parent fix

The first required-IPC regression used an otherwise valid inspection with `Root.Path` set to `/approved\x00other`. The original absolute/clean/nonroot path check accepted the embedded NUL. The production decoder returned an `inspected` result with nonnil inventory and `err=<nil>`, which failed `TestAuxiliaryTransportRejectsRootNUL`.

The minimal command was:

```sh
GOPROXY=off GOSUMDB=off VIRMILL_TEST_REQUIRE_IPC=1 ./scripts/go test -mod=vendor -race ./internal/helper \
  -run '^TestAuxiliaryTransportRejectsRootNUL$' -count=1 -v
```

The first sandbox execution stopped at `net.FileConn` with `getsockopt: operation not permitted`; that was a capability failure, not evidence about the decoder. The explicitly authorized ordinary-user IPC escalation then reproduced the logic failure (`FAIL`, helper `0.014s`). The finding and command were sent to the parent before any production correction by this agent.

The parent added `auxiliaryAbsolutePath`: bounded valid UTF-8, canonical absolute nonroot POSIX syntax, no backslash, control characters or Unicode format characters. Both auxiliary policy and typed response validation now use it. The parent also retained native inventory error codes STALE_PLAN and INCOMPLETE_BACKUP. The post-fix matrix includes the NUL regression, representative additional path violations and those mappings. This demonstrates malformed-response rejection; it does not establish an unprivileged exploit against the authenticated root helper.

## Executed checks

The full post-fix matrix passed with real private descriptor IPC, required capability checking and the race detector. After tightening the duplicate-key fixtures, the final run was:

```sh
GOPROXY=off GOSUMDB=off VIRMILL_TEST_REQUIRE_IPC=1 ./scripts/go test -mod=vendor -race ./internal/helper \
  -run '^TestAuxiliaryTransport' -count=1 -v
```

Result: all ten new tests and their subtests passed, no skips or race reports; `ok virmill.local/core/internal/helper 1.205s`.

The existing positive compatibility check was also executed:

```sh
GOPROXY=off GOSUMDB=off VIRMILL_TEST_REQUIRE_IPC=1 ./scripts/go test -mod=vendor -race ./internal/helper \
  -run '^TestSealedSnapshotTransferAndImmutability$' -count=1 -v
```

Result: passed without a skip or race report; `ok virmill.local/core/internal/helper 1.011s`. Both successful commands used the approved ordinary-user private-IPC escalation after the outer sandbox denied socket introspection. No host configuration, privileged endpoint, remote host or VM was used. The new Go file was formatted with the pinned toolchain; whitespace checks passed.

## Remaining boundaries

The request binds a root ID, not a trusted root path string. The decoder checks that ID and path syntax; exact policy root mapping, native member paths, owners, labels, generations, layout truth and producer guards remain privileged executor responsibilities. A syntactically valid metadata claim from this test seam does not prove those facts. The tests also do not cover installed socket ownership/credentials, durable capture journals, FD delivery acknowledgment, encrypted publication or crash/reboot recovery.

No further blocking defect was reproduced in this transport matrix after the parent correction. Only the new test file and this review were edited for this assignment. Runtime changes, evidence ledger and release tracking remain parent-owned.
