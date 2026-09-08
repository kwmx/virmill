# Specification validator parity review

Reviewed on 2026-09-08 against the preserved
[package validator](../../virmill-v1-spec/qa/validate_package.py), the embedded
Go schemas, and the existing validation, network-intent and plugin tests. This
is local contract evidence for REL-03 and prerequisites for EXT-01/SEC-05.
It does not exercise a plugin handshake, grant/expiry enforcement, execution,
real networking, command-help parity or recovery. All 71 scenarios remain
required; no acceptance status is promoted by this review.

## Existing coverage and boundaries

The Python validator checks all eight normative schemas, five YAML examples,
one plugin manifest, eleven negative inputs, Markdown destinations, reference
labels and unique requirement IDs. It enables `FormatChecker` in addition to
its VM/network/lab/BackupPolicy semantic checks.

Go's [schema initialization](../../internal/validation/validate.go) loads and
compiles every embedded schema through an offline loader. Existing
`TestBundledExamplesOffline` therefore exercises compilation as well as the
four YAML examples matched by `examples/*/*.yaml`. Its glob misses the root
BackupPolicy YAML and the plugin JSON manifest. The new positive test reads
those two from the preserved specification, rather than adding more tests of
the four already visited copies.

`TestYAMLAndTerminalSafety` already covers duplicate YAML fields, anchors,
unsafe tags, multiple documents and terminal controls.
`TestPluginSchemasAreOfflineAndValidateResults` covers typed dynamic results,
unknown/duplicate JSON fields and refusal of file/network schema resolution.
`TestColdRecoveryPointSchemaIsOfflineStrictAndExplicit` adds capture-contract
coverage beyond the normative package. These checks are retained unchanged.

`Document` validates shape and safe names. The shared
[`document.validate`/`lab.validate` service](../../internal/app/service.go)
then invokes the pure [lab validator](../../internal/app/lab/validate.go), or
the corresponding VM/network validator. A CIDR, unresolved network, duplicate
machine or cycle can pass shape validation and must fail the semantic step.
The new tests exercise this existing composition without a provider or store;
they do not introduce a parallel semantic implementation.

## Eleven negative inputs

The new file is
[`specification_parity_test.go`](../../internal/validation/specification_parity_test.go).
Nine original negative inputs had no focused existing regression at the
relevant Go boundary. The other two already have direct network-intent tests
and are not duplicated.

| Normative negative | Existing or new Go regression |
| --- | --- |
| Unknown lab field | New `TestSpecificationLabSchemaRejectsUnknownField` |
| Multiple normal default routes | Existing `TestRouteOverlapAndDefaultRoute` in `internal/app/network` |
| Unresolved network | New `TestSpecificationLabSemanticNegatives/unresolved_network` |
| Dependency cycle | New `TestSpecificationLabSemanticNegatives/dependency_cycle` |
| Duplicate machine identity | New `TestSpecificationLabSemanticNegatives/duplicate_machine_identity` |
| Host DHCP on guest-only network | Existing `TestLabDHCPAndGuestOnly` in `internal/app/network` |
| Invalid CIDR | New `TestSpecificationLabSemanticNegatives/invalid_cidr` |
| Plugin entrypoint traversal | New `TestSpecificationPluginManifestNegatives/entrypoint_traversal` |
| Unimplemented protocol version | New `TestSpecificationPluginManifestNegatives/unimplemented_protocol_version` |
| RPC both result and error | New `TestSpecificationRPCEnvelopeAlternatives/both_result_and_error` |
| Numeric RPC identifier | New `TestSpecificationRPCEnvelopeAlternatives/numeric_identifier` |

`TestSpecificationPreviouslyUnvisitedExamples` closes the two positive-example
omissions. The RPC regression also accepts request, notification, null-result
and null-error-ID shapes, so refusal cannot be implemented by rejecting every
envelope or treating all null values as missing.

## Reproduced difference and remaining limits

Before correction, `Schema("operation-plan", ...)` accepted invalid
`createdAt` and `expiresAt` date-time values even though the normative validator
asserts their format. The vendored compiler documents that Draft 2020-12
format assertions are disabled by default; `initSchemas` did not call
`AssertFormat`. `TestSpecificationPlanDateTimeFormats` reproduced this with
ten failing subtests: nonsense, February 30, hour 25, missing timezone and
offset `+24:00`, independently in both fields. Four valid UTC, fractional,
positive-offset and negative-offset inputs passed. This is a schema-boundary
defect; it does not establish an executable invalid plan or authorization
bypass because the typed plan and operation checks are separate.

The parent recorded `bundled-schema-formats-before-001`, then enabled
`AssertFormat` for bundled schemas only under
[ADR 0023](../adr/0023-bundled-schema-format-assertions.md). Dynamic plugin
schema behavior remains unchanged. The parent recorded the passing package
run as `bundled-schema-formats-after-001`. The focused pre-fix command was:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor ./internal/validation -run '^TestSpecificationPlanDateTimeFormats$' -count=1 -v
```

The review author independently ran the final package tests after that change:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor ./internal/validation -count=1
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race ./internal/validation -count=1
```

Both passed (0.031s and 1.225s respectively). All six new top-level tests pass,
including the nine newly covered normative negatives and ten format negatives.
These are pure local tests, with no provider, daemon, IPC or guest calls.

BackupPolicy timezone existence and five-field cron semantics are still not
implemented by the shared service. It explicitly returns `NOT_IMPLEMENTED`
with `semanticValidation: "not-implemented"`, so the positive schema test is
not a schedule-validation success claim. The Python validator performs those
checks. The Go network validator also deliberately checks more than Python's
minimal helper (usable canonical subnets, selected-network addressing and
gateway rules); byte-for-byte acceptance equivalence is not asserted.

Link, reference-label and requirement-ID checks remain the responsibility of
the package/documentation validators. This test contribution does not
reimplement them or run a command-help/native acceptance suite. No normative
schema, dependency, fixture, ledger or release file was changed by the test
author. The parent owns the separately recorded production correction.
