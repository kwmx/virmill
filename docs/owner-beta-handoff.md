# Owner beta handoff — updated 13 September 2026

**Virmill 1.0.0-beta.3 is installed and verified on the authorized Fedora 44
test VM.** This is an owner-test pre-release, not a certified 1.0. All 71
acceptance scenarios remain mandatory, with 2 accepted, 57 in progress and 12 not
yet implemented in the full release tracker.

**Current beta:** `1.0.0-beta.3`, build revision `0627800`. It fixes the DEB
packages, which now install on Debian 13 and Ubuntu 24.04 (container-tested), and
returns an unused Import started from the VM list to the list on Esc. The in-place
upgrade from beta.2, CLI and 80×24 TUI smoke test, TUI workspace, Jobs → Activity,
network form and source detection walkthroughs passed. Two clean builds were
byte-identical. See [beta.3 verification](evidence/beta3-release-run.md) and the
earlier [beta.2 verification](evidence/beta2-release-run.md), which also covers the
generated SPICE creation fixture `virmill-spice-e79f41dbb545`, retained stopped.

**Remove VM** offers unchecked disk choices, exact-name confirmation and a
separate review. The CLI supports `--delete-disk vda` (repeat or use a comma list).
Native CLI one-disk deletion and actual 80×24 TUI two-disk deletion passed on
generated BIOS fixtures. Shared-disk refusal, cancellation, CLI/TUI plan parity,
exact native absence and source preservation passed. Existing system guests and
all historical jobs were preserved. See [selected-disk evidence](evidence/disk-removal-run.md)
and [removal instructions](vm-removal.md).

The previous **Jobs → Activity** update remains included.
**Jobs → Activity** opens the selected job directly with full scrollable messages,
UTC times, paging and labeled Refresh/Back buttons. **Checked** confirms a
successful refresh. Read failures keep earlier events and show the full issue.
The actual 80×24 CLI/message comparison, refresh and return-to-job test passed;
existing guests, all 32 jobs, networks and media were unchanged. See
[Activity evidence](evidence/job-activity-run.md). The prior runtime evidence remains scoped to its recorded build.

**Disks and boot** maintains a complete order when you move a device.
**Attach only** excludes installer media from booting and closes the numbering gap;
re-enabling it inserts it back into the order. Prepared ISO sources receive the
host-supported SATA suggestion; saved controller choices remain unchanged.
The real 80×24 wizard edit/continue/draft-preservation test passed with existing
guests, jobs, networks and media unchanged. See [boot-order evidence](evidence/creation-boot-order-run.md).

VM setup's Networks step now offers **Create network** and **Refresh networks**
while preserving CPU/RAM, disks and adapter choices. Native cancel/refresh and
CLI/TUI plan comparison passed. Network submission exposed missing helper setup;
no job or network was created. Complete native network handoff remains blocked
pending explicit helper authorization. Submission errors now appear in full at
the top of the scrollable review with next steps, rather than only in the footer.
The actual 80×24 error/recovery display and unchanged-resource checks passed.
See [network handoff evidence](evidence/creation-network-handoff-run.md).

**VMs → More → Remove VM** keeps disks by default. Selected
deletion requires stopped BIOS VMs, registered raw/qcow2 file volumes and a
complete readable reference graph. Firmware/TPM and other protected layouts
remain unsupported for removal. Backups and unselected files are retained.
See [removal instructions](vm-removal.md) for limitations and failure recovery.
**VMs → More → Change VM automatic startup** now shows current settings and a
simple toggle with a separate before/after preview. Actual TUI/CLI plan comparison
and native enable/restore-to-Off jobs passed without starting or stopping the
guest. See [automatic startup evidence](evidence/autostart-guided-run.md).
**Networks → Create network** offers simple purpose/subnet choices, advanced
access controls, export, and a complete scrollable error reader. Actual NAT,
isolated-lab and guest-only TUI previews passed and matched CLI plans. Back
retains settings and cancellation preserves existing resources. The two retained
test networks' missing allocation records were recovered from their original
verified evidence. See [network form evidence](evidence/network-form-run.md).
If the coordinator is unavailable, Overview and Settings now explain how to
start it, offer **Retry connection**, and keep optional sign-in setup under
**More help**. The test VM's service is enabled at sign-in. See
[startup help](coordinator-startup.md).
Completed local VM jobs offer **Open VM** when their saved result identifies an
existing VM; removal instead reports the selected deletion or retained disks.
Guest tools checks runtime state first and offers reviewed start guidance for
stopped guests. Prose and review warnings wrap by words. See [job/setup evidence](evidence/guided-job-completion-run.md).
CPU/RAM settings show live and next-boot values, prefill editable fields and review
exact before/after changes. Unsupported layouts explain their limits; running
guests receive shutdown guidance. The preceding native stopped-VM TUI edit and
readback passed; see [resource settings](evidence/observed-resources-run.md).

