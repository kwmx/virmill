# 13 — Testing, acceptance and evidence

## Evidence classes

Every test records the commit, specification revision, dependency versions, host/kernel/CPU, connection URI class, guest/source digest, commands, observed results and retained diagnostic artifacts. Never include credentials. Distinguish **unit**, **simulated contract**, **real backend**, **physical hardware**, and **manual accessibility** evidence. Passing a fake provider test does not certify libvirt, a guest boot, physical USB or host networking.

Use disposable dedicated hosts for destructive, power-loss and host-network tests. Ordinary CI must never discover a developer's production VMs and mutate them. Test fixtures carry an ownership marker and cleanup fails closed if that marker is absent. Network rollback tests require console recovery access.

## Required acceptance scenarios

The IDs below form the release traceability registry. Each scenario must have an automated test where feasible and recorded real-system evidence where specified. A row containing several conditions passes only when every condition passes. `contracts/requirements.json` mirrors this registry.

| ID | Scenario and required outcome | Minimum evidence |
|---|---|---|
| CORE-01 | Discover system/session inventories separately; no duplicate identity or silent adoption. | Real backend |
| CORE-02 | Start, graceful stop, force stop, reboot, pause/resume, autostart and delete preserve the promised disks and state. | Real backend |
| CORE-03 | CPU/RAM/boot edits correctly distinguish live and persistent state and required shutdown. | Real backend |
| CORE-04 | An adopted VM with unknown XML namespaces/device settings retains them after an unrelated edit. | Unit + real backend |
| CORE-05 | An external concurrent edit invalidates the stale plan; no overwrite of newer unrelated settings. | Real backend |
| CORE-06 | Restricted session/backend capability is explained before mutation, not mislabeled as success. | Real backend |
| IMP-01 | ISO install, cloud image and existing disk creation each reach their declared verification level. | Real backend |
| IMP-02 | VMware OVA with supported Linux guest, multiple disks and NICs imports with correct boot/disk order. | Real backend |
| IMP-03 | Generic OVF/OVA fixture imports through the generic path, without pretending all appliances are supported. | Real backend |
| IMP-04 | Unsupported source/hardware, encrypted source and missing driver produce actionable preflight results. | Unit + fixtures |
| IMP-05 | Traversal, symlink, special file, XML entity, archive bomb and external backing references are rejected. | Automated security |
| IMP-06 | Conversion failure, low space and canceled job preserve original source and safely reconcile staging. | Real backend |
| IMP-07 | Registration/start/guest boot/guest reachability are reported as distinct success stages. | Real backend |
| IMP-08 | Multi-system OVF selection, missing source digest and source mutation between plan/apply are handled explicitly. | Fixtures + real backend |
| NET-01 | NAT guest has configured DNS/internet; a static target remains on its designated lab network. | Real packet/guest tests |
| NET-02 | A guest with internet plus two lab NICs uses only the intended normal default route. | Real guest tests |
| NET-03 | Services-only lab permits its declared DHCP/DNS but rejects other host services and external forwarding. | Real packet tests |
| NET-04 | Guest-only segment has no host L3/DHCP; approved guest static addressing works between guests. | Real packet tests |
| NET-05 | IPv6/link-local/RA paths match policy; no false isolation claim from IPv4-only tests. | Real packet tests |
| NET-06 | CIDR selection detects LAN/VPN/planned overlaps without treating the default route as universal overlap. | Unit + real backend |
| NET-07 | Existing LAN bridge works; DHCP remains externally owned unless explicitly configured otherwise. | Real Ethernet host |
| NET-08 | NetworkManager bridge creation commits on confirmation and restores connectivity on timeout/crash. | Physical/disposable host |
| NET-09 | Certified Netplan/networkd bridge creation and independent rollback work, including process interruption. | Physical/disposable host |
| NET-10 | Ordinary Wi-Fi bridging is declined with NAT alternative; Wi-Fi NAT succeeds. | Physical Wi-Fi host |
| NET-11 | Port forwards obey bind/exposure policy, maintain stable targets and survive firewall reload/reboot. | Real packet tests |
| NET-12 | Dual-homed paths are flagged; lab deletion blocks when external guests still reference its network. | Real backend |
| STO-01 | Pool creation, disk allocation/growth/move and space accounting preserve permissions/labels and guest data. | Real backend |
| STO-02 | Full clone is independent; linked clone retains declared immutable base dependencies. | Real backend |
| STO-03 | Base/template deletion and garbage collection refuse any referenced or uncertain object. | Unit + real backend |
| SNAP-01 | Cold snapshot/revert preserves disk, configuration and required firmware/TPM identity; source branch remains safe. | Real backend + guest |
| SNAP-02 | Supported live disk snapshot/revert uses safe graph transitions, including injected partial failure. | Real backend |
| SNAP-03 | Unsupported memory/TPM/live combinations fail before mutation or use explicitly approved cold capture. | Real backend |
| BAK-01 | Encrypted backup includes all required artifacts; restore succeeds with original VM files and app database unavailable. | Independent real restore |
| BAK-02 | Supported live backup respects consistency; shutdown/interruption produces partial status, never a valid incomplete backup. | Real backend |
| BAK-03 | Failed quiesce cannot leave the guest frozen; independent guardian meets the documented bound under daemon crash. | Real guest fault injection |
| BAK-04 | Retention/pruning honors pins, repository locks and active jobs; verification detects corrupted/missing data. | Real repository |
| BAK-05 | Scheduled capture obeys timezone, missed-run and credential-unavailable policy; no silent consistency downgrade. | Service/reboot tests |
| BAK-06 | Isolated test restore catches identity/address conflicts and reports the exact readiness verified. | Independent real restore |
| DEV-01 | USB attach/detach works live and persistently where supported, with serial and port identity behavior verified. | Physical USB host |
| DEV-02 | Identical devices, missing device, disconnect/reconnect and host-use conflict never attach the wrong device silently. | Physical USB host |
| DEV-03 | PCI/IOMMU discovery is read-only; unsupported passthrough never changes bootloader or host drivers. | Physical host |
| DEV-04 | Serial/SSH/viewer access and supported shares/clipboard honor opt-in and endpoint exposure rules. | Real guests |
| GUEST-01 | Cloud-init and supported guest transports establish a non-root user/keys and report readiness truthfully. | Real guest |
| GUEST-02 | Guest recipe runs only in approved guest context; input hash, privilege, retries and completion evidence are recorded. | Real guest |
| GUEST-03 | Missing guest agent/network/SSH credentials produce guidance, not guessed IPs or insecure host-key bypass. | Real guest |
| LAB-01 | Multi-VM DAG applies successfully; second unchanged apply is a no-op with stable identities. | Real backend |
| LAB-02 | Cycle, duplicate identity, unknown template and address conflict fail during validation/preflight. | Unit + real backend |
| LAB-03 | Mid-DAG failure can resume safely; destruction preserves external networks, retained disks and backups. | Real backend |
| LAB-04 | Template capture generalizes only a copy; clones receive intended identity and remain bound to immutable version. | Real guest |
| LAB-05 | Coordinated cold capture restores into a new namespace with correct dependencies and disclosed consistency window. | Real backend |
| JOB-01 | Closing either client leaves the job running; reconnect shows coherent status and ordered events. | Service integration |
| JOB-02 | Crash/reboot reconciles uncertain effects rather than blindly rerunning destructive steps. | Fault injection |
| JOB-03 | Idempotency key retries yield the same operation; changed input with reused key is rejected. | Unit + integration |
| JOB-04 | Multiple clients and external changes are serialized/rechecked correctly, including resource busy conditions. | Integration |
| SEC-01 | Wrong UID/socket clients and unauthorized helper actions are rejected; no generic root execution primitive exists. | Security integration |
| SEC-02 | Secrets and guest sensitive artifacts remain absent from default logs, telemetry and diagnostics. | Automated security |
| SEC-03 | Malicious paths, filenames, terminal control sequences and subprocess inputs cannot escape or inject commands. | Fuzz + security tests |
| SEC-04 | Plugin confinement blocks undeclared files/sockets/network; sandbox unavailable fails closed. | Linux sandbox tests |
| SEC-05 | Plan/hash/expiry/package substitution invalidates grants; a plugin cannot turn read permission into mutation. | Security integration |
| EXT-01 | Reference action and a second-language fixture pass handshake/schema/plan/execute/error contracts. | Simulated contract |
| EXT-02 | Simulated provider supports lifecycle/reconcile and reports unsupported capabilities without modifying core. | Simulated contract |
| EXT-03 | Malformed/duplicate-key/oversized frames, timeouts, crash and cancellation are handled safely. | Fuzz + contract |
| EXT-04 | Package signature/digest/path checks, upgrade/rollback and grant revocation behave as documented. | Security integration |
| EXT-05 | Plugin-added structured action is reachable from both CLI and TUI with the same approved operation. | Interface integration |
| UX-01 | Every mandatory operation has equivalent CLI/TUI behavior, validation, permission and recovery paths. | Registry + interaction tests |
| UX-02 | Resize, navigation, focus, cancellation and errors work in supported terminals without color-only meaning. | Automated + manual |
| UX-03 | JSON/NDJSON are stable and clean; noninteractive commands never hang for hidden prompts. | Golden/contract tests |
| REL-01 | RPM/DEB fresh install, upgrade, migrations and uninstall preserve user VMs/data and follow approved host setup. | Clean host matrix |
| REL-02 | Dependency-free core/protocol packages and sample plugin build on declared OS/architecture test targets. | Cross-build CI |
| REL-03 | User/admin/plugin docs and examples pass validation and correspond to actual command help. | Documentation CI |
| REL-04 | Entire mandatory suite passes on the frozen support matrix; remaining issues and exclusions are explicit. | Release evidence review |

