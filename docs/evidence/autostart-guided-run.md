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
A separate native fixture can toggle only the retained managed stopped guest's
autostart and restore it through two reviewed durable jobs. Neither reboots the
host or starts/stops a guest. Unknown outcomes must be inspected without replay.
Native execution and installation evidence will be recorded below when run.
