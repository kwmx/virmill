# Installed NVRAM binding CLI/TUI observation

This is the separate TUI-002 test tool for native-002. Native-001 stopped during
preflight because its `virsh vol-list --name` option was unsupported. Native-002
uses the supported strict Name/Path table observation. Both native-001 and
TUI-001 remain frozen; their files and run history are not rewritten. The TUI-002
Python source changes only the selected native artifact directory and its own
recipe/output identity from TUI-001. Its ten synthetic tests, exact
`resourceUUID` cross-artifact comparison and read-only behavior are retained.
The installed product stays at the parent's reviewed
`dc2ab1a7af8c023c1485d36d0e858a65264ace3e` deployment; this is a test-tool correction.

This single-use fixture is authored for the parent to review and execute on the
already authorized disposable host as its ordinary `virmill-test` user. Authoring
and the synthetic self-tests do not constitute an installed/native run.

The fixture requires successful completion of `nvram-binding-native-002.py`,
including restoration of native A, reconciliation of the same operation, removal
of that recipe's scoped fault trigger, and zero remaining locks. It consumes
that recipe's private `report.json`, `creation-plan-response.json`,
`first-binding.json`, `result-complete.json`, `baseline-guests.json` and
`native-A.xml`. It does not reproduce or apply the creation recipe.

The parent supplies the same deployment identity used by the native run:

```sh
python3 nvram-binding-tui-002.py --revision FULL_40_HEX_REVISION --deployment-sha256 FULL_64_HEX_MANIFEST_SHA256
```

Both values must be lowercase hexadecimal with exact lengths. The run root is
fixed to `/home/virmill-test/virmill-tests/run-65930c6-20260907`; this fixture does
not discover or authorize another host. It validates the bundle's
`packages/<revision-prefix>/deployment.json`, installed `/usr/bin/virmill` and
`/usr/bin/virmilld` hashes, the active private coordinator's unit/root/PID and
`/proc/<pid>/exe` hash, and the installed version. The final runtime identity
must equal the native recipe's recorded runtime and remain unchanged throughout.
Private XDG paths select the Virmill coordinator only; `systemctl --user` and
read-only `virsh` retain the ordinary host environment. Ambient `VIRSH_DEBUG` and
`VIRSH_LOG_FILE` are removed from both child environments.

Actual CLI reads use `plan show` and `vm creation result`, each in JSON, table and
NDJSON modes. All three envelopes must equal each other and the successful
native fixture artifacts. The plan must retain its exact ID, digest, version 1
declaration policy and unverified initialization flag. The result must retain
the exact successful operation/plan/VM/receipt/binding identities, first declared
path and historical fingerprint, with `complete: true`,
`nvramDeclarationBound: true`, and `nvramInitializationVerified: false`.
Guest boot/setup/connectivity flags remain false.

Two separately spawned actual `virmill tui` clients run through private 80×24
PTYs. The fixture verifies the selected current **Jobs → plan show** or
**VMs → vm creation result** action before opening its ID form. It then compares
every current detail page with the exact installed CLI's indented envelope,
paging by ten rows with overlap until the entire envelope is visible. It never
searches the accumulated ANSI transcript for evidence of the current page.
The bounded screen decoder applies cursor positioning, carriage returns,
overwrites and erasures, including split UTF-8/CSI sequences. Unknown terminal
controls, unsupported cell widths, incomplete output or mismatched pages fail
closed. It supports the pinned plain Bubble Tea renderer, not arbitrary terminal
applications. If native output needs another control, preserve that failed run
for review instead of weakening the comparison.

Only Tab, Down, Enter, PageDown and the final detach `q` navigation keys are sent.
Validated UUID characters are entered only after the read-only ID form is
visible. No apply key, approval digest, guest lifecycle command or reconciliation
request is sent. No guest console is attached. Process cleanup addresses only
each unreaped `Popen` child; it does not signal a process group, coordinator,
hypervisor, guest or another console session.

Before and after, read-only transactional SQLite snapshots retain every selected
plan, job, metadata, event, lock and dedup column, including exact BLOB bytes
encoded as base64. All rows and their order must remain identical. The fixture
checks the entire guest UUID inventory, observes each guest stopped on both
sides of each inactive-XML read, and retains exact before/after XML and state
bytes. It also checks the baseline six guests and restored new native A against
the native recipe. These are endpoint observations; they do not exclude an
unobserved external writer between checks or independently verify disk/auxiliary
file contents.

Outputs use a newly created private directory `nvram-binding-tui-002/`; every
artifact is created exclusively. Existing output prevents a rerun. Artifacts
include per-command raw stdout/stderr and command records, both ANSI transcripts,
verified current page snapshots, exact before/after journal and guest snapshots,
and `report.json`. Interrupted or failed observations remain inconclusive and
retain their available logs. There is no cleanup/recovery mutation or automatic
retry of the native recipe.

Bounds are four minutes overall, 30 seconds per command, 10 seconds per current
screen wait, five seconds for normal TUI EOF, and bounded own-child termination
cleanup of one second then two seconds after escalation to `kill`. Command
stdout/stderr share a 2 MiB ceiling and 32 MiB total; commands are capped at 100.
Each ANSI transcript is capped at 4 MiB and each TUI envelope at 128 pages.
Reaching an output ceiling is inconclusive. Each journal snapshot has at most
100,000 rows and 32 MiB of cell data. Input files are bounded regular files, binary
hash reads are capped at 128 MiB, and serialized artifacts are capped at 96 MiB.
The allowed guest inventory is capped at 32, while the native artifact comparison
requires exactly its baseline guests plus the new VM.

The embedded local seam tests invoke only synthetic Python children and the
screen decoder. They make no native, PTY, database or remote calls:

```sh
python3 tests/fixtures/protection/disposable-recorded-run/nvram-binding-tui-002.py --self-test
```

Ten tests passed at authoring: stale-page erasure/overwrites, incremental
UTF-8/CSI decoding, unsupported/truncated controls, alternate-screen reset,
wrapping, duplicate JSON/invalid UUID refusal, command timeout/output ceiling,
exact stderr/nonzero exit preservation, own-child cleanup targeting, and private
XDG/debug-environment separation. Source
compilation also passed. Native execution and actual installed TUI qualification
remain the parent's separate evidence.

This fixture contributes prerequisites to UX-03, IMP-07 and REL-03 only. A bound
declaration does not prove historically first assignment, file provenance or
fresh initialization. `completeCaptureVerified`, `independentRecoveryVerified`
and `guestBootVerified` remain false. No snapshot/backup recovery, TPM hardware,
guest-console or release acceptance is claimed.
