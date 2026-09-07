# Creation NVRAM declaration binding: validator review

Reviewed 2026-09-08 against the working implementation of
[ADR 0021](../../docs/adr/0021-creation-nvram-declaration-binding.md).
HEAD was `4c5817674e8c036081715237ad1e768869f377c5`; the new NVRAM production
files and wiring were still working-tree changes. This review owns only
[nvram_validation_linux_test.go](../../internal/creating/nvram_validation_linux_test.go)
and this document. It covers validator and stored-proof behavior for IMP-07,
SNAP-01 and JOB-02 prerequisites. Lifecycle, Plan/Apply/reopen/path-drift tests and
UI review belong to the other agents.

No remaining integration blocker was found in this validation scope after the
parent added the proof-local canonical JSON check. The tests perform no native
observation or auxiliary-file read. They use scalar fixtures and a private,
generated SQLite journal with a service that has no backend.

## Concrete issue found and correction reviewed

The initial `loadNVRAMBinding` used `wire.Decode` followed by semantic validation.
`wire.Validate` rejects identical decoded JSON keys, while Go struct decoding
matches field names without regard to case. Therefore a stored object containing
`"schemaVersion":2,"SchemaVersion":1` can decode as version 1; an analogous
`"format":"qcow2","Format":"raw"` can hide a conflicting nested value. A
case-only field name, omitted empty-valued `loaderStateless`, or `null` for that
string can also lose shape information during typed decoding.

The parent corrected this locally in
[loadNVRAMBinding](../../internal/creating/nvram_linux.go):
after strict decoding, compare canonical JSON of the original bytes with
canonical JSON of the complete typed proof. This rejects aliases, missing fields
and null-to-zero-value coercion without changing shared wire semantics. Normal
key-order and whitespace differences remain accepted. The record's fields have
no `omitempty`; every emitted field, including nested empty scalar fields, is
therefore part of this version's required shape.

This issue was identified by source review. The parent applied the correction
before the first executable test run, so this contribution does not claim a
recorded pre-fix runtime failure. The new tests verify the corrected behavior,
including conflicting and same-value aliases at the top, resource, firmware and
NVRAM object levels. Exact and escaped duplicate keys remain refused by the
existing wire validator.

## Checks and limits

| Boundary | Exercised behavior |
|---|---|
| [nvramPath](../../internal/creating/nvram_linux.go) | Interior spaces and Unicode filenames accepted; empty/root/relative/dot/traversal/doubled/trailing separators, surrounding whitespace, controls, NUL, bidi format characters and invalid UTF-8 refused. The 4,096-byte limit is tested at and above the boundary, both directly and through proof validation. |
| [nvramFirmwareMatches](../../internal/creating/nvram_linux.go) | Exact raw/qcow2 and secure/non-secure mapping controls pass. Missing/changed loader, template, format, readonly and security fields fail. The renderer's absent stateless policy is required; explicit `yes` and unreviewed explicit `no` fail. These are declaration semantics, not firmware compatibility tests. |
| [validateNVRAMBinding](../../internal/creating/nvram_linux.go) | Plan/input/operation/resource and receipt identity mismatches fail; creation binding is independently recomputed, so jointly altering receipt/proof bindings does not pass. Firmware digest and observation fingerprint syntax and matching are checked. Version 0 cannot validate a version-1 proof; unknown/negative recipe and unknown proof versions fail. |
| [creationRecipeVersion](../../internal/creating/devices_linux.go) | Legacy version 0 remains a valid historical BIOS/UEFI recipe. Version 1 is restricted to UEFI; unsupported/negative versions fail. This test does not execute or upgrade a legacy recipe. |
| Stored proof loading | Missing metadata is absent, not a zero proof. Valid and reordered/indented complete records load identically. Malformed/truncated/null/wrong-type/trailing JSON, unknown fields at every nesting level, duplicate keys, aliases, every omitted field, bad versions and malformed paths return an error with a nil proof. Rejected bytes remain unchanged in the journal. Canceled loading returns a nil proof and cancellation. |

The validator is an internal boundary, not an arbitrary-plan authorization API.
Its callers must retain the existing plan/recipe checks and receipt-version
decoding. The inspected service result path invokes `creationRecipeVersion`
before `loadOptional` and `nvramResult`; definition/reconciliation call the
binding step before marking completion. No independent claim about complete
lifecycle correctness is made here.

The historical observation fingerprint is intentionally not a claim of perpetual
configuration equality. The proof records a declaration, not an inode,
initialization receipt, file owner, no-symlink result or read grant. The creation
firmware digest links the reviewed inputs; these validators do not open and
rehash current firmware files. Initial materialization, native root policy,
freshness and stopped-file capture remain separate ADR 0020 work. A passing
Secure Boot mapping test does not qualify guest key state or boot support.

## Executed checks

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race ./internal/creating -run '^TestNVRAMValidation' -count=1
```

All five focused tests passed under the race detector. An intermediate expanded
test run failed because the test attempted to remove `resource.uuid` instead of
the actual `resourceUUID` key. The fixture was corrected and now asserts that
every selected field exists before deleting it; this was not a production bug.
No runtime or shared-contract files were edited by this contribution. No SSH,
host services, privileged operations, firmware execution or native file access
was used. These results do not promote any release acceptance scenario.

Production review snapshot SHA-256 values, for locating the inspected state while
parallel integration continues:

```text
dc5b6889df75b843b3c15ecdc722cb3d164a46fec85a859d123c334af11bcc74  internal/creating/nvram_linux.go
ec309f62b971f2b944217c5b96be060e57d2d0c520c4ee73635dd4b990f62ebd  internal/creating/devices_linux.go
be083b32a0f1abb20f461f50daf325dd4d9872cf04e7e3bb24c5759b5c7a55d0  internal/creating/service_linux.go
ea6b78df4f941087e8dbc3595e1f341d833f20af51f658f1662efaa30628aeba  docs/adr/0021-creation-nvram-declaration-binding.md
```
