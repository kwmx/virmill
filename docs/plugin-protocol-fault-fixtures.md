# Python protocol fault fixtures

`tests/fixtures/plugins/protocol-faults/main.py` is an executable, dependency-free
Python peer for the host runner tests. It is deliberately invalid in selected
cases and is not an installable plugin. It has no manifest, package signature,
provider, guest, or external service. It never performs a VM operation or opens a
network connection. The only file probe reads a generated test fixture through the
real confined runner.

The tests extend the existing Go/Python summary conformance and Go SDK cancellation
tests with observable failures across process pipes. `fixtureFault` is a private
test selector supplied in request parameters; it is not a plugin API or permission.

## Evidence boundaries

`TestPythonProtocolFaultTransport` runs the checked-in Python peer as the ordinary
user in a private temporary working directory. Its constructor is an explicitly
**test-only transport seam**. Production `Session.Call`, JSON validation, framing,
bounded logging, cancellation, and `Session.Close` consume the real process pipes.
The seam reconstructs the reader/process wiring because the production constructor
always requires confinement. It does not bypass or alter production permission
checks. It supplies transport regression evidence only, never sandbox evidence.

`TestPythonProtocolFaultConfined` runs the same fault matrix through production
`Start` and its bubblewrap/seccomp/resource-limit constructor. It first probes the
real sandbox. An unavailable sandbox is explicitly skipped, not counted as a
successful confined run. With `VIRMILL_TEST_CONFORMANCE=1`, unavailable confinement
is a test failure. There is no unconfined fallback in this test or in production.

`TestPythonProtocolSandboxBoundaries` uses only the production confined constructor.
It checks that an undeclared generated host file is unreadable, AF_INET and AF_UNIX
socket creation return `EPERM`, sensitive environment variables are absent, and
the worker UID is non-root. Socket creation probes never connect to an endpoint.
The generated host fixture must remain unchanged.

These tests contribute simulated protocol evidence to EXT-01/EXT-03 and focused
local boundary evidence to SEC-04/SEC-05. They do not promote any acceptance scenario
to release verification. The full grant substitution/expiry matrix, installed
package identity/trust, guest/hardware qualification, required OS/MAC matrix,
heartbeat/ping policy, graceful cancellation protocol, descendants, and independent
provider reconciliation retain their separate evidence requirements.

## Fault matrix

| Fixture selector or test | Observable requirement |
| --- | --- |
| `partial-exit` | An incomplete JSON response followed by exit is never a successful result. |
| `unterminated-exit` | Even complete JSON without its physical newline is rejected on exit. |
| `duplicate-key` | Escaped-equivalent duplicate `result` keys fail the session. |
| `oversized-line` | A response exceeding 8 MiB fails promptly, terminates its writer, and cannot leave a reusable session. |
| `wrong-id` | A response to an unissued host ID fails the session. |
| `invalid-utf8` | Invalid UTF-8 inside a response fails the session. |
| `stdout-log` | A non-protocol diagnostic line on stdout fails the session. |
| `request-response-hybrid` | An envelope containing both a method and a result fails the session. |
| `wrong-version` | A JSON-RPC 1.0 envelope cannot satisfy a 2.0 request. |
| `unknown-notification` | An unknown unsolicited notification fails the session. |
| `negotiated-version-mismatch`, `missing-protocol-version`, `non-object-initialize-result` | Initialization must select version 1.0 in an object result. Invalid negotiation terminates the worker and cannot be retried on the same session. |
| `stderr-flood` | A 256 KiB stderr flood retains only 64 KiB; the following invalid response still fails promptly. This checks size, not display redaction. |
| `apply-before-initialize` | A plugin host-API request before initialization cannot establish a successful session, even if that API request was denied. |
| `apply-during-plan` | A read token and claimed read grant cannot turn `host.operation.apply` into success. The peer checks the correlated `PERMISSION_DENIED` host response. A valid application error does not poison the transport. |
| `hang` | Initialization and execution deadlines fail without a result and terminate the worker. Explicit cancellation is issued only after the peer reports entering the active work boundary. |
| `stop-reading` | A request larger than the pipe capacity cannot trap the coordinator's write past cancellation/deadline. |
| `flood-after-reply` | Closing a peer that floods stdout after a result is bounded, reaps the worker, and prevents later success from queued output. |

Every terminal framing/envelope/correlation test checks that the process has been
reaped and a later `ping` cannot succeed. Time bounds are test responsiveness checks,
not product performance promises. Hung peers produce no fake progress or effects.
The mutation fixture reports only the observed host denial; it has no mutation
adapter and never touches a real resource.

## Running

Use the pinned repository toolchain and vendored dependencies:

```sh
./scripts/go test -mod=vendor ./internal/plugins -run '^TestPythonProtocolFaultTransport$' -count=1 -v
./scripts/go test -mod=vendor -race ./internal/plugins -run '^TestPythonProtocolFaultTransport$' -count=1 -v
VIRMILL_TEST_CONFORMANCE=1 ./scripts/go test -mod=vendor ./internal/plugins -run '^TestPythonProtocol(FaultConfined|SandboxBoundaries)$' -count=1 -v
```

The confined command requires an ordinary Linux/amd64 user, working user/network
namespaces, bubblewrap, prlimit, and `/usr/bin/python3`. It must not be run as root.
Do not install packages or change host security policy just to obtain a pass. A
blocked sandbox is a capability/evidence limitation. Run only on an authorized
environment, recording the exact revision, fixture digest, environment and result
in the project evidence ledger.
