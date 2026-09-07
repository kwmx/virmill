# Virmill

Virmill is a local Linux CLI/TUI virtualization suite under implementation in Go,
using QEMU/KVM through libvirt. The complete 1.0 contract is in
[virmill-v1-spec](virmill-v1-spec/README.md).

**Development source; 1.0 is not release-qualified.** All mandatory workflows
remain required. See [implementation status](docs/implementation-status.md),
[release evidence](docs/evidence/README.md) and [support matrix](docs/support-matrix.md).
An executable build or simulated test does not establish hardware support.

Build and run the current development tools using [getting started](docs/getting-started.md).
The [OVA preparation workflow](docs/import-preparation.md) converts complete disk
sets into verified independent artifacts through the CLI or TUI. The
[VM creation adapter](docs/vm-creation.md) adds explicit hardware mappings,
define-last storage phases and reviewed definition recovery. Native storage,
guest qualification and other required creation sources remain in progress.

The public executable is `virmill`, the user coordinator is `virmilld`, and bounded
privileged work belongs to `virmill-host-helper`. No publication destination has
been selected. Local module names make no repository or domain ownership claim.
