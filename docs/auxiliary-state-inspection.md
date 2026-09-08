# Firmware and TPM member metadata

This development workflow lists the complete explicit auxiliary member metadata
for one stopped VM. It reads directory entries and filesystem metadata through
the authenticated host helper. It does not read firmware/TPM content, initialize
state, grant file access or create a backup.

```sh
virmill vm recovery auxiliary inspect VM_UUID \
  --connection qemu:///system --input '{"rootID":"state"}' --output json
```

In the TUI, select **Protection → vm recovery auxiliary inspect** and enter:

```json
{"id":"VM_UUID","input":{"rootID":"state"}}
```

Replace `VM_UUID` with the exact canonical native UUID. The administrator chooses
the `state` root ID; it is not an arbitrary filesystem path. Both views call the
same coordinator service. JSON and NDJSON return the same typed observation, and
the TUI supports paging through the result. Inspection is read-only and creates
no plan or durable operation. The observation's `jobID` is only a request nonce;
it cannot be used with `operation show` as a persisted job.

The ordinary coordinator and root helper must both contain this implementation.
Follow [managed helper setup](managed-volume-access.md) for the private signing
key, public identity, kernel-authenticated socket and administrator-owned policy.
Do not share the private key. Existing helper policies deny this additional
operation, including policies that permit managed-disk ACL grants.

An administrator may add an `auxiliary` list to `/etc/virmill/helper-policy.json`.
Every entry needs the following exact values; wildcards are not supported:

| Field | Administrator selection |
| --- | --- |
| `actorUID` | Ordinary coordinator user already listed in `actors` |
| `keyID` | That actor's approved public signing-key fingerprint in `keys` |
| `resourceID` | One native VM UUID |
| `rootID` | An existing `roots` mapping that contains all explicit state paths |
| `stateUID`, `stateGID` | Actual expected owner/group of the VM's state files |
| `maxBytes` | Positive complete payload bound, at most 267386880 bytes |
| `maxMembers` | Positive complete member bound, at most 128 |
| `allowCapture` | Keep `false`; capture is not implemented by this workflow |

The selected root must be canonical, administrator-owned and unwritable by group
or others. Descendant paths cannot cross mounts or symlinks. Native state files
must have the exact configured owner/group, be ordinary files with one link, and
have available birth/mount/change identity and bounded ACL/SELinux metadata.
An error explains why observation cannot be established; it does not repair
ownership, rewrite SELinux policy or broaden permissions. Duplicate matching
permission entries are refused. Do not substitute guessed QEMU UID/GID numbers.

Use `vm recovery inspect VM_UUID` first to view the configured native layout.
The auxiliary workflow additionally requires a persistent stopped VM, disabled
autostart, and no managed-save state. It changes none of those settings. NVRAM
must be an explicit nonempty ordinary file. An emulator TPM must identify an
explicit `file` or `dir` source and declared version. Many libvirt definitions
omit the TPM state path; those remain unsupported here. Do not invent a path or
alter an existing guest merely to make an inspection pass. The current adapter
returns `INVALID_INPUT` with a native-path diagnostic when the source path is
absent; this is an unsupported configuration, not an invitation to supply a
guessed path. On failure, JSON/NDJSON stdout contains the typed error envelope
and stderr contains its matching human-readable summary.

The response contains root, directory and member generations, timestamps,
ownership, modes and hexadecimal ACL/SELinux attributes. It includes hidden
ordinary TPM members. A TPM root `.lock`, when present, is recorded separately
and excluded from payload totals. Its presence does not establish an acquired
lock, inactive producer or recoverable TPM. An empty TPM set, nonempty/control
lock ambiguity, special file, alias, changed native state, path replacement,
unsupported xattr or exceeded bound refuses the complete result. Cancellation
closes held descriptors and produces no successful observation.

Every success retains `captureVerified`, `independentRestoreVerified` and
`guestBootVerified` as `false`. Repeated stable metadata is an observation at that
time; the helper holds no lease after returning. The actual cold capture,
encrypted-state handling, durable delivery, full restore and guest checks remain
mandatory work. See [ADR 0022](adr/0022-authenticated-auxiliary-inventory.md) for
the boundary and [the release tracker](implementation-status.md) for open scope.
