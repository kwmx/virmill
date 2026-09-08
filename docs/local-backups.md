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
`backup result` contains the exact repository snapshot ID, capture UUID and
manifest SHA-256. Preserve these non-secret recovery coordinates separately from
the coordinator database. Repository checks read all stored data and honor
restic's locks. No command prunes, forgets, unlocks or repairs data automatically.

## Recover without the original database

Use a fresh ordinary-user XDG profile or a capture catalog where the selected
capture UUID is absent. Preserve the original catalog. Recover using the saved
coordinates and independently retained credential:

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

TUI forms accept `{"path":"...","input":{...}}` for repository actions and
`{"id":"...","input":{...}}` for backup create/restore. Result takes the
operation UUID. All interfaces retain the same review and failure behavior.

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
