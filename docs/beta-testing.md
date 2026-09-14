# Owner beta testing

Virmill `1.0.0-beta.4` is an owner-test development beta, not a certified
Virmill 1.0 release.
All 71 acceptance scenarios remain required. This guide supports documentation
parity (REL-03) and the install/release evidence handoff (REL-01, REL-04); it
does not mark those scenarios passed. See the [beta decision](adr/0030-beta-delivery.md)
and [support matrix](support-matrix.md).

The beta handoff includes the frozen revision, package and executable SHA-256
values, deployment metadata, and links to the tests actually run.
Match those values before installation. An unsigned development package and a
successful build are not release qualification. There is no published download
URL. Use only an explicitly designated disposable host and media you may use;
preserve existing guests, source images and their backing files.

## Install and start

The current package names below come from `scripts/package.py`. Select the
format for the designated host, from the directory containing the reviewed
artifacts. Compare `sha256sum PACKAGE_FILE` with the handoff and `checksums.json`
for each file first. These commands are installation instructions, not evidence
that the complete Fedora or Debian/Ubuntu install matrix has passed.

| Variant | Local install command |
| --- | --- |
| RPM core | `sudo dnf install ./virmill-1.0.0-0.beta.4.x86_64.rpm` |
| RPM helper, when needed | `sudo dnf install ./virmill-host-helper-1.0.0-0.beta.4.x86_64.rpm` |
| DEB core | `sudo apt install ./virmill_1.0.0~beta.4_amd64.deb` |
| DEB helper, when needed | `sudo apt install ./virmill-host-helper_1.0.0~beta.4_amd64.deb` |

Downloads from a GitHub release use `.` instead of `~` in the DEB file names
(for example `virmill_1.0.0.beta.4_amd64.deb`), because GitHub does not allow `~`
in asset names. The package version inside is still `1.0.0~beta.4`; use the
release's `SHA256SUMS`, which lists the downloaded names.

Versions after 1.0.0-beta.3 can install newer releases with `virmill update`;
see [updates](updates.md). 1.0.0-beta.3 and older are updated by hand.

The core contains `virmill`, `virmilld`, the user service, documentation under
`/usr/share/doc/virmill`, and examples under `/usr/share/virmill/examples`.
The separate helper contains the privileged executable and system units.
Installing it grants no actor permission and supplies no active policy.
An administrator must separately review [helper setup](managed-volume-access.md),
[network grants](network-helper.md), and any
[auxiliary-state authority](auxiliary-state-inspection.md). Never run the CLI,
TUI or image preparation as root. Do not disable host security or broaden grants
to turn a refusal into a pass.

As the ordinary coordinator user, start one coordinator, then inspect:

```sh
systemctl --user daemon-reload
systemctl --user start virmilld.service
virmill version --output json
virmill doctor
virmill host capabilities --connection qemu:///system --output json
virmill storage pool list --output json
virmill vm list --output json
virmill tui
```

Alternatively, run `virmilld` in a foreground terminal and the CLI/TUI in another
with the same user and XDG environment; do not also start the service. These
examples use the default local `qemu:///system`. Some VM workflows also accept
`qemu:///session`; network creation and privileged capture require system mode.
Keep the selected connection consistent across planning, applying and inspection.

Bare `virmill` opens the resource workspace. Select a VM row and press Enter for
its details; Tab focuses visible action buttons. `a` opens the current section's
actions and `:` shows the full action catalog. Use labeled forms for common
workflows; advanced file import is optional when that form offers it. Read the current
[TUI navigation guide](tui-navigation.md) for focus, search, review and terminal
options. CLI and TUI use the same shared service.

## Review every mutation

Uppercase IDs and `/absolute/...` paths below are placeholders, never defaults.
Copy a suitable installed example into your private test directory and replace
its pool UUID, paths, source mappings, hardware and network intent. Use stable
UUIDs from inventory; a familiar display name is not an identity check.

Each `--plan` command returns a plan, not permission to execute it. Read every
page, exact sources/destinations, devices, space budget, changes and risk IDs:

```sh
virmill plan show PLAN_ID --output json
virmill plan apply PLAN_ID --digest REVIEWED_FULL_DIGEST \
  --idempotency-key UNIQUE_REQUEST_KEY --ack ACK_ID_FROM_THIS_PLAN \
  --wait --timeout 5m --output json --non-interactive
```

Repeat `--ack` for **every** required acknowledgement in that exact plan; omit
the placeholder when none is required. Use a fresh request key for a new approved
operation. In the TUI, open the plan confirmation, explicitly check its acknowledgements,
select **Apply reviewed plan**, and press Enter. The interface submits the exact
reviewed digest. Cancellation, stale state or missing authority must
produce an error, not a reduced policy or successful result. A lost response
does not authorize another apply.

