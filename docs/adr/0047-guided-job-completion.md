# 0047 — Connect completed jobs to their VM and check guest setup readiness

Status: implemented; validation recorded in the scoped evidence ledger.

The TUI previously ended VM workflows at a generic Completed screen and opened
SSH installation fields for stopped guests. Both required users to reconstruct
the next step. The required simple default workflow needs explicit next actions.

Completed local VM jobs now read their immutable plan through `plan.show`.
Creation also requires the durable `vm.creation.result` receipt to match the job,
plan, connection and target VM. A verified single target exposes Open VM; opening
it reads fresh inventory before showing controls. Unknown operations retain the
normal job view. Missing or inconsistent results explain the issue and offer a
read-only refresh. Historical completion does not assert current runtime state.
Canceled navigation invalidates pending observations. No job is replayed.

Guest tools first observes state and channel configuration through the shared
service. A running guest with a channel opens installation options; stopped,
paused or saved guests receive state-specific guidance and only valid reviewed
next actions. Preparing options and Windows help remain accessible. Configuration,
software installation and guest responsiveness remain distinct observations.

Prose wraps on word boundaries; structured alignment and long identifiers retain
their contents. No new dependency, persisted schema, mutation recipe or shared
service contract is introduced. CLI equivalents remain plan show, vm creation
result, vm show, vm guest-agent show and guest tools install. These changes refine
UX-01/UX-02 and GUEST-03 without reducing any mandatory v1 scope.
