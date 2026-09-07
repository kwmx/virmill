# Gate report — development state, not a release handoff

Specification revision: 1.0-draft.2. All 17 families and 71 acceptance scenarios
remain mandatory. Source package audit: 49 indexed files, 48 present and digest
matching; missing kickoff document recorded in ADR 0001. No scope was reduced.

| Gate | Current result | Remaining exit evidence |
|---|---|---|
| 0 intake | Available docs/schemas read; ADRs, unknowns and 71-row tracker created | Missing kickoff noted; actual disposable host not designated |
| 1 spike | Native official binding builds with dlopen and runs libvirt test driver; OVA/XML/USB/manifest/network-intent fixtures implemented | Actual system/session/event/import/USB/packet/rollback/cold-restore matrix and guest versions |
| 2 foundations | SQLite WAL/FULL, schema-3 migration backup/barrier, atomic recovery lock transfer, strict RPC, UID checks, immutable plan/dedup and crash observation | Full state/step schema, all recovery adapters, schedules, independent guardians and real effect faults |
| 3 VM operations | Shared CLI/TUI, native inventory/lifecycle subset, prepared-appliance define-last creation and catalog identity, reviewed definition recovery and explicit failed-creation disposition | All creation entry paths/full forms/configuration/retained-pin release/console/guest paths and real VM evidence |
| 4 import/network | Bounded package inspection, confined all-disk OVA conversion/atomic artifact publication, explicit disk/NIC mapping, native pool/network reads and topology validation | Guest adaptation and registration/boot qualification; all network mutation/policy/rollback/packet flows |
| 5 protection/devices | Confined generation-bound failed-creation graph/deletion adapter; manifest and USB selector libraries | Actual capture/restic/restore/templates/snapshots/USB/sharing integrations |
| 6 labs/automation | Schema, reference and DAG validation | Actual DAG apply/reconcile/teardown, provisioning, scheduling and coordinated restore |
| 7 plugins | SDK, shared scaffold/pack/install/version/grant plans, confined read-only actions, two-language and persistent simulated provider fixtures | Unsigned/development override, quarantine, brokers, structured forms and full extension/provider matrix |
| 8 distribution/UI | Three named binaries, generated help/completions, unsigned development core/helper RPM/DEB | Full interface parity/accessibility, clean distro install/upgrade matrix and production installers |
| 9 release | Evidence tooling and release blocker check present | Entire complete checklist remains unqualified |

Evidence is retained in `evidence/ledger.jsonl` with logs and source/fixture digests.
The [creation evidence summary](evidence/creation-slice.md) links the latest scoped
checks, reproducible development artifacts and the still-blocked release gate.
The core race suite, SDK cancellation, private IPC, confined two-language execution,
simulated provider restart recovery and portable common-package builds pass their
recorded scopes. This is not KVM boot, USB, packet routing or independent VM restore
evidence. The helper's privileged filesystem action was not executed.

A read-only outer-host probe confirmed `/dev/kvm` exists and UID 1000 has access;
the default execution sandbox hides it. This does not authorize destructive tests.
There is no designated disposable host/VM/network/USB target or guest media matrix.
No system package, service, bridge, VM, device attachment or host storage operation
was applied; only build outputs and disposable application/test fixtures were written.

ADR 0005 defines the owner's MVP request as a development milestone within the
unchanged full 1.0 scope. Real generated-file tests now cover six source formats,
two split VMDK disks, contained backing-chain flattening, missing/outside extents,
CLI/daemon preparation and hash verification. Synthetic phase tests additionally
cover cancellation cleanup and reconciliation after lost publication acknowledgement.
These do not qualify VMware guests or libvirt boot. ADR 0006 adds native creation
code with define-last storage phases, catalog identity and a reviewed recovery
operation for fully verified retained volumes. Native test-driver XML validation
covers BIOS and UEFI/TPM definitions; installed QEMU help probes cover device
metadata. Neither exercises native storage streams, initializes firmware/TPM or
boots a VM. Coordinator fixtures exercise cancellation, tampered volume refusal,
lost acknowledgements, complete lock transfer, rollback and schema-1 migration.
ADR 0007 adds durable retention pins, guarded deletion of recorded new file
generations, and schema-1/2 migration to schema 3. Generated-file metadata and
native simulated-inventory tests protect backing references and replacements;
coordinator fixtures cover partial deletion, cancellation, old-job references
and restart observation. Actual native deletion remains unqualified.
The next dependency-ready work includes other VM creation entry paths,
lifecycle/configuration completion, retained-pin release and protection handlers.
The root release checklist and requirements matrix must pass before version 1.0.
