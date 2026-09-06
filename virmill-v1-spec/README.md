# Virmill — complete v1 implementation specification

**Product name:** Virmill · **Command:** `virmill` · **Specification revision:** 1.0-draft.2 · **Date:** 2026-09-06

**Status:** a design and implementation contract, not an implemented VM manager. Included schemas and the small reference plugin are specification aids. Product commands throughout this package describe the application the agent must build; they are not commands available to install today.

## Product decision

Build a complete, local-first Linux virtualization suite with an equally capable CLI and TUI. Use **Go**, **QEMU/KVM through libvirt**, a durable local job coordinator, and versioned out-of-process plugins. All requested local workflows ship in **1.0**: VM creation/import and configuration, versatile multi-NIC networking, USB attachment, snapshots/restore, templates/cloning, backups, multi-VM labs, and automatic guest setup. Remote infrastructure management is an optional future plugin, not a core product dependency. Prepare the architecture for macOS and Windows without claiming those host platforms are supported in v1.

The development gates in this package are implementation checkpoints within one release. They are **not an MVP roadmap** and do not authorize dropping mandatory features.

## Product identity

**Virmill** is the approved standalone product name, and **`virmill`** is the public executable. Use `virmilld` for the private local coordinator, `virmill-host-helper` for the bounded privileged helper, `VIRMILL_` for application-specific environment-variable names, and `virmill/v1` for declarative API versions. The complete naming and path contract is in [packaging and operations](docs/15-packaging-and-operations.md#product-identity-and-namespaces).

Revision `1.0-draft.2` updates product identity, identifiers, paths and examples without reducing the agreed v1 features. The specification does not assign an actual domain, repository owner or package-registry account. Example namespaces are documentation identifiers, not published services.

## Reading order

| Reader/task | Start here |
|---|---|
| Product owner | [Product and scope](docs/00-product-and-scope.md), [risks and capability policy](docs/17-risks-and-capability-policy.md) |
| Implementation agent | [AGENTS.md](AGENTS.md), [agent kickoff prompt](START-HERE-AGENT-PROMPT.md), [implementation plan](docs/14-agent-implementation-plan.md) |
| Core developer | [Architecture and language decision](docs/01-architecture-and-go-decision.md), [domain contracts](docs/02-domain-model-and-contracts.md) |
| Interface developer | [CLI](docs/03-cli-specification.md), [TUI](docs/04-tui-specification.md) |
| VM workflows | [Import](docs/05-import-and-creation.md), [networking](docs/06-networking.md), [storage and protection](docs/07-storage-snapshots-backups.md) |
| Guest/lab workflows | [Devices and provisioning](docs/08-devices-and-guest-setup.md), [labs and templates](docs/09-labs-and-templates.md) |
| Security/reliability | [Jobs, privileges, recovery](docs/10-jobs-security-and-recovery.md) |
| Plugin developer | [Plugin development guide](docs/11-plugin-development.md), [wire protocol](docs/12-plugin-protocol.md), [reference plugin](examples/plugins/vm-summary/README.md) |
| Release engineer | [Acceptance tests](docs/13-testing-and-acceptance.md), [packaging and operations](docs/15-packaging-and-operations.md), [release checklist](SHIP-CHECKLIST.md) |
| Evidence | [References and attachment audit](docs/16-references-and-input-audit.md) |

## Decisions adopted where the owner did not specify otherwise

- Standalone, independently branded repository/application, distributed through native packages and optional script-catalog installers. The original shell-profile bundle is an opt-in guest recipe, not the virtualization engine or a runtime dependency.
- Linux x86-64 is the v1 host target. Fedora and Ubuntu/Debian receive packaged, tested configurations. Other Linux distributions are not automatically certified.
- English interface and documentation initially; Unicode display names and translation-ready strings. Virmill is the owner-approved product name; MIT remains the licensing intent. Name selection does not establish domain/package availability or trademark clearance.
- `qemu:///system` is the primary backend connection. Explicit discovery of a user's `qemu:///session` inventory is supported, with its actual restrictions shown and no silent movement of domains between connections.
- The GUI guest console opens in an external viewer; the TUI manages it and provides terminal/SSH access. The product is not a graphical desktop viewer embedded in a terminal.
- VM backup is a core feature using a bundled integration with restic and safe libvirt capture workflows. Core features must work without downloading third-party plugins.
- The long-running coordinator is a user service. Closing the interface does not terminate jobs. Unattended work across logout/reboot requires explicit setup of service persistence and available credentials.
- PCI/IOMMU inspection is in v1. Automatic GPU host reconfiguration, remote USB forwarding, and specialized passthrough orchestration are extension work; ordinary host-local USB is fully in scope.

## Normative terminology and precedence

**MUST** is a release requirement. **SHOULD** requires a written justification when not followed. **MAY** is optional. A hardware/backend restriction is acceptable only under the capability policy: detect it before mutation, explain it, and provide a supported alternative where possible. An unimplemented mandatory code path may not be mislabeled as hardware-limited.

Precedence: explicit product-owner decisions in `00` > safety invariants in `10` > domain/wire/schema contracts > workflow documents > illustrative examples. Resolve contradictions by recording an ADR and updating all affected files, not by silently choosing a convenient interpretation. JSON schemas validate declarative inputs; semantic checks specified in the documents are equally mandatory.

## Package use

Put this package into a new repository, read `START-HERE-AGENT-PROMPT.md`, and implement in the documented order. Keep this specification under `spec/` if the implementation uses its own root `README.md` and `AGENTS.md`. No project repository has been created or modified by preparing this package.

## Included specification aids

The package includes 18 numbered design chapters, 8 JSON schemas, validated declarative examples, a small Go reference plugin with tests, 71 release-acceptance scenarios, an agent kickoff prompt, implementation rules and a complete-release checklist. See the [package validation report](qa/VALIDATION-REPORT.md) for the checks actually performed and their limits.
