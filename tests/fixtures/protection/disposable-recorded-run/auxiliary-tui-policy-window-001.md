# Positive auxiliary TUI policy window 001

This is an authored, single-use wrapper for the parent operator on the already
authorized disposable host. **It has not been executed natively by its author.**
Only `--self-test` is local authoring work. Its evidence can contribute SEC-01,
UX-01, UX-03 and SNAP-01 prerequisites; it does not promote acceptance, capture,
independent restore, guest boot, or firmware/TPM readiness.

The wrapper temporarily applies the exact saved native003 metadata-only policy
grant, invokes the immutable positive TUI001 observer, and conditionally restores
the original public policy. It creates no guest, state file, pool, service, job,
permission rule or helper key. It never starts/stops services or guests, changes
native XML, reads TPM/NVRAM/private-key payloads, cleans prior artifacts or retries
a failed window. The TUI process remains the ordinary user.

## Immutable prerequisites

The only execution root is
`<test-vm-home>/virmill-tests/run-65930c6-20260907`. The actor must be nonroot
with home `<test-vm-home>`. The parent must keep policy writers idle for the
entire window, and ensure the existing coordinator, helper and helper socket are
already active. No prerequisite is repaired by this recipe.

| Input | Exact pin |
|---|---|
| Runtime revision | `490b88cba5bf6e0837e2cb5780e56211c22a7d27` |
| Deployment manifest | `5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c` |
| Native003 Python source | `32b047be8a48c1a2b1d191b1515d3d632329a0eacc6322bb661e7ccc91d36bcc` |
| Native003 passed report | `4447e177dd0df0aabfbda022495a0f081c1a23ec5d747c5458105d8207377d8c` |
| TUI001 Python source | `b3a80e536dc644037c486d6f6e7dd8f1a35001e2d4a4c5d47474ac4d1f75a925` |
| Original public policy bytes | `1ec86a37a44cac12e7fa7c16c542ec83e552e348ea2f2cec841597dc999d6ed4` |

The program also pins all three installed binary hashes, coordinator unit
`<test-vm-login>-490b88c.service` and actual PID `30897`, and helper PID `31125`.
It freshly hashes both actual running executable identities before and after the
window. The root helper process must hash to
`4d67ec32a0aa891d16af237885fe45ba95ee5377829f86ab4761aba89b42917a`.
Source inventory/digest values remain those in the pinned deployment manifest;
the fixture source is separately pinned and does not change the runtime revision.

Both immutable Python programs must already exist at the fixed paths:

```text
ROOT/sources/auxiliary-inspection-native-003/auxiliary-inspection-native-003.py
ROOT/sources/auxiliary-inspection-tui-001/auxiliary-inspection-tui-001.py
```

The wrapper opens bounded single-link regular sources, refuses symlink components,
checks SHA256 before compilation, and executes only the verified bytes under a
non-main module name. It creates no pycache. The actual child uses Python `-B` and
the exact TUI001 self-source pin. The source paths must remain immutable during
execution; these observations do not defeat a malicious host administrator or
establish an atomic snapshot against external writers.

Native003 must have passed all six reviewed checks with original policy restored,
all preservation flags true, and all capture/boot claims false. The exact nine
UUIDs in the wrapper must remain stopped, persistent, autostart-disabled and
without managed save. Their inactive XML hashes must equal native003's eight
saved baseline hashes plus final UUID
`074d9083-1953-4941-b006-9e301bb6d907`. All coordinator row digests/schema must equal
its baseline: 36 plans, 32 jobs, 24 metadata rows, 177 events, 32 dedup rows, zero
locks and zero triggers (301 total rows).

The current public helper actor, key ID/public key and endpoints must equal the
saved identity. The current public policy must match native003's saved restore
receipt generation, exact original bytes, original mode/UID/GID/link count/size,
mtime and all bounded xattrs. Atime alone is excluded because legitimate reads
can change it. Historical inode/ctime from the original pre-native policy are
not mistaken for the later restored generation.

The exact bytes in native003's `policy-grant-receipt.json` are used as the grant.
Its decoded content is independently reconstructed by the pinned `policy_variant`
from the original policy and current approved identity. It adds only root ID
`auxiliary-fixture-003`, path
`/var/lib/virmill-host-helper/auxiliary-fixture-003`, this VM, actor/key, state
UID/GID 107, 8192 maximum bytes, three maximum members and `allowCapture=false`.
Older entries stay exact; broader grants, changed access metadata and changed
xattrs are refused. The byte digest is saved in durable intent and supplied
directly to TUI001, without reserializing the approved grant bytes.

## Execution and conditional restoration

The parent uploads this wrapper under a new exclusive source directory and pins
its final SHA256. The invocation is:

