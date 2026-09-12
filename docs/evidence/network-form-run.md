# Guided network creation and inline declaration parity

This slice adds ordinary TUI controls to the existing network.create workflow.
The shared service accepts either a declaration file or one inline document;
both receive identical schema, policy, planning and apply validation. Recipe
versions, native identity and helper authority remain unchanged. Export writes
a new full declaration; preview/back/error/cancel preserve in-memory options.
See ADR 0048 and the network creation guide.

Three agents implemented the form, application/CLI parity tests and a guarded
native preview fixture. Root owns shared-service/CLI contracts, workspace and
export integration, review, remote operations and traceability. Pure fixture
predicates passed in network-form-fixture-unit-001. No mock, synthetic view or
successful plan qualifies network packets, routing or guest behavior.

`network-form-core-001` passed the integrated Go race suite with required IPC
fixtures. Seven profile variants compare file/inline semantic review, exact
durable recipes and readback; invalid and unsupported input reaches no provider
effects or reservations. CLI dispatch preserves existing file and structured
error behavior. Root's workspace tests cover preview/back, canceled reads,
connection refusal, complete declaration export and the advanced file path.

Independent integration review found background preparation could interrupt the
new modal and Esc could mislabel a pending apply as canceled. Both were corrected;
`network-form-ui-002` passed the full TUI race suite after the first fix, and
`network-form-submit-003` passed targeted form/workspace regressions after the
pending-apply guard. Vet passed. Unchanged cached tests are regression evidence,
not fresh native qualifications.

## Installed beta

Source `31f303459c4041cd9a6500419eb69c86527db492`, implementation-tree digest
`fa474c2c97ee1aff05dfb3ad7b389babab413b03bc086bd31f27f5714d2e6c0e`.
`network-form-packages-001` passed all three artifact/private-service checks.
`network-form-upgrade-001` and `network-form-restart-001` passed on the authorized
Fedora 44 VM, preserving all 29 prior jobs, journal tables, guests and source-media
records. The new ordinary-user coordinator PID is 92306.

Installed SHA-256:

- CLI: `c6b0c9ef1518961d1456a304bb7a473620036078abd6bfd9a72c0fb9b637fb09`
- Coordinator: `e1219f02172fb375bcbcb4076d66d92dde10d2117d818d5f0e578e872820ef96`
- Core RPM: `a89b94fae6c75514d4be04c875dc7c60ee834262d11c77086014e7f5d2e8e8d0`

Packages are unsigned beta artifacts. No helper policy was changed and nothing
was published. This upgrade does not certify new network/guest/hardware profiles.

## Native preview blocker and complete issue display

`network-form-tui-native-001` failed at its first NAT preview. The installed form
opened and collected values correctly; the shared service refused automatic
selection with UNRESOLVED_ALLOCATION. The retained guest-only network
`76e1eaa8-42b2-46ac-93d3-be9d5a31caac` has logical subnet `10.193.78.0/24` in the
original protected-network-native-003 evidence. A second retained guest-only
network `ac06e66c-5f16-4997-b02a-55b327c522e6` has `10.193.75.0/24` in
protected-network-native-002. Their old fixture journals are separate from the
current coordinator. A read-only network cidr check with both original facts
passed exact native-intent matching; no subnet was guessed or network modified.
The failed TUI run preserved all VM, network and job inventories and source media.

The full service error included recovery instructions, but the form showed only
two rows. A follow-up adds Read full issue with complete scrollable text and
retained controls. The first full-suite run, network-form-issue-ui-004, failed an
assertion that compared newly initialized viewport dimensions as though they
were edited settings. The test now initializes its viewport before taking the
snapshot. network-form-issue-ui-005 passes the full TUI race suite, including
complete error UUID/recovery text, resizing, page boundaries and back navigation.
Vet passed. This correction is not yet verified in the installed native TUI.

The ordinary user's allocation configuration was absent. A guarded fixture was
prepared to restore only those two validated allocation facts, retaining the
normal search pools and refusing any existing configuration overwrite. It changes
no native network, helper policy, guest or job. fixture-allocation-facts-native-001
failed during SSH banner exchange; a subsequent read-only observation returned
No route to host. There is no verified repair report or configuration-write claim.
Before retrying, inspect that exact fixture's report and configuration presence.
The original failed evidence is retained. The three-profile native preview/parity
check remains incomplete until the same authorized VM is reachable and the
allocation facts are safely restored.

## Restored access and installed preview verification

The VM became reachable after reboot. `fixture-allocation-facts-native-002`
refused before any settings write because the coordinator socket was absent;
its preservation flag means the initial inventory could not be captured, not
that a mutation occurred. The first service start/read check raced readiness and
the user manager then stopped after the short SSH login ended. A held test login
kept the service available; `network-coordinator-post-reboot-002` passed readiness
and read all 29 retained jobs. No guest was started.

`network-issue-packages-001` passed all three package/private-service checks.
`network-issue-upgrade-001` and `network-issue-restart-001` installed and activated
source `4d5fe54a809fcd5eff0d022f4dde91b89fb11803`, preserving the prior jobs,
journal tables, VM inventory and source-media metadata. Coordinator PID: 1944.

- CLI SHA-256: `85763d43ee642bf08421381b2d6d66bb0efe7eb6bdaab8d4a0a46debbe6ba257`
- Coordinator: `225b110248788cff2f615e0a2fd528a403f1942b76fad7942b8ad06ad3151c42`
- Core RPM: `a6b548bc869c5ba37aa3407994153238b7e6fd0ca97888a2a327e5e30b54a9e1`
- Built implementation digest: `c02c3675e0c01b5c18c229b34e13b959be9e2a360c9861862d7cd506208e7c9a`

`fixture-allocation-facts-native-003` passed both exact native-intent matches,
created only the previously absent user allocation settings, and verified their
normal readback. VM/network/job inventories were unchanged. Configuration digest:
`2916a1bb382bfefbb14b5985d00f83ecee95caa9e4eb2f2a45dcb68714959162`.

`network-form-tui-native-002` passed actual 80×24 NAT, lab and guest-only previews,
CLI/TUI semantic parity, Back retention, cancellation, unsupported-session refusal
and preservation of guests, networks, jobs and source metadata. Zero plans were
applied. Six preview plans remain as durable review records. This verifies the
installed form and shared planning, not network activation or packet isolation.
Its ledger source digest includes concurrent unbuilt startup-help edits; tested
binaries are explicitly bound above and in the native report to frozen 4d5fe54.

`beta-user-service-signin-001` enabled the ordinary-user coordinator for future
sign-ins on the disposable VM and verified enabled/active status. No helper,
guest autostart or login lingering was enabled. Remote reports and screens are
retained under `~/virmill-tests/network-issue-4d5fe54`.

## Startup recovery guidance

A dependency-ready agent added an offline Overview/Settings recovery card for
accepted typed COORDINATOR_UNAVAILABLE replies. It explains that VM state is
unknown, shows the ordinary-user service-start command, and offers Retry
connection and optional startup help. Retry uses existing observation requests;
no UI service mutation was added. Root reviewed the integration and static
analysis passed. `coordinator-recovery-ui-001` records the full TUI race suite,
including stale replies, existing workflow preservation and retry behavior.
