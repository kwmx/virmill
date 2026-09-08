# Auxiliary inspection CLI and actual TUI observer 001

This is an authored, single-use **read-only** recipe for the parent operator on
the authorized disposable host. Authoring does not execute it natively. It
provides UX-01, UX-03, SEC-01 and SNAP-01 prerequisites and promotes no acceptance
scenario. It verifies installed CLI output and the current screen of a real
`virmill tui` process in an 80 by 24 private PTY.

The recipe changes no policy, service, domain XML/state, guest, pool, journal or
permission. It never uses sudo, invokes an apply/lifecycle operation, reads an
existing TPM/NVRAM/private-key payload, attaches a guest console, or repairs a
failed prerequisite. Its writes are exclusively new local observation artifacts
and its own PTY input. Cleanup signals only its own unreaped TUI/command children.

## Required prior evidence and external policy preparation

Native001 failed in preflight and remains failed. Native002 subsequently failed
after creating a never-started fixture when the test did not recognize the CLI's
typed stderr diagnostic. These are retained fixture failures, not inferred
product bugs. Neither failed report can authorize this observer to pass.

There is no successful-native default. The parent must explicitly select
`auxiliary-inspection-native-002` or `auxiliary-inspection-native-003`, supply its
exact recipe SHA256 and report SHA256, and obtain a report with `status: passed`,
empty failures, all six reviewed checks, complete preservation flags and original
policy restoration. The native report's source/runtime identity must match the
selected pins. All preceding failed native reports are copied and hash-recorded
without reclassification. A failed report stops preflight; there is no success
exception or native rerun in this observer.

The parent subsequently reported native003 passed all six stages, with original
policy restored, nine stopped definitions and all 301 coordinator rows preserved.
Its supplied recipe SHA256 is
`32b047be8a48c1a2b1d191b1515d3d632329a0eacc6322bb661e7ccc91d36bcc`
and report SHA256 is
`4447e177dd0df0aabfbda022495a0f081c1a23ec5d747c5458105d8207377d8c`.
The reported new UUID is `074d9083-1953-4941-b006-9e301bb6d907`. These are parent
execution observations, not author-executed evidence. This observer still checks
the actual report, runtime, native definitions and policy before running a TUI.

The selected recipe determines the exact namespace. For native003 it is root ID
`auxiliary-fixture-003`, root path
`/var/lib/virmill-host-helper/auxiliary-fixture-003`, and VM name
`virmill-auxiliary-fixture-003`; the UUID comes only from that selected report.
Native002 uses its corresponding `002` namespace. No implicit TPM path is guessed.

Choose one explicit mode per invocation:

| Mode | Parent-prepared public policy | Expected observation |
|---|---|---|
| `denial` | Exact original policy restored by the successful native run | `PERMISSION_DENIED`, no data, current TUI error rendering |
| `positive` | Exact metadata-only grant bytes from that run's `policy-grant-receipt.json`, with original mode/owner/ACL/SELinux metadata | Complete declared member metadata, no artifact or readiness claim |

Positive mode additionally requires `--prepared-positive-policy`. It cannot
fall back to denial. The parent must separately authorize and prepare that
short policy window, pin the resulting public policy hash, keep policy writers
idle, and restore the original policy afterward through separately reviewed
administration. **The observer neither grants access nor restores that grant.**
Its successful positive report means policy stayed unchanged during observation;
it is not evidence that the parent has subsequently removed the grant.

Both modes compare the actual policy's exact bytes with the selected saved policy
and supplied hash, and verify original access metadata and all bounded xattrs.
Positive mode independently reconstructs the expected single actor/key/VM/root
grant from the original policy and native owner observations, preserving every
older policy entry with `allowCapture=false`. The current public helper identity
must retain the same actor/public key/fingerprint. Neither a broad substitute
grant nor different formatting/content is accepted. Expected policy atime changes
from legitimate reads are excluded; inode/change identity, mode/ownership, mtime,
size and xattrs remain compared before and after the UI.

## Frozen runtime and invocation

