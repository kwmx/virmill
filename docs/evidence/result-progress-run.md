# Creation and cleanup result progress evidence

[ADR 0016](../adr/0016-active-creation-result-observations.md) corrects premature
recovery guidance in result observations. Queued, validating, running and verifying
creation/resume/cleanup jobs report explicitly incomplete progress, including
whether their durable records exist. They do not become approval forms or trigger
recovery effects.

The tests cover every active state with missing or partial records, a synthetic
blocked upload that later succeeds, wrong actors, coordinator recovery, unknown
receipt/proof versions and fields, and inconsistent terminal success. Result reads
preserve locks and do not allocate, define or delete. Cleanup success needs matching
proof/disposition records; delete completion additionally checks every recorded
absence. Guest boot and readiness remain separate from all of these stages.

These active-job and cleanup scenarios use deterministic coordinator fixtures.
They do not qualify native upload timing, crash during a real effect, native cleanup
or hardware behavior. The `creation-progress-*` entries record the first
creation/resume correction at 6f0322d. The `result-progress-*` entries record the
combined creation and cleanup correction at 11a8174. Exact source digests, command
outputs and fixture classes are retained in the append-only ledger.

The installed journal readback procedure checks only already-recorded successful
and uncertain operations. It must preserve the earlier uncertain operation and
its locks, return explicit completion only for the successful creation receipt,
keep guest-readiness flags false and leave every guest stopped. No new native
creation, cleanup, start or stop is part of that readback check.

The final runtime was **11a8174f2be8128a29cc8c7d03144d4258a578c1**, source digest
`e50e118d7799af57df946ef4737203a5acd0b08da363daec64e14b334b110824`.
Full race tests, static analysis and package/private-IPC checks passed. Two builds
produced identical seven artifact hashes. The installed core RPM hash was
`19accf2df0a490387fbcf4a86f5fac0fff7f1d156dd1b8b33e80f04780988b92`.
The helper binary/package remained unchanged.

Installed readback passed. Successful creation
`28701ac3-325d-4596-8103-38e19f74b7cb` returned `complete: true`, verified volumes
and definition, and false guest-readiness flags. Earlier uncertain creation
`b5983e68-bfcc-42f0-bf4e-3877029b756b` returned `complete: false` with
RECOVERY_REQUIRED/exit 6. A real TUI read showed the successful result. Job records
and the three legacy locks were unchanged; all three guests remained stopped.
The final guest XML hash remained
`e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2`.

The final full release-policy evaluation failed as expected: every mandatory
acceptance case and the complete checklist still have outstanding work. This is
not a completed or reduced 1.0 release. The next work remains the full documented
configuration, recovery, console, networking, protection, devices and fixture
matrix; hardware claims stay blocked wherever actual evidence is unavailable.
