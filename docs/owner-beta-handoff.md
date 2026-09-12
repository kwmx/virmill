# Owner beta handoff — updated 12 September 2026

**Virmill 1.0.0-beta.1 is installed and running on the authorized Fedora 44 test
VM.** This is the owner-test beta requested within one day. Complete 1.0 remains
unqualified: all 71 acceptance scenarios remain mandatory, with 2 accepted,
57 in progress and 12 not yet implemented in the full release tracker.

**Current UI update:** installed CLI and coordinator use `e8965ca`.
Import and creation now save choices across restart, including advanced settings,
with clear Resume/Start new controls. The unified OVA/ISO/disk import, hardware
controls, guided guest-agent connection setup and saved backup receipts remain
available. A real Fedora 44 guest passed missing-agent installation, verification,
native agent ping and a second run that skipped installation. The installed
80×24 TUI passed basic draft edit/restart/resume/Start new checks. Existing guests,
jobs and source media were preserved. See [current evidence](evidence/wizard-drafts-tools-run.md)
for hashes and limits. TUI guest-tools submission, other guest profiles/desktop
integration and complete release qualification remain open. Exit any open TUI
and run `virmill` again.

One historical fixture job remains recovery-required because the previous build
rejected its own agent connection change after installing packages. Its owned
guest `virmill-tools-f9c6c7b4` is retained running for investigation. The corrected
fresh test guest `virmill-tools-71001e0c` is stopped. Preserve these test records;
the old job is not evidence of a new failed installation in the current build.

## Start testing now

From your computer:

```sh
ssh virmill-test@virmill-test.home
virmill version --output json
virmill
```

The ordinary user's `virmilld.service` is running. It has not been enabled for
automatic startup. If it is stopped or the VM reboots, use
`systemctl --user start virmilld.service`. Run the CLI and TUI as `virmill-test`,
not with sudo. The separate privileged helper remains inactive and its existing
policy is unchanged. Workflows requiring it need their documented exact grants.

Use the [beta walkthrough](beta-testing.md), [generated CLI reference](cli-reference.md),
[guest recipes](guest-recipes.md), and [backup/recovery guide](local-backups.md).
Installed copies are under `/usr/share/doc/virmill`, with examples and the plugin
SDK under `/usr/share/virmill`. Original user media remains under `~/images`.
Start with new disposable guests; preserve the retained qualification guests,
source images and recovery artifacts.

## Original beta build (historical)

Source revision: `1eccfc4fdb051ac6262db8cd6d01094b7133239a`.
Implementation-tree digest: `ca79f6b609cd61a68af905a7a367bdfd15836ade6b8614df378d635916b5c8e4`.
Go 1.27.1, vendored dependencies, offline native Linux amd64 build. Both build
passes produced identical binaries and RPM/DEB packages. No release was published.
The unsigned beta artifacts are in `build/beta-owner-release/dist` in the source
workspace, alongside `owner-beta-checksums.json` and the frozen source archive.

| Artifact | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.1.x86_64.rpm` | `b81d6be8ea2a0cfb7af3648b5836c1abac01e1936b81d38587080b7382451c8c` |
| `virmill-host-helper-1.0.0-0.beta.1.x86_64.rpm` | `6b526a53d7da3becbdffbb7f7c75947f2db4deb084f40d43b052d712711a225b` |
| `virmill_1.0.0~beta.1_amd64.deb` | `8f230410dd8a1c1ea3f0442ada1eff49e725a557f4867a47857e7c26cddff985` |
| `virmill-host-helper_1.0.0~beta.1_amd64.deb` | `b491824efbf618eaceccbdbf922349fcc566d4f778f553e602e961e9cce8acd5` |
| `virmill-1.0.0-beta.1-source.tar.gz` | `9ed7c951792cb773d8c66ad94bc0e4fef985bea554602ea2ba6fd053fa3ca980` |
| installed `/usr/bin/virmill` | `7b8c89660590d55c1d413c41a252a94673f4b710bb4a8c11eecf5ea244f80a1a` |
| installed `/usr/bin/virmilld` | `99ad4f7bae1861af2287def6cc106ad14365a181b7e6ed5755fc20839611a1c4` |
| installed `/usr/libexec/virmill-host-helper` | `bd1b8de9deb3c7d9018a6c2fad4e056f8d08f7ae4caeb5f0d5da693e3b72d3e2` |

Remote RPMs, checksums and installation results are retained in
`~/virmill-tests/beta-owner-1eccfc4`. The coordinator reports the exact revision
above and `releaseQualified: false`.

## What passed

- Final integrated race suite: **804 top-level tests**, 4,710 passing test records,
  zero failures. There were 26 explicitly skipped opt-in probes; skipped work is
  not accepted evidence. Final static analysis passed.
- Final native SSH transport: held executable digest/mismatch handling, sealed
  credentials, strict host-key refusal, literal arguments and remote exit 3.
  This used the authorized VM host, not a nested guest provisioning workflow.
- Package structures, staged installer/uninstaller preservation and real private
  daemon/CLI/TUI integration: all three integration tests passed without skips.
- Actual Fedora RPM upgrade from the older development packages. Installed
  executable hashes match the frozen build; version, doctor, host capabilities,
  VM/pool/network inventory, USB discovery, jobs and recipe validation passed.
  The installed 80×24 TUI started and exited through keyboard input.
- Encrypted full two-disk backup, repository data check, recovery with an empty
  journal, new independent BIOS guest restore and actual second-disk boot marker
  passed at `b78cc23`. Those backup/restore implementation files are unchanged in
  this final revision; this is scoped native evidence, not a rerun of every guest
  or firmware matrix. See [recovery evidence](evidence/beta-recovery-run.md).

The first install fixture used an incorrectly transcribed expected revision.
Package installation and binary hash verification had already succeeded; the
fixture then failed its version assertion. The unchanged failed evidence remains
in `beta-owner-install-native-001`. The correct full revision was verified in
`beta-owner-install-native-002`, without reinstalling or copying a running
journal. Guest/network definitions, source-media metadata and the inactive helper
policy were preserved in both checks.

The full release gate was also run (`beta-owner-full-release-check-001`) and
correctly remains blocked on 69 unaccepted scenarios and the unchecked shipping
checklist. The beta handoff does not override that result.

## Contributions and remaining gaps

Three agents supplied guest recipe CLI/TUI tests, operator documentation and a
focused transport/service integration review. Root integrated the slice, fixed
executable digest and connection binding, ran final verification and exclusively
coordinated remote changes. The UI contribution passed 55 focused test records;
these deterministic tests do not prove guest hardware behavior.

The beta offers integrated image preparation and VM creation, lifecycle and
configuration editing, managed network workflows, durable jobs, cold BIOS capture
and new-identity restore, encrypted local backup/recovery, reviewed existing-guest
SSH recipes, and the plugin protocol/SDK tools. Their detailed support boundaries
are in the guides and evidence matrix.

The complete 1.0 checklist still requires implementation or qualification for
full lab application/teardown, USB/PCI attachment and reconnection, physical bridge
adoption/rollback, remaining isolation/routing profiles, folder/viewer workflows,
backup schedules/retention, identity-preserving restore, firmware/TPM restoration,
and the full guest/source/OS and installation matrices. No physical hardware
claim follows from the synthetic guest or process fixtures. See the
[requirements-to-evidence matrix](requirements-to-evidence.md) for all 71 IDs.

`.gitignore` keeps generated packages, builds, media, local test access/state and
credentials out of Git; no tracked file matches the ignore rules. Vendor source,
fixture recipes, operator docs and deliberate release evidence remain tracked.
