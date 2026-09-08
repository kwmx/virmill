# ADR 0023: Assert formats in bundled schemas

Status: accepted routine contract correction; execution evidence is recorded
separately. All 71 mandatory scenarios and existing authorization remain required.

The supplied Python specification validator explicitly enables `FormatChecker`.
Virmill's bundled schemas use `date-time` for operation-plan creation/expiry and
prepared-import creation timestamps. The vendored Go compiler treats `format`
as an annotation by default for Draft 2020-12. Merely compiling those schemas
therefore does not enforce the intended timestamp constraint.

Enable the compiler's existing `AssertFormat` option when compiling the bundled
schemas. Keep the supplied package, schema IDs, declarative API and dependency
versions unchanged. Resolve every schema locally as before. This follows the
normative validator's behavior and tightens enforcement of an existing contract;
it introduces no new command, schema version or timestamp representation.

This change concerns bundled schema validation. Plugin-provided dynamic schemas
retain their documented compiler behavior; no new plugin-schema vocabulary or
permission is inferred here. Machine output still uses UTC RFC 3339 timestamps.
Schema-valid timestamps do not prove freshness, a valid grant, authorization or
recoverability; those checks remain independently required by the shared service.

The parity review and pre/post regression records distinguish schema acceptance
from complete workflow, guest, hardware and release evidence. No persisted data
or journal schema is rewritten by this validation correction.
