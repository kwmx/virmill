# Disposable auxiliary inventory RPM upgrade 001

This is a prepared, single-use recipe for parent-only execution on the already authorized disposable VM as ordinary `virmill-test`. It has not been executed on that VM by its author. Authoring and `--self-test` do not qualify installation, helper authentication or any native workflow. The only permitted run root is `/home/virmill-test/virmill-tests/run-65930c6-20260907`.

The recipe installs exactly two development RPMs and replaces this run's ordinary coordinator. It does not start or change a guest, invoke auxiliary inspection, edit helper policy, start the helper, reconcile a job, read private key bytes or open TPM/NVRAM payload bytes. Acceptance contributions are REL-01 upgrade-preservation, REL-03 installed help/reference and SEC-01 inactive-helper/authority-preservation prerequisites only. All release, capture and independent recovery flags remain false.

## Frozen input and old runtime pins

The new runtime is `490b88cba5bf6e0837e2cb5780e56211c22a7d27`, implementation source digest `26e4f8410395a37606d577073d0bdff424610ae04b83df678e61d618ae38eb06`, and tracked input inventory SHA256 `f0585986ff6693a306a54e86d48db9eecbed5cddca83941d461d3e2bb2727576`. The parent supplied new deployment SHA256 `5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c` after its separate frozen reproducibility/package checks.

The parent prepares a new exclusive `ROOT/packages/490b88c/` containing only `deployment.json` and these two RPMs. The recipe requires the full revision and digest arguments to equal the reviewed constants, then checks the manifest bytes, exact source digests, each RPM's SHA256 and RPM name/version/release/architecture. It refuses scriptlets/triggers and any package target outside the fixed executable/unit/documentation/schema/SDK/example paths. No dependency installer or package resolver is invoked.

| New artifact | SHA256 |
|---|---|
| `virmill-0.0.0-0.dev.x86_64.rpm` | `3c5340f1991df4c002cce5f81e4ede021fa1a3bd1f38d2ebad4f252dd14f1335` |
| `virmill-host-helper-0.0.0-0.dev.x86_64.rpm` | `422bd189747c0e051939dcb0be2e448d98918069854e4810ae4f267fd426cc13` |
| `/usr/bin/virmill` | `9cc64d8aa14e7f5d5992514910364f316d3d12fab784393630d7c160628ac886` |
| `/usr/bin/virmilld` | `44522f6cbe2dd7eb54134038adfee4bd650ced9e4ae5aafe4e042471158cd7aa` |
| `/usr/libexec/virmill-host-helper` | `4d67ec32a0aa891d16af237885fe45ba95ee5377829f86ab4761aba89b42917a` |

The previous deployment is exactly `dc2ab1a7af8c023c1485d36d0e858a65264ace3e`. Its existing `ROOT/packages/dc2ab1a/deployment.json` must hash to `c0b4ef70f038291e4f4803e7934e660b740654c2e2e660bc3784cb86939914f9`. Public old runtime hashes are pinned in the recipe and checked against both that manifest and installed bytes:

| Old executable | SHA256 |
|---|---|
| `virmill` | `50228ee2df4b54885aa85cf95740b9a126700ac031d10e40181c599deef1ad9d` |
| `virmilld` | `75c1390cc0cf259cf2243ac81ebd52b8e276a7e3611105e7d2ecfa577d00e25b` |
| `virmill-host-helper` | `f6d7eef0cb186b3b18356604d577b39810b0e879942ea4c4d6b4a4f4f828990a` |

The old coordinator must still be `virmill-test-dc2ab1a.service`, active/running with PID `27851`, the exact working directory, ordinary owner, `/usr/bin/virmilld` command line and old executable SHA256 through `/proc/27851/exe`. Its PID/start-time identity is rechecked immediately before stopping. An expired or replaced old unit refuses preflight; this recipe never revives it or chooses another coordinator. `rpm -V` and installed CLI version must also match before installation.

## Preservation baseline

The source of the old baseline is the successful [NVRAM binding TUI-003 recipe](nvram-binding-tui-003.md), its saved `report.json` and `journal-after.json`, plus the public `docs/evidence/logs/nvram-binding-tui-003.log`. The report identity, old PID/binary pins and preservation flags must match. All six durable SQLite tables are read in one query-only transaction, with exact full cells/BLOB bytes and row order compared with that saved snapshot. Counts must remain 36 plans, 32 jobs, 24 metadata rows, 177 events, 32 dedup rows and zero locks. Every job must already be terminal, user_version must be 3, no unknown table may exist and zero triggers may exist. The complete current schema and rows must remain exact after upgrade; no migration is expected.

Exactly these seven guest UUIDs are required; their inactive XML hashes are pinned in the Python fixture to TUI-003's public values:

- `2ec994ce-2950-498c-8b19-d2f7dbb53a78`
- `666c692d-da0e-4119-9554-727c4af3c751`
- `a19bf9ee-cd7f-4921-baac-39ce1694eb35`
- `ae630461-91d3-4f07-ad88-e6842c3dc3ea`
- `b9496482-2eeb-40e1-892b-4e291c108c52`
- `d4c95f21-28bc-428d-9e5f-ceda025d279e`
- `f78674f3-bf3a-43e5-81f9-4283e2472024`

