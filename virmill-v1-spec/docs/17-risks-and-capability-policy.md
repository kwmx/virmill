# 17 — Risk register and capability policy

## Capability is a precise contract

Each operation reports one of `supported`, `supported-with-prerequisites`, `unsupported-on-this-configuration`, or `not-implemented`. The last state blocks release for mandatory v1 workflows. Include a stable reason code, relevant host/guest facts, tested support class and safe alternatives. A disabled button alone is not an explanation.

The complete product is bounded by an explicit support matrix. “Fully finished” means all mandatory workflows are implemented, integrated, documented and accepted on that matrix; it cannot mean every OVA, physical device, guest driver and hypervisor capability works identically. Do not use the matrix to hide unfinished code.

## Material risks and required controls

| Risk | Required control | Release evidence |
|---|---|---|
| Disk loss from overlay/backing mistakes | Immutable graph, dependency checks, staged capture, safe pivot/commit, preserve uncertain files | Snapshot/restore and interrupted graph tests |
| Successful-looking but unusable backup | Complete manifest, independent storage, checksums, isolated restore without original DB | Independent cold/live restore drills |
| TPM/firmware identity mismatch | Preserve auxiliary state; cold capture when safe live capture is not supported | UEFI/TPM guest restore |
| Host network lockout | Existing bridge preference, supported network manager adapters, independent rollback and console plan | Crash/timeout rollback on physical host |
| False isolation | Separate guest/host/egress policy, IPv4/IPv6 tests, dual-homed warnings | Packet-flow tests |
| Untrusted appliance exploits parser/tool | Bounded parsing, confined workers, no external references, no host recipe execution | Malicious fixture tests |
| Third-party plugin privilege escalation | Grants plus enforced OS confinement, signed package binding, bounded helper, fail closed | Sandbox and grant-substitution tests |
| Multiple tools edit one VM | Preserve unknown XML, re-observe, fingerprints and explicit conflicts | Concurrent edit fixtures |
| Job replay repeats destructive effect | Journal before/after, stable identity, per-step reconciliation, uncertain-state handling | Process/power interruption tests |
| Architecture depends on Linux everywhere | Neutral domain/service interfaces and compile tests; isolate native backend | Common-package cross-platform tests |
| Broad first release becomes incomplete | Requirement traceability and one release gate; do not relabel omitted features optional | Full acceptance review |

## Explicit exclusions, not missing v1 work

Real remote infrastructure/provider management, local macOS/Windows VM hosts, remote-client USB redirection, automatic GPU/bootloader/VFIO configuration, live migration/HA clusters, a graphical VM viewer implemented inside the TUI, universal application-consistent live lab snapshots and guaranteed arbitrary-OVA conversion are not v1 promises. The architecture has extension points for relevant future additions.

Host-local USB, complete supported local networking, snapshots/restore, templates/clones, backups, multi-VM labs, automatic supported guest setup and a functioning plugin platform are not exclusions. They are release requirements.

## Defaults requiring no further owner decision

Use the owner-approved product name Virmill and command `virmill`, Go, Linux x86-64, libvirt/QEMU/KVM, local user coordinator, English UI, external graphical viewer, MIT licensing intent and bundled core backup integration. Freeze exact dependency/distro/guest versions through verified implementation tests, not guesses in this design document. Any future change to these decisions gets an ADR and owner-visible scope impact.

No further product questionnaire is required before implementation. Hardware/distro evidence collection and API compatibility checks are engineering tasks, not reasons to restart the requirements discussion.
