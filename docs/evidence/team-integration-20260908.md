# Parallel work and integration — 2026-09-08

The owner authorized up to three agents alongside the integration owner. All
71 acceptance scenarios remain mandatory. Architecture, shared contracts,
production fixes, integration, release tracking and **all remote execution** stay
with the parent. No subagent used SSH or changed the developer host's services,
network or storage. Existing disposable guests and source media are retained.

| Agent | Dependency-ready deliverable and ownership | Acceptance contribution | Actual verification / integration |
|---|---|---|---|
| Nash (`host_prefixes`) | New single-use native-002 fixture and notes after the installed `virsh` rejected native-001's inventory option; subsequently new read-only TUI-003 observer and notes. Previously executed recipes remain immutable. | IMP-07, SNAP-01, JOB-02; UX-03 / REL-03 for the shared UI observer | Parent parser integration passed 29 synthetic cases. Parent native-002 reached durable A, repeated A, B refusal and successful restored-A reconciliation, then failed a pool-statistics assertion. Failed evidence retained; parent supplemental TUI-003 passed 20 plan and 10 result pages with unchanged journal/seven stopped XML definitions. No agent native run. |
| Beauvoir (`pci_inventory`) | New TUI-002 fixture/notes, then only `internal/app/protection/cold_point_adversarial_test.go` and `docs/reviews/cold-point-adversarial.md`. No production or contract edits. | UX-03 / REL-03; SNAP-01, BAK-01 and BAK-04 declared-integrity prerequisites | Ten synthetic terminal tests passed. Parent integrated the new cold-point cases and ran both protection and validation packages under the race detector in `cold-point-adversarial-001`: passed. No production defect reproduced, capture or restore qualified. |
| Russell (`plugin_faults`) | Package input implementation in `scripts/package.py`, `scripts/reproducibility.py`, `tests/integration/package_inputs_test.py` and its hygiene review; then only `docs/reviews/cold-capture-endpoint-readiness.md`. | REL-01 / REL-02 / REL-03; SNAP-01 / SEC-01 / JOB-02 endpoint prerequisites | Package implementation committed in frozen dc2ab1a after 20 generated tests. Parent actual reproducibility, package checks and disposable upgrade passed. Later helper/coldfiles readiness review completed; its separate sandbox FD cases skipped, not passed. The parent frozen full race suite had already required and executed IPC. |

Runtime qualification uses immutable commit
`dc2ab1a7af8c023c1485d36d0e858a65264ace3e`. Later test recipes and reviews have their
own digests; their existence is not represented as a new installed binary.
The ledger and [native binding record](nvram-binding-run.md) distinguish the
observed native boundaries, failed fixture attempts, synthetic tests and remaining
qualification. The complete [requirements matrix](../requirements-to-evidence.md)
continues to report every acceptance case as unaccepted.

The next capture implementation dependencies are explicit auxiliary permission
in administrator policy, independent native member/root/generation binding,
complete-set leases and checks, dedicated authenticated FD success/error handling,
and durable coordinator publication. Existing ACL permission must not gain
confidential-read authority merely because another helper action is implemented.
A sealed object or a helper send receipt cannot prove durable complete capture.