Both modes require this frozen deployment:

| Item | Expected value |
|---|---|
| Revision | `490b88cba5bf6e0837e2cb5780e56211c22a7d27` |
| Deployment SHA256 | `5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c` |
| CLI SHA256 | `9cc64d8aa14e7f5d5992514910364f316d3d12fab784393630d7c160628ac886` |

The pinned deployment file at
`/home/virmill-test/virmill-tests/run-65930c6-20260907/packages/490b88c/deployment.json`
also supplies the exact daemon/helper binary and source inventory hashes. The
installed binaries must match. The active ordinary coordinator must be
`virmill-test-490b88c.service`, with the expected working directory, same recorded
PID and a fresh matching `/proc/PID/exe` hash. The helper service/socket must
already be active; its reported PID and process-start timestamp must stay stable.
No service activation or restart is performed.

The ordinary observer checks the installed helper binary hash and service PID;
it **does not freshly hash the root helper process executable**. The matching
native report retains that earlier privileged observation. The parent may make a
separate fresh root-process check before the policy window. This limitation is
explicit in the observer report and is not bypassed with sudo.

Use the actual uploaded source file, whose `__file__` SHA256 is checked. Invocation
template for the denial mode after a separately successful native003 run:

```text
python3 /ACTUAL/UPLOADED/auxiliary-inspection-tui-001.py \
  --mode denial \
  --native-recipe auxiliary-inspection-native-003 \
  --native-recipe-sha256 32b047be8a48c1a2b1d191b1515d3d632329a0eacc6322bb661e7ccc91d36bcc \
  --native-report-sha256 4447e177dd0df0aabfbda022495a0f081c1a23ec5d747c5458105d8207377d8c \
  --policy-sha256 EXACT_RESTORED_PUBLIC_POLICY_SHA256 \
  --recipe-sha256 EXACT_UPLOADED_TUI_RECIPE_SHA256 \
  --revision 490b88cba5bf6e0837e2cb5780e56211c22a7d27 \
  --deployment-sha256 5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c \
  --binary-sha256 9cc64d8aa14e7f5d5992514910364f316d3d12fab784393630d7c160628ac886
```

For a separately prepared positive policy window, use `--mode positive`, its
exact prepared policy hash, and `--prepared-positive-policy`; all other source,
report and runtime pins remain required. This is a template, not an authoring
execution or permission to prepare policy automatically.

The fixed run root is
`/home/virmill-test/virmill-tests/run-65930c6-20260907`. `environment.json` selects
the existing private coordinator. Private XDG variables apply only to Virmill;
systemctl and read-only virsh retain the invoking ordinary host environment.
Ambient `VIRSH_DEBUG` and `VIRSH_LOG_FILE` are removed. Each mode creates a new
exclusive `auxiliary-inspection-tui-001-denial/` or
`auxiliary-inspection-tui-001-positive/` directory. Existing output refuses reuse;
the same mode cannot silently rerun against a newer native fixture.

## Current-screen parity and preservation

The CLI executes the exact auxiliary-inspection command in JSON, NDJSON and table
modes. JSON/NDJSON are each one clean stdout envelope. Expected CLI denial has
exit code 4 and the exact separate diagnostic
`PERMISSION_DENIED: root ID is not approved by helper policy` followed by newline.
Blank, extra or different stderr fails. Successful CLI observations require empty
stderr. A TUI error instead appears inside its rendered shared-service envelope;
the TUI process exits normally on detach.

The real PTY client navigates Overview → Protection, verifies the current selected
`vm recovery auxiliary inspect` action, and types only the JSON form containing
the selected UUID and root ID. It sends Tab, Down, Enter, Page Down, Page Up and
final detach `q`; no apply key or authorization digest is sent.

Every page is checked against the exact Go-indented current CLI envelope at the
frozen three-action Protection layout. The decoder applies cursor movement,
wrapping, erasure and split UTF-8/CSI sequences. Historical transcript text is
never current-screen proof. Unknown controls, unsupported character-cell widths,
missing pages, stale overlapping pages or incomplete EOF fail the observation.