```text
/usr/bin/python3 -B -u /ACTUAL/UPLOADED/auxiliary-tui-policy-window-001.py \
  --recipe-sha256 EXACT_REVIEWED_WRAPPER_SHA256 \
  --execute-reviewed --exclusive-policy-window
```

The wrapper exclusively creates `ROOT/aux-tui-policy-window001`, mode 0700, and
refuses an existing positive TUI output directory. It fsyncs output files and the
output directory; the output directory's creation is also fsynced in `ROOT`.
`intent.json` is durable before the first grant attempt and records the exact
expected generation, original public policy, grant bytes/hash, reviewed root
program hash, child argv and lost-acknowledgement rule.

All policy reads and replacements reuse native003's exact `ROOT_PROGRAM` and
`Run.replace_policy`/`Run.verify_policy_restored`. The inherited root program is
not copied or modified. The wrapper restricts its root dispatch to policy
read/replace and metadata/process observations, and allows only grant/restore
replacement phases. Every virsh call is read-only. The native compare-and-replace
primitive checks the last observed generation before preparing and before
renaming a new single-link file, preserving original owner/mode/xattrs/mtime,
fsyncing file and parent, and returning an observed receipt. Its fixed exclusive
temporary names are `.auxiliary-fixture-003-grant` and
`.auxiliary-fixture-003-restore` beneath `/etc/virmill`; any retained collision
refuses, and the wrapper never deletes or repurposes it.

After a known grant acknowledgement and current-generation recheck, the wrapper
invokes only TUI001 `--mode positive --prepared-positive-policy`, passing every
runtime/source/native-report pin and the exact granted-policy byte hash. The
observer checks CLI JSON/NDJSON/table and actual 80x24 TUI pages, fresh typed read
correlation, native inventory equality and its own policy/journal/guest/pool/media/
file preservation. **The wrapper writes no output while this child is active:**
the observer includes the wrapper directory in its existing-file snapshot.
The command receipt is written only after the child finishes.

The wrapper reads the newly created actual TUI report, validates its pinned
positive result, current pages and preservation flags, and compares it with the
child's printed JSON and successful exit. There is no denial fallback. The raw
report is retained, and `tuiResult` records a validated TUI result separately from
the outer `status`. A true TUI result remains true if later policy restoration
fails; that outer window remains inconclusive.

Finally, restore is attempted only when the last policy acknowledgement is known
and a fresh policy read exactly matches its generation. Unknown acknowledgement
or concurrent change means **no forced replacement**, `manualReviewRequired=true`
and a retained failure requiring parent comparison. A lost restore reply cannot
be retried. After a known restore, exact original bytes/access metadata/xattrs/
mtime are rechecked. Abrupt termination/power loss can leave the grant in place;
durable intent is a recovery record, not an automatic watchdog or authorization
to rerun this recipe. Even a conservative failed compare may retain a temporary
file for manual review.

Final independent checks repeat all nine stopped XMLs, 301 journal row digests/
schema, prior protected helper journal/state metadata, generated fixture metadata,
and running binaries/PIDs. Protected state observations are metadata-only and do
not enumerate or read key/TPM/NVRAM payload bytes. TUI001 owns its detailed
pool/source-media/file preservation checks during observation; a failed TUI
preflight does not establish those checks. No output or resource is removed.

## Bounds and authoring evidence

Native command collection retains its reviewed 2 MiB per-child, 32 MiB aggregate,
2048-command and 20-minute limits. The TUI child has a 600-second deadline covering
its six-minute observation and three-minute final budgets. Wrapper final checks
have five minutes. Only owned unreaped Python/observer children can be terminated
by the inherited collector; no guest, coordinator or helper process is signalled.
Artifacts have a 32 MiB bound; policy bytes/xattrs retain native003's tighter
16 KiB/8 KiB bounds. Failure retains all artifacts and returns nonzero.

```sh
python3 -B tests/fixtures/protection/disposable-recorded-run/auxiliary-tui-policy-window-001.py --self-test
```

Self-tests import pinned source definitions without executing their native entry
points. They use temporary original source files and in-memory policy transport
seams only: no subprocess, PTY, IPC, protected file, VM or privileged operation.
They cover source hash/symlink refusal, no pycache, original metadata/generation,
grant broadening, durable intent ordering, actual positive report requirements,
exact child pins, root-dispatch limits, uncertain/lost acknowledgements, concurrent
policy changes and preservation of a true TUI result after restoration failure.
Native execution and public evidence recording remain exclusively parent-owned.

On 2026-09-08, the author ran the command above: **13 tests passed in 0.031 s**,
with zero failures/skips. In-memory Python compilation, UTF-8, final-newline and
trailing-whitespace checks passed. The reviewed wrapper is 32294 bytes with SHA256
`f7994d0e8b67a3a5327d3ea0ba4fee9c8e8b59df70c32ca910885f1c6be01833`.
Both imported source hashes were rechecked against the immutable pins above.
