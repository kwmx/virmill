# 01 — Architecture and Go/Rust decision

## ADR-001: Choose Go for the core

Both Go and Rust can target Linux, macOS and Windows. Both have libvirt bindings; Rust is not rejected because it lacks them. Go's libvirt bindings are CGo bindings, so this application must not promise a fully static, dependency-free virtualization executable. [S01] [S02] [S04] [S05]

| Concern | Go | Rust | Decision for this project |
|---|---|---|---|
| Core work | Straightforward concurrent orchestration, services and command-line tooling | Strong compile-time ownership and control, suitable for system software | The program primarily orchestrates existing virtualization components; choose Go. |
| Local libvirt | Official Go API plus XML model package | Libvirt Rust binding exists | Validate needed APIs with real fixtures; use Go's established API/XML separation. |
| Interface implementation | Cobra and Bubble Tea provide CLI/TUI building blocks | Rust has viable CLI/TUI options; it is not disqualified | Choose one coherent Go stack rather than mixing languages in v1. |
| Safety | Managed memory in ordinary Go; still possible races and unsafe native boundaries | Ownership/type checking helps constrain memory and concurrency mistakes; unsafe/native code still needs review | Operational correctness, permissions, disk transactions and restore tests dominate this product's risk. |
| Portability | OS adapters and native dependencies still necessary | Same architectural requirement | Do not select a language on a false “compile once, hypervisor everywhere” premise. |
| Plugin ABI | Native `plugin` has significant portability/build compatibility restrictions | A native Rust ABI is not the public extension contract either | Language-neutral subprocess protocol, independent of core language. |

**Engineering judgment:** Go is the better default for delivering and maintaining this particular orchestration-heavy suite. This is not a universal performance or development-speed claim. Rust remains a valid language for plugins or a future native adapter where there is a concrete reason.

Use Cobra for commands and Bubble Tea with compatible components for TUI state/rendering. Pin and test the selected major versions; do not mix sample code from incompatible framework releases. [S06] [S07]

## Component boundaries

```text
                 virmill CLI                virmill TUI
                      \                        /
                         local typed client API
                                   |
                 virmilld: unprivileged user coordinator
        domain services | planner | journal | scheduler | event bus
                    /              |               \
           Linux libvirt     bounded host      plugin runner
           backend adapter   helper client     (unprivileged)
                 |                 |                  |
           libvirt API     virmill-host-helper   plugin executable
                 |         root, typed          JSON-RPC/stdin-out
              QEMU/KVM     allowlist only       confined workspace
```

`virmilld` is a private local service, not an externally reachable management server. The main executable may expose `virmill daemon` internally while packages provide the appropriate service entrypoint. Package the small privileged helper separately from the interface.

### Responsibilities

- **Domain:** platform-neutral resource identifiers, desired specifications, observed state, operation plans, capability reasons and errors.
- **Application services:** semantic validation and workflows. This is the sole owner of business rules shared by CLI, TUI and plugins requesting core actions.
- **Planner:** immutable plans containing resolved inputs, before-state fingerprints, risks, required grants, steps and recovery strategies.
- **Coordinator:** durable state machine, scheduling, resource locks, child-process supervision and reconciliation.
- **Backend:** libvirt calls, domain/network/volume translation, live/persistent state differences and backend capability reporting. Do not parse `virsh` tables as an API.
- **Host adapters:** network configuration, firewall integration, service persistence, device enumeration, credentials, viewer launch and platform paths.
- **Privilege helper:** approved host-network changes, narrowly scoped filesystem/state operations, and independent timeout recovery. Never a generic root command runner.
- **Extension host:** manifest validation, process lifecycle, grants, protocol, structured UI contributions and conformance enforcement.

## ADR-002: Backend state remains authoritative

Libvirt owns the actual domain/network/volume definitions and live state. The application database owns jobs, tags, lab/template relationships, user intent, plugin settings and evidence. Observed backend inventory is a cache, not a second authoritative VM registry.

Retain original XML and surgically patch relevant nodes; generic marshal/unmarshal through a known-field struct is insufficient for unknown namespaces or settings. New VM definitions may use structured generation. Changes to adopted VMs require lossless semantic preservation tests.