## Guest/source matrix

Freeze at implementation gate 1: at least one supported Fedora-family Linux guest, Ubuntu/Debian-family cloud guest, a generic Linux ISO, a supported VMware-origin Linux OVA, a generic non-VMware OVF/OVA fixture and a Windows UEFI/TPM guest built from legitimately obtained installation media. Add a BSD/network-appliance boot fixture for the generic import limitations matrix. Use compatible versions verified at the time of testing. Do not distribute proprietary images, licenses, credentials or vulnerable appliance images in the repository.

Document exact tested CPU architecture, firmware, disk/controller model, guest driver readiness and provisioning channel. Guest classes are targets for creation and capability reporting, not promises that any disk bearing that OS name can be converted.

## Fault injection priorities

Terminate the client, coordinator, helper, conversion worker and plugin independently at pre-effect/post-effect/pre-journal boundaries. Simulate full disk, read-only repository, lost libvirt connection, guest agent timeout, failed thaw, hot-unplug, corrupt archive and interrupted database migration. Recovery must favor preservation over cleanup when ownership or effect completion is uncertain.

Performance measurements cover inventories of 10/100/500 simulated VMs, real multi-job conversion/backup contention, event backpressure and TUI input latency under active jobs. Record measured budgets before release; do not fabricate VM-per-host capacity promises from simulation.

## Release decision

A green unit test suite is necessary but insufficient. All mandatory rows require evidence at their stated level. An actual hardware limitation may qualify a configuration, not excuse an unimplemented mandatory flow. A failing safety invariant blocks release. An owner-approved scope change must update this document, the product scope and the release checklist explicitly.
