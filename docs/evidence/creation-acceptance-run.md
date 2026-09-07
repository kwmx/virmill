# Retained creation acceptance — 2026-09-07

Development revision **12d7bba1e5e958324bf6199041359e344c153213**, source digest
`1af76a2288eb7e735633c422dbd6a47b08fb6059e9fb2f52c605b955d5ba6f09`,
implemented the separate recovery operation in [ADR 0017](../adr/0017-reviewed-creation-acceptance.md).
The [operator guide](../creation-acceptance.md) documents input, review, failure,
cancellation and result semantics. All 71 full acceptance cases remain open.

Full race tests, static analysis, portable common-package builds and staged
package/private-IPC checks passed from a clean source archive. Two builds produced
identical three binaries and four packages. The installed core RPM SHA-256 was
`4510142395eda74615931e67b5b99f2d9a144f9fe5335ea299c1c8a5df9a63ae`.
The helper package remained unchanged. The full release check failed on the
outstanding mandatory cases and ship checklist, as recorded without alteration.

Coordinator tests cover incomplete/wrong-actor/stale inputs, explicit device
acknowledgements, cancellation, inherited locks, complete retained-byte checks,
corrupt/unknown proofs, active results, ownership substitution, reopening the
journal after proof commit and a failed terminal journal write after ownership
commit. Native in-memory tests exercise strict captured XML, state and reference
predicates. Generated local-file tests exercise whole-set read guards and changed
files. These tests do not certify native crashes, power loss or hardware behavior.

The owner-authorized disposable Fedora host was upgraded without changing its
three stopped guest definitions or existing jobs. Its retained uncertain disk was
root-owned mode 0600. The installed CLI preview returned PERMISSION_DENIED while
preserving every plan, job, lock and guest XML byte.

For the positive native fixture, an external `setfacl` step granted read-only
access to that **single retained volume**. This is an explicit test prerequisite;
Virmill's bounded helper does not yet manage this access. SELinux remained
Enforcing. Exact `acl-2.4.0-1.fc44.x86_64` and `libacl-2.4.0-1.fc44.x86_64`
packages and installation times are recorded in the environment supplement.

A real PTY TUI requested the plan, displayed original hardware and explicit Q35
`qemu-xhci` USB, virtio balloon and reset-watchdog choices, and accepted the exact
digest with its required acknowledgements. The TUI detached while the coordinator
continued through validating, running and verifying to success:

- Plan: `6ab45dcf-bcad-4859-b4d8-727b110e33c6`.
- Digest: `c36cba960f8259c643e04bbd6046055a362df69259669071c93e3dc74fd9757a`.
- Acceptance operation: `6d4792c7-4e09-40ab-95ef-9b4cc05be8af`.
- Original operation: `b5983e68-bfcc-42f0-bf4e-3877029b756b`.
- Original VM: `ae630461-91d3-4f07-ad88-e6842c3dc3ea`.

Full guarded readback matched the original retained container SHA-256
`8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70`.
The VM XML stayed exactly
`e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824`.
Managed ownership was observed through Virmill. The original plan and receipt
were byte-for-byte unchanged; its job became **partial**, linked to the acceptance
child. It still returns an incomplete creation result with exit 6. The acceptance
child returns `complete: true` with its separate proof and false guest-readiness
flags. All inherited locks were released only after confirmation.

The original ACL, owner/group and mode 0600 were restored exactly. The recorded
restore command emitted a warning about following symlinks; the warning is kept
in the evidence log. Future fixture ACL restoration must use physical traversal
(`setfacl -P`) and validate the recorded path/generation. The historical script is
retained exactly as executed; it is not a general-purpose permission installer.
CLI and TUI result readback passed after ACL restoration, with no journal changes.

All three guests remained stopped with unchanged XML. No guest was started,
redefined, detached or deleted; no disk content was written. Original source media
was outside all mutation targets. The prior separate guest's XML remained
`e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2`,
and the unrelated guest's XML remained
`bbe62f376a3943d795cdac4fd67ff56087efa43024b27cacf0578217a1a44408`.

The current coordinator is transient user unit `virmill-test-12d7bba.service`,
with a two-hour runtime bound and the private run environment from the first
disposable-host run. No persistent Virmill service or release publication was
enabled. Next required work includes bounded managed-storage access, native
active-effect crash recovery, other creation profiles, general adoption and the
remaining configuration/networking/protection/device/console matrix. This scoped
acceptance does not qualify firmware/TPM recovery or complete 1.0 support.
