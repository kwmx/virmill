# Discovery and plugin integration work

All 71 mandatory acceptance scenarios remain in the tracker. This work advances
prerequisites and fault handling without accepting a complete scenario.

| Agent | Acceptance IDs | Owned deliverable |
|---|---|---|
| `pci_inventory` | DEV-03; contributes CORE-06, UX-01, REL-03 | Native read-only adapter and tests in `internal/backend/libvirt/pci_inventory_linux*`, PCI fixtures and adapter documentation. Follow-up owns `internal/app/network/prefixes_test.go`. |
| `host_prefixes` | NET-06; contributes NET-05, CORE-06, UX-01 | Linux address/all-table route collector and tests in `internal/platform/linux/network_prefixes_linux*`, synthetic wire fixtures and collector documentation. Follow-up owns `internal/app/host_inventory_test.go`. |
| `plugin_faults` | EXT-03, SEC-04/05; contributes EXT-01 | Python fault fixture, `internal/plugins/runner_faults_test.go`, `runner_close_test.go` and fault-fixture documentation. |
| Parent integration | Shared contracts, architecture, integration, release tracking | Neutral types, service/CLI/TUI/schema, runtime fixes, user documentation, final verification and evidence ledger. Sole operator of the disposable SSH host. |

Agents used no SSH and made no remote or local host configuration changes.
Their independent files were reviewed and integrated by the parent. After initial
deliveries, dependency-ready parser/service/lifecycle tests were assigned without
overlapping production edits. User authorization for the disposable VM remains
the only authority for remote mutations; existing guests and source media are
preserved.

PCI parser tests cover identity, namespace, group consistency and bounded errors.
An actual native in-memory libvirt driver enumerated three synthetic PCI nodes
and filtered out a USB node; that driver omitted configured driver labels.
Agent fuzz runs reported 137,257 PCI parser executions and 163,586 network XML
parser executions without failures. The prefix collector's synthetic dumps cover
both families, defaults, policy tables, peers and multipath. Its agent fuzz run
reported 1,056,594 executions without failures. An ordinary-user current-namespace
smoke observed 20 prefixes outside the outer tool sandbox; no addresses were
logged. These are scoped development observations, not physical or packet tests.

The parent preserved `plugin-fault-regressions-001`: malformed framing, wrong
IDs/envelopes and premature host API calls did not always retire the worker.
`plugin-fault-fix-001` passed the original 18-case process matrix with the race
detector after the runtime fix. The agent added three initialization-result cases
and passed all 21 through the production confined launcher plus a real kernel
file/socket/environment boundary probe. The parent separately recorded
`plugin-close-regression-001` and `plugin-close-fix-001`: active Close first blocked
behind a hung call, then passed after worker-stop signaling and once-only cleanup.

Parser/service review also identified empty ambiguous mask attributes, canceled
empty observations and missed inactive static routes. Parent changes now refuse
ambiguous input, honor cancellation before observations and retain configured
static-route destinations, including their documented default routes. Shared UI
tests verify both commands and the JSON CIDR form without producing a plan/job.

A worktree full-suite run while the static-route test update was pending failed
only its old direct-IP-only expectation. The updated test includes the direct
route and passed before source freeze; this was a fixture expectation change,
not a routing test or a suppressed failure. Revised parser fuzzing completed
359,980 executions without a failure.

Final immutable-source verification and any actual installed read-only inventory
observations are recorded separately in the append-only ledger. Native managed
read-access evidence belongs to clean `b7fe053` in
[managed-access-run.md](managed-access-run.md), not to this newer source.

Remaining qualification includes physical PCI/IOMMU, actual VPN and policy-route
conflict cases, allocation/revalidation, network packet/isolation and rollback,
USB reconnection, firmware/TPM capture/restore, full crash recovery and plugin
confinement across the supported OS/MAC matrix. Unimplemented workflows are
implementation work, not hardware skips. No release publication is authorized.
