# VM Summary — runnable reference plugin source

This small Go program demonstrates Virmill plugin protocol 1.0 using **synthetic VM records supplied in each request**. It does not call libvirt, inspect your machine, execute guest commands, create VMs or implement the main application. It uses only the Go standard library.

## Build and test

The source uses Go 1.22-compatible language features. Build with an appropriately maintained installed Go toolchain; this is not a claim that the product will pin the old 1.22 toolchain.

```sh
go test ./...
go build -o vm-summary .
python3 smoke_test.py ./vm-summary
```

The Python test requires Python 3.10 or newer and only its standard library. It checks initialization, metadata, read-only planning, synthetic execution, stale context, empty selection, unknown methods, duplicate keys, denied permission and protocol mismatch. The included Go tests exercise frame validation. The smoke test does not prove host sandboxing or hardware behavior.

## Input and output

Action ID is `summary`. `input` accepts optional `sortBy: "name" | "state"`. `context.selectedVMs` contains one or more `{id, name, state}` records selected and redacted by the future host. Request the `vm.read` permission with scope `selection`.

Initialize, call `describe`, then `action.plan`. Pass the returned `planToken` unchanged with the same input/context to `action.execute`, adding `operationID` and the host's grant references. It returns `{count, rows}`. A changed context is rejected. The opaque token is not an authorization grant or a substitute for the host's independently approved canonical plan.

The source manifest declares only Linux/amd64 because no distribution binaries are included. The dependency-free program can be cross-built for other supported Go targets; add verified platform artifacts/digests when actually packaging them. Do not relabel a Linux executable as a Windows/macOS binary.

## Deliberate example limits

The program is synchronous and immediately read-only. It has no long-running operations, progress/heartbeat loop, concurrent cancellation engine, persistent effects, host-API client, sandbox runner or signing mechanism. It is not the production SDK. Empty selection is rejected. It requires the protocol's baseline 8 MiB allowance and caps selection at 5,000 records.

Production plugins must use the complete SDK/conformance framework the implementation agent is required to deliver. See `docs/11-plugin-development.md` and `docs/12-plugin-protocol.md` in the specification root for the full contract. This source is provided to make the wire format tangible, not to imply the application or SDK has been built.