Use optimistic state checks plus operation locks. Libvirt and the operating system do not provide a global transaction spanning every resource. External tools may race with this application: detect drift, re-read before each critical step, and stop/reconcile instead of claiming universal isolation. Intent persistence does not make unsafe replay safe.

## ADR-003: Durable local service, not UI-owned jobs

Use a systemd user service for the coordinator on certified Linux hosts. A single OS user's terminals share one coordinator and one journal. Authenticate local connections using Unix peer credentials; use a private socket under the user's runtime directory. No default TCP listener.

Closing a terminal or TUI detaches from jobs. Logging out is a separate event: installation offers explicit lingering/service persistence for unattended work. Without it, show that schedules/jobs cannot be guaranteed across logout. At reboot, replay the journal for reconciliation; a conversion may need restarting from a clean stage rather than byte-level resumption. Systemd documents lingering as the mechanism for long-running user services without a logged-in session. [S08]

Support multiple simultaneous clients for one operator. A multi-tenant authorization product is not implied. Other users' libvirt actions are treated as external changes; do not claim all writers share this application's locks.

## ADR-004: Plugins are processes, not linked libraries

Use versioned JSON-RPC 2.0 over supervised stdin/stdout. No `plugin.Open`, shared `.so` ABI, embedded extension interpreter with full host access, or plugin-supplied root scripts. Go's native plugin documentation explicitly identifies platform and build-compatibility restrictions. [S09]

Process separation improves crash containment; it is not by itself a security sandbox. The Linux runner must implement the confinement policy in the security document. A language-neutral protocol lets a future remote provider be written in Go or Rust without rewriting the local core.

Mandatory workflows are built-in modules/adapters delivered with the release. Users do not have to find plugins to unlock backups, labs or snapshots. Third-party plugins reuse stable extension points rather than being privileged alternate implementations of the core.

## ADR-005: Portability means replaceable services, not identical capabilities

Define interfaces for `ComputeProvider`, `NetworkProvider`, `StorageProvider`, `DeviceProvider`, `GuestTransport`, `ServiceManager`, `CredentialStore`, `SandboxRunner` and `ConsoleLauncher`. Common packages must not import Linux syscalls or libvirt XML.

macOS may use a QEMU/HVF adapter or an Apple Virtualization framework adapter; Windows may use QEMU/WHPX or another separately certified provider. These are alternatives to evaluate when porting, not choices already implemented. Apple exposes native virtualization APIs and QEMU documents OS-specific acceleration. [S10] [S11]

An ARM host does not make an x86 guest hardware-accelerated merely because a management application was recompiled. Store guest architecture and virtualization/emulation mode explicitly. Never silently fall back from hardware virtualization to software emulation.

Cross-platform CI should compile and test platform-neutral domain, CLI/protocol and plugin packages with a fake provider. It must not label those builds as a working macOS/Windows VM manager.

## Repository layout to implement

```text
cmd/virmill/                   CLI and TUI entrypoint
cmd/virmilld/                  optional dedicated coordinator entrypoint
cmd/virmill-host-helper/       bounded privileged mechanism
internal/domain/            specifications, observed state, errors, policies
internal/app/               creation, import, VM, network, protection, labs
internal/operations/        planner, executor, locks, journal, recovery
internal/backend/libvirt/   Linux native integration and XML patching
internal/platform/linux/   devices, network, firewall, service, sandbox
internal/transport/local/   private IPC, peer authentication, streaming
internal/ui/cli/             command registry and formatting
internal/ui/tui/             models, views, forms, navigation
internal/plugins/           runner, registry, grants and lifecycle
internal/store/             SQLite migrations and repositories
sdk/go/                     public extension SDK; separate module/version
schemas/                    versioned public declarative formats
packaging/                  RPM, DEB, services, policies, completions
examples/                   tested labs, recipes, plugins
tests/                      contract, integration, security and hardware tests
docs/                       user, developer, plugin, operations, release evidence
```

The exact SQL access helper and minor dependency choices may be selected during implementation. Public contracts and security boundaries are not discretionary dependency choices.