Positive requests deliberately have fresh `jobID` and `binding` values. Only
those two typed fields may differ between CLI and TUI. Their UUID/digest shapes
and uniqueness are checked, and all other fields must agree exactly with the
current CLI and the selected native run's final restored inventory. Wrapped
fragments retain original JSON line boundaries, so the whole TUI response is
reconstructed from actual current pages and parsed strictly. Overlapping pages
must agree even on nonce values. The three payload members and separate empty
`.lock`, total bytes, ownership, generation and native mapping remain exact.
Capture, independent restore and guest-boot flags stay false, with no artifact.

Denial requires null data and the exact permission error. It additionally pages
down and back up even if the error fits one viewport, checking the current screen
again. It makes no claim that a denied response contains a usable inventory.

Before and after the clients, compare all coordinator row digests and SQLite
schema in bounded read transactions, requiring no locks or triggers. The initial
rows must equal the selected native baseline. Every old and newly retained domain
must remain stopped, persistent, without autostart/managed save and with exact
inactive XML around repeated state observations. No domain is adopted or changed.

Pool comparisons retain configuration and all volume XML bytes except explicit
native access-time measurements. Directory-pool allocation/available counters
may vary only with fixed capacity and a consistent sum. File-volume normalization
permits only the one direct `target/timestamps/atime` leaf, containing 1–20 decimal
seconds digits and an optional 1–9 digit fraction, such as
`1788824533.293235434`. Observed values are separately recorded. Duplicate,
malformed, nested or attributed atime leaves fail; capacity, allocation, physical
size, path, ownership, labels, other timestamps and all other XML remain exact.

Existing declared media and supplied source media are freshly checked through
ordinary metadata access only. Unreadable or ambiguous paths fail; no permission
grant is inferred. Large media and selected historical EFI hashes are not rehashed
by this observer. Prior native byte-hash evidence is retained as historical, not
described as fresh. Existing disposable files are checked by metadata, with
transactional comparison covering the active SQLite files. Protected helper
journal/state trees are not independently read as root by this recipe.
These before/after observations do not exclude an unobserved external writer or
establish an atomic snapshot of guest, process or filesystem state.

## Outputs, bounds and authoring checks

Artifacts include the selected successful native report, earlier failed reports,
public policy snapshots, journal/guest/pool/media/file observations, each CLI
stdout/stderr, raw TUI ANSI, current page snapshots, reconstructed TUI JSON and
`report.json`. Any failure keeps all output and produces `status: inconclusive`.
Independent final preservation checks still run after another check fails. No
native cleanup, policy rollback, retry, grant or service action follows failure.

Limits are six minutes for observation, three minutes for final checks, 30 seconds
per command, ten seconds per current-screen wait, five seconds for TUI EOF,
512 read commands, 2 MiB per command output, 32 MiB aggregate command output,
4 MiB ANSI, 128 pages, 100000 journal rows/32 MiB row data, 128 volumes and 20000
prior file metadata entries. Only each owned unreaped child receives bounded
termination/kill waits of one and two seconds.

The authoring command is:

```sh
python3 tests/fixtures/protection/disposable-recorded-run/auxiliary-inspection-tui-001.py --self-test
```

Self-tests use pure parsers/current-screen simulations and original bounded
ordinary Python children. They open no PTY, native executable, libvirt connection,
coordinator database, helper socket or protected state path. They cover successful
native prerequisite refusal, exact policy mode, typed stderr, no fallback, wrapped
page reconstruction and nonce boundaries, stale/changed/missing pages, unsupported
terminal controls, volume/pool normalization, command authority and XDG separation.
Native TUI results remain pending parent execution and separate evidence recording.

On 2026-09-08, all 13 offline authoring tests passed with zero failures and zero
skips. Python compilation, UTF-8, final-newline and trailing-whitespace checks
passed. No native, PTY, helper/coordinator IPC, root or remote execution occurred
during authoring.
