# Repository hygiene and package inputs — 2026-09-08

This review found no tracked ignored files, tracked symlinks, or VM/media/key/database/build artifacts by the path and blob-size checks described below. It found concrete package input and stale-output selection defects, now corrected in `scripts/package.py` and `scripts/reproducibility.py`. The correction has local generated-fixture tests; native RPM/DEB builds from the next frozen checkpoint remain a separate check.

This is a REL-01 packaging prerequisite, a REL-02 build-input/reproducibility prerequisite, and a REL-03 documentation-input prerequisite. It does not complete any of those acceptance scenarios. It provides no VM, firmware, TPM, recovery, installation, cross-platform or release qualification.

## Observed source snapshot

The index remained at `4c5817674e8c036081715237ad1e768869f377c5`. The working tree was mutable during review. The final inventory below was observed at **2026-09-07 21:45:01 UTC / 2026-09-08 00:45:01 Asia/Riyadh**, before adding this review. There were 1,938 tracked entries: 1,045 under `vendor/` and 893 elsewhere. The initial inventory had 16 prospective files; the later inventory had 30 because independent implementation/evidence work continued.

Checks using `git ls-files -s -z`, `git ls-files --others --exclude-standard -z`, `git ls-files -ci --exclude-standard`, filesystem metadata, and `git cat-file --batch-check` found:

- Zero tracked entries matched ignore rules; zero tracked symlink modes. All 30 prospective paths were regular files at the final metadata check.
- No tracked/prospective path matched the reviewed VM disk, firmware-state, TPM-state, credential, database, package, archive or build-object suffixes/directories. Checks included `.iso`, `.ova`, `.qcow2`, `.vmdk`, `.raw`, `.img`, `.nvram`, `.fd`, `.permall`, `.volatilestate`, `.key`, `.pem`, `.db`, `.sqlite`, `.efi`, `.o`, `.a`, `.so`, `.zip`, `.tar`, `.gz` and ignored private/output directory names.
- Index object metadata showed no media-sized non-vendor blob. The largest were the evidence ledger (274,014 bytes), requirements JSON (197,300), generated evidence matrix (127,931), and implementation status (110,324). Blob contents were not read to obtain sizes.
- A metadata-only walk of the four original package source trees found no symlink entries. Before this correction their suffix predicate selected 95 docs, 24 schema, 5 SDK and 21 example paths; four then-untracked documentation/JSON files qualified for automatic inclusion.
- The root `dist/` names were the four expected development packages plus `checksums.json`. No unexpected root artifact name was observed. Generated outputs under the separate frozen `build/cold-4c58176` checkout were excluded from this audit.

The final prospective-file inventory inspected by path/type was:

```text
docs/adr/0021-creation-nvram-declaration-binding.md
docs/creation-nvram-binding.md
docs/evidence/environments/disposable-cold-4c58176.json
docs/evidence/logs/cold-native-cross-001.log
docs/evidence/logs/cold-native-packages-repro-001.log
docs/evidence/logs/cold-native-packaging-001.log
docs/evidence/logs/cold-native-upgrade-001.log
docs/evidence/logs/nvram-binding-core-001.log
docs/evidence/logs/nvram-binding-vet-001.log
docs/evidence/logs/nvram-reconcile-negative-step-001.log
docs/evidence/logs/nvram-result-child-integrity-001.log
docs/evidence/logs/nvram-result-child-integrity-002.log
docs/evidence/logs/nvram-result-child-integrity-003.log
docs/evidence/nvram-binding-run.md
docs/reviews/creation-nvram-binding-implementation-review.md
docs/reviews/reconcile-integrity-compatibility.md
internal/creating/nvram_lifecycle_linux_test.go
internal/creating/nvram_linux.go
internal/creating/nvram_result_recovery_linux_test.go
internal/creating/nvram_validation_linux_test.go
internal/operations/reconcile_compatibility_test.go
internal/operations/reconcile_integrity_test.go
internal/ui/cli/creation_nvram_test.go
internal/ui/tui/creation_nvram_test.go
tests/fixtures/protection/disposable-recorded-run/cold-native-upgrade-001.py
tests/fixtures/protection/disposable-recorded-run/nvram-binding-native-001.md
tests/fixtures/protection/disposable-recorded-run/nvram-binding-native-001.py
tests/fixtures/protection/disposable-recorded-run/nvram-binding-tui-001.py
tests/fixtures/protection/disposable-recorded-run/nvram-binding-upgrade-001.py
tests/integration/package_inputs_test.py
```

