# ADR 0027: Reference provider conformance through the shared service

Status: accepted implementation decision; release qualification is separate.

The normative plugin protocol requires a simulated provider with stable fake
identities, partial effects, restart/reconciliation and explicit unsupported
capabilities. Real remote management remains optional. The existing developer
conformance command assumed every workspace implemented the summary action,
which prevented the required provider fixture from using the same CLI/TUI path.

`plugin test` now selects the reference provider profile for a provider manifest.
This profile accepts only the versioned reference identity, a sole provider
extension, no declared permissions and no network access. Arbitrary providers
are not given guessed credentials or mutation requests. The profile uses the
existing confined process runner for both initial execution and restart, and
validates declared method input/output schemas offline before checking actual
resource identities and lifecycle behavior. The language-neutral protocol and
SDK contracts do not change.

The fixture mutates only bounded JSON state inside one private generated workspace.
It supports create/start/stop/delete, stable paginated inventory and the required
status/reconciliation families. Configuration, networks, devices, snapshots,
backups, guest transports and cancellation remain explicitly unsupported by this
simulated provider. These declarations do not exclude any mandatory local core
workflow. Operation IDs and idempotency keys cannot overwrite different effects;
pagination cursors bind to the persisted inventory generation. Persistence uses
an fsynced file replacement and directory sync. Multiple independent provider
processes sharing one workspace are not supported by the fixture.

The conformance run records a partial effect, closes the process, starts a new
one against the same workspace, and verifies status and explicit reconciliation
without replaying apply. The receipt remains partial with a recorded reconciliation
observation. A schema-valid success response alone is insufficient; semantic
identity, state, generation, idempotency and unsupported-operation assertions
must also pass.

The shared developer service discards the generated workspace after the run.
The structured result identifies the checks and observed partial receipt; it
does not claim an independently retained backup or a real VM. CLI and TUI select
the same workspace path and service method. Source/build recipes and process
tests ship with the fixture, while an actual executable hash is written into
the generated development manifest. No publication destination is introduced.