## Suggested sequence

Start with a small offline BIOS disk set and no NICs. Keep all source writers
stopped throughout preparation and capture. Choose an existing active file pool
with enough reviewed headroom; initial allocation and copied bytes are distinct
from the [creation space budget](creation-space-estimates.md).

1. **Prepare independent disks.** Adapt
   [selected-disks.json](../examples/import/selected-disks.json), including every
   backing file and an absent destination under a private parent:

   ```sh
   virmill import prepare-disks /absolute/source-directory \
     --input "$(cat /absolute/reviewed-selected-disks.json)" --plan --output json
   ```

   Apply the reviewed plan, then run `virmill import result PREPARATION_OPERATION_ID`
   and `virmill import verify /absolute/prepared-directory`. Preparation success
   verifies independent artifacts; it does not prove an installed OS will boot.
   OVA testing instead starts with `import inspect /absolute/appliance.ova`, then
   [import prepare](import-preparation.md). ISO testing uses
   [import prepare-install](installation-preparation.md); it produces copied
   media and empty disks, not an installed guest.

2. **Create a new stopped guest.** Adapt
   [prepared-disks.json](../examples/creation/prepared-disks.json), retaining
   explicit disk order and no NICs for this first test:

   ```sh
   virmill vm create PREPARATION_OPERATION_ID \
     --input "$(cat /absolute/reviewed-creation.json)" --plan --output json
   ```

   Apply, then inspect `vm creation result CREATION_OPERATION_ID` and
   `vm show NEW_VM_UUID`. Require the completed definition and verified copies.
   [Creation](vm-creation.md) does not start the guest. New hypervisor identity
   does not replace copied guest credentials, hostnames or application identity.

3. **Exercise lifecycle deliberately.** Run `vm start NEW_VM_UUID --plan`,
   review/apply, and inspect `vm show NEW_VM_UUID`. Repeat separately for
   `vm reboot NEW_VM_UUID --plan` and `vm stop NEW_VM_UUID --plan`.
   Confirm the stopped state before proceeding. Graceful timeout has no hard-stop
   fallback. Boot, a guest-agent ping and application readiness are different
   observations; record what was actually seen.

4. **Test a separate managed network.** Adapt
   [protected-lab.yaml](../examples/networks/protected-lab.yaml), check an explicit
   candidate with `network cidr check --input '{"candidates":["10.197.241.0/24"]}'`
   or use the documented [automatic allocation](network-allocation.md), then:

   ```sh
   virmill network create /absolute/reviewed-network.yaml --plan --output json
   ```

   Obtain the exact administrator grant from this preview before applying; inspect
   `network creation result NETWORK_OPERATION_ID`. Protected profiles need
   `protectedNetworks`, not the older allowed-host `networks` grant. Services-only
   DHCP also enables managed DNS; guest-only has no host L3 or DHCP and requires
   explicit static guest addresses. Creation attaches no guest. Firewall entries
   and a successful result are not packet proof. Test traffic only in a separately
   reviewed disposable guest topology. See [network creation](network-creation.md).

5. **Capture the stopped guest.** Require persistent state, autostart disabled,
   no managed save, complete supported dependencies and read access. Inspect
   `vm recovery inspect NEW_VM_UUID`, then:

   ```sh
   virmill snapshot create NEW_VM_UUID \
     --input '{"sourceRoot":"/absolute/approved-disk-root"}' --plan --output json
   ```

   Review/apply, then inspect `snapshot list` and `snapshot show SNAPSHOT_UUID`.
   All source disk paths must be inside `sourceRoot`. NVRAM/TPM requires a separate
   exact `auxiliaryRootID` capture grant; unresolved implicit TPM paths are refused.
   Captures under `$XDG_DATA_HOME/virmill/captures` (default
   `~/.local/share/virmill/captures`) are private but **not encrypted at rest**.

6. **Restore a supported set to a new identity.** The current restore adapter
   accepts BIOS qcow2 disks and raw ISO media:

   ```sh
   virmill snapshot restore SNAPSHOT_UUID \
     --input '{"name":"Restored beta guest","poolID":"POOL_UUID"}' --plan --output json
   ```

   Review/apply. Inspect the operation and its new VM UUID: independent volumes,
   stopped definition and disconnected NICs must match the review. Start the new
   guest only through another reviewed plan. Preserve the original and compare
   the intended guest data. See [capture and restore](cold-capture-and-restore.md)
   for refusal cases and retained partial resources.

