# Evidence recorder source-root review

Reviewed on 2026-09-08 for REL-01/REL-03 provenance prerequisites. This review covers the uncommitted `--source-root` change in `scripts/record-evidence.py`, its generated-repository tests, and the actual local frozen `490b88c` inputs/artifacts. It is not a release approval or a broader repository security assessment. Only this review file was edited; no real evidence entry, requirement, recorder or test was changed by the reviewer.

The selected-source digest and revision work for the actual frozen checkout. One introduced validation gap was reproduced: a nested Go directory that is not an independent Git checkout root was accepted and silently inherited the recorder parent's revision. The parent corrected it during this review; the focused reproduction now refuses before execution, and all six updated tests pass. No remaining integration blocker was reproduced in this bounded review. The independently verified `490b88c` worktree was valid before and after that correction.

## Reviewed snapshot

The recorder repository HEAD was `490b88cba5bf6e0837e2cb5780e56211c22a7d27`. Initial reviewed working-file SHA256 values were:

```text
9828e17ac241050eacd7d6fe2916bbdb4126b16055437081efc052e9565eb6fc  scripts/record-evidence.py
21ea428a66dcb4fd5c2557c623a6629a9b6b178b746d12d9a75a1e6d63829c4d  tests/integration/evidence_source_root_test.py
```

At this snapshot the recorder resolves `ROOT / --source-root`, requires the resolved directory to stay inside ROOT, and checks only that `go.mod`/`go.sum` are files. It hashes the historical implementation-tree scope there, runs the command independently at `ROOT / --cwd`, and then calls `git rev-parse HEAD` inside the selected directory. Nondefault entries add canonical `sourceRoot` and `recorderRevision`; fixture paths and logs remain relative to the recorder repository.

## Introduced finding: parent Git discovery accepts a non-checkout

The new preflight at `scripts/record-evidence.py`'s `source_root` validation does not require Git's top-level directory to equal the chosen source directory. A directory containing synthetic `go.mod`, `go.sum` and `internal/payload.txt`, but no independent Git root, passed. Its command executed and its entry reported:

```json
{"exit":0,"commandRan":true,"sourceRoot":"fake-checkout","revisionIsParent":true}
```

The source digest described the fake directory while `revision` came from the enclosing recorder Git repository. No forged Git metadata or race was needed. The existing test named `test_outside_or_non_checkout_source_is_refused_before_command` checks `../`, `internal` and `missing`; none is a non-checkout directory with both required Go files, so the test does not cover this case.

The minimal correction is a pre-command `git rev-parse --show-toplevel` check in the selected directory, requiring its resolved result to equal resolved `source_root`. Failure must occur before running the command or publishing a log/ledger row. Test an ordinary Go directory inside the recorder repository, and retain a passing real worktree case: a worktree's `.git` is a file, so requiring a `.git` directory would be incorrect. Also require failure when Git cannot resolve the selected checkout. Do not silently discover the parent repository as a substitute.

This finding was sent to the parent with the observed result; the reviewer made no production edit. A compact generated-only reproduction using the existing test fixture is:

```sh
python3 - <<'PY'
import runpy, sys
test = runpy.run_path('tests/integration/evidence_source_root_test.py')['EvidenceSourceRoot'](
    'test_default_source_scope_preserves_legacy_recorder_behavior')
test.setUp()
try:
    chosen = test.root / 'fake-checkout'
    (chosen / 'internal').mkdir(parents=True)
    (chosen / 'go.mod').write_text('module generated.noncheckout\n')
    (chosen / 'go.sum').write_text('')
    (chosen / 'internal/payload.txt').write_text('generated non-checkout bytes')
    result = test.run_record('noncheckout', 'fake-checkout', command=[sys.executable, '-c',
        'from pathlib import Path; Path("ran").touch()'])
    print(result.returncode, (test.frozen / 'ran').exists())
    if result.returncode == 0:
        print(test.entries()[0]['sourceRoot'], test.entries()[0]['revision'] == test.revisions[test.root])
finally:
    test.doCleanups()
PY
```

At the reviewed snapshot it printed exit 0, marker true, `fake-checkout`, and parent-revision true. All Git/index mutations and recorder output in this reproduction are confined to its generated temporary repositories.

The parent then added the exact resolved Git top-level check before log/ledger handling and command execution. Two new tests cover the nested Go directory and an actual detached worktree with a `.git` file. The same focused reproduction now exits 2 with no command marker, log or ledger; the updated six-test suite passed in 0.907 seconds. Final reviewed working-file hashes were:

```text
8b88c35971d17d7ee1b271535342d226257681cb36e1d1084259e92bf074753c  scripts/record-evidence.py
83c2a5d0d16d99d7d3c4662cf8ba3a9d460e8735f54f557ae158c112bcefeeb7  tests/integration/evidence_source_root_test.py
```

The parent separately retained actual pre/post ledger evidence for the same command, `python3 tests/integration/evidence_source_root_test.py`. `evidence-source-root-regression-002` ran the expanded six tests against the old recorder and failed exactly `test_nested_go_directory_cannot_inherit_parent_revision` (exit 1; six tests in 0.899 seconds; one failure). Its log SHA256 is `8377852823b5e4185abf5dc76419d79969a2c92a6ab95a1e0f8d8aaa8d6af222`. `evidence-source-root-regression-003` used the corrected recorder and passed all six (exit 0; 0.880 seconds), log SHA256 `0a15661d7b2bc1d2adfb01b89e9fc9f3c12fb7a50a2c5778f49ecf88f6d6f782`. The reviewer read those retained logs; the independent 0.907-second rerun is additional local checking, not a replacement for either evidence entry.

## Path, command and legacy behavior

Additional generated-repository probes observed the following behavior:

| Probe | Actual result | Interpretation |
|---|---|---|
| In-repository source-root symlink to the real synthetic frozen checkout | Accepted; recorded `sourceRoot` was resolved `frozen`, not the alias | Canonicalization is the implemented behavior; source-root symlinks are not categorically rejected. The recorded canonical target is unambiguous for this stable case. |
| Source-root symlink to a generated directory outside the recorder root | Exit 2; sentinel command never ran | Resolved root containment is enforced before command execution. |
| `--source-root frozen --cwd .` | Exit 0; entry selected `frozen`, but command output was `parent source` | The two options are deliberately independent. Selecting a source root does not prove a command consumed that source. |
| Source file under `frozen/internal` symlinked to a generated external file | Both runs passed; changing the external file changed the digest while Git revision stayed the same | Root containment does not imply per-file containment. This is inherited from the historical `is_file`/`read_bytes` digest traversal, not introduced by this flag. |
| Default source root with `--cwd frozen` | Existing regression passed; revision/digest retain recorder scope and no `sourceRoot`/`recorderRevision` is added | Backward compatibility is intentional. Historical cwd values cannot be reinterpreted as selected-source attestations. |

The old source digest algorithm is unchanged: it includes the named implementation trees and Go module files, includes untracked ordinary files under those trees, follows file symlinks, and excludes docs, examples and vendor. Neither this change nor the four tests establish immutable or exhaustive source attestation. A selected source checkout may be dirty; the supplied regression deliberately changes its file between runs while retaining the same Git revision and observes a changed digest.

Other existing limitations remain: the source digest is taken before the command, the revision is read after it, and fixture hashes are read after it. There is no source/ref stability recheck across the command. `--cwd` and fixture paths are separate from the new source-root containment check. A successful command can read another checkout, installed binary or external dependency; a recorder cannot infer all consumed inputs from cwd. These are release-use boundaries, not newly demonstrated command execution or privilege vulnerabilities. For frozen release work, require externally fixed checkout identity, clean tracked state before/after, a complete tracked input inventory, exact package install manifests and artifact hashes, and an inspected command whose actual inputs match that record. Do not relabel development-tree or synthetic tests as release acceptance.

## Actual frozen 490b88c provenance

Read-only checks of `build/auxiliary-490b88c` found its Git top-level was exactly `<dev-home>/project/virmill/build/auxiliary-490b88c`, HEAD was `490b88cba5bf6e0837e2cb5780e56211c22a7d27`, and tracked status was clean. This is a valid selected checkout under the proposed stronger boundary.

The reviewer enumerated its Git index, required regular mode 100644/100755 at stage 0, compared the exact path set to `docs/evidence/environments/source-490b88c.json`, and independently checked every record's byte count and SHA256. All 2,031 records matched. Recomputing the unchanged implementation-tree algorithm yielded `26e4f8410395a37606d577073d0bdff424610ae04b83df678e61d618ae38eb06`. All three generated binary and four RPM/DEB hashes matched the parent's deployment manifest. No artifact was executed or rebuilt during this review.

The public provenance files matched these SHA256 values:

```text
f0585986ff6693a306a54e86d48db9eecbed5cddca83941d461d3e2bb2727576  source-490b88c.json
21daefa8c91ce144a3b586a593f5fd8af911c354455c0572e1b744d7078b0f97  package-490b88c-virmill.json
d51b177c87d01232460bcef5f5f733dc2d09f778abcfe610c6065fe549e15e7e  package-490b88c-virmill-host-helper.json
```

The historical `auxiliary-inventory-repro-001` entry recorded cwd `build/auxiliary-490b88c` but the recorder's then-default parent digest `934dbf6735ecdbbba7e5e697e4ef6f45537911985a1f296e7572a6b6fbec169d`. That digest must remain labeled as the parent snapshot, not compiled source. The later `auxiliary-inventory-packages-001` correctly records explicit `sourceRoot`, the independent `26e4…` selected digest, complete source inventory and both package manifests as fixture digests. The explanation in `docs/evidence/auxiliary-inventory-run.md` preserves this distinction without overwriting the original ledger. Equal recorder and selected revision strings at this particular HEAD are not evidence that their working trees were identical.

The parent-operated `auxiliary-inspection-native-001` entry also binds `sourceRoot: build/auxiliary-490b88c` and that same frozen implementation digest, separately recording the later test recipe and source inventory as fixture hashes. Its command runs through parent-owned SSH from cwd `.`; this is a legitimate reason for command cwd and selected source root to differ. Deployment/revision/binary/recipe pins in the command and separate actual runtime checks are needed to connect the selected source to the remote executable. The entry is **failed** (exit 1); its correct source binding neither relabels that native result nor creates an acceptance claim. The reviewer did not execute that command or inspect native state.

## Checks actually run and handoff

`python3 tests/integration/evidence_source_root_test.py` initially passed all four existing tests (0.722 seconds), then passed all six parent-updated tests (0.907 seconds). The separate temporary-repository probe exercised non-checkout acceptance, inside/outside symlinks, command/source mismatch and external file-symlink hashing; the exact results are above. The focused non-checkout reproduction was independently rerun after the parent fix and refused before command/log/ledger creation. The frozen input/artifact validation independently passed 2,031 tracked regular files, the limited source digest, all seven artifacts and clean tracked state. No SSH, native VM/IPC, host service, installed program, real evidence append or root-repository index mutation occurred.

The new Git checkout-root defect and missing regressions are corrected. Existing digest/command/stability limitations need explicit interpretation and frozen-build evidence, not silent changes to historical digest semantics or old ledger entries. The actual frozen artifact hashes and source inventory passed the read-only checks described here. No acceptance scenario was promoted.