This is a path/type and selected policy/source audit, not a claim that every repository byte has been examined for secrets. No actual key, TPM, NVRAM, guest disk or supplied-media payload was opened. An innocuously named text file could still contain sensitive data; index inclusion remains a review decision.

## Intentional material and ignore policy

`vendor/` is intentionally retained pinned offline dependency source. The supplied `virmill-v1-spec/` tree stays at its archive root for audit. Neither is an accidental downloaded-media cache.

The seven `tests/fixtures/protection/capture-manifest/*.fixture` files are explicitly documented original text/checksum fixtures, including an empty TPM-named fixture. They are not recoverable disks, NVRAM or TPM state. The four `tests/fixtures/network-prefixes/*.hex` files are documented synthetic rtnetlink records with a generator and checksums. The UEFI/TPM probe directory contains original source/recipes and public marker definitions; compiled firmware/guest artifacts belong under ignored build output. Those fixture README classifications were read without interpreting similarly named files as real secrets or firmware state.

`.gitignore` intentionally permits `docs/evidence/logs/*.log` while ignoring transient logs elsewhere. `docs/evidence/README.md` requires append-only evidence, retaining failed/blocked observations. These public records must not be removed merely because they are logs. The current package suffix contract still excludes `.log` and `.jsonl`; the source repository remains the complete evidence archive.

The ignore rules cover root output/private/cache trees, common guest-media/state formats and credentials. A read-only `git check-ignore --no-index -q` probe confirmed that hypothetical `.efi`, `.o`, `.zip`, `.tar.gz`, and `tpm2-00.volatilestate` files outside ignored output roots are not automatically ignored. None appeared in the tracked/prospective inventory. A focused artifact-name lint or additional patterns could guard against future accidental additions; no ignore change or actual-file deletion is required by the observed inventory. The parent retains `.gitignore` ownership.

## Corrected package selection defects

**Unreviewed workspace inputs.** Previously the packager recursively read every matching suffix under `docs`, `schemas`, `sdk/go` and `examples`. `is_file()` and `read_bytes()` followed file symlinks and did not consult Git or ignore rules. Four then-untracked docs/JSON files would have been included. A path-only rule probe also demonstrated that hypothetical ignored `docs/.env.json` and `docs/credentials/token.json` still satisfied the old package predicate; neither was found in the actual candidate inventory.

`tracked_inputs()` now requires `git rev-parse --show-toplevel` to equal the exact checkout root and obtains NUL-delimited index entries with `git ls-files --stage -z`. A linked Git worktree is supported; an export without an index or a nested directory inheriting a parent repository is refused. Selected source entries must have a regular-file index mode and a readable regular working-tree file. Missing selected files, symlinks in selected paths/ancestors, indexed symlinks even when replaced by working-tree regular files, and nonregular files fail.

The sole untracked/generated input exception is the explicit three-path list: `build/bin/virmill`, `build/bin/virmilld`, and `build/bin/virmill-host-helper`. These have the same file/component checks. Existing suffixes, install targets, helper/core separation, executable modes and development labels are retained. Tracked CLI reference/completions may contain newly generated working-tree bytes; requiring index membership does not require those bytes to equal the index. Consequently packaging still requires a frozen source checkpoint and exact build/input evidence to bind content.

These native packaging scripts require Linux descriptor operations and Python 3.11 or newer for descriptor-relative `shutil.rmtree`; the core/protocol cross-build targets are unaffected. Git must be present with a real index for the exact source checkout. The tests used the versions recorded below; no fallback to recursive workspace discovery or less safe removal is supplied.

**Stale output attribution.** Previously reused RPM output trees were copied with recursive globs, and every RPM/DEB in `dist` entered checksums and the two-run comparison. An unchanged stale artifact could therefore be reported alongside current output. No actual stale root package was observed in this audit.

Each package's exact owned staging and RPM output directory is now recreated, refusing symlink/non-directory roots and ancestors. Removal is limited to those four owned directories and uses descriptor-relative symlink-safe removal. Unrelated output directories and symlink targets remain untouched. Copying requires the one expected `RPMS/x86_64/<package>-0.0.0-0.dev.x86_64.rpm` output; an exit-zero builder without that file is a failure even if a stale destination exists. Checksums/reporting enumerate exactly two RPMs and two DEBs. Unrelated `dist` files are neither included nor deleted. Reproducibility collection enumerates exactly three binaries plus those four packages and fails if any expected output is absent or nonregular.

