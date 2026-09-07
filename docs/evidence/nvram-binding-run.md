# Creation NVRAM declaration binding evidence

This implements [ADR 0021](../adr/0021-creation-nvram-declaration-binding.md)
as a prerequisite for IMP-07, SNAP-01 and JOB-02. Shared CLI/TUI result and review
coverage also contributes to UX-03 and REL-03. No acceptance scenario is promoted.

New UEFI recipes require a versioned, durable declaration binding before marking
definition complete. The record ties the original plan, input, operation and VM
identity to the reviewed firmware digest and observed NVRAM mapping. Repeated
observations cannot replace it. Legacy fieldless recipes retain their historical
semantics and visibly report `legacy-unbound`. Declaration binding opens no
firmware/TPM state file and does not verify initialization, freshness or recovery.
The [operator guide](../creation-nvram-binding.md) documents these stages.

## Integrated checks

`nvram-binding-core-001` passed the complete Go race suite with private IPC and
generated disk tools explicitly required. Its recorded source digest is
`92fe1cfeed2f61dddad9872fbec73cd84eaec69b155ed012fecd6989c0de8cae`;
HEAD was `4c5817674e8c036081715237ad1e768869f377c5` with the binding changes
in the working tree. The log records its actual revision and input digest.
Native VM creation and firmware execution were not part of this local check.

The lifecycle tests exercise the real creation service, engine and private SQLite
journal with injected native observations. They cover definition acknowledgement
loss, reopen/reconciliation, first binding, identical repeats, changed mapping,
publication failure, cancellation, legacy recipes and missing required records.
The validator tests cover complete exact-case JSON shape, identity and digest
bindings, firmware mappings and canonical paths. CLI/TUI tests use the same
application service and journal; mutating backend fixture calls refuse execution.
They cover stored review reuse, required acknowledgements, canceled approval,
progress/error/legacy results, discarded stale success and machine output.

Two issues found during integration were corrected: a persisted cancellation
request could be overlooked during inspection, and recovery/result reads did not
recheck immutable plan/input digests before interpreting the recipe version.
An active canceled inspection now remains uncertain. A later explicit reconcile
may observe an unchanged accepted recipe even after its original expiry, retaining
the cancellation history. Changed plan or recipe bytes fail before observation.

The subsequent compatibility review reproduced a negative journal-step panic in
`nvram-reconcile-negative-step-001`. That failed evidence is retained. The parent
added the missing lower-bound check so malformed steps return an error before a
handler call. Post-fix compatibility and final integration results are recorded
separately; the earlier passing full suite is not evidence for this later fix.

`nvram-result-child-integrity-001` reproduced a separate result-read gap: a
successful recovery child could still return completion after its own plan or
recipe changed, because only the referenced original was checked. Result reads
now check both. `nvram-result-child-integrity-003` passed both race regressions,
including unchanged native call counts, binding bytes, child job and locks, and
successful reads after restoring the exact original journal bytes. The intervening
`-002` failed in the test's restoration step: the test serialized its caller-owned
plan after Apply had sorted the shared acknowledgement slice. The fixture now
restores the raw saved body; no production workaround was added for that test bug.

`nvram-recovery-integration-001` then passed the combined creation binding,
validator, child-result and common recovery race regressions, including the final
recovery-review fields. `nvram-binding-vet-001` passed static analysis of all core
packages. The [package-input audit](../reviews/repository-hygiene-20260908.md)
also led to tracked-file-only source selection, no-follow file handling and exact
output inventories. `nvram-package-inputs-001` passed all 20 temporary Git/worktree
and generated package-orchestration tests. Its RPM subprocess is simulated;
actual RPM/DEB rebuild and installation evidence remains separate.

## Frozen build and installation

The immutable runtime checkpoint is
`dc2ab1a7af8c023c1485d36d0e858a65264ace3e`, with implementation source digest
`61431ca6259ec10ce4e36fe506b8e69496993990db457e0b6a3b5128dcc319e2`.
The [full tracked source inventory](environments/source-dc2ab1a.json) records
1,974 regular files, including the documentation, examples and vendor inputs
outside that implementation digest's historical scope. Separate
[core](environments/package-dc2ab1a-virmill.json) and
[helper](environments/package-dc2ab1a-virmill-host-helper.json) install manifests
bind the exact packaged files. The ledger records their hashes.

`nvram-binding-repro-001` passed two offline builds with identical hashes for
all three binaries and four native RPM/DEB packages. `nvram-binding-packages-001`
passed the three actual package/staged-install/private CLI-daemon workflow tests.
`nvram-binding-cross-001` compiled the common packages, SDK and example for the
recorded non-Linux targets; it did not run those targets.

`nvram-binding-frozen-core-001` passed the full Go race suite with private IPC,
confined workers and generated disk tools required. Its log contains 1,976
passing test/subtest records and two skips: the separate privileged root-access
fixture and opt-in host-prefix smoke check. It executed generated confined Go
and Python plugins. Injected VM observations do not verify physical hardware.

