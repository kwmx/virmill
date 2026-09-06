# Gate report — development state, not a release handoff

Specification revision: 1.0-draft.2. All 17 families and 71 acceptance scenarios
remain mandatory. Source package audit: 49 indexed files, 48 present and digest
matching; missing kickoff document recorded in ADR 0001. No scope was reduced.

| Gate | Current result | Remaining exit evidence |
|---|---|---|
| 0 intake | Available docs/schemas read; ADRs, unknowns and 71-row tracker created | Missing kickoff noted; actual disposable host not designated |
| 1 spike | Native official binding builds with dlopen and runs libvirt test driver; OVA/XML/USB/manifest/network-intent fixtures implemented | Actual system/session/event/import/USB/packet/rollback/cold-restore matrix and guest versions |
| 2 foundations | SQLite WAL/FULL, version guard/backup, locks, strict RPC, UID checks, immutable plan/dedup and crash observation | Full state/step schema, all recovery adapters, schedules, independent guardians and real effect faults |
| 3 VM operations | Shared CLI/TUI service, native inventory and subset of planned lifecycle/vCPU edits | Complete creation/configuration/storage/console/guest paths and real VM evidence |
| 4 import/network | Bounded package inspection and semantic topology validation | Conversion/registration/all network mutation/policy/rollback/packet flows |
| 5 protection/devices | Graph, manifest and USB selector safety libraries | Actual capture/restic/restore/templates/snapshots/USB/sharing integrations |
| 6 labs/automation | Schema, reference and DAG validation | Actual DAG apply/reconcile/teardown, provisioning, scheduling and coordinated restore |
| 7 plugins | SDK/scaffolder, signing verifier, sandbox, two-language and persistent simulated provider fixtures | Complete installed lifecycle/grants/broker/UI/provider contract matrix |
| 8 distribution/UI | Three named binaries, generated help/completions, unsigned development core/helper RPM/DEB | Full interface parity/accessibility, clean distro install/upgrade matrix and production installers |
| 9 release | Evidence tooling and release blocker check present | Entire complete checklist remains unqualified |

Evidence is retained in `evidence/ledger.jsonl` with logs and source/fixture digests.
The core race suite, SDK cancellation, private IPC, confined two-language execution,
simulated provider restart recovery and portable common-package builds pass their
recorded scopes. This is not KVM boot, USB, packet routing or independent VM restore
evidence. The helper's privileged filesystem action was not executed.

A read-only outer-host probe confirmed `/dev/kvm` exists and UID 1000 has access;
the default execution sandbox hides it. This does not authorize destructive tests.
There is no designated disposable host/VM/network/USB target or guest media matrix.
No system package, service, bridge, VM, device attachment or host storage operation
was applied; only build outputs and disposable application/test fixtures were written.

The next dependency-ready work is complete immutable VM source/resource contracts,
backend lifecycle completion, supervised confined disk inspection/conversion and the
remaining recovery handlers, followed by safe actual creation and protection slices.
The root release checklist and requirements matrix must pass before version 1.0.
