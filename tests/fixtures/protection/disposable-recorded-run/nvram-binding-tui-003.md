# Installed NVRAM binding supplemental CLI/TUI observation 003

This is a separate read-only observation of the operation already reconciled by
native-002. Native-002 completed first-A binding, idempotent repeated-A
observation, rejection of B, restoration of A and successful reconciliation. Its
final fixture assertion then compared full pool XML across allocation and failed
because only allocation/available-space statistics changed. That failed result
remains immutable. Native-001/002 and TUI-001/002 are not rewritten or rerun;
TUI-002 was never executed. No production correction is implied. The installed
product stays at the parent's reviewed
`dc2ab1a7af8c023c1485d36d0e858a65264ace3e` deployment.

The parent alone reviews and executes this fixture on the authorized disposable
host as ordinary `<test-vm-login>`. Authoring and synthetic tests are not native
qualification. The only run root is
`<test-vm-home>/virmill-tests/run-65930c6-20260907`.

The fixture accepts only native-002's exact failure list
`["existing pool XML changed"]`, status `uncertain-preserve-resources`, final stage
`final A reconciliation`, all four exact earlier passing checks, preserved old
XML/journal/disk observations, a removed fault trigger and zero locks. Missing,
additional or different failures/claims refuse this exception. It binds existing
VM `666c692d-da0e-4119-9554-727c4af3c751`, plan
`2bd38ffb-6931-493b-bd01-5b56c68ff574` and operation
`8cafa2bf-f148-4189-bf93-34340f1c432a`, plus the recorded final-A XML hash.

It consumes native-002's private `report.json`, `creation-plan-response.json`,
`first-binding.json`, `result-complete.json`, `baseline-guests.json`, `native-A.xml`
and saved `command-047.json`/`command-388.json`. It preserves a raw copy of the
failed report and its SHA256, status, failure and earlier checks in the new
report. A successful supplemental observation never relabels native-002 passed.

The parent supplies the same deployment identity used by the native run:

```sh
python3 nvram-binding-tui-003.py --revision FULL_40_HEX_REVISION --deployment-sha256 FULL_64_HEX_MANIFEST_SHA256
```

The parent must upload and execute the actual `.py` file, rather than feed it
through stdin: the fixture hashes its own `__file__`. That test-tool hash is
separate from the installed runtime revision/deployment identity. Both arguments
must be lowercase hexadecimal with exact lengths. The fixture checks
`packages/<revision-prefix>/deployment.json`, installed CLI/daemon hashes, the
active coordinator unit/root/PID, `/proc/<pid>/exe` and installed version. Runtime
identity must equal the native recipe's recorded runtime and remain unchanged.
Private XDG directories select Virmill only; `systemctl --user` and read-only
`virsh` keep the ordinary host environment. Ambient `VIRSH_DEBUG` and
`VIRSH_LOG_FILE` are removed.

The pool comparison verifies that the saved commands were exactly successful
read-only `pool-dumpxml` calls on the reviewed pool. Saved statistics must match
the parent's observed pair:

| Statistic (bytes) | Before (047) | After (388) |
| --- | ---: | ---: |
| capacity | 272029974528 | 272029974528 |
| allocation | 145265324032 | 145318100992 |
| available | 126764650496 | 126711873536 |