**RPM path interpretation.** The generated `%install` command now shell-quotes the literal stage path and uses quoted `RPM_BUILD_ROOT`. `%files` paths are double-quoted with literal quote/backslash escaping, preserving ordinary spaces. Selected source and checkout paths containing `%` or ASCII control characters are refused before packaging because RPM macro expansion precedes shell parsing. Shell quoting alone does not protect RPM macros. This follows the upstream [RPM spec file format](https://rpm.org/docs/4.20.x/manual/spec.html) rules for scriptlets and `%files` literals. Local tests validate rendering; unusual checkout-path behavior throughout native RPM internals is not qualified here.

## Provenance scope and remaining qualification

`scripts/build.sh` embeds `git rev-parse HEAD` while building working-tree bytes and regenerating tracked reference/completions. An embedded revision alone therefore does not attest a mutable tree. `scripts/record-evidence.py` historically hashes the implementation trees and root module files, omitting docs, examples and vendor. During this review the parent documented that existing scope and added the label `implementation-tree-v1` to future records without changing the digest algorithm or rewriting old entries.

The next frozen build is planned to retain an all-tracked-input SHA256 inventory and exact package install manifests/artifact hashes separately. Those records, the immutable commit, generated reference/completion bytes and native tool versions must be considered together. This audit did not produce that next build inventory. The reproducibility script still compares two successive runs under one local environment; it does not itself establish independent-environment or declared-platform reproducibility.

The existing docs suffix contract also means packaged evidence narratives can refer to logs/ledger files available only in the repository, and development review documents remain package candidates once tracked. The approved fix preserves that content contract. Installed-document scope/link validation and final docs/help validation remain REL-03 work; this review does not claim those checks passed.

## Checks actually run

Local tools reported **Python 3.14.6** and **Git 2.55.0**. No dependencies were installed or downloaded.

```text
PYTHONDONTWRITEBYTECODE=1 python3 tests/integration/package_inputs_test.py
Ran 20 tests in 0.230s — OK

python3 scripts/traceability.py --check
71 acceptance records validated; matrix consistent.

PYTHONDONTWRITEBYTECODE=1 python3 virmill-v1-spec/qa/validate_package.py
ModuleNotFoundError: No module named 'yaml' — not passed
```

The new tests use generated temporary Git repositories/worktrees and ordinary files/FIFOs/links only. They exercise indexed/untracked/ignored selection, fixed/generated source refusal, link/type/missing-file failures, bounded owned cleanup, stale exclusion, exact missing-output failure, archive layout and literal RPM rendering. Two orchestration checks replace only the `rpmbuild` subprocess with an explicitly test-only result and generated fake RPM bytes; input selection, temporary staging, DEB assembly, manifests and checksum accounting execute. This is not evidence that a native RPM builder accepted the new spec.

Python AST parsing passed for the three changed/new Python files. `git diff --check` passed for the owned patch. Metadata/name/rule inventories above completed successfully. The normative package validator was blocked at its local PyYAML import; no success is inferred from that failure. No real package build, installation, host service, VM, SSH operation, root-repository index write, evidence-ledger edit or commit was performed by this review agent. Parent integration and frozen native package checks remain required before publishing new package evidence.

## Parent integration follow-up

The frozen `dc2ab1a` runtime subsequently passed two actual offline RPM/DEB builds,
package/staged-install/private coordinator checks and one disposable Fedora RPM
upgrade. The exact all-tracked source inventory and package install manifests are
linked from [the integrated run](../evidence/nvram-binding-run.md). These results
supersede the earlier pending-build statement for that exact checkpoint, without
changing this audit's historical observations or qualifying the full release.

The parent changed the `/.tools/` ignore rule to `/.tools`, covering both the
ordinary local toolchain directory and the frozen checkout's toolchain symlink.
No toolchain file or link was tracked. Additional `.o`, `.a`, `.so`, `.so.*`,
`.efi`/`.EFI` and named TPM volatile/save-state patterns guard accidental output
outside the expected ignored build trees. Original source/fixture recipes and
public evidence remain tracked. The subsequent metadata audit observed 1,974
tracked and 18 prospective regular files, no symlinks/nonregular entries and no
tracked ignored files. No source media or confidential payload was opened by
that metadata audit. Later contributions still require their own inclusion review.

A read-only parent dependency probe also found `/usr/bin/python3` 3.14.7 with
PyYAML 6.0.3 available, but no `jsonschema` module. The default Python remains
3.14.6 without PyYAML. The standalone Python specification validator therefore
still lacks a complete runtime in either inspected interpreter. No dependency
was installed, and the separate passing vendored Go schema/example checks are
not represented as an execution of that Python validator.
