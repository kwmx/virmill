# Virmill

Virmill is a local Linux CLI/TUI virtualization suite under implementation in Go,
using QEMU/KVM through libvirt. The complete 1.0 contract is in
[virmill-v1-spec](virmill-v1-spec/README.md).

**Development source; 1.0 is not release-qualified.** All mandatory workflows
remain required. See [implementation status](docs/implementation-status.md),
[release evidence](docs/evidence/README.md) and [support matrix](docs/support-matrix.md).
An executable build or simulated test does not establish hardware support.

The public executable is `virmill`, the user coordinator is `virmilld`, and bounded
privileged work belongs to `virmill-host-helper`. No publication destination has
been selected. Local module names make no repository or domain ownership claim.

