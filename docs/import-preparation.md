# Prepare independent OVA disk artifacts

This development workflow converts all disks attached to one selected OVF system
into independent qcow2 files. It preserves the original archive and descriptor.
It does not define a VM or establish guest boot readiness. Run the ordinary-user
coordinator as described in [getting started](getting-started.md).

First inspect the archive:

```sh
virmill import inspect /path/to/appliance.ova --output json
```

Manifest checks accept SHA1, SHA256 and SHA512, including the horizontal spacing
used by VirtualBox (`SHA1 (file) = digest`). Every provided digest must match its
member. SHA1 is reported as weak; valid checksums do not authenticate a publisher
or establish guest readiness. This behavior is shared by the CLI and TUI inspector.

Use the report's exact system and disk IDs. Provide a mapping for every disk,
including an explicit source format and upper bound on its virtual size. Allowed
format names are `raw`, `qcow2`, `vmdk`, `vdi`, `vpc` (VHD) and `vhdx`. A single
system can be selected automatically; multiple systems always require `systemID`.
The following example has two VMDKs with a 32 GiB limit each:

```sh
mkdir -m 700 "$HOME/virmill-imports"
virmill import prepare /path/to/appliance.ova --plan --output json --input '{
  "destination":"./virmill-imports/prepared-appliance",
  "systemID":"appliance",
  "disks":[
    {"id":"boot","format":"vmdk","maximumVirtualBytes":34359738368},
    {"id":"data","format":"vmdk","maximumVirtualBytes":34359738368}
  ]
}'
```

Run this example from your home directory, or replace the destination with an
absolute path under the directory you created. Relative paths resolve in the CLI
or TUI client's working directory. The destination must not exist. Its parent
must be user-owned, not writable by other users, and contain no symlink components.

Review the selected system, disk order, source digest, converter version/digest,
destination, staging path and free-space requirement in the plan.
Preview reads and hashes the archive but does not create output or run guest code.

When the descriptor declares a streamOptimized VMDK or a single-file format, the
disk is converted in place from the archive
([ADR 0060](adr/0060-one-copy-import.md)). The plan lists it under
`disksReadInPlace`, and only the other members are unpacked into the staging
folder. Preview measures such disks with `qemu-img measure` in the sandbox. The
free-space requirement counts that measured output and the unpacked members,
and the measurement becomes the converter's output limit. Other disks are
unpacked, converted and budgeted at their virtual size plus a quarter, as before.
Apply the actual returned values:

```sh
virmill plan apply PLAN_ID --digest PLAN_DIGEST \
  --idempotency-key YOUR_UNIQUE_KEY --ack write-import-artifacts \
  --wait --output json --non-interactive
virmill import result OPERATION_ID --output json
virmill import verify /absolute/path/to/prepared-appliance --output json
```

`--wait` returns the operation's actual state. `import result` exposes the durable
receipt after success. `import verify` checks the current files against the supplied
receipt's declared hashes; that check alone does not authenticate a receipt from
another party. The coordinator's reconciliation also matches its immutable stored
receipt. Neither command boots a guest.

In the TUI, open **VMs**, select **import prepare**, and enter the same mapping in
the request form: `{"path":"/path/to/appliance.ova","input":{...}}`. Review the
returned plan, press `a`, and type its digest to authorize. **Jobs** provides
operation inspection, cancellation and reconciliation. **VMs** also exposes
`import result` and `import verify`.

Outputs are `manifest.json`, `source.ovf`, `import-report.json` and ordered
`disks/disk-000.qcow2`, `disk-001.qcow2`, etc. Disk members are mode 0400 to discourage
accidental modification; the owning user can still change them. Treat these as
immutable preparation artifacts. A later VM creation operation must create its
own writable disks and explicitly select firmware, controllers and networks.

A changed source/tool invalidates the plan. Missing extents/backing files,
encrypted or dirty images, unsupported dependencies, conversion/check/compare
errors and resource limits produce a failed or recovery-required operation.
Nothing overwrites an existing destination. Request cancellation using
`operation cancel OPERATION_ID`; the active confined worker stops before its
private staging is removed. Large archive reads reach cancellation at member
boundaries. An uncertain failure retains the named `.virmill-import-PLAN_ID`
directory and its resource lock. Reconcile with `operation reconcile OPERATION_ID`;
it observes completed publication without resuming partial conversion. Retained
staging currently requires deliberate operator cleanup after the operation and
its dependencies have been resolved; no automatic garbage collector runs.

The reproducible [fixture recipe](../tests/fixtures/import/README.md) distinguishes
file conversion from the still-required real guest qualification.

The [generated split-VMDK probe](../tests/fixtures/import/multidisk-probe/README.md)
provides an original two-disk OVA recipe with all extents. Its
[native run](evidence/multidisk-crash-run.md) passed this workflow, durable
interruption/retention and a later KVM BIOS second-disk read. It contains no OS or
NICs and therefore does not qualify the required VMware Linux/multi-NIC fixture.
