# Plugin development

The wire protocol is [protocol 1.0](../virmill-v1-spec/docs/12-plugin-protocol.md).
The public Go SDK is a separate local source module under `sdk/go`. It imports
neither libvirt nor the application's internal packages. `virmill.local/sdk` is
an offline module coordinate, not a published package service.

Create a buildable developer example with the source tooling:

```sh
./scripts/go run -tags libvirt_dlopen ./cmd/virmill-dev scaffold \
  /absolute/new/plugin /absolute/virmill/sdk/go dev.example.vm-report
```

The scaffolder copies the current SDK into the plugin source, adds a module-local
replace directive, executable source, manifest, test and README. Build with the
pinned toolchain (`go build -o vm-summary .`) and run `go test ./...` inside the
scaffold. No nonexistent SDK URL is fetched.

With the coordinator running, `virmill plugin validate DIRECTORY` validates source
metadata; `virmill plugin test DIRECTORY` executes a confined synthetic summary
conformance fixture. Both actions are available in TUI Plugins. These commands do
not install or grant persistent permissions. The current harness checks negotiated
identity/version, description, unknown methods, no-effect planning, execution,
stale context and shutdown. It uses no real VM inventory. The SDK and Python fixtures
both pass these checks; a simulated provider also persists a fake partial effect and reconciles it after restart. Broader extension lifecycle and pagination conformance remain work.

The SDK registers context-aware handlers, processes cancellation concurrently,
serializes stdout writes and emits heartbeats. Handlers must honor cancellation;
malicious or unresponsive code remains the supervisor's responsibility. The host
runner bounds frames, output buffering and initialization/call timeouts. Plugin-origin
host requests are denied in the current conformance invocation.

Distribution packing uses real executable SHA-256 values, an exact sorted file
inventory, RFC 8785 canonical bytes and Ed25519. The raw 64-byte signature covers
the canonical index, and the canonical index's SHA-256 is the package identity.
The development pack command accepts a local hex-encoded Ed25519 private key file;
keys are never generated or published implicitly. `Pack`/`Verify` tests generate
ephemeral test keys in memory. No release signing identity has been selected.

Full `plugin new/pack/install/update/rollback/permissions/call` command integration,
atomic version activation, invocation-grant binding, brokered HTTP, provider
conformance and structured plugin forms are not complete. Do not advertise this SDK
snapshot as the accepted extension platform. Normal sandbox execution fails closed
when bubblewrap, namespaces or resource constraints are unavailable; there is no
unconfined fallback.
