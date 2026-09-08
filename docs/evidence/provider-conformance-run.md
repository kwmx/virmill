# Reference provider conformance

EXT-02 is accepted at its required **simulated-contract** level. The reference
provider performs lifecycle operations on generated JSON resources, retains
stable namespaced identities and paginated inventory, rejects unsupported
capabilities, and reconciles durable partial effects after process restart.
This does not qualify a real remote provider or any local virtualization,
backup, networking or device workflow.

The provider fixture is source revision `85243a1`; its shared service harness,
CLI/TUI dispatch tests and operator recipe are frozen at `090b342`. The latter
implementation digest is
`996721fca6d26ca6852f91280c3b72d9d3caef566583f2cbe3b3dc5dbd79a909`.
The fixture identity is `example.virmill.provider-fixture` version `0.2.0`.
It uses protocol 1.0, the repository-local Go SDK, no permissions and no network.
Go 1.27.1 and the pinned dependencies were used offline.

Executed evidence:

- `provider-lifecycle-contract-001`: standalone race and real SDK subprocess
  tests passed, including child termination/restart, lifecycle, pagination,
  malformed input, stale requests, corrupt state and lost acknowledgement.
- `provider-lifecycle-confined-001`: the existing confined core restart test
  passed against the new fixture.
- `provider-shared-race-002`: all 322 plugin, CLI and TUI test records passed,
  with no failures or skips. This includes the new confined reference profile
  and existing Go/Python action conformance. The profile verifies nine reported
  checks and observes the retained partial receipt after reopening its process.
- `provider-shared-ui-003`: actual private coordinator, JSON CLI and an 80×24
  PTY passed. The TUI selected Plugins → plugin test, supplied the fixture path,
  paged the long result and displayed its exact provider identity and confinement
  flag. Both interfaces exercised real confined processes on generated state.
- `provider-shared-repro-001`: two same-environment offline builds produced
  identical three binaries and four unsigned RPM/DEB packages.
- `provider-shared-artifacts-001`: package metadata, staged installer/uninstaller
  preservation and private coordinator/CLI/TUI checks passed.
- `provider-shared-documents-001`: eight checks passed over 220 staged payload
  files and 123 Markdown documents; 185 local links resolved with no missing
  targets. This checks installed bytes and links, not every tutorial's behavior.

Earlier failed checks remain in the append-only ledger. `provider-shared-race-001`
passed 321 checks but lacked the generated Go summary fixture in the fresh frozen
checkout. The fixture was built from the current SDK before the successful
repeat. `scripts/build-conformance-fixtures.sh` now supplies reproducible setup.
`provider-shared-ui-001` passed CLI conformance but its PTY driver combined the
search control key and query, selecting the wrong form. The second driver reached
the correct result but waited for an identity field below the viewport instead
of paging it. The third driver separated search events and paged the result;
the compiled product remained the same throughout. Corrected driver hashes are
recorded separately from the frozen executable source.

The service removes its private generated workspace after returning the report.
Reported partial receipt details prove observations within this conformance run;
they are not a retained backup. The standalone process tests separately inspect
durable generated state. Concurrent independent provider processes sharing one
workspace are outside this fixture's contract. Cancellation is explicitly
unsupported by the simulated lifecycle, and the host enforces bounded calls.

The [fixture guide](../../tests/fixtures/plugins/provider/README.md) documents
method payloads, failure behavior, build commands and both interfaces.
[ADR 0027](../adr/0027-reference-provider-conformance.md) records why the developer
command selects this exact reference profile without extending core into remote
host management. The remaining acceptance scenarios and the full checklist
are assessed independently.
