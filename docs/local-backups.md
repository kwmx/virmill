# Encrypted local backup and recovery

Virmill uses the administrator-installed `/usr/bin/restic` to encrypt complete
local cold recovery sets. The CLI and Protection section of the TUI use the same
durable operations. `backup create` succeeds only after restoring the new
repository snapshot to independent staging and verifying every captured member.
This verifies the recovery set; it does not boot a guest.

The native adapter has been exercised with restic 0.19.1. Repository storage is
an explicit canonical local path, outside the capture catalog and cache. The
repository and its parent must be private directories owned by the ordinary
coordinator user. The password is read from an explicit 0600 single-link file
under a 0700 parent; its bytes never enter command arguments, plans or logs.
Keep the credential outside the repository and capture paths, and retain a
separate recoverable copy. Lost credentials cannot be reconstructed by Virmill.

## Create and verify

Use a new repository path and an existing private credential file. The following
commands create plans. Review each plan and apply its exact digest and required
acknowledgements using `plan apply`; a preview is not execution.

```sh
virmill backup repository init /absolute/private/new-repository \
  --input '{"passwordFile":"/absolute/private/credential"}' --plan
virmill backup create CAPTURE_UUID \
  --input '{"repository":"/absolute/private/new-repository","passwordFile":"/absolute/private/credential"}' --plan
virmill backup result BACKUP_OPERATION_UUID --output json
virmill backup repository check /absolute/private/new-repository \
  --input '{"passwordFile":"/absolute/private/credential"}' --plan
```

Create the input capture first using [cold capture](cold-capture-and-restore.md).
In the TUI, choose a capture in **Protection**, select **Back up**, and browse for
the repository and its private password file. Review the plan before applying.
Repository checks read all stored data and honor restic's locks. No command
prunes, forgets, unlocks or repairs data automatically.

## Save a recovery receipt

After a backup succeeds, open **Protection → More → Recover capture from backup**.
Choose its entry with Left/Right, then **Save recovery receipt**. Browse to a
folder and choose a new filename. Existing files are never replaced. Keep this
file with your recovery records, separately from the repository password.

The CLI provides the same receipt:

```sh
virmill backup receipts
virmill backup receipt show BACKUP_OPERATION_UUID
virmill backup receipt export BACKUP_OPERATION_UUID /absolute/private/recovery.json
```

Only completed backups with a full restore-and-verify proof have receipts. The
recent list searches the latest 1,000 jobs for the current user and connection;
it is not an archive catalog. For an older backup, use its operation UUID with
`backup receipt show` or `export`, even if it no longer appears in the recent
list. Those commands still need the original operation history.

A saved `virmill/v1` `BackupReceipt` contains the repository path, exact snapshot
ID, capture UUID, manifest hash, operation ID and verification date. It contains
no password or private key. These are recovery selectors, not proof that a
repository still exists or that its current contents are valid.

## Recover without the original database

Use a fresh ordinary-user XDG profile or a capture catalog where the selected
capture UUID is absent. Preserve the original catalog.

In **Protection → More → Recover capture from backup**, press **Ctrl+O** on
**Saved backup** to browse for your receipt. If no recent backup is available,
**Enter** opens that browser. Choose the encrypted **Backup folder** and its
separate **Password file**, then **Preview recovery**. Update the folder if the
repository has moved to another mount path. The saved receipt works without the
original Virmill database; the encrypted repository and its password are still
required. After recovery succeeds, select the capture in **Protection** and
choose **Restore** to review a new VM name and destination storage pool.

For CLI inspection, `virmill backup receipt read /absolute/private/recovery.json`
validates the saved document without writing to the repository. CLI recovery
uses its selectors and your independently retained credential:

```sh
virmill backup restore FULL_RESTIC_SNAPSHOT_SHA256 \
  --input '{"repository":"/absolute/private/repository","passwordFile":"/absolute/private/credential","captureID":"CAPTURE_UUID","manifestSHA256":"FULL_MANIFEST_SHA256"}' --plan
virmill backup result RESTORE_OPERATION_UUID --output json
virmill snapshot show CAPTURE_UUID --output json
virmill snapshot restore CAPTURE_UUID \
  --input '{"name":"Recovered guest","poolID":"POOL_UUID"}' --plan
```

Repository recovery restores the complete set into the current catalog, then
verifies its canonical manifest, receipt, exact directory contents, sizes and
hashes before sealing its root read-only. No existing capture is overwritten.
The separate `snapshot restore` plan creates independent volumes and a new
disconnected VM within that adapter's supported profiles. Starting it requires
a separate lifecycle plan.

Loading or saving a receipt never authorizes recovery. Recovery repeats the
repository checks and verifies the recovered data before reporting success.
Malformed, incomplete, changed or unsafe receipt files are refused with an issue;
choose a valid saved receipt rather than copying IDs from an error message.
`backup result` takes an operation UUID and reports that operation's proof.
All interfaces retain the same review and failure behavior.

## Interrupted operations

Intent is durable before repository writes or extraction. Uncertain effects
retain their repository, partial destination, job and locks. Inspect
`operation show` and use `operation reconcile`; reconciliation never reissues an
init, backup or restore. A complete exact restored set may be verified after a
lost acknowledgement. A backup without its full roundtrip completion proof
remains unresolved even if restic has a snapshot with that operation tag.

Staging is retained for diagnosis; budget space for the source capture, encrypted
repository and a full restored copy. Same-user filesystem writers must be
coordinated. The [adapter contract](local-restic-adapter.md) documents pathname,
process and archive trust boundaries. Schedules, retention, automatic retry,
remote repositories and complete firmware/TPM guest restoration are not provided
by this slice; their mandatory local acceptance work remains in the tracker.
