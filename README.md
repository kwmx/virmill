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
[existing-disk preparation workflow](docs/existing-disk-preparation.md) copies
explicitly selected files with guarded backing/extent access. The
[ISO preparation workflow](docs/installation-preparation.md) copies media and
creates verified empty disks. The [VM creation adapter](docs/vm-creation.md) adds explicit hardware mappings,
define-last storage phases and reviewed definition recovery.
[Declared cloud-image provisioning](docs/cloud-provisioning.md) adds identity-bound
NoCloud seeds with explicit guest account and network intent.
[Failed-creation cleanup](docs/creation-cleanup.md) adds explicit retention or
guarded deletion with durable recovery. Native storage,
guest qualification and other required creation sources remain in progress.

The public executable is `virmill`, the user coordinator is `virmilld`, and bounded
privileged work belongs to `virmill-host-helper`. No publication destination has
been selected. Local module names make no repository or domain ownership claim.

CPU/RAM, boot-order and retained-media editing: [configuration guide](docs/vm-configuration.md).
Graceful reboot with durable event verification: [reboot guide](docs/vm-reboot.md).
Backup declarations and timezone-aware occurrence previews:
[backup policy guide](docs/backup-policy-validation.md).
Keyboard action search and compact navigation: [TUI guide](docs/tui-navigation.md).

Managed-volume read access: [reviewed helper workflow](docs/managed-volume-access.md)
and [native evidence](docs/evidence/managed-access-run.md).
Read-only device and subnet facts: [PCI and CIDR discovery](docs/host-network-discovery.md).
Managed NAT/lab creation and reviewed activation recovery:
[network guide](docs/network-creation.md), [helper setup](docs/network-helper.md),
and [recorded qualification results](docs/evidence/owned-network-run.md).

Recovery prerequisites: [native firmware/TPM inspection](docs/cold-state-layout.md)
and [declared manifest verification](docs/recovery-manifest-verification.md).
New UEFI creation also records a [durable NVRAM declaration binding](docs/creation-nvram-binding.md).
An explicit administrator policy enables [auxiliary member metadata inspection](docs/auxiliary-state-inspection.md).
These observations do not establish complete capture or independent recovery.

Reference provider development: [fixture guide](tests/fixtures/plugins/provider/README.md)
and [accepted simulated-contract evidence](docs/evidence/provider-conformance-run.md).
