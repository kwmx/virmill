# Prepare an existing disk set

`import prepare-disks` prepares independent qcow2 copies of explicitly selected
local disk files. Its successful operation ID can feed [VM creation](vm-creation.md).
This development workflow has generated-file conversion evidence; it does not
establish guest boot, driver compatibility or native storage support.

Shut down every writer of the source files first, including backing files and
split extents. Keep them offline through completion. The service holds Linux OFD
guards compatible with QEMU's permission locks before hashing and throughout
conversion. A conflicting QEMU writer or unavailable OFD locking refuses the
operation. Programs that disable or ignore these locks require the operator's
offline-source assertion. Content and identity are also rechecked; a digest does
not authenticate a publisher.

Select a directory containing the files, and list every file the parser may read.
The directory itself is never mounted into the worker. For example, a qcow2 disk
at `disks/boot.qcow2` can reference `../base.raw` when both files are selected.
A split VMDK needs its descriptor and all extents. Absolute backing paths, external
protocols, missing or unselected dependencies, encrypted/dirty images and external
qcow2 data files are refused. Paths and ancestors must not be symlinks. The source
files remain unchanged; this command does not use an existing disk in place.

Create a private destination parent owned by your ordinary user. The destination
must not exist. Adapt [the example](../examples/import/selected-disks.json), then
preview:

```sh
virmill import prepare-disks /absolute/path/to/source-directory \
  --input "$(cat /absolute/path/to/reviewed-selected-disks.json)" \
  --plan --output json --non-interactive
```

The input requires `offlineSources: true`, a `destination`, a `files` list and a
`disks` list. Each file has a relative `path` and optional expected `sha256`.
Each root disk needs a unique `id`, selected `path`, explicit `format` and
`maximumVirtualBytes`. Formats are `raw`, `qcow2`, `vmdk`, `vdi`, `vpc` (VHD) and
`vhdx`. Limits are 1–64 root disks, 10,000 selected files, 1 MiB–512 GiB per virtual
disk and 64 TiB total physical source bytes. Worker resource limits can refuse
large dependency graphs; these bounds are not tested capacity claims. Output
space is budgeted conservatively before acceptance. Both source and destination
paths resolve in the CLI/TUI client's working directory; file-list paths remain
relative to the selected source directory.

Review all source names, identities, SHA-256 values, backing chains, root-disk
order, tool identity, space requirement and destination. Preview hashes and
inspects sources but publishes nothing. Apply the returned values:

```sh
virmill plan apply PLAN_ID --digest PLAN_DIGEST --idempotency-key UNIQUE_KEY \
  --ack write-import-artifacts --ack offline-source-files \
  --wait --output json --non-interactive
virmill import result OPERATION_ID --output json
virmill import verify /absolute/path/to/prepared-disks --output json
```

In the TUI, open **VMs → import prepare-disks**. Enter
`{"path":"/absolute/path/to/source-directory","input":{...the same input...}}`.
Review the returned plan and its acknowledgements, press `a`, and enter its digest.
Use **Jobs** to inspect, cancel or reconcile; **VMs** exposes result and verification.

The output contains `manifest.json` with kind `PreparedDiskSet`,
`disk-source-report.json`, and ordered `disks/disk-NNN.qcow2` files. Each converted
disk passes size inspection, `qemu-img check` and content comparison. All files
are flushed before an atomic publication, with the private receipt committed
first. Disks have mode 0400. The report preserves source proofs and hardware is
explicitly unknown; there is no invented OVF descriptor. All firmware, CPU,
controller, boot, clock and network assumptions belong to the separate creation
plan. Use [the disk-only creation example](../examples/creation/prepared-disks.json)
as a starting point, replacing its pool and hardware placeholders. It requests no
NICs; any NIC you add needs `sourceIndex: -1` and an explicit network mapping.

Preparation is incomplete if any conversion or final source check fails. Originals
remain, publication is refused, and uncertain `.virmill-disks-PLAN_ID` staging and
job locks are retained. A cancellation stops and joins the worker before removing
only its unpublished staging. `operation reconcile OPERATION_ID` observes a
complete published set against the durable receipt after interruption; it never
reuses partial output or repeats conversion. A missing publication remains
recovery-required. After successful publication, original files are not needed to
verify the independent copies. Source provenance in the journal conservatively
protects referenced Virmill volumes from cleanup until a future explicit reference
disposition workflow; it is not an automatic garbage-collection permission.

ISO installation, NoCloud provisioning, downloaded cloud-image provenance,
in-place disk use, guest adaptation and the complete guest/host qualification
matrix remain required work. See the [fixture recipe](../tests/fixtures/import/README.md)
and [release tracker](implementation-status.md).
