# Accepting a retained creation definition

`vm creation accept` resolves an uncertain creation when the existing stopped
BIOS definition matches its original intent plus an explicitly selected chipset
device policy. This is a separate reviewed operation. It leaves the original job
partial, links its recovery child and keeps the original plan/receipt unchanged.
General VM adoption, other hardware changes and firmware/TPM acceptance remain
separate required workflows.

Inspect the original job, creation result and actual VM before choosing devices:

```sh
virmill operation show ORIGINAL_OPERATION --connection qemu:///system
virmill vm creation result ORIGINAL_OPERATION --connection qemu:///system
virmill vm show VM_UUID --connection qemu:///system
virmill vm creation accept ORIGINAL_OPERATION \
  --input "$(cat accept-devices.json)" --plan --connection qemu:///system
```

Copy [the input example](../examples/creation/accept-retained-devices.json) and set
every device choice to the observed, intended policy. That example explicitly
accepts Q35 USB `qemu-xhci`, virtio ballooning and a **reset watchdog**. A reset
watchdog can forcefully reset a guest after it is later started. It is not a
suggested default. A mismatch, unknown device/namespace, changed identity, disk,
NIC, boot order or other original setting refuses acceptance.

The preview shows original hardware, accepted devices, retained volume identities,
configuration/file fingerprints, acknowledgements and downtime. The VM must
already be stopped and persistent, with autostart disabled and no managed-save
image, snapshots or checkpoints. It must remain exclusively offline throughout
readback. The original prepared source and environment must remain available and
unchanged. Fresh firmware/TPM state is not covered by disk verification; this
adapter refuses those profiles pending their auxiliary-state recovery workflow.

The coordinator needs ordinary-user read access to each exact retained file and
support for the QEMU OFD permission guards. Missing permission is reported during
preview. No root retry or permission broadening is performed. Libvirt connection
authorization alone does not imply filesystem read permission. Bounded helper
integration for permission-managed storage remains outstanding; use only an
explicitly authorized test environment with the required existing access.

Review and apply the exact plan digest using `plan apply`, including every listed
acknowledgement. All original resource locks transfer atomically to the acceptance
job. Execution holds read guards for the entire volume set, hashes every byte,
repeats configuration/generation checks and persists a separate proof. It then
rechecks before committing managed ownership. Guarded readback can take substantial
time and may be repeated during reconciliation. Cooperative QEMU locks cannot
exclude arbitrary external writers that ignore the protocol.

```sh
virmill operation watch ACCEPTANCE_OPERATION --connection qemu:///system
virmill vm creation result ACCEPTANCE_OPERATION --connection qemu:///system
```

In the TUI, choose **VMs → vm creation accept**, enter
`{"id":"ORIGINAL_OPERATION","input":{"devicePolicy":{...}}}` with the complete
policy, inspect the same plan and approve its exact digest. Jobs provide watch,
cancel and reconcile; **vm creation result** reads the separate acceptance result.
Active observations report `complete: false`, with an optional acceptance proof.
Successful acceptance confirms catalog registration and retained-byte verification;
guest boot, setup and connectivity flags remain false. No definition, guest start,
disk write, allocation, upload, detach or deletion occurs.

Cancellation or failure retains inherited locks. If completed verification was
durably recorded, `operation reconcile` rechecks the bytes/configuration before
finishing catalog registration. It never repeats a host mutation. With no complete
proof, inspect the failure and review a fresh acceptance using that uncertain
acceptance job's ID. Changed files/configuration or conflicting ownership must be
resolved explicitly; no generic retry bypasses these checks. Partial parents keep
their ancestry and never become successful creation results.

Ownership records for this path use version 2 and link to the acceptance proof.
Older binaries refuse that record/operation. SQLite remains version 3 and original
version-1 receipts remain unchanged. See [ADR 0017](adr/0017-reviewed-creation-acceptance.md)
and the [release evidence ledger](evidence/README.md) for qualification limits.
