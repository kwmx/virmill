# Creating a VM from prepared disks

This development adapter defines a powered-off VM from a successful local OVA or
[existing-disk preparation](existing-disk-preparation.md), or
[ISO preparation](installation-preparation.md) operation.
Optional [NoCloud provisioning](cloud-provisioning.md) can attach an identity-bound
seed to an explicitly declared compatible cloud-image disk set.
Native disk streaming and guest behavior are not qualified.
Use an explicitly authorized disposable libvirt environment for application tests.
The complete release checklist remains open.

First run the [import preparation workflow](import-preparation.md). Keep its
successful operation ID and all prepared files. `vm create` accepts that operation
ID, not an arbitrary artifact path. It rechecks the files and private conversion
receipt. A rewritten public manifest does not authorize creation.

Inspect the target with `storage pool list/show` and `network list/show`, using
the same explicit `--connection` throughout. Select an already active file-based
pool (`dir`, `fs` or `netfs`) and existing active networks. Creation does not
activate these resources. Pool space must meet the displayed conservative budget.

Copy [the example input](../examples/creation/prepared-ova.json) and replace its
placeholder pool/network UUIDs and hardware choices. It illustrates two original
disks and two original NICs; it is not ready to apply unchanged. Map each prepared
disk ID once, with a supported bus and unique positive boot order covering all disk/media boot candidates.
Map every prepared medium through `hardware.media`; use zero only to explicitly
exclude a medium from boot candidates. Media is copied into its own read-only
managed CD-ROM volume and included in verification and recovery.
Map every original NIC by its zero-based order among the descriptor's NIC items.
Use `sourceIndex: -1` only for an additional adapter. To keep an original adapter
initially disconnected, select its intended network and set `link: down`.
Disk-only sources have no known original NICs or hardware configuration. Use
[the disk-only example](../examples/creation/prepared-disks.json), make every
hardware assumption explicitly, and use `sourceIndex: -1` for every new NIC.

The current adapter accepts explicit x86_64 KVM Q35/i440fx machine versions,
1–64 disks, up to four read-only SATA/SCSI media and up to 32 NICs. Disk buses are SATA (at most six total disk/media devices), virtio and
virtio-scsi. NIC models are virtio, e1000e and rtl8139, subject to preflight.
Driver compatibility inside the appliance remains the operator's mapping decision.
CPU mode, memory, UTC/local time, BIOS/UEFI and display choice are explicit.
`vnc-unix` requests a local socket listener; `none` requests no graphical console.
Serial devices are included. Interactive viewer/serial attachment is still a
separate required workflow. A device definition does not prove guest reachability.

UEFI requires absolute installed code/template paths and their explicit `raw` or
`qcow2` format. The selected system descriptor and libvirt capabilities must match.
Secure Boot requires an enrolled-key template. `tpm: true` requires advertised
TPM 2.0 emulator/CRB support. Fresh NVRAM/TPM state is for clone creation, not
restoration of an encrypted guest's keys. The backend refuses silent substitutions.

Preview with the reviewed JSON input:

```sh
virmill vm create PREPARATION_OPERATION_ID \
  --connection qemu:///session \
  --input "$(cat /absolute/path/to/reviewed-creation.json)" \
  --plan --output json --non-interactive
```

Review the source hardware, target pool/network identities, per-disk order,
generated UUID/MACs, pinned capabilities/firmware, space budget and risks. Clone
identity does not change hostname, machine ID, SSH keys, passwords or application
identity inside the copied disks. Guest adaptation is reported `not-run`.

`plan apply PLAN_ID` requires `--digest`, `--idempotency-key` and every
`--ack` listed by that exact plan. Creation always requires `host-mutation`,
`copy-managed-volumes` and `new-vm-identity`; NICs add `network-attachment` and
UEFI adds `new-firmware-state`; media adds `attach-readonly-media`.
Apply repeats preflight. No VM starts automatically.

In the TUI, open **VMs → vm create** and enter
`{"id":"PREPARATION_OPERATION_ID","input":{...the same JSON parameters...}}`.
The shared service returns the same plan. Press `a`, review the acknowledgement
list and enter its full digest to authorize. The editor accepts up to 128 KiB and
shows a bounded tail while editing; detailed guided hardware forms remain required.
Esc discards the form or approval. Detaching does not stop an accepted daemon job.

`operation show/watch OPERATION_ID` reports actual persisted phases. The definition
is created only after all independent volume copies pass readback. Then
`vm creation result OPERATION_ID` reports each volume and `volumesVerified` /
`defined`. `guestBootVerified`, `setupVerified` and `connectivityVerified` remain
false. VM inventory marks a successfully cataloged matching definition `managed`;
discovery never adopts unrelated domains. Starting the VM is a separate reviewed
`vm start VM_UUID` operation and does not establish setup or connectivity readiness.

## Failure and recovery

Cancellation before allocation can finish canceled. Cancellation after allocation
retains new volumes and the original artifacts, records recovery-required status
and keeps resource locks. Disk transfer cancellation is signaled to the native
stream; a returned error is not proof that no bytes were written. Do not delete
listed resources manually to make a failed job appear successful.

If the definition may already exist, run `operation reconcile OPERATION_ID`.
Reconciliation observes the original intent and never repeats allocation, upload
or definition. An unmatched definition or changed network stays unconfirmed.

If every copied volume was verified but definition failed before registration,
preview `vm creation resume OPERATION_ID --connection ORIGINAL_CONNECTION --plan`.
The TUI exposes **VMs → vm creation resume** with the same operation ID. Review and
apply its fresh plan. It transfers all original locks, rechecks source/target,
reverifies the retained volumes and defines the original powered-off VM. The
parent remains partial and links to its recovery operation. Use the recovery ID
for result, cancellation and reconciliation; another failed recovery can itself
be the parent of a new reviewed recovery. No volume is reallocated or reuploaded.

An incomplete or unverified disk set is refused by this resume adapter. Those
resources can use the explicit [creation cleanup disposition](creation-cleanup.md):
retain and pin them, or preview guarded deletion of proven new generations.
Changed original capabilities,
firmware, source or network settings also require resolution before this exact
definition can resume. These are implementation limitations, not passed recovery
acceptance scenarios. ISO/cloud/existing-disk entry paths, full configuration and
guest verification remain mandatory work.

On first opening an older schema-1 or schema-2 application database, this build
writes a private, consistent `journal.db.pre-v3-UUID.db` backup and upgrades to
schema 3. Keep that backup for operator recovery. Older binaries must refuse schema 3;
replacing the live database with a pre-migration backup can lose later operation
history and must not be used as an automatic downgrade.
