# Cross-build contract

Run `./scripts/cross-build.sh` from the checkout. It builds every declared target;
it takes no arguments and cannot select a smaller successful matrix. The existing
test targets are `darwin/arm64` and `windows/amd64`.

This supplies compile-only evidence for REL-02. The
[acceptance scenario](../virmill-v1-spec/docs/13-testing-and-acceptance.md) requires
dependency-free core/protocol packages and sample plugins to build on declared
test targets. The [architecture contract](../virmill-v1-spec/docs/01-architecture-and-go-decision.md)
requires portable common packages without claiming a working macOS or Windows VM
manager. Linux x86-64 remains the v1 host baseline; cross compilation does not
qualify a host adapter, terminal, sandbox, SQLite runtime, firmware or guest.

Each target has exactly 13 artifacts:

| Component | Output below the target directory | Build boundary |
| --- | --- | --- |
| Domain | `core/internal-domain.a` | Common package archive |
| Provisioning | `core/internal-app-provision.a` | Common package archive |
| Strict wire JSON | `core/internal-wire.a` | Common package archive |
| Validation | `core/internal-validation.a` | Common package archive |
| Operations | `core/internal-operations.a` | Common package archive |
| Shared CLI | `core/internal-ui-cli.a` | Common package archive |
| Shared TUI | `core/internal-ui-tui.a` | Common package archive |
| Go SDK | `sdk.a` | SDK package archive |
| SDK protocol | `protocol.a` | Protocol package archive |
| SDK summary action implementation | `sdk-summary.a` | Library archive; no executable entrypoint is generated |
| SDK tests | `sdk.test` or `sdk.test.exe` | Test binary compiled with `test -c`, never run |
| Standalone summary action | `vm-summary` or `vm-summary.exe` | Executable from `examples/plugins/vm-summary` |
| Reference provider | `provider-fixture` or `provider-fixture.exe` | Executable from `tests/fixtures/plugins/provider` |

The reference provider is the current simulated fixture. Building it does not
execute its fake effects or establish plugin conformance on either foreign
target. The action and provider binaries are compile artifacts without new
target-specific signed packages or installation manifests. The SDK action
library is named `.a` on both targets to avoid representing it as an executable.

The script uses the repository-local toolchain through `./scripts/go`. It forces
`GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `CGO_ENABLED=0`, `GOENV=off`,
`GOWORK=off` and module mode. It clears caller `GOFLAGS`, experiments and Go tool
directory overrides, selects each exact `GOOS`/`GOARCH`, and fixes architecture
tuning to `GOAMD64=v1` and `GOARM64=v8.0`. Builds use `-trimpath`,
`-buildvcs=false` and an empty linker build ID. The root module uses its checked-in
vendor tree. The SDK and samples use `-mod=readonly`; the reference provider's SDK
dependency resolves through its existing local replacement. No download, module
update, Git command, native compiler or foreign executable is required.

“Dependency-free” here means no native C/libvirt dependency is needed for this
compile check. Higher shared packages still use pinned vendored Go dependencies.
CGo-disabled SQLite code compiling does not prove a functional database-backed
coordinator. Full Linux binaries and native behavior require their separate
build and runtime checks.

Before generating outputs, the script verifies required input paths and both
targets in the pinned compiler's `tool dist list`. Every run gets a separate
`build/cross/run.*` directory, with target subdirectories and these inventories:

- `toolchain.txt`: the actual compiler version and build-host platform.
- `build-settings.txt`: the fixed environment and flags.
- `artifacts.txt`: all 26 relative artifact paths in matrix order.
- `artifact-sha256.txt`: SHA-256 of each artifact, attributed to the same relative
  path; generated only after all builds succeed and every output is a nonempty
  regular file without a symlink.

Failures stop the matrix, preserve the partial run directory for inspection and
omit the completed checksum inventory. Previous run directories are preserved;
their artifacts cannot satisfy a missing output in a new run. Output hashes bind
the observed artifact bytes. They do not independently establish source
provenance or deterministic reproduction across changing source revisions; the
evidence recorder must bind the exact immutable source used for qualification.

`python3 -B tests/integration/cross_build_contract_test.py -v` exercises the shell
orchestration with a generated compiler substitute: exact targets and flags,
separate outputs and hash attribution, missing targets/inputs, compiler failure,
missing/empty/symlink artifacts and stale-output refusal. These tests are distinct
from running the real pinned cross compiler. `sh -n scripts/cross-build.sh` checks
shell syntax.

During authoring on 2026-09-08, Go 1.27.1 on Linux/amd64 completed the full
26-artifact matrix. The eight orchestration tests passed without skips. A repeat
attempt correctly stopped on a concurrent shared CLI source edit with an
undefined symbol and retained its partial outputs; it provides no reproducibility
result. Final release evidence must run the complete matrix again on frozen
source. No target executable was run during these authoring checks.
