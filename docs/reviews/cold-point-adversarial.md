# Cold recovery point adversarial validation review

Reviewed 2026-09-08. No production defect was reproduced. This change adds only
generated-data tests of the existing `ColdRecoveryPoint` declaration and ordinary
member-integrity checker; it does not implement or qualify capture or restore.

The reviewed boundary is
[`CaptureManifest.Validate`](../../internal/app/protection/capture_manifest_validation.go),
strict schema-backed decoding in
[`capture_manifest.go`](../../internal/app/protection/capture_manifest.go), and
[`CheckManifestContext`](../../internal/app/protection/capture_verify.go).
The distinction between declared integrity and authenticated capture follows
[ADR 0020](../adr/0020-cold-recovery-boundary.md).

Existing tests already cover malformed and ambiguous JSON, nil collections,
duplicate member IDs, incorrect roles, source identity and stopped-state
contradictions, digest syntax, unsafe and colliding paths, member sizes, per-chain
backing bounds, corruption, missing files, aliases, special files and
cancellation. The review used `capture_manifest_validation_test.go`,
`capture_verify_test.go`, `manifest_test.go`, `manifest_open_linux_test.go` and the
schema tests in `internal/validation/cold_capture_test.go` to avoid repeating
those scenarios.

The new cases in
[`cold_point_adversarial_test.go`](../../internal/app/protection/cold_point_adversarial_test.go)
exercise the remaining distinct branches:

| Test | Added evidence |
| --- | --- |
| `TestColdPointExplicitEmptyMappingsCannotHideRequiredMembers` | Explicit empty disk, TPM and secret mapping arrays reach semantic completeness checks, including a secret referenced only by TPM encryption. All four cases return `INCOMPLETE_BACKUP`; they do not stop at the nil-array guard used by older omission cases. |
| `TestColdPointEncryptionReferenceUnionRequiresEveryDisposition` | A TPM-only secret is required without duplication in the general source list. A second, distinct TPM encryption UUID needs its own disposition; supplying it makes the declaration valid. |
| `TestColdPointDistinctIdentitiesCannotShareOrSwapArtifactRoles` | Distinct TPM names and distinct secret UUIDs cannot reuse one member. Swapping NVRAM and auxiliary-inventory references fails despite unchanged member identities and counts. All three cases return `INVALID_INPUT`. |
| `TestColdPointAggregateSourceInventoryBound` | Exactly 1,024 combined disk/backing entries pass strict decoding. Entry 1,025 fails while individual chains and member counts remain within their separate limits. |
| `TestColdPointForgedAuxiliaryClaimsRemainUnverifiedAndIntegrityChecked` | Generated auxiliary bytes contain forged positive capture, recovery, boot and helper-authentication claims plus contradictory guest/state text. Matching declared hashes permit only member-integrity reporting: all three verification claims remain false. Same-size corruption of that auxiliary member returns `INCOMPLETE_BACKUP` and no report. |

Every new declaration refusal is checked through both direct validation and
strict decoding. Decoding must return the zero manifest on failure. The member
test creates private temporary ordinary files only; none is genuine VM, firmware,
TPM or helper output. It skips its Linux-only file-reader portion on other
systems; the declaration cases remain portable.

The auxiliary artifact is currently opaque to this checker. Accepting matching
bytes does not authenticate its contents, source identity or inventory
completeness. The forged-content case confirms that limitation remains explicit;
it is not evidence that provenance was validated. Likewise, accepting a declared
independent disk or complete source list does not observe an actual backing graph.

Validation completed with the pinned repository toolchain, offline dependencies
and vendor mode:

- Focused `go test -mod=vendor ./internal/app/protection -run '^TestColdPoint' -count=1`:
  passed, 0.043 s; five test functions and ten leaf cases.
- `go test -mod=vendor -race ./internal/app/protection ./internal/validation -count=1`:
  passed, protection 3.988 s and validation 1.148 s.
- `go vet -mod=vendor ./internal/app/protection ./internal/validation`: passed.
- `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -mod=vendor -c ./internal/app/protection`:
  compiled a Mach-O arm64 test binary in a temporary directory; not executed.

All commands above used `./scripts/go`, `GOPROXY=off` and `GOSUMDB=off`. No native
backend, guest, privileged operation or remote host was used.

The [acceptance definitions](../../virmill-v1-spec/docs/13-testing-and-acceptance.md)
make this prerequisite evidence for SNAP-01 and BAK-01 member completeness, and
for only the corrupted/missing-data verification portion of BAK-04. It provides
no cold snapshot/revert, encrypted independent restore, repository retention or
pruning evidence. SNAP-02 concerns live graph transitions and receives no direct
coverage here; BAK-02, BAK-03, BAK-05 and BAK-06 are likewise outside this review.
No acceptance scenario is promoted by these tests.
