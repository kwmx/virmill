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
