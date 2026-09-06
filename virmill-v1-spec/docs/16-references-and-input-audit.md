# 16 — References and input audit

## How to read the references

External documentation was consulted on **2026-09-06**. Source labels support technical background, not claims that this application exists or that its proposed workflows have passed tests. Exact dependency versions and the tested support matrix must be frozen during implementation. Upstream documentation may describe features newer than an installed distribution’s packages.

The architecture, defaults, limits, release gates and extension protocol are proposed Virmill design decisions. They are not requirements quoted from upstream vendors. No source license grants redistribution rights to guest operating-system images.

### [S01] Libvirt Go language bindings

https://libvirt.org/golang.html

Official API and XML Go modules.

### [S02] Libvirt Go module source/documentation

https://github.com/libvirt/libvirt-go-module

Native binding and build/runtime integration.

### [S03] virt-manager / virt-viewer

https://virt-manager.org/

External graphical guest viewer.

### [S04] Libvirt Rust bindings

https://github.com/libvirt/libvirt-rust

Rust is a viable binding alternative, not absent from the ecosystem.

### [S05] Go and Rust platform documentation

https://go.dev/doc/install/source#environment

Language/OS target information; also https://doc.rust-lang.org/rustc/platform-support.html . A target is not a virtualization feature guarantee.

### [S06] Cobra package documentation

https://pkg.go.dev/github.com/spf13/cobra

CLI framework; pin a tested version.

### [S07] Bubble Tea upstream

https://github.com/charmbracelet/bubbletea

TUI framework; pin mutually compatible components.

### [S08] systemd loginctl manual

https://www.freedesktop.org/software/systemd/man/loginctl.html

User lingering and service persistence.

### [S09] Go native plugin documentation

https://pkg.go.dev/plugin

OS and build compatibility restrictions; IPC alternatives.

### [S10] Apple Virtualization framework

https://developer.apple.com/documentation/virtualization

A possible future native macOS backend, not implemented v1 support.

### [S11] QEMU glossary / accelerators

https://www.qemu.org/docs/master/glossary.html

Host-dependent acceleration including KVM/HVF/WHPX; also https://www.qemu.org/docs/master/system/whpx.html .

### [S12] qemu-img manual

https://qemu-project.gitlab.io/qemu/tools/qemu-img.html

Image operations, formats, checks and safety constraints.

### [S13] virt-v2v VMware input documentation

https://libguestfs.org/virt-v2v-input-vmware.1.html

VMware OVA support boundaries; conversion overview https://libguestfs.org/virt-v2v.1.html and guest support https://libguestfs.org/virt-v2v-support.1.html .

### [S14] cloud-init NoCloud datasource

https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html

Guest initialization input mechanism.

### [S15] Libvirt network XML format

https://libvirt.org/formatnetwork.html

Network types, forwarding, host/firewalld access and isolation distinctions.

### [S16] cloud-init network configuration version 2

https://docs.cloud-init.io/en/latest/reference/network-config-format-v2.html

Supported configuration subset and renderer considerations.

### [S17] Libvirt networking guide

https://wiki.libvirt.org/Networking.html

Bridge setup and ordinary wireless bridge limitations.

### [S18] NetworkManager D-Bus API

https://networkmanager.dev/docs/api/latest/gdbus-org.freedesktop.NetworkManager.html

Checkpoint and rollback APIs.

### [S19] Netplan try reference

https://netplan.readthedocs.io/en/stable/netplan-try/

Timed confirmation and documented rollback caveats.

### [S20] qemu-img safety notes

https://qemu-project.gitlab.io/qemu/tools/qemu-img.html

Same upstream source as S12; live image mutation restrictions.

### [S21] Libvirt snapshot XML

https://libvirt.org/formatsnapshot.html

Snapshot representation; related https://libvirt.org/kbase/snapshots.html .

### [S22] restic project and documentation

https://restic.net/

Encrypted/deduplicated backup engine; operational reference https://restic.readthedocs.io/en/stable/100_references.html .

### [S23] Libvirt efficient live full disk backup

https://libvirt.org/kbase/live_full_disk_backup.html

Backup API workflow and interruption considerations.

### [S24] Libvirt backup XML

https://libvirt.org/formatbackup.html

Backup job configuration and push/pull mechanisms.

### [S25] Libvirt domain XML

https://libvirt.org/formatdomain.html

Virtual hardware, host devices, USB redirection and firmware/TPM configuration.

### [S26] Linux VFIO documentation

https://www.kernel.org/doc/html/next/driver-api/vfio.html

Device assignment and IOMMU grouping constraints.

### [S27] RFC 8785: JSON Canonicalization Scheme

https://www.rfc-editor.org/rfc/rfc8785

Canonical bytes for approved plans and signed package inventories.

### [S28] Bubblewrap upstream

https://github.com/containers/bubblewrap

Low-level Linux confinement tool; security depends on the applied policy.

### [S29] JSON-RPC 2.0 specification

https://www.jsonrpc.org/specification

Request/response/notification semantics; NDJSON framing is this project’s convention.

### [S30] polkit manual

https://www.freedesktop.org/software/polkit/docs/latest/polkit.8.html

Privileged operation authorization.

### [S31] SQLite write-ahead logging documentation

https://www.sqlite.org/wal.html

Concurrency and persistence characteristics of the proposed journal store.

## Uploaded bundle audit

The conversation attachment `shell-profile-for-a-linux-vm.zip` was inspected as an archive in the working environment; its scripts were **not executed**. SHA-256:

```text
ca0634a1aea5aecba0e561547fe84a55ff5cf5a552f38dc387dd8744ad69f1b5
```

The archive includes `lib/common.sh`, `install.sh`, `uninstall.sh`, a script-catalog YAML manifest and the `lib/` directory. Its manifest identifies the `linux-vm-shell-profile` entry, version `1.0.0`, and a non-root shell-profile installation. The installer manages Bash/Zsh configuration in the selected user’s home/ZDOTDIR, with preservation/backup behavior and uninstall support. It is not a host VM-management engine.

The proposed reuse is an **optional, pinned guest-setup recipe**, run as the intended guest user with a reviewed plan. The current source archive is not bundled into this specification: reference it by verified provenance/digest when implementing the integration. No user repository was contacted or modified, and no host or VM was configured while preparing the specification.

## Specification-aid validation versus application validation

The accompanying QA report records checks actually performed on this document package: local links, reference-label resolution, schema/example consistency, negative fixtures, and build/protocol smoke tests of a tiny synthetic read-only plugin. These checks do **not** validate libvirt, real guests, physical USB, network rollback, production confinement, independent VM restore or a complete SDK. Those remain acceptance requirements for the future implementation.
