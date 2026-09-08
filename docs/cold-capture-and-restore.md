# Cold capture and restore

A cold recovery point copies every nonempty disk and removable medium, retains
original persistent XML, records native versions and the emulator digest, and
includes the configured firmware code and required NVRAM/TPM members. Source
files and guests are preserved. This workflow never stops a running guest for you.

Use a persistent stopped VM with autostart disabled and no managed-save state.
Resolve unsupported devices and external secrets before capture; they produce a
visible refusal, never a partial successful backup. The beta handles raw/qcow2
file sources, file volumes in active directory pools, and contained relative
backing chains with explicit formats. Disk copies are flattened and compared by
qemu-img inside an ordinary-user sandbox. Absolute backing references, block and
network storage, external qcow2 data files and unsupported runtime dependencies
remain unavailable in this adapter. All disk paths must be inside sourceRoot.
The coordinator needs read access; use the separately reviewed storage-access
grant/revoke workflow for managed volumes when necessary.

Plan the capture through either the Protection TUI page or CLI:

```sh
virmill snapshot create VM_UUID --input '{"sourceRoot":"/var/lib/libvirt/images"}' --plan
virmill plan show PLAN_UUID
virmill plan apply PLAN_UUID --digest PLAN_DIGEST --idempotency-key UNIQUE_KEY \
  --ack offline-source-read --ack private-recovery-state --wait
virmill snapshot list
virmill snapshot show SNAPSHOT_UUID
```

The plan displays the immutable snapshot UUID and every selected source. Captures
are stored under `$XDG_DATA_HOME/virmill/captures` (default
`~/.local/share/virmill/captures`). Files are private and immutable by convention;
they are not encrypted at rest. Do not expose that directory to other users.
Capture success establishes the complete captured set, not guest boot or an
independent encrypted repository backup. Restic repository backup is a separate
mandatory workflow and is not implied by this command.

For NVRAM or TPM state, add `auxiliaryRootID` naming administrator policy for this
exact VM and actor. The helper must allow capture and independently resolve the
complete explicit state inventory. It holds source pins and QEMU/swtpm cooperative
read guards, transfers a sealed anonymous archive, rechecks sources and records
acknowledged delivery. Persistent bytes never enter JSON, logs or job metadata.
Directory TPM capture requires an existing producer `.lock`. Implicit TPM paths,
file-based TPM producer locking, secrets and unsupported producer layouts refuse.
The helper never creates or truncates a source or its producer lock. Advisory
locks do not exclude an uncooperative writer or privileged native restart; exact
native state, generations, membership, ACLs and labels are rechecked throughout.

The initial native restore adapter restores BIOS qcow2 disks and raw ISO media:

```sh
virmill snapshot restore SNAPSHOT_UUID \
  --input '{"name":"Restored test guest","poolID":"POOL_UUID"}' --plan
virmill plan apply PLAN_UUID --digest PLAN_DIGEST --idempotency-key UNIQUE_KEY \
  --ack new-restored-identity --ack all-network-interfaces-disconnected --wait
```

Restore allocates independent new volumes, verifies their bytes, and defines the
new VM last. Original configuration is preserved except for the reviewed new
UUID/name, source paths and removal of network interfaces. It remains powered
off. Start it separately after inspecting the restore result. The guest retains
captured application identity and credentials. A new hypervisor UUID does not
reset the guest operating system's identity.

Firmware/TPM restore staging, identity-preserving replacement and checkpoint
revert remain blocked in this beta adapter; it refuses those sets instead of
resetting state. This limitation does not remove them from mandatory 1.0 scope.

On cancellation, copy failure or a crash, inspect `operation show` and use
`operation reconcile`. Existing staging, journals and uncertain volumes are
retained. The system never replays copying or allocations because a response was
lost. A complete catalog is reconciled against its durably recorded manifest
digest. A partial set has no successful complete receipt. A lost native define
acknowledgement is resolved by observing the new UUID and exact volumes, without
redefining or touching the original guest. Explicit resume and cleanup of partial
restore allocations are still incomplete; retain them for inspection.
