# Guided automatic startup

CORE-02/CORE-05/UX-01/UX-02 remain partially implemented/qualified. ADR 0049
replaces the ordinary TUI settings-file requirement with fresh current values,
a toggle and a separate preview, using the existing shared durable operation.
Strict boolean input validation now runs before plan creation and again before
execution; persistent VM requirements are explicit. Review contains exact
before/after autostart values, bound to the observed VM fingerprint.

Agent form tests passed seven new scenarios and existing guided-form tests.
Agent shared service tests passed strict invalid-input/no-plan, running/stopped/
paused and same-value compatibility, stale external changes, transient refusal,
lost acknowledgement and failed readback. These use deterministic providers.
Root integrated fresh VM reads, stale/canceled reply refusal, common-menu access,
plan Back retention, pending submission handling and before/after review text.

`autostart-guided-core-001` passed app, CLI, UI registry and operations race tests,
but failed two TUI compact-menu assertions: adding an option made More too long.
The selected-VM More menu now shows automatic startup in place of creating an
unrelated new VM. Creation remains directly available from the VM list and All
tools. `autostart-guided-ui-002` passed the complete TUI race suite after that
correction. Static analysis passed. Original failed evidence is retained.

The prepared actual-terminal fixture compares TUI/CLI plans without applying.
A separate native fixture can toggle only the retained test guest's
autostart and restore it through two reviewed durable jobs. Neither reboots the
host or starts/stops a guest. Unknown outcomes must be inspected without replay.
Native execution and installation evidence will be recorded below when run.

Preflight confirms the retained Fedora fixture is stopped with autostart off. Its
application catalog labels it external because the original guest-tools fixture
created it outside the managed-creation catalog. Mutation authorization is bound
to the original passing fixture report, exact UUID/name, fixture metadata and
recorded independent disk path, not inferred from a VM name or catalog claim.

## Installed beta and live results

Installed source: `d4036d2b834042f37bb3dd7995385ffede4688a6`.
`autostart-guided-packages-001` passed all three package/private-service checks.
`autostart-guided-upgrade-001` and `autostart-guided-restart-001` passed, preserving
all 29 prior jobs, journal tables, observed guests and original media metadata.
The user service remains enabled at sign-in. No helper package/policy changed.

- CLI SHA-256: `80092a0c95c6c08096244addce23d3a7dfa2c6fd8043b02f6845438634967ec0`
- Coordinator: `316adcd009ed60dff3669e4cb04c8630135c84e77360d6c4ed348da5ce0bd13b`
- Core RPM: `6c26e16f834eb053cb6bf45de5cba3b501597ec124f6f0192b9146319244bba0`

`autostart-guided-tui-native-001` passed actual 80×24 navigation, fresh observed
Off value, requested On toggle, explicit Preview, exact CLI/TUI semantic parity,
Back retaining On and cancel returning to the selected guest. No plan was applied
in that run. Guest/native XML/state/autostart, prior jobs and media were unchanged.
Root inspected the actual form and readable Off → On review screens.

`autostart-guided-cycle-native-001` then passed two actual reviewed durable jobs
on the original owned test fixture:

- Enable: `dacdfe2b-a0d3-4207-af79-f0e34c69ab69`.
- Restore Off: `9c5a7c7d-a61c-4b31-901b-91775adf9445`.

Both succeeded with native policy readback. The final guest state is stopped,
autostart false, with original VM XML/inventory, all prior jobs and source media
preserved. There are now 31 job records, including these two successful checks.
Original fixture report digest:
`db552d4ded49400ee58bfddb13bffc0bfac4adcc8a05fe927434015219d8fa59`.

The package/native ledger source digest includes the post-freeze fixture-only
correction that binds original test ownership separately from the application
catalog. Runtime source is frozen d4036d2; installed executable hashes are bound
above and in both reports. No guest boot, host reboot, delete workflow, complete
CORE-02 acceptance or complete 1.0 qualification follows from these scoped checks.
Reports/screens remain under `~/virmill-tests/autostart-d4036d2`; local unsigned
packages and checksums are retained in `build/autostart-delivery/`.

Agents supplied the toggle component, service failure/recovery tests and native
preview fixture. Root owned service validation/review, integration, the native
reversible cycle, all VM actions, packaging and release tracking. All 71 acceptance
statuses remain unchanged; publication has not occurred.