`nvram-binding-upgrade-001` installed the exact development RPMs on the authorized
Fedora disposable VM. Installed binaries, the active coordinator's executable
and package hashes matched. All prior 31 plan/job histories and associated
journal rows, six stopped definitions and selected probe disk hashes were
preserved. The old creation result remained visibly `legacy-unbound` with
initialization false. This is one development upgrade, not the full supported
installation/distribution/recovery matrix.

## Native declaration experiment

The runtime remained the frozen `dc2ab1a` deployment. Test recipe revisions are
recorded separately; they were not silently represented as rebuilt product code.
`nvram-binding-native-001` failed during read-only preflight because the installed
`virsh vol-list` does not support `--name`. No new plan, job, guest, lock or fault
trigger was created. The corrected table parser passed 29 synthetic checks in
`nvram-binding-native-parser-002`. Both original files and failed logs remain.

`nvram-binding-native-002` created one new, never-booted, no-NIC guest using the
existing original EFI probe source and a distinct managed disk copy. Its VM UUID
is `666c692d-da0e-4119-9554-727c4af3c751`, original plan
`2bd38ffb-6931-493b-bd01-5b56c68ff574`, and operation
`8cafa2bf-f148-4189-bf93-34340f1c432a`. The following boundaries were observed:

- A first NVRAM declaration A became durable before a narrowly scoped SQLite
  trigger rejected publication of the definition receipt. The job remained
  uncertain with its retained resources and three locks.
- Explicit repeated observation of A retained the exact original binding and
  historical fingerprint and encountered the same receipt-publication failure.
- After removal of that exact trigger, changing only this new guest's declared
  NVRAM path to B caused explicit reconciliation to refuse `SOURCE_CHANGED`.
  The original A binding, receipt, disk and locks remained.
- Restoring the exact saved XML A and reconciling the same operation succeeded.
  The final creation result reported complete definition, declaration bound and
  initialization false. The original allocation identity, generation, content
  and pool volume inventory remained, and all operation locks were released.

The recipe still exited **failed** at its final pool assertion: it compared active
pool XML byte for byte across creation, including allocation and available-space
counters. Saved command responses 047 and 388 differ only in those counters:
allocation increased from 145265324032 to 145318100992 bytes, and available space
fell from 126764650496 to 126711873536 bytes. Capacity stayed 272029974528 bytes;
all other pool XML bytes stayed identical. These are filesystem space observations,
not an attribution of that entire delta to the new disk. The final runtime recheck
was after the assertion and did not execute. The separate read-only observation below confirms the retained successful
operation and installed runtime. The failed native run remains failed and was
not rerun against another guest.

The failure cleanup removed the scoped trigger and observed zero locks, all
prior journal rows unchanged, and all six earlier stopped XML definitions
preserved. All seven guests remain stopped. Declared disk/media metadata and
selected public EFI/probe content hashes were retained; larger earlier guest
disks were checked by metadata, not rehashed. No TPM/NVRAM payload was opened.
The injected SQLite failure is not process-crash or power-loss evidence.

## Installed CLI/TUI supplemental confirmation

`nvram-binding-tui-fixture-003` passed 15 synthetic checks before native use.
`nvram-binding-tui-003` then **passed** on the exact installed `dc2ab1a` binaries
and unchanged coordinator PID. This separate single-use, read-only fixture
accepted only native-002's exact known final assertion failure, retained its raw
failed report and hash, and independently compared saved and current pool XML.
Only allocation/available numeric text was excluded from the exact configuration
comparison; capacity, identity, source, target, permissions and every other byte
remained part of the check. It attributed no filesystem-space delta to one disk.

Actual `operation show` confirmed the same succeeded job. `plan show` and
`vm creation result` agreed in JSON, table and NDJSON formats. Two separate
80×24 installed TUI clients displayed the same shared responses: **20 current
plan pages and 10 current result pages** matched the CLI envelopes. Comparisons
used reconstructed current screens, not accumulated transcript searches. No apply,
reconciliation, console attachment or guest lifecycle request was submitted.

The operation retained the exact first binding SHA256
`2aaf64781d9c74e8e6e1873d444126c99bf8d93e3badfd702a5030bfe37f948e`,
original path and historical fingerprint. The result reported definition complete,
declaration bound, and initialization/guest boot/setup/connectivity false. Before
and after journal snapshots were identical: 36 plans, 32 jobs, 24 metadata rows,
177 events, 32 dedup records and zero locks. Both read transactions found zero
triggers. All seven stopped guest definitions and state observations were unchanged.
The native-002 failed result remains in the ledger; this supplemental pass does
not rewrite it or claim a process-crash/restore test.

## Remaining qualification

The earlier two-boot TPM/NVRAM marker results are described in
[the native firmware run](cold-fixes-native-run.md). They do not prove new file
freshness, initialization, capture or restore. The creation record means first
**durably recorded declaration**, not historical first assignment or file creation.
No native method counter was collected; unchanged receipt, volume generation,
content and XML are the observed preservation evidence.

Complete auxiliary capture, durable complete-set publication, encrypted backup,
independent restore, guest encryption recovery and the remaining native matrices
are still required. All 71 acceptance scenarios remain unaccepted.
