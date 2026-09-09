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