In the TUI, select the VM and choose **Capture** to enter the source directory.
For restore, choose **Snapshot restore** in Protection, use the capture ID field
and a parameters file containing the inner name/poolID object. Both views retain
the same plan ID and digest.

Then test [encrypted local repository backup and fresh-profile recovery](local-backups.md).
`backup create` performs an independent restored-set verification before success;
save its repository snapshot ID, capture UUID and manifest hash with your recovery
instructions. Keep repository credentials separate from the source capture and
repository. Repository recovery and creation of a new VM are separate plans.

For an existing running Linux guest with a verified SSH key/address and a
non-root account, use [reviewed guest recipes](guest-recipes.md). Readiness,
check, apply and verify are separate durable stages. This does not install the
initial account or automatically establish the address-to-VM identity.

## Jobs, failures and useful reports

```sh
virmill operation list --output json
virmill operation show OPERATION_ID --output json
virmill operation watch OPERATION_ID --follow --output ndjson --timeout 5m
```

Retain the exit status, typed error, plan/digest, operation ID, event cursor and
the relevant `import result`, `vm creation result`, `network creation result`
or `snapshot show` output. `journalctl --user -u virmilld.service` provides
coordinator process logs. [NDJSON watch](cli-streaming.md) can resume with
`--after LAST_SEQUENCE`. Client timeout or detachment leaves accepted work running;
use `operation cancel OPERATION_ID` for an explicit safe-boundary cancellation.
Use `operation reconcile OPERATION_ID` to observe an uncertain effect, not replay
it. Never edit the SQLite journal, delete locks or retry an allocation blindly.

Include the frozen build hashes, host/native versions, exact input, expected and
observed result, and whether originals were preserved in a bug report. Redact
credentials and private paths before sharing. Never include helper private keys,
TPM/NVRAM payloads, disk contents or captured guest secrets. Keep failed artifacts
for owner review; a recovery-required operation is not a passed test.

## Incomplete workflows and support claims

- UEFI/TPM restore staging, identity-preserving replacement, checkpoint revert
  and general partial-restore resume/cleanup remain unavailable. Declared NVRAM
  binding does not prove firmware initialization. Capture locks exclude supported
  cooperative writers, not malicious root or an uncooperative native restart.
- Local capture itself is unencrypted; encrypted repositories use the separate
  backup workflow. Retention execution and unattended scheduling remain
  incomplete. [BackupPolicy validate/preview](backup-policy-validation.md)
  checks declarations/occurrences; it installs no schedule or repository.
- Full lab application/teardown, physical-bridge adoption and rollback, USB/PCI
  attachment workflows, enabled-IPv6 and internet-only networking, and general
  resource cleanup remain incomplete. Discovery is not permission to attach.
- Guest/source, firmware, controller, provisioning and installer matrices remain
  incomplete. Existing scoped boot and persistence probes certify no guest family.
  Cross-build compatibility of portable code does not establish non-Linux host
  support. Clean install/upgrade/migration, distro security and physical networking
  qualification remain subject to the full [release tracker](implementation-status.md).

## Stop and uninstall

First inspect outstanding jobs and resolve or retain their resources. Stop only
your test guests through reviewed lifecycle operations. For an upgrade, preserve
the stopped coordinator's state and configuration before replacing executables;
do not copy a live SQLite database as a recovery backup. Preserve local captures
separately. This is not an automatic downgrade or migration-recovery procedure.

Stop the user coordinator with `systemctl --user stop virmilld.service` (or stop
the foreground process). If this beta exclusively owns the configured helper and
no helper operation is active, its administrator can stop
`virmill-host-helper.socket` and `virmill-host-helper.service` before removal.
Remove only the packages installed for this test:

```sh
sudo dnf remove virmill virmill-host-helper
# Or, on the designated Debian/Ubuntu host:
sudo apt remove virmill virmill-host-helper
```

Uninstall is not guest, disk, capture, policy or firewall cleanup. Retain those
resources and the journal for separate owner review; compare the original guest
and media inventory after removal. For an optional source-checkout rehearsal,
use an absent disposable destination, keeping the generated stage for uninstall:

```sh
python3 packaging/install.py plan --stage build/package-stage/virmill --destdir /absolute/disposable/root
python3 packaging/install.py install --stage build/package-stage/virmill --destdir /absolute/disposable/root
python3 packaging/install.py uninstall --stage build/package-stage/virmill --destdir /absolute/disposable/root
```

This removes only unchanged manifest files and is not native package
qualification. See [packaging](packaging.md).
