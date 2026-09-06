# Running the development source

The complete 1.0 suite has not been delivered or certified. Use the
[requirements matrix](implementation-status.md) to distinguish implemented code,
executed tests and outstanding release requirements.

Build with the pinned Go archive in `contracts/dependencies.lock.json`. If it is
not already present, explicitly run `python3 scripts/bootstrap.py` to download and
verify it into `.tools/`. No global toolchain installation occurs. Dependencies are
vendored; `make build` is offline. This is a CGo application with native libvirt
runtime dependencies, not a standalone hypervisor.

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

VM mutation commands produce a preview. `plan apply` requires its full digest,
an idempotency key and all acknowledgements, such as `--ack host-mutation`.
Hard power-off also requires `data-loss-hard-stop`. Apply rechecks state and intent.
Do not apply these development adapters to production VMs. No disposable VM host
has been authorized in this implementation session.

Closing the CLI or TUI does not terminate daemon jobs. `operation show`, `watch`
and `reconcile` inspect durable state. Stopping the daemon marks unfinished work
uncertain at restart. No systemd persistence or lingering is enabled automatically.
