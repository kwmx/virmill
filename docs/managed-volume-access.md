# Reviewed access to managed disk files

`storage access grant` previews a persistent read grant for one selected managed
volume. `storage access revoke` previews restoration of the original access ACL.
Both run through the ordinary-user coordinator, durable operation engine and
separately authorized root helper. The **Storage** TUI actions use the same service.
This development slice does not qualify the complete v1 permissions matrix.

The initial adapter supports local `qemu:///system`, active file pools and a
stopped persistent VM without autostart or managed-save state. Select a disk target
such as `vda`, never a caller-supplied disk path. The volume must have the exact
`virmill-VM_UUID-disk-NNN.qcow2` or `virmill-VM_UUID-media-NNN.iso` identity under an
administrator-approved root. Retained media must be explicitly read-only. The
helper independently resolves the VM, pool, volume key and target through libvirt.
Files must be ordinary, single-link, without symlinks, with complete statx birth
identity and supported Linux POSIX ACLs. Unsupported layouts fail before mutation.

## Administrator setup

Installation alone enables no helper authority. The packaged socket and empty
example policy deny ordinary actors by default. Actual policy, key registration,
socket access and service activation are administrator-reviewed host changes.
Perform these only on the intended authorized host; never copy credentials into
chat, a plugin, the repository or an operation input.

1. As the coordinator user, create its private configuration directory at
   `$XDG_CONFIG_HOME/virmill` (default `~/.config/virmill`), mode 0700. Generate an
   Ed25519 PKCS8 PEM key at `helper-key.pem`, mode 0600, without replacing an
   existing key. One available external setup tool is `openssl genpkey -algorithm
   ED25519 -out helper-key.pem`, run with umask 077 in that directory. OpenSSL is
   a setup option, not a Virmill runtime dependency. The observed disposable host
   has OpenSSL package `3.5.8-1.fc44.x86_64`.
2. Run `virmill host helper identity` or the **Settings** action. Register its
   public key and SHA-256 key ID, together with the ordinary actor UID, in
   `/etc/virmill/helper-policy.json`. Start with the packaged example under
   `/usr/share/doc/virmill-host-helper/` and the actual installed packaging layout; do not
   register a guessed key. The policy must be a bounded, root-owned, single-link
   regular file, readable by the coordinator, without group/other write.
   Root directories must be canonical, root-owned and not group/other writable.
   For example, root ID `images` can designate `/var/lib/libvirt/images`; a
   subordinate pool may retain its normal libvirt ownership and labeling.
3. Give only the selected actor access to the helper socket. One explicit setup
   is a systemd socket drop-in selecting an existing administrator-approved group,
   `SocketGroup=GROUP`, `SocketMode=0660`, `DirectoryMode=0755`. The actor must
   have that group in the coordinator's actual login credentials. Choose the
   group deliberately; do not silently add users to broad libvirt or sudo groups.
   Alternatively an administrator can grant narrow directory traversal and
   socket access ACLs, accounting for socket recreation on restart. The helper
   still checks the signed request, current policy and kernel peer on every call.
4. Start `virmill-host-helper.socket` after reviewing its drop-in and policy.
   The helper service is root and socket-activated; `virmilld` remains the user.
   A new login/coordinator restart is needed after changing group membership.
   `host helper identity` reports public identity/policy status, not a claim that
   the socket, MAC rules or native storage access have been qualified.

Example policy shape (replace every placeholder with reviewed values):

```json
{"apiVersion":"virmill/v1","keys":{"PUBLIC_KEY_SHA256":"PUBLIC_KEY_HEX"},"roots":{"images":"/var/lib/libvirt/images"},"actors":[1000]}
```

No environment variable overrides the production helper socket, policy or journal.
The private key stays in the coordinator's configuration directory; plans retain
only its public fingerprint. Each request uses a fresh, at-most-15-minute signature
and binds actor, native resource, approved root, complete access metadata, operation
ID and final plan digest. Policy changes are reloaded for each connection.

## Grant, use and restore

Keep the VM stopped and exclude external writers through restoration. Generate a
preview:

```sh
virmill storage access grant VM_UUID --input '{"target":"vda","rootID":"images"}' --plan
```

Review the exact volume, original owner/group/mode/ACL, actor UID and kernel groups,
desired access ACL, key fingerprint, downtime and risk acknowledgements. Apply
using `plan apply` with that plan ID, full digest, a new idempotency key and every
listed acknowledgement. The TUI uses the same digest confirmation dialog; its
grant form is `{"id":"VM_UUID","input":{"target":"vda","rootID":"images"}}`.
Client detach leaves the accepted job running. Read `storage access result JOB_ID`
and `operation show JOB_ID` for completion. Results are historical observations;
`currentAccessRechecked` is false, so they do not promise present-day access.

The ACL adds actor read permission while preserving prior effective permissions
for other users/groups and the actor's non-read requests. Existing masked group
rights are normalized before broadening the mask. ACL group write/execute
combinations that cannot be represented without adding a non-read request are
refused. Ownership, file bytes, guest XML and security labels are not write targets.
SELinux/AppArmor and directory traversal rules remain authoritative; this does not
guarantee a later data read will succeed under every host policy.

After the authorized read workflow, preview restoration:

```sh
virmill storage access revoke ORIGINAL_GRANT_JOB_ID --plan
```

Review/apply it separately. The helper retrieves the successful root-owned grant
record and restores its exact original ACL/mode in one bounded metadata syscall.
It refuses different file generation, content-related metadata, owner/group, access
ACL or native mapping. A freshly reviewed ctime is bound at apply; an older grant's
ctime may have advanced through nested metadata-only grants that were subsequently
revoked. Unwind overlapping grants in reverse order. Booting/writing the disk or
changing its VM/pool configuration before restoration invalidates this conservative
workflow; keep the grant and original proof visible for administrator recovery.
Revocation cannot recall already open reader descriptors or retained copies.

## Failures, recovery and removal

Before each effect the engine rechecks state and permissions. The helper opens the
approved root and exact relative file with held descriptors, rejects link aliases,
takes a cooperative QEMU read guard and independently rechecks native mapping.
QEMU guards do not constrain arbitrary privileged writers or QEMU locking disabled
by another actor. The plan requires an explicit exclusive-offline-volume assertion.

The user journal commits the operation before contacting the helper. The helper
fsyncs a root-owned intent before changing the ACL and then observes the result.
Missing acknowledgements enter recovery-required with resource locks retained.
`operation reconcile JOB_ID` sends only an observation request for that same job;
it does not repeat the ACL write. An intent plus matching current metadata can
produce a recovered completion record. Missing/corrupt intent, changed mapping or
nonmatching metadata remains unresolved; preserve evidence and obtain administrator
recovery. There is no guessed chmod, automatic deletion or blind retry. Cancellation
is honored before helper dispatch; a dispatched effect must be observed.

Root records live at `/var/lib/virmill-host-helper/JOB_ID.access-intent.json` and
`JOB_ID.access-complete.json`, in a mode-0700 directory with mode-0600 files.
Receipt version 1 and the new durable operation names are additive; old binaries
refuse unknown recipes. Existing immutable operation inputs are not migrated or
rewritten. Back up the root journal and user journal consistently for recovery.

Revoke outstanding grants while the helper is still available before disabling
the socket or removing its policy/key. Package removal alone does not undo ACLs
or erase journal evidence. Preserve the key reference, policy, root records and
user journal until outstanding jobs and grants are resolved. Do not delete
original media, guest volumes or helper records as an uninstaller shortcut.
