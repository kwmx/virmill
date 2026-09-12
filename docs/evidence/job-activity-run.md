# Direct job activity

Scope: JOB-02/03 and UX-01/02 partial workflow evidence. All 71 acceptance
scenarios remain mandatory; no requirement status is promoted.

`job-activity-ui-001` passed the complete TUI and CLI packages with race checking
(7.172s and 3.426s). Regressions cover direct selected-row access, ordered pages
with global sequence gaps, failed-page retention and retry, invalid identities,
late replies, complete sanitized messages at 80×24 and preventing unrelated
completion navigation. Failures move the viewport to the explanation while
retaining earlier events and page history. These are synthetic UI/service tests.

`job-activity-fixture-001` passed three offline native-fixture decoder tests.
The native fixture reads the retained removal job and compares actual complete
TUI messages to the CLI, refreshes and returns to the same job. It does not create
plans or jobs, execute helper operations, or mutate guests or supplied media.
Native results are appended after execution.

Agents supplied presentation, independent regression tests and the native fixture.
Root integrated the controller, shared service calls, documentation and release
tracking, and owns all remote actions. No service/API/schema/dependency change.
The separate network helper authorization remains pending and is not bypassed.

`job-activity-packages-001` passed all three package/private-IPC tests.
`job-activity-upgrade-native-001` and `job-activity-restart-native-001` installed
and activated source 8ebcbfc while retaining all 32 jobs and recorded journal,
guest and source-media observations.

`job-activity-native-001` read and matched all four actual events, but its Refresh
check failed: the unchanged result could finish within one render frame, leaving
no screen update for the fixture to observe. Its original failure and preservation
report are retained. The UI now shows the last successful check time in UTC;
the corrected fixture requires that time to advance before comparing messages.
This provides user-visible refresh feedback and verifies a completed read without
requiring a transient loading frame. `job-activity-fixture-002` passed its three
local decoder checks. Final runtime and native results follow below.

`job-activity-ui-002` passed the full TUI and CLI race suites (7.200s and
3.396s) after adding successful-read feedback.
