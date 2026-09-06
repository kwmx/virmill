# 00 — Product contract and v1 scope

## Objective

Make everyday local VM work reliable and accessible from a terminal: import an appliance, adjust its virtual hardware, give it appropriate connectivity, attach a USB device, build a repeatable lab, and recover it after a bad change. A knowledgeable operator should be able to inspect exactly what happened; a less experienced user should not need to hand-edit XML for normal tasks.

The product is a standalone application, not a larger shell profile, a new hypervisor, a cloud control plane, or a replacement for all host administration tools.

## Locked owner requirements

| Decision | Required interpretation |
|---|---|
| Standalone identity | Product: **Virmill**. Public executable: **`virmill`**. Product UI, documentation, packages and namespaces use this identity, without personal/studio co-branding. |
| Full suite in v1 | Creation/import, VM configuration, local devices, networking, snapshots/restore, templates/cloning, backups, multi-VM labs, and automatic guest setup are mandatory. |
| Local first | Managing VMs on the current Linux host must not depend on accounts, a cloud service, external controllers, or internet access after dependencies/images are available. |
| Extensible | A real, documented, tested plugin mechanism ships in v1; remote providers can be added through it later. |
| Linux primarily | Design for later macOS/Windows adapters; do not make current Linux capability depend on those ports. |
| Flexible networking | A VM may have multiple NICs on multiple networks, including an internet-facing connection plus several lab networks. NAT and LAN bridging are supported. |
| Agent implementation | Contracts, acceptance tests, failure semantics, packaging, and plugin-development documentation must be explicit. |

## Mandatory feature families

| ID | Family | v1 completion standard |
|---|---|---|
| F01 | Host onboarding | Diagnose virtualization, services, permissions, firmware, storage, networking, converter and viewer dependencies; offer approved setup and recovery. |
| F02 | Inventory | Discover local system/session connections explicitly; list, inspect, tag, search, adopt, and reconcile existing VMs without taking destructive ownership of their disks. |
| F03 | Lifecycle | Start, graceful stop, reboot, suspend/resume, managed save where supported, autostart, explicit hard power-off, and safe removal. |
| F04 | Creation/import | ISO, supported cloud images, existing disks, OVF/OVA, and native exports; multiple disks/NICs; compatibility report; staged conversion and boot diagnostics. |
| F05 | Configuration | CPU/RAM, disks/controllers, NICs, boot, BIOS/UEFI, supported Secure Boot/TPM, display/audio, guest channels, and advanced XML with safeguards. |
| F06 | Networking | NAT, existing LAN bridges, guided bridge creation on supported host stacks, host-accessible lab networks, guest-only lab segments, multiple NICs, DHCP/DNS/reservations, route intent, port forwards, policy and diagnostics. |
| F07 | USB/sharing | Host-local USB inventory, exclusive attachment, persistent selectors, temporary attachment, detach/reconnect handling, safe mounted-storage checks, and supported folder/clipboard integration. |
| F08 | Storage | Directory pools, qcow2/raw managed disks, volume import, capacity/allocated-size reporting, expand, relocate, flatten, exports, backing-chain protection and dependency views. |
| F09 | Snapshots/restore | Safe disk restore points, metadata/state association, snapshot browsing, revert, branch-aware deletion/merge, and memory capture where verified capabilities allow. |
| F10 | Templates/clones | Versioned immutable templates, full and linked clones, identity regeneration, cloud-init regeneration, and reference-aware retention. |
| F11 | Backups | Complete cold captures, supported live disk captures, encrypted local repositories, policies/schedules/retention, verification, isolated restore tests and recovery without the original application database. |
| F12 | Labs | Validated declarative multi-VM definitions, dependency-aware plan/apply/start/stop, network memberships, health gates, lab restore points and ownership-safe teardown. |
| F13 | Guest setup | Cloud-init for compatible guests; explicit guest-agent/SSH setup workflows; approved recipes; setup status and secret hygiene. |
| F14 | CLI/TUI | Full normal-workflow parity, noninteractive CLI, JSON/NDJSON contracts, completions, keyboard-driven TUI, change previews and error recovery. |
| F15 | Jobs/recovery | Persistent jobs, event history, cancellation boundaries, safe retries, schedule handling and reconciliation after UI/daemon/host interruption. |
| F16 | Plugins | Installation, permissions, enable/disable/version rollback, process isolation, protocol negotiation, SDK/scaffolder, example plugin and conformance tests. |
| F17 | Release quality | Packages, safe installer/uninstaller, diagnostics, support matrix, migration/upgrade tests, documented limitations and evidence-backed acceptance. |

All are required release families. A feature may have a capability-limited implementation, such as a memory snapshot unavailable for a particular VM, but the family itself cannot be deferred.

## Certified scope and honest limits

The host architecture baseline is x86-64 with working hardware virtualization. Guest profiles cover contemporary Linux and Windows plus conservative appliance/legacy profiles. “Supported” means a published profile/fixture and tested workflow, not every historical OS or every vendor's appliance. Imported unknown guests get a generic profile, compatibility warnings and a console; they are never falsely marked as verified.

The Linux release matrix targets Fedora and Ubuntu/Debian configurations described in the packaging document. Separate support dimensions are host distribution, network manager, backend versions, guest profile, storage layout and physical hardware. Presence of a backend feature flag is necessary, not proof the whole workflow is tested.

A standard terminal cannot serve as a full graphical VM desktop. Launch an external viewer securely. Clipboard, dynamic resizing, audio and shared-folder behavior depend on the guest and viewer; capability and setup status must be visible. [S03]

### Explicitly outside the mandatory v1 baseline

Remote Proxmox/libvirt/cloud providers; macOS/Windows local execution; cross-architecture acceleration; clustering/HA/live migration orchestration; remote USB-over-IP; unattended GPU/IOMMU/bootloader reconfiguration; SR-IOV/vGPU orchestration; arbitrary enterprise storage provisioning; a web dashboard; a public plugin marketplace; mandatory telemetry; and a built-in AI assistant.

PCI/IOMMU inventory and useful eligibility explanations are included. Advanced backend settings may be exposed safely, but do not advertise a certified automatic GPU-passthrough product. This adopts the earlier recommendation to make ordinary USB the initial device workflow.

## Default experience

First launch performs read-only diagnosis, asks before installing dependencies or changing permissions, offers a managed storage location, and previews a NAT network rather than silently modifying the host. Existing VMs appear as externally managed until adopted. Newly created untrusted appliances use a safe network selection prompt, not automatic LAN bridging.

A routine operation has a clear end condition. An import can be `defined`, `started`, `guest-ready`, or `readiness-unverified`; a backup can be `captured`, `repository-verified`, or `restore-tested`. These are different labels, not interchangeable success claims.

## Release definition

Version 1.0 is a feature-complete supported local product, including documentation and recoverability. It does not imply universal hardware compatibility, zero bugs, or autonomous proof of correctness. The agent must supply actual evidence and publish limitations. No numerical delivery estimate or scope reduction is implied by this specification.
