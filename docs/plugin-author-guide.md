# Plugin development

The wire contract is [protocol 1.0](../virmill-v1-spec/docs/12-plugin-protocol.md).
The independently versioned Go SDK under `sdk/go` imports neither libvirt nor core
internals. `virmill.local/sdk` is a local source coordinate, not a published service.

## Generate and build

With `virmilld` running, create a reviewed source plan:

```sh
virmill plugin new ./vm-report --language go --type action \
  --id dev.example.vm-report --sdk-directory /absolute/virmill/sdk/go --plan
```

Replace the example ID with your own namespace. Installed packages default to
`/usr/share/virmill/sdk/go`; source runs may set `VIRMILL_SDK_DIRECTORY` on the
coordinator or use the explicit flag. Without PATH, the last component of the ID
becomes the directory name. Relative paths are resolved by the CLI/TUI before RPC.

Inspect the plan's `review`, paths, file digests and acknowledgement IDs. Apply
using the actual values returned; example identifiers are not real authorization:

```sh
virmill plan apply PLAN_ID --digest PLAN_DIGEST \
  --idempotency-key YOUR_UNIQUE_KEY --ack write-plugin-artifact --wait
```

The generator creates source, manifest, tests, README and a copy of the reviewed
SDK with a module-local replace directive. It never downloads imports or runs a
build. Inside the new directory, use the pinned toolchain:

```sh
go test -buildvcs=false ./...
go build -buildvcs=false -o vm-summary .
```

`virmill plugin validate DIRECTORY` checks the manifest. `plugin test DIRECTORY`
runs the confined synthetic summary conformance fixture. These checks do not grant
persistent permissions or certify other action/provider implementations. The harness
covers identity/protocol negotiation, description, unknown methods, no-effect
planning, execution, stale context and shutdown in Go and Python. Source tooling
`cmd/virmill-dev scaffold DEST SDK_DIR ID` remains available for development.

## Sign and install

Prepare a dedicated distribution directory containing the manifest, executable,
runtime files, license and optional documentation. Set every entrypoint's actual
SHA-256 in the manifest. Symlinks, special files, duplicate/colliding paths,
unsigned extras and hidden trailing archive bytes are rejected.

Use an existing local 64-byte Ed25519 private key encoded as hexadecimal, stored
outside the distribution directory. No key is generated or published implicitly.
Tests use ephemeral fixture keys. Private key bytes do not enter plans, journals
or packages; plans retain its file reference.

```sh
virmill plugin pack ./distribution --key-id YOUR_KEY_ID \
  --signing-key-file /private/path/signing-key.hex \
  --destination ./vm-report.tar --plan
```

Apply the reviewed output plan with `write-plugin-artifact`. The archive contains
a sorted file inventory in `package-index.json`, canonicalized using RFC 8785.
`package-index.sig` is the raw 64-byte Ed25519 signature over those exact bytes.
The inventory's SHA-256 is the package identity; each entry binds path, size,
executable status and SHA-256. `formatVersion` is `1`.

Obtain the author's public key through your reviewed trust procedure. Installation
requires its actual 32-byte hexadecimal value and explicit requested grants:

```sh
virmill plugin install ./vm-report.tar --key-id YOUR_KEY_ID \
  --public-key REVIEWED_PUBLIC_KEY_HEX \
  --input '{"permissions":[{"name":"vm.read","scope":"selection"}]}' --plan
```

The plan shows package, key fingerprint and permissions. Apply with every listed
acknowledgement: `plugin-installation-change`, the exact `trust-key-sha256:...`
and any `grant:...` entries. Unknown keys are not globally trusted on first use.
Checksums alone do not establish author identity.

Install starts disabled. Inspect with `plugin list`, `plugin show ID` and
`plugin permissions show ID`. Plan `plugin enable ID`, then apply its exact plan.
Only the currently implemented confined action capabilities can be enabled.

## Invoke and manage versions

```sh
virmill plugin call dev.example.vm-report summary \
  --input '{"parameters":{"sortBy":"name"},"vmIDs":["SELECTED_VM_UUID"]}' --plan
```

Planning runs under confinement and receives only selected VM IDs, names and states.
A reviewed invocation binds their fingerprints, package digest, action schema, input
and expiring grants. Apply with the exact `invoke-plugin:dev.example.vm-report`
acknowledgement. `plugin result OPERATION_ID` reads the schema-checked durable
result. This runtime supports read-only actions; it rejects mutating effects and
denies all plugin-origin host API requests.

`plugin update ARCHIVE` uses the same key/grant options as install. It retains the
prior version and disables the new one pending enable review. `plugin rollback ID`
switches to the prior retained package, disabled. Replacing an already known semantic
version with different bytes is refused.

`plugin permissions grant ID` and `revoke ID` accept an explicit `permissions`
array through `--input`. They disable new invocations. `plugin disable ID` also
blocks new work. If active or uncertain invocations exist, supply
`--input '{"activeJobs":"finish"}'` after reviewing them, or resolve them through
Jobs first. They remain pinned to their original package. Removal retains package
recovery data and every plugin-created resource; no data migration runs.

All actions are reachable in TUI Plugins. Mutation forms accept JSON with `id` or
`path` and `input`; for calls, `input.action` supplies the action ID. Review the
returned plan and type its full digest to authorize the listed effects.

## Failure behavior and remaining contract

Changed sources, stale VM facts, new active jobs and revoked scopes invalidate
apply. Files are staged privately and never overwrite an existing destination.
Activation commits after verified payloads are flushed. Lost acknowledgement after
activation resolves by observing the exact committed plan. An uncertain invocation
without a durable result remains recovery-required. Detachment never authorizes
replay or kills the coordinator's job.

The SDK handles concurrent cancellation, serialized writes and heartbeats. The host
bounds frames, output and deadlines, including a plugin refusing to read stdin.
Bubblewrap, namespaces, seccomp and process resource limits are required; there is
no unconfined fallback. The process gets a read-only verified package and private
workspace, with no home, database, host devices, network, credentials or helper socket.

Unsigned/development installation override, quarantine, typed mutation mediation,
HTTP/artifact/secret brokers, persistent data migrations, structured contribution
forms and full provider/action conformance remain mandatory unfinished work.
This is a development extension platform, not accepted Virmill 1.0 support.
