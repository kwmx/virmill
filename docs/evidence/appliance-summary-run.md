# Appliance summary correction — 9 September 2026

This slice covers IMP-01, IMP-03, UX-01 and UX-02. It does not complete these
acceptances or qualify the full 1.0 release.

Implemented: automatic metadata description after OVA file selection, editable
name/CPU/RAM defaults, detected OS and devices, explicit OS discrepancies and
unsupported NVRAM restoration, advanced controls before destination selection,
and in-session settings handoff to creation. Full preparation verification and
reviewed apply remain unchanged. See ADR 0038 and the TUI navigation guide.

Delegation: Erdos implemented bounded seek-based description and independent
workspace integration tests; Zeno implemented namespace-aware profile metadata
and receipt compatibility tests; Arendt implemented the summary and navigation.
The parent integrated shared service/CLI contracts, schema, creation handoff,
release tracking and all remote operations. No agents mutated the remote VM.

Software evidence `appliance-summary-software-001` passed affected-package race
tests. A 10 GiB sparse ordinary tar fixture exercised seeking with 4,098 bytes
read; it is parser behavior evidence, not guest or hardware validation. Native
owner-media description and installed TUI checks are recorded separately below.

Limitations: metadata is a declaration, not boot compatibility or integrity
proof. No imported Windows guest boot, NVRAM restoration, physical USB behavior
or complete acceptance claim is made. Unfinished wizard drafts are currently
retained only within a TUI session. All 71 scenarios remain required.

## Native run and correction

`appliance-summary-upgrade-001` and `appliance-summary-restart-001` installed
source `6c48617025ddf10a6607d7b0efa98752fab138d2` and activated its coordinator.
All existing guest inventory, three journal jobs, journal tables and supplied
media metadata were unchanged. Package/staged installer/private IPC checks
passed (`appliance-summary-packages-001`, three tests).

`appliance-summary-native-001` read the owner's actual 36,551,735,808-byte
DFIR-Win11 OVA in 0.114 seconds, showing its summary in 0.269 seconds.
CPU 2, RAM 8192 MiB, Windows 11, UEFI and USB/audio declarations were returned
without hashing payloads. The walkthrough failed because Advanced Back left
the summary scrolled below the edited basics; values remained present. This
failed checkpoint is retained. The correction resets focus to the VM-name row
and improves metadata word wrapping. A focused regression reproduces it.

## Final installed result

`appliance-summary-native-002` passed against the same original DFIR OVA at
80×24 and 120×36. The installed CLI and running coordinator identify source
`58c614606337f8fb87e3bbcbaa9d0d6bbaa9ce69`; implementation-tree digest is
`c55d4ea8b49c8bd34114b58671b8ed55b1fd6daa2ba683a2fccb5e0dfca994d5`.
Metadata took 0.113 seconds; automatic summary appeared in 0.266 and 0.265
seconds. Actual screen captures show Windows 11, CPU 2, 8192 MiB RAM, the
120 GiB disk, SATA AHCI and declared UEFI. The workflow edited name/CPU/RAM,
opened Advanced before any destination existed, and retained visible edits
on return and after destination navigation. No preparation, Apply or guest
boot was performed in this read-only walkthrough. Existing guest inventory,
job list and source media generation were unchanged.

The previous slice's native two-disk preparation and powered-off definition
remains documented in hardware-ux-run.md; it is not reclassified as a boot test.
The final software/navigation and three package/IPC checks passed under
`appliance-summary-navigation-001` and `appliance-summary-packages-002`.
Guarded package/restart records are `appliance-summary-upgrade-002` and
`appliance-summary-restart-002`; active ordinary-user daemon PID was 67928.

| Installed artifact | SHA-256 |
| --- | --- |
| Core RPM | `12cf98769799dc07f9e9ff5a94d0a9eb1bd6cd75417d3e65ba8eb9ba884498b8` |
| `/usr/bin/virmill` | `f7659ed6a2d497ed118ec278b2cf70cc721c63e9f20c9d6db77b6779bbef300c` |
| `/usr/bin/virmilld` | `b86e73b1a25a8329779b7c68bae7a9c71385d87cf6b33ebb2fb5425826895a97` |

Remote packages and current-screen captures remain in the private test run
`~/virmill-tests/appliance-summary-58c6146`, including `native-002`. The helper
package and its policy were not changed. No release publication took place.
