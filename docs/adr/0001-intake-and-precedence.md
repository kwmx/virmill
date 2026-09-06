# ADR 0001 — Specification intake, discrepancies and precedence

Status: accepted engineering interpretation, 2026-09-06. No scope amendment.

Precedence is owner decisions in chapter 00, safety in 10, domain/wire/schema
contracts, workflows, then examples. The owner's current request also fixes
identity, complete scope and host-mutation review.

1. `PACKAGE-CONTENTS.json` lists `START-HERE-AGENT-PROMPT.md`, but that file is
   absent. All other indexed files match their recorded bytes and SHA-256.
   The earlier QA claim of no missing links does not describe this checkout.
   Continue from README, AGENTS and all numbered chapters; do not fabricate the
   absent author's content or rewrite the original audit. Track the missing input.
2. README suggests moving the spec to `spec/`, while chapter 15 fixes the archive
   root as `virmill-v1-spec/`. Preserve that root and use separate implementation
   docs. This also preserves the supplied hashes and traceability.
3. Domain text permits Unicode display names, while `metadata.name` is a restricted
   ASCII identifier. Treat this field as the stable declarative/backend-safe name;
   introduce a separate optional `metadata.displayName` in the implementation
   schema, normalized to NFC and rejecting controls/path separators. Do not loosen
   stable resource ID syntax. Bundle schema additions; retain original schemas.
4. Workflow coverage exceeds the baseline schemas (advanced routing, disk import,
   reservations, IPv6 route intent). The schema README explicitly says it is not
   complete. Add method-specific/advanced schemas when each path is implemented;
   do not interpret absent properties as permission to omit mandatory workflows.
5. Backup policy requests use `filesystem-consistent`/`application-consistent`;
   actual capture classifications use `filesystem-quiesced`/`application-quiesced`.
   Keep requested policy separate from observed guarantees; never upgrade the
   observed consistency label without successful hooks/capture evidence.
6. A lab no-op is valid, while operation-plan requires at least one step. Represent
   no-op as an explicit read-only observe/verify step with zero effects. No fabricated
   host mutation or relaxation of the schema is necessary.
7. Go 1.23.2 in prior QA is historical example evidence, not a dependency pin.
   Inspect actual environment and record verified fetched versions and digests.
   Unavailable native/guest versions remain unknown, never invented.
8. Permission declarations and plugin plan tokens are not authorization. Package,
   plan, actor, invocation and resource bindings are independently checked by core.
   A missing sandbox fails closed. No developer bypass is enabled by default.

Implementation contract changes are mirrored in root schemas and operator docs,
with compatibility tests against the immutable supplied examples. The original
specification remains evidence, not an implementation status report.