New creation forms now suggest **Local display (SPICE)** when advertised by the
host. The selected display and desktop requirement appear before creation; saved
VNC/none choices remain unchanged. Actual generated ISO preparation, creation,
boot marker, TUI console selection and a private graphical viewer connection
passed at their recorded levels. The viewer closed without stopping the guest;
the fixture then stopped it through a reviewed operation. See
[creation/console evidence](evidence/spice-creation-run.md), including a corrected
viewer-process test observation and the retained original failure.

Import/creation retain resumable choices and advanced hardware controls. The
previous real Fedora 44 guest-tools install/repeat/ping evidence remains in
[guest-tools results](evidence/wizard-drafts-tools-run.md). Actual Fedora TUI submission, idempotent verification, native ping and opening
the resulting VM now passed; see [current TUI evidence](evidence/guided-job-completion-run.md).
Other guest profiles, desktop-sharing opt-ins and complete release qualification
remain open. Graphical viewing needs a desktop session; a plain SSH
terminal can use configured serial access. Exit any open TUI and run `virmill`
again to load the update.

The new generated ISO fixture `virmill-spice-8c6fbc43309d` is retained stopped,
with its receipt-bound managed media. It contains only a test boot marker, not an
installed operating system. Existing guests, jobs and source media were preserved.

One historical fixture job remains recovery-required because the previous build
rejected its own agent connection change after installing packages. Its owned
guest `virmill-tools-f9c6c7b4` is retained for investigation; the host reboot
changed runtime state, so inspect it before continuing recovery. The corrected
fresh test guest `virmill-tools-71001e0c` is stopped. Preserve these test records;
the old job is not evidence of a new failed installation in the current build.

## Start testing now

From your computer, using the test VM's login and host from the private
`.virmill-local/test-host.json` (never committed):

```sh
ssh <test-vm-login>@<test-vm-host>
virmill version --output json
virmill
```

Current unsigned RPM/DEB packages, the source archive and `SHA256SUMS` are
retained in `build/beta3-delivery/`. The installed build revision is
`0627800b70d6e04e826df72d7dc0e257c1c0d7df`, and `virmill version` reports
`1.0.0-beta.3`. The beta.2 packages in `build/beta2-delivery/` are superseded;
their DEBs do not install on Debian or Ubuntu.

| Current artifact | SHA-256 |
| --- | --- |
| `virmill-1.0.0-0.beta.3.x86_64.rpm` | `fa75f7658731b3087aab1762833dec644204d8039d8a4003929982e55b9ecfae` |
| `virmill-host-helper-1.0.0-0.beta.3.x86_64.rpm` | `fe3a154d8b50cd31db47876de1c51c854b988bc3457fb73e7f7e802295c41986` |
| `virmill_1.0.0~beta.3_amd64.deb` | `8c0d005b6eddef4dda9e6ace1305edb3aa648835d19e09025faff8de355e32ea` |
| `virmill-host-helper_1.0.0~beta.3_amd64.deb` | `0707956630e810902968aa2a71a3d51b2051f00c6c6104e3d5b2f36b3f14eae0` |
| `virmill-1.0.0-beta.3-source.tar.gz` | `e8fff234771fd97ff2f3973ea3702cc36b9698d7653da0441690f3e5b93453af` |
| installed `/usr/bin/virmill` | `eaa0379aeb6982c28ec7237e78b64fcfcd51c441877e1800ffe08916263ccfa5` |
| installed `/usr/bin/virmilld` | `4926c3198380c77647acd3dedfbaf0a53026fc135d4de2684c578488a4e83430` |
| installed `/usr/libexec/virmill-host-helper` | `2321037e07a666798fbb896c9aad9a3f120e11f16bedd4fc2dae8eb71e15904e` |

The ordinary user's `virmilld.service` is running and enabled at sign-in.
If it is stopped, use
`systemctl --user start virmilld.service`. Run the CLI and TUI as `<test-vm-login>`,
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
