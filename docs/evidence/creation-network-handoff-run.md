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

`creation-network-handoff-packages-001` passed all three package/private-IPC
checks. `creation-network-handoff-upgrade-native-001` installed the exact core
RPM and verified its binaries without replacing the running coordinator.
`creation-network-handoff-restart-native-001` then activated the new coordinator
with all 32 existing jobs, journal tables, VM inventory and source-media metadata
preserved. The helper remained unchanged.

Runtime source: `40798309a4180d5adfd32f5d37da5b26abebfd8f`.
CLI SHA-256: `0ffb03503f246a2615992b0039f6f222a57b7f38d861177a7a393e5767ee62c7`.
Coordinator SHA-256: `f4b3ca7f34deaa318cb07858a3648eb67525bcfce85861c1ed2d41d444e99e8f`.
Core RPM SHA-256: `e68a95b5e3ec157d720830d67b451177adbb33c9db8bb87d216a6392a99a64d4`.
Unsigned packages and manifests are retained in `build/creation-network-delivery`.
The authorized remote stage is `~/virmill-tests/creation-network-4079830`.

`creation-network-handoff-native-001` passed actual cancel/refresh settings
preservation and normalized TUI/CLI network-plan parity, then failed after Apply:
the coordinator lacked its private helper signing key. No job was accepted and
no new network appeared; all prior guest/job/network inventories and source-media
metadata remained unchanged. Allowed-host lab creation still requires the helper
for disabled-IPv6 rules. The fixture's original contrary assumption was wrong.
The recorded failure remains retained; a mock cannot replace its missing native
completion evidence.

That run also exposed a truncated submission error. `creation-network-issue-ui-001`
passed the complete TUI package with race checking (6.683s) after resetting the
review scroll to the complete wrapped error, retaining the exact plan/request,
and adding helper-setup or Jobs guidance. Four long-error cases page through every
word at 80×24, then return to unchanged network and VM settings. The fixture now
captures refusals immediately and supports an exact-plan pause for a separately
approved helper grant; five pure helper tests passed in
`creation-network-handoff-fixture-002`.

Automatic approval review rejected the proposed test-host helper bootstrap
(private-key creation, public identity registration and socket activation),
requesting explicit owner authorization beyond earlier testing/sudo permission.
No bootstrap command ran. The owner was asked for this exact scope plus a single
reviewed test-network grant. Ordinary UI packaging work continues while that
approval is pending.

Independent review of the unexecuted helper setup found two issues: registering
a key also enables existing managed-storage helper operations under the policy's
`images` root, and replacement must preserve the original policy group as well
as mode. The fixture now requires explicit exact existing-root authorization,
preserves UID/GID/mode, and checks socket/firewalld prerequisites before writing
policy. The owner approval request was revised to disclose that storage authority.
The helper setup remains unexecuted pending that approval.

Corrected runtime `e318527fb0d97b45d4d8859f45f11583eb427186` passed
`creation-network-corrected-packages-001`,
`creation-network-corrected-upgrade-native-001` and
`creation-network-corrected-restart-native-001`. The corrected core was installed
and activated with all 32 prior jobs and each fixture's observed guest inventory,
journal tables and source-media metadata preserved.

Corrected CLI SHA-256: `1a62bb0f7a565f6fae39aaa3bfc1c663d98c637178d7d678cc1ce39196d19d95`.
Coordinator SHA-256: `be18dd82a8517e971092a539c460624609cf02eef75444cc29bb2a5f88caf752`.
Core RPM SHA-256: `6c731f8907111efcb15d170a6962962b887a368c5add3c4e7fc00b3fe304394c`.
Current unsigned artifacts: `build/creation-network-corrected-delivery/`.
Current native stage: `~/virmill-tests/creation-network-e318527`.
Fixture-only source changes after runtime freeze are bound by ledger source
digests; exact installed runtime is bound by the binary hashes above.

`creation-network-refusal-native-001` captured the corrected full 80×24 error
and no accepted job, then failed in the fixture's unsupported 512×32 diagnostic
resize. Root decoded the retained actual PTY transcript and inspected the full
message. The fixture was corrected to its supported 120×36 diagnostic size;
`creation-network-handoff-fixture-004` passed its six pure checks. The original
failed report and screenshots are retained, and the continuation uses a new
private stage rather than overwriting them.

`creation-network-refusal-native-002` passed on the installed corrected runtime.
The actual 80×24 wizard preserved CPU/RAM/disks and disconnected, unselected
adapters through cancel and refresh; normalized CLI/TUI network plans matched.
The precise missing-key refusal displayed its complete issue and administrator
guidance. No matching job was accepted, no network was created and all prior
guest/job/network inventories and source-media metadata were preserved. This
passes error/preservation behavior only: `completedJobReturnedToWizard` is false.
The successful-job handoff remains locally tested but natively blocked pending
the explicit helper authorization. Root inspected the recorded screen.

Final report and issue screen are retained under
`~/virmill-tests/creation-network-refusal-e318527/creation-network-handoff/` and
`build/creation-network-corrected-delivery/`. Previous failures remain in their
original stages and ledger entries. No package publication occurred.
