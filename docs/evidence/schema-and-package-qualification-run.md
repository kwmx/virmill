# Schema and installed-document qualification

All 71 acceptance scenarios remain mandatory and unaccepted. The parent owns
shared contracts, production corrections, package integration and this ledger.
These local checks do not change the installed disposable runtime `490b88c` or
qualify additional host/guest configurations.

Beauvoir added nine missing normative negative cases and two omitted positive
examples in the pure Go validation tests. Existing tests cover the other two
normative negatives. Ten additional malformed timestamp cases reproduced a
bundled schema enforcement gap: the Draft 2020-12 compiler did not assert formats.
The parent enabled the existing compiler option under
[ADR 0023](../adr/0023-bundled-schema-format-assertions.md), preserving schema IDs,
dependencies and dynamic plugin-schema behavior.

| Evidence ID | Actual result and limits |
| --- | --- |
| `bundled-schema-formats-before-001` | Failed all ten malformed creation/expiry timestamp cases; four valid timestamp forms passed. A schema-boundary defect, not evidence of an executable invalid plan or authorization bypass. |
| `bundled-schema-formats-after-001` | Entire Go validation package passed after correction, including all new normative and format regressions. Pure schema/semantic checks, without native networking, plugin execution or backup scheduling. |
| `schema-format-integrated-core-001` | Full offline vendored Go race suite passed with required private IPC, confined plugin conformance and generated-disk tools: 2,462 passing records, zero failures, 29 packages. Two opt-in root/host smoke tests skipped and remain unqualified. Shared CLI/TUI tests executed; this was not a new native deployment. |
| `schema-format-integrated-vet-001` | Whole-repository offline static analysis passed. |
| `specification-python-validator-001` | Failed the unmodified supplied Python validator on the known missing kickoff-document link from ADR 0001. An isolated hash-pinned eight-package environment included an actual date-time checker. All 49 supplied files remained unchanged. |

Russell prepared that isolated Python environment and recorded exact interpreter,
wheel, format-checker and source-inventory hashes. The parent verified and retained
the [environment](environments/specification-python-20260908.json),
[source inventory](environments/specification-source-20260908.json),
[build-only lock](../../tests/qa/spec-python-requirements.lock) and actual failed
execution. See the [Python review](../reviews/specification-python-validator.md)
and [Go parity review](../reviews/specification-validator-parity.md) for distinct
shape/semantic boundaries and remaining BackupPolicy validation gaps.

Nash independently audited all 179 staged payload hashes/modes of frozen 490b88c.
Its 102 Markdown files contained 1,269 broken installed link occurrences across
31 documents, mainly references to intentionally excluded evidence logs. Those
references generally resolve in the source checkout. The parent accepted
[ADR 0024](../adr/0024-installed-documentation-links.md): deterministic package-only
link relocation, explicit source-checkout references for omitted inputs, and an
installed copy of the essential language-neutral plugin protocol. Implementation
and package execution evidence are recorded separately when completed.

`installed-document-transform-tests-001` passed all 26 pure transformation tests.
The complete frozen document corpus exposed an initial false indented-code
refusal after leading inline code; the agent fixed it and added the exact
regression before release. The parent reviewed the full transformer and executed
its final tests. No package/native behavior is inferred from these pure tests.

`installed-document-package-inputs-001` passed all 23 generated-repository tests,
including transformed links, separately installed protocol, source-byte
preservation, omitted source/log payloads, missing input and target collision.
Beauvoir independently reviewed the parent wiring: all original installed paths
and modes remain exact; only the public protocol is newly selected. Native RPM
assembly, deterministic rebuild and final installed-document closure still need
the frozen-checkpoint evidence below. Synthetic RPM command seams in these tests
are not native RPM validation.

The parent independently executed the review's bounded frozen audit in
`installed-documents-before-001`, with a zero-missing-target assertion. It
**failed** with exactly 1,269 missing occurrences, 397 target paths and 31
documents, while verifying all 179 staged payload hashes/modes and passing nine
checker self-tests. This preserves the actual old-package failure instead of
reporting successful audit execution as successful document closure. See the
[reproducible frozen review](../reviews/installed-document-links-490b88c.md).

The independent staged-document observer had a false-pass defect found in review:
it looked for a source-tree log prefix in installed paths. A generated complete
manifest with an excluded log reproduced that failure in
`installed-document-observer-before-001`; the corrected installed-prefix check
passed all five observer self-tests in `installed-document-observer-after-001`.
The actual newly assembled-package observer has not run yet.

`source-freeze-approval-blocked-001` records an automatic approval-review rejection
before the final Git staging process could start. The reviewer reported an account
usage limit; the owner had already authorized the work. An earlier 91-file staging
operation succeeded, but subsequent changes remain unstaged/untracked. No new
checkpoint or corrected native package build is claimed. No alternate Git write
or approval bypass was attempted. Local fixes and regression tests continued.
The source is prepared for a reviewed freeze, two native package builds and
independent stage/link/private-CLI checks when approval review is available.
