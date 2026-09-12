# Network creation from VM setup

Scope: UX-01/02, NET-01/03/04/06 and JOB-03 partial workflow evidence.
All 71 acceptance scenarios remain required. No acceptance status is promoted.

`creation-network-handoff-ui-001` passed the complete TUI and CLI packages with
race checking (6.920s and 3.466s). Tests cover exact prepared/unprepared setup
restoration, network-list-only refresh, unavailable identities, canceled/stale
replies, separate network/preparation job identities, the editing-draft barrier,
failure return and success return. Eighteen active dialog/submission states
prevent automatic navigation from discarding an interaction. Independent review
identified that guard before the recorded passing run. Early test-only fixture
assumptions about duplicate refresh and preparation identities were corrected.

`creation-network-handoff-fixture-001` passed three pure native-fixture helper
tests. This is fixture validation, not native networking evidence. The fixture
uses a previously generated prepared source, changes no VM, and permits at most
one new, explicitly reviewed lab network with host access Allow. It preserves
existing guest/network/job inventories and source-media metadata, retains its new
network, and checks actual 80×24 TUI/CLI plan parity and wizard return.

Agents implemented the form controls, independent handoff regressions and native
fixture. Root integrated the state/draft contracts, review correction, operator
documentation and release tracking, and coordinates all remote mutations.
Dependencies are unchanged and remain pinned. SSH and passwordless sudo were
reverified on the authorized Fedora test VM before deployment.

Native results and package identity are appended after execution. This workflow
does not qualify guest packet routing, isolation, physical devices or the complete
network acceptance profiles.