It requires one direct, unnested `unit='bytes'` leaf per statistic, bounded
nonnegative integers, positive capacity and the observed cohort's consistent
sum. Only allocation/available numeric text may vary. Capacity,
root/name/UUID/source/target/permissions/label, attributes, formatting and every
other XML byte remain exact. Duplicates, unknown shape, malformed numbers,
DTD/entities and unrelated changes fail. The same comparison runs on fresh
read-only pool XML before and after the UI. The 52776960-byte difference is
reported as pool statistics, not attributed to the new disk. Libvirt documents
these fields as pool space measurements and notes that pool metadata can be
cached. [Storage XML documentation](https://libvirt.org/formatstorage.html#storage-pool-general-metadata).

An initial `operation show` must confirm the existing operation is already
`succeeded`; no reconciliation is requested. CLI reads then use `plan show` and
`vm creation result` in JSON, table and NDJSON modes. All envelopes must equal
each other and the saved successful plan/result artifacts within the failed
native run. The plan retains its exact ID/digest, version-1 declaration policy
and initialization=false flag. The result retains the exact successful
operation/plan/VM/receipt/binding, first path and historical fingerprint, with
`complete: true`, `nvramDeclarationBound: true`, `defined: true` and
`volumesVerified: true`. Initialization, guest boot, setup and connectivity
remain false.

Two separately spawned actual `virmill tui` clients run in private 80×24 PTYs.
The fixture verifies the current **Jobs → plan show** or **VMs → vm creation
result** selection before opening its ID form. Every detail page must match the
current CLI's indented envelope, with overlapping ten-row paging through the
whole envelope. Historical ANSI transcript text is never used as current-screen
proof. The bounded screen decoder applies cursor movement, carriage returns,
erasure, wrapping and split UTF-8/CSI sequences. Unknown controls, unsupported
cell widths, incomplete output or mismatched pages fail. This is the unchanged
pinned Bubble Tea observation machinery from TUI-001/002.

Only Tab, Down, Enter, PageDown and final detach `q` keys are sent. UUID text is
entered only in the verified read-only form. No apply key, approval digest,
lifecycle or reconciliation request is sent; no guest console is attached.
Cleanup signals only each unreaped `Popen` child, never a process group,
coordinator, hypervisor or guest.

Read-only transactional journal snapshots retain every plan, job, metadata,
event, lock and dedup column, including exact BLOB bytes encoded as base64. All
rows/order must stay identical, no locks may exist initially and no journal
trigger may exist before or after. All seven UUIDs are checked against the six
native baseline guests plus restored A. Each guest is observed stopped on both
sides of its inactive-XML read; all before/after state and XML bytes remain exact.
These endpoint observations do not exclude an unobserved external writer or
verify disk/auxiliary file contents. The original large-disk metadata-only and
selected EFI hash evidence is retained as reported, without stronger byte claims.

Outputs use a new private `nvram-binding-tui-003/` directory and exclusive file
creation. Existing output prevents rerun. Outputs include raw commands, ANSI
transcripts/current pages, journal/guest snapshots, failed native report copy,
saved/fresh pool XML, operation response and final `report.json`. The final
report has separate `nativeFixture` failure history and supplemental pool/UI
results. It may report `status: passed` only when the same completed operation,
unchanged pool configuration, full current CLI/TUI pages and preservation checks
all pass. Failed observations remain inconclusive. There is no repair mutation
or automatic retry of native-002.

Bounds remain four minutes overall, 30 seconds per command, 10 seconds per screen
wait, five seconds for TUI EOF and own-child termination waits of one then two
seconds. Commands share a 2 MiB output ceiling, 32 MiB total and 100-command cap.
ANSI is capped at 4 MiB per client and each envelope at 128 pages. Journal
snapshots are capped at 100000 rows/32 MiB cell data. Regular input files, binary
reads (128 MiB), pool XML (64 KiB) and output artifacts (96 MiB) are bounded.

Local tests invoke only synthetic Python children, pure pool/eligibility checks
and the screen decoder; they make no native, PTY, database or remote calls:

```sh
python3 tests/fixtures/protection/disposable-recorded-run/nvram-binding-tui-003.py --self-test
```

Fifteen tests passed at authoring: the original ten terminal/child/environment
regressions plus allowed numeric statistic drift, refused configuration/capacity
changes, duplicate/malformed statistics, exact saved read-command validation and
eligibility for this single known failed run. Adversarial cases use subtests.
Compilation also passed. Installed/native execution remains parent-owned.

This contributes prerequisites to UX-03, IMP-07 and REL-03 only. Declaration
binding does not prove historical first assignment, file provenance or fresh
initialization. `completeCaptureVerified`, `independentRecoveryVerified` and
`guestBootVerified` remain false. No snapshot/backup recovery, TPM hardware,
console or release acceptance is claimed.