Read-only virsh calls observe each guest stopped on both sides of its inactive XML read. Extra, missing, active or changed definitions refuse the recipe. Only disk/CD-ROM source declarations are resolved for metadata observation, including read-only `vol-path`; TPM and NVRAM paths are not used as byte inputs.

A fixed read-only `sudo python3` observation records device/inode/mode/owner/link-count/size/mtime/ctime for `ROOT/sources`, `ROOT/prepared`, the owner-supplied `/home/virmill-test/images` tree, and declared disk/media files within this run or `/var/lib/libvirt/images`. It recursively checks bounded source/prepared/supplied-media trees, refusing symlinks or special files. These are metadata preservation checks; source/guest disk content is not rehashed. It observes `ROOT/config/virmill/helper-key.pem` with O_PATH/stat only, never a readable open or hash.

The same observer hashes the public `/etc/virmill/helper-policy.json` and the known disposable socket drop-in, plus bounded UUID-named JSON/completion records directly inside `/var/lib/virmill-host-helper`. Other helper journal entries are refused before their content is opened. Journal and policy hashes are retained privately without exposing their content. No helper key or native firmware/TPM payload is eligible for those hash reads. Helper service and socket must stay inactive. No system daemon-reload, socket activation, ACL, policy, device or VM command is issued.

## Parent execution and failure boundary

The parent uploads the actual Python file at a new source path; do not feed it through stdin because the fixture hashes `__file__`. After reviewing the recipe and its digest, the parent alone executes on the authorized VM:

```sh
/usr/bin/python3 -u sources/auxiliary-inventory-upgrade-001/auxiliary-inventory-upgrade-001.py \
  --revision 490b88cba5bf6e0837e2cb5780e56211c22a7d27 \
  --deployment-sha256 5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c
```

The new private output directory is `ROOT/auxiliary-inventory-upgrade-001/`. Its prior existence, including a failed attempt or symlink, permanently refuses this recipe. Files use exclusive creation and fsync; the output directory entry and intent are synced before changing the coordinator or packages. `upgrade-intent.json` retains old/new identities, exact package hashes, old PID, journal counts and guest hashes. A separate `install-attempt.json` is persisted immediately before the only install call.

The only runtime/package mutations are, in order:

1. Stop exactly `virmill-test-dc2ab1a.service` after rechecking its observed identity; require inactive/PID 0 and unchanged journal rows.
2. Recheck the two RPM inputs and run `sudo -n /usr/bin/rpm -Uvh --replacepkgs` with exactly their two fixed paths. Verify RPM ownership/integrity and all three new executable hashes, including the inactive helper.
3. Create exactly `virmill-test-490b88c.service` with `systemd-run --user`, `RuntimeMaxSec=7200`, `Restart=no`, this run's working directory and the five verified private XDG directory values. The unit must not already exist.

Private XDG values apply only to Virmill and the explicit new-unit environment. `systemctl --user`, systemd-run and virsh retain the ordinary host environment. The actual new PID, start time, owner/cwd/command and `/proc/<pid>/exe` SHA256 are verified, plus installed CLI revision, generated command help and the installed `/usr/share/doc/virmill/cli-reference.md` entry for `virmill vm recovery auxiliary inspect`. The metadata command itself is not invoked. The old unit stays inactive and the new unit is left running within its two-hour cap after success.

On failure the recipe retains its stage, intent and command output. Where a baseline exists it attempts independent read-only journal/guest/protected observations and records any additional observation failures. It never repeats installation, restarts the old runtime, deletes a guest, clears a lock, modifies a policy or repairs an uncertain result. If the RPM command or its sudo wrapper times out, actual package completion is uncertain; a retained failed report is not permission to retry. Only this recipe's unreaped subprocess is terminated on a command timeout, with no process-group or daemon signal. The parent must inspect package state and both fixed units before proposing a new reviewed action.

Commands are bounded to 30 seconds normally, 120 seconds for the one RPM invocation, 2 MiB output per command, 32 MiB aggregate, 180 commands and ten minutes overall. Initial startup wait is ten seconds. Journal reads have a five-second progress deadline and 32 MiB cell bound. Protected inventory is capped at 10,000 entries, disk declarations at 32, public/journal content reads at 8 MiB each, executable reads at 128 MiB and RPM reads at 256 MiB. Observations are endpoint comparisons, not an atomic freeze excluding external writers.

## Local authoring checks

Only this safe authoring mode was run locally:

```sh
python3 tests/fixtures/protection/disposable-recorded-run/auxiliary-inventory-upgrade-001.py --self-test
```

Eleven checks passed at authoring. They cover exact revision/source/deployment identities, duplicate/nonfinite JSON, canonical path input, exact unit/start arguments, package path boundaries, prior report identity/counts, saved BLOB decoding, generated regular-file/ancestor-symlink refusal and single-use output preservation. The fixed privileged observation program is compiled as text only. No native command, package install, coordinator, key, database, SSH or protected host path is used by self-tests. Final syntax and whitespace checks are part of the authoring handoff; actual execution and public evidence recording remain parent-owned.
