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

`job-activity-packages-002` passed all three final package/private-IPC tests.
`job-activity-upgrade-native-002` and `job-activity-restart-native-002` installed
and activated the corrected beta, preserving all 32 jobs, journal tables, guests
and source-media observations. `job-activity-native-002` passed actual 80×24 direct
Activity access, all four full event messages matched to CLI, a completed refresh
with advancing Checked time and no duplicate events, and Back to the same job.
VM, job, network and supplied-media observations were unchanged. No apply was
attempted. Root inspected the refreshed Activity and return-to-job captures.

Installed source: `dc45ee39165ea4dd5699e4f771dd1b1d85a02f99`.
CLI SHA-256: `ed22c433a6e788b3b5ce86824d78ac7e7093664111e1332666af9404bdab2394`.
Coordinator SHA-256: `5504d95235c7f7c184898be65e43faaafe8750f6482a2529e36f2deafa0883cf`.
Core RPM SHA-256: `1cd30924398ce2ad0a94b701481f6f4fa1db1bcb7ba63a0395846e696e840346`.
Unsigned RPM/DEB artifacts and manifests are in `build/job-activity-final-delivery/`.
Native stage: `~/virmill-tests/job-activity-dc45ee3`; report and captures are in its
`job-activity-tui/` directory, also copied into the local delivery directory.
This is recorded-event observation, not new crash-recovery qualification.

The owner requested committing and pushing the beta to the configured main
branch. Pre-push checks found no ignored-but-tracked files, outgoing media,
packages, credential files or local test configuration. All 4,006 outgoing blobs
were checked for private-key/token markers; the only matches were synthetic
rejection-test strings in two provisioning tests, inspected as such. No outgoing
blob exceeded 10 MiB. This bounded check is not a full security audit. The Git
remote is `git@github.com:kwmx/virmill.git`; no example destination is used.

Automatic approval review rejected the combined final commit/push command before
execution: it requires explicit owner confirmation that `kwmx/virmill` is the
intended destination for source and evidence-log egress. The final evidence is
committed locally in a separate command. No GitHub push or release was performed
at this point; destination approval remains pending. This does not affect the
verified installed beta or its recorded tests.
