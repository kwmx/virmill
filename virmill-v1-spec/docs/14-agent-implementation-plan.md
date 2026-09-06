# 14 — Agent implementation plan: one complete release

## Operating rule

Implement the entire agreed v1. The gates below sequence dependencies; none represents a reduced public release. Do not substitute screenshots, mock success, placeholder handlers or an architecture-only skeleton for functioning workflows. Do not stop after a gate and rename omitted features “future enhancements.”

| Gate | Build and prove | Exit evidence |
|---|---|---|
| 0 — Specification intake | Read all normative docs; create requirement/test mapping, ADR register and unknowns log. Confirm boundaries and safe disposable test targets. | Every acceptance ID has a planned test and owner; no silent scope changes. |
| 1 — Host/backend spike | Freeze supported distribution/package/guest matrix; prove libvirt enumeration, event handling, XML preservation, representative imports, real USB, network rollback and complete cold restore. | Executed spike evidence; concrete adapter/capability decisions and reproducible dependency lock. |
| 2 — Foundations | Domain/schema/error contracts, local IPC authentication, SQLite migrations, plan/grants, journal, locks, supervised jobs and independent watchdog mechanisms. | Crash/idempotency/permission tests; no production mutations in CI. |
| 3 — End-to-end VM operations | Working CLI plus usable TUI shell; lifecycle, creation, configuration, console/guest connections and storage fundamentals through shared services. | Real backend vertical slices, unknown XML and live/persistent tests. |
| 4 — Import and network completion | Secure archive inspection/conversion, source-specific translation, all mandatory network classes/multi-NIC policies, bridge setup/recovery, forwards and diagnostics. | Import fixtures and packet/physical rollback evidence. |
| 5 — Protection and devices | Snapshot graph, clone/template lifecycle, USB, guest sharing/setup, complete captures, restic repository integration and independent restore. | Restore without original DB/disks; physical device tests; freeze guardians. |
| 6 — Labs and automation | Declarative DAG, desired-state reconciliation, provisioning recipes, coordinated lab restore, backup schedules, persistent service configuration. | No-op reapply, interrupted DAG recovery, reboot/credential cases. |
| 7 — Extension platform | Actual plugin SDK/scaffolder, schema validation, package signing, confinement, structured TUI actions and mock provider. | Two-language conformance and permission/transport adversarial tests. |
| 8 — Interface and distribution | All command/TUI parity, interactive recovery, user/admin/plugin tutorials, RPM/DEB, upgrade/uninstall behavior and portable common-package builds. | Terminal/accessibility evidence and clean-host install matrix. |
| 9 — Release qualification | Full acceptance suite, fault injection, documentation review, reproducible source/package artifacts, final capability/support report. | Signed-off complete checklist; no mandatory stub or untested success claim. |

## Required implementation artifacts

Maintain `docs/implementation-status.md` with requirement ID, implementation paths, tests, evidence class/result and outstanding work. Maintain a structured capability registry consumed by UI, CLI, docs and provider tests. Generate command reference and JSON schemas from a common registry where feasible, and detect drift in CI.

Store ADRs for material decisions: dependency versions, XML patch strategy, firewall adapter, host networking ownership, backup capture lifecycle, TPM handling, plugin package verification and platform interfaces. ADRs explain alternatives, consequences and evidence. They cannot quietly demote product requirements.

Use small, testable commits by subsystem/vertical slice. Before code changes, inspect repository state and existing tests. Do not overwrite user work, assume a repository is empty, or use production VMs for convenience. Do not fetch/run untrusted installation scripts from source appliances.

## Suggested work decomposition

The shared domain/contracts and security/job foundations have one integration owner. Parallel agents may implement import, networking, protection, UI and extension modules after interface contracts are frozen. Each module uses fakes for early testing, then demonstrates real adapters before sign-off. Contract changes require downstream test regeneration, not ad hoc copies of data types.

Prioritize risky real workflows early rather than polishing a dashboard first. The most expensive correctness failures are data loss, wrong-device attachment, broken host networking, inconsistent backups and false readiness. A polished menu does not compensate for these.

## When blocked

Report the exact failing dependency, reproducible test, data at risk and remaining scope. Continue independent work. Distinguish “not implemented,” “not tested,” “unsupported by this environment” and “product exclusion.” Never present a runtime that was not available as tested. Hardware evidence may require operator execution of a documented disposable-host test; it cannot be replaced with a simulated pass.

## Final handoff from the implementation agent

Deliver source, reproducible build instructions, exact supported matrix, installation packages, changelog, user/operator/plugin manuals, schema/SDK versions, test evidence, known issues and a complete requirement traceability report. Include restore drills and host-network recovery instructions. The application is release-ready only when the documented acceptance gate is met, not merely when the compiler succeeds.
