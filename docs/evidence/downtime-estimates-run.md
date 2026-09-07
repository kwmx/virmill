# Downtime estimate correction evidence

Runtime **15a1f2c28b2d5262e16fca43618624f0bfdeeb7d** corrects U010 using
[ADR 0015](../adr/0015-operation-downtime-estimates.md). Its source digest is
`23443527cb8f4ff80c9a2d92c434a425714461f1ad2994c2168698f28a5ef637`.

The full race-enabled Go suite, static analysis, portable builds and temporary
package/IPC tests passed. Two offline build/package runs produced identical seven
artifact hashes in `logs/downtime-build-001.log`. The development core RPM hash is
`a28f4d09dbc3b5d506fb828ccc23934a1801167689d3372f270892d0b64a9621`.
These tests do not qualify physical hardware.

The owner-authorized disposable Fedora host was upgraded while all guests were
stopped and no new operation was active. Installed hashes and RPM integrity were
verified. The entire existing operation list and all three guest definitions were
unchanged. No helper upgrade or persistent service enablement was performed.

An actual CLI `vm set --plan` and a separate actual TUI JSON form preview both
reported `requiresDowntime: true`, with notes explaining the already-stopped
prerequisite and unestimated runtime writes/duration. Both plans were left as
previews; no operation was accepted and no guest definition changed.

The previously applied boot-order plan retained its original false estimate and
exact digest `6b727dcf11b6156296c267ce1a25d177557a357b02691bf237c92617ede64660`.
This is intentional immutable history. New previews contain the correction; old
reviews are not rewritten after an upgrade. The earlier boot/media native evidence
continues to name its actual runtime, 8a092b4.

The final tested guest XML remained
`e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2`.
Exact upgrade and PTY procedures are retained in
`tests/fixtures/configuration/disposable-recorded-run/`. This preview check did not
start, stop or redefine a guest. Full 1.0 qualification remains incomplete.

Repository hygiene was checked with `git ls-files -ci --exclude-standard`: no
ignored files were tracked. Pattern checks cover private test-host configuration,
binaries, packages and source media. The ignore rules now also exclude `*.7z`
outside the dedicated images directory, protecting against accidental staging of
supplied guest archives. Audit logs, text XML fixtures and reproducible recipes
remain intentional tracked deliverables. No Git push or publication was performed.
