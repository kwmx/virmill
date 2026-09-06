# ADR 0002 — Build inputs and native boundary

Status: accepted engineering decision; support certification remains blocked.

Core module `virmill.local/core` and SDK module `virmill.local/sdk` are local build
identifiers only. They are not publication coordinates or download destinations.
Use a repository-local checksum-verified Go toolchain. Pin module versions and
checksums after successful resolution. Vendor dependencies for offline builds.

The official `libvirt.org/go/libvirt` adapter uses its documented `libvirt_dlopen`
build tag. This retains CGo and the native runtime dependency, while allowing
compilation without distro development headers. It is not a static hypervisor.
Runtime capability probes remain necessary. No virsh table parsing is an API.

Keep domain, protocol and planner types platform-neutral. Native host adapters
live behind OS build constraints. Tests use isolated fixtures; no runtime fake
backend is selectable in a release binary.

References consulted: https://libvirt.org/golang.html and the official binding
repository README. Exact native packages are recorded in the environment evidence.

