# Fixed CPU/RAM configuration evidence

Source revision: `65930c63498f58b19f962e86a4b20f83841de872`.
Source-tree digest:
`a38a73c2db98ebad23e2fc5657b17fe3421dc5fa3014dba90e432872f37ccc74`.
This is development evidence. The full 71 acceptance scenarios and release
checklist remain unaccepted; no real VM or guest behavior is certified.

`vm set` now supports fixed CPU/RAM edits on powered-off persistent VMs through
the shared service, CLI and TUI. It preserves unrelated XML bytes, existing memory
units and matching maximum/current allocations. Dependent topology, pinning, NUMA,
balloon/hotplug/backing layouts, unknown resource attributes and managed-save state
are refused. The native backend repeats capability and source checks, and both
public and secure readback must prove the expected result.

This fixes two risks in the prior count-only edit: a changed vCPU count could
conflict with untouched topology/tuning, and ordinary libvirt XML can omit display
passwords. Secure XML differences are now refused before definition. Opaque XML
may contain custom credentials even in the ordinary view, so new plans persist
requested counts and XML fingerprints, not the document. Returned schema/native
definition errors withhold input/XML values. See [ADR 0011](../adr/0011-fixed-resource-configuration-preservation.md).

The durable operation is `vm.configure-resources`, preventing older registries
from executing it through legacy `vm.set`. SQLite schema 3 is unchanged. A lost
acknowledgement can reconcile from the original expected hash and secure readback
after journal reopen without redefining. Unproven effects retain uncertainty and
locks. Cancellation after the call begins does not pretend a verified change was
undone. Legacy uncertain edits still need explicit disposition.

| Evidence | Observed result and boundary |
|---|---|
| [configuration-core-001](logs/configuration-core-001.log) | Full Go race suite passes. Fixtures cover exact XML preservation, mixed units, overflow/inexact conversion, node limits, dependent policy, stopped/saved/live checks, stale fingerprints, hash-only plans, value-free errors, failed definition, in-flight cancellation, legacy registry refusal and real SQLite reopen with a synthetic backend. Default-sandbox native worker/IPC skips remain explicit. |
| [configuration-native-simulated-001](logs/configuration-native-simulated-001.log) | Official libvirt bindings redefine only an in-memory test domain and verify CPU/RAM plus opaque metadata and synthetic disk-path preservation. The test driver actually redacts a synthetic VNC password; preflight and reconciliation refuse ordinary XML as complete evidence. No display server, host domain or guest was started. |
| [configuration-xml-fuzz-001](logs/configuration-xml-fuzz-001.log) | A bounded 15-second, two-worker XML resource parser run completes 662,883 executions without a failure. This is parser evidence, not full preservation or host safety certification. |
| [configuration-static-portable-001](logs/configuration-static-portable-001.log) | Vet, common-package Darwin arm64/Windows amd64 cross-builds and SDK race tests pass. No new dependency or non-Linux virtualization claim. |
| [configuration-build-001](logs/configuration-build-001.log) | Two offline builds produce identical three binaries and four unsigned development RPM/DEB packages. Generated help and configuration documentation are included. |
| [configuration-artifacts-001](logs/configuration-artifacts-001.log) | Three package/private-daemon tests pass. Packaged resource requests validate schema, withhold a rejected dummy credential and refuse production access to a test URI before any host effect. Existing actual confined OVA/disk/ISO preparation and NoCloud preview still pass. Install/uninstall occurs only in a temporary staging root. |
| [configuration-release-gate-001](logs/configuration-release-gate-001.log) | Expected exit 1: all 71 acceptance scenarios and SHIP-CHECKLIST remain blocked. The failed release result is retained. |

The CLI reports `0.0.0-dev`, the revision above and `releaseQualified: false`.
CLI SHA-256:
`c3c9c0fd7de130ed1c68d0b6ee7b19d36ba4f6ad7828b31f087fdda21a28e3b9`;
daemon SHA-256:
`9f78fcbe521fd68b75aa3241bdb1462327cec5bebc7cc9403325aae3e5d35142`.
The build log records all binary/package hashes. All seven new evidence entries
bind the exact source revision/digest. The complete append-only ledger contains
82 entries and all 80 recorded log hashes verify; two legacy intake records retain
their original shape. No package was installed on the host or published.

Progress contributes to CORE-03/04/05/06, JOB-02/04, SEC-02/03, UX-01/03 and
REL-01/02/03. Real native configuration, guest sizing, adopted XML preservation and
external-writer safety remain unqualified. Libvirt has no atomic compare-and-swap
for full definition. Virmill locks its own clients and repeats observations;
`exclusive-configuration-writer` acknowledgement requires coordination with other
administrators and does not claim a kernel-enforced external lock.

Next dependency-ready work includes boot order, installer console/media ejection
and the remaining lifecycle/configuration controls. Complete networking, USB,
firmware/TPM capture/restore, labs, guest recipes, plugin capabilities and guided
interface work remains required. No owner authorization for host mutation,
publication or scope reduction is inferred.

Operator guide: [VM configuration](../vm-configuration.md).
Fixture recipes: [configuration fixtures](../../tests/fixtures/creation/README.md).
Current status: [requirements-to-evidence matrix](../requirements-to-evidence.md).
