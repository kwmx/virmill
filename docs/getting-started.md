# Running the development source

The complete 1.0 suite has not been delivered or certified. Use the
[requirements matrix](implementation-status.md) to distinguish implemented code,
executed tests and outstanding release requirements.

Build with the pinned Go archive in `contracts/dependencies.lock.json`. If it is
not already present, explicitly run `python3 scripts/bootstrap.py` to download and
verify it into `.tools/`. No global toolchain installation occurs. Dependencies are
vendored; `make build` is offline. This is a CGo application with native libvirt
runtime dependencies, not a standalone hypervisor.

The current build and package scripts require a Git checkout at the project root;
linked worktrees are supported. Package sources must be tracked regular files.
The native package scripts require Linux and Python 3.11 or newer.
Untracked documentation, local media and stale package outputs are excluded;
selected symbolic links are refused. Use a clean, reviewed revision for a
reproducibility run: tracked working-tree edits are still development inputs, and
an embedded Git revision alone does not attest their contents. RPM/DEB checksums
cover exactly the two core/helper package pairs produced by the current builder.

```sh
make build
make verify
build/bin/virmill version --output json
build/bin/virmill --help
```

To inspect the application using private disposable application state:

```sh
sandbox=$(mktemp -d /tmp/virmill-dev.XXXXXX)
chmod 700 "$sandbox"
export XDG_RUNTIME_DIR="$sandbox/runtime"
export XDG_STATE_HOME="$sandbox/state"
export XDG_CACHE_HOME="$sandbox/cache"
mkdir -m 700 "$XDG_RUNTIME_DIR" "$XDG_STATE_HOME" "$XDG_CACHE_HOME"
build/bin/virmilld
```

Run another terminal with the same variables and `build/bin/virmill tui`,
`build/bin/virmill doctor --output json`, or
`build/bin/virmill lab validate examples/labs/multi-network-lab.yaml`.
The example templates are unresolved references, not downloadable guest images.
Validation does not create networks or guests.

`vm list --connection qemu:///system` uses the native read-only libvirt API.
`qemu:///session` is selected explicitly. Remote URIs and `test:///default` are
rejected by the application. The native test-driver fixture exists only in tests.
Existing inventory remains externally managed; inventory discovery never adopts it.
A newly created domain is marked managed only after its durable catalog record and
native identity match. See [VM creation](vm-creation.md) for the prepared-appliance
adapter, explicit mappings, staged result and definition-only recovery.

VM mutation commands produce a preview. `plan apply` requires its full digest,
an idempotency key and all acknowledgements, such as `--ack host-mutation`.
Hard power-off also requires `data-loss-hard-stop`. Apply rechecks state and intent.
Do not apply these development adapters to production VMs. The owner has now
authorized one remote disposable VM for installation and guest tests. Its SSH
destination and setup inventory are local-only; see `.virmill-local/test-host.json`
in the active development workspace. This does not authorize the development host
or any other discovered target, or establish guest/hardware support.

Closing the CLI or TUI does not terminate daemon jobs. `operation show`, `watch`
and `reconcile` inspect durable state. Stopping the daemon marks unfinished work
uncertain at restart. No systemd persistence or lingering is enabled automatically.
