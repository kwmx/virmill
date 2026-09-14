# Preparing an ISO installation

`import prepare-install` copies selected ISO media and creates independent empty
qcow2 disks through the shared CLI/TUI service. This development workflow verifies
local artifacts. It does not install an OS, start a VM, prove that an ISO boots or
supply unattended configuration. Full ISO installation, cloud/NoCloud guest verification,
console/media-ejection workflows and guest qualification remain required for 1.0.

Keep the selected ordinary media file offline. The service refuses symlinks,
noncanonical paths and cooperating QEMU writers, pins identity/change time/SHA-256,
and repeats validation before execution and publication. Advisory locks cannot
exclude programs that ignore them. No original media bytes are changed.

Copy [the preparation example](../examples/import/installation.json). Choose an
absent destination inside a private user-owned directory and replace the example
capacities with reviewed values. Supply an optional `sha256` from a trusted source;
an absent digest is recorded as locally measured, without publisher authentication.
`mediaID` and disk IDs must be unique. There must be 1–64 blank disks, each between
1 MiB and 512 GiB and divisible by 512 bytes. Media is limited to 64 GiB, must be
2048-byte aligned and contain recognized ISO9660/UDF volume identifiers within the
bounded recognition window. These identifiers do not validate its filesystem or
El Torito boot catalog. Unknown formats fail with an explicit unsupported result.

```sh
virmill import prepare-install /absolute/path/to/installer.iso \
  --input "$(cat /absolute/path/to/reviewed-preparation.json)" \
  --plan --output json --non-interactive
```

Preview records source provenance, media recognition, blank-disk sizes, tool
identity, private stage and conservative space budget. It creates no artifact.
Apply the returned plan ID/digest with a unique idempotency key and both
`write-import-artifacts` and `offline-source-files` acknowledgements. Use
`operation show/watch` for durable progress, `operation cancel` to request
cancellation, and `import result OPERATION_ID` for the completed receipt.
`import verify /absolute/prepared-directory` independently checks its files.

The TUI exposes **VMs → import prepare-install**. Enter
`{"path":"/absolute/installer.iso","input":{...the same preparation JSON...}}`.
Submit, review the same service plan, then press `a` and enter the full digest to
approve its listed acknowledgements. `import result`, `import verify` and the
Operations views use the same services. Guided creation forms remain unfinished;
the current form accepts the documented JSON input.

The artifact contains `media/media-000.iso`, every `disks/disk-NNN.qcow2`,
`installation-report.json` and a `virmill/v1` `PreparedInstallation` manifest.
Media is copied, flushed, made read-only and SHA-256 checked. Blank disks are
created inside the existing unprivileged QEMU/bubblewrap worker; format, exact
virtual size, absence of backing files, `qemu-img check` and a complete positive
zero map must pass. No partition or filesystem is created. The receipt distinguishes
empty disks from converted source disks and never invents an OVF or source chain.

A private immutable receipt is durable before publication by no-replace rename.
Cancellation joins the worker before deleting unpublished staging. Failures retain
uncertain staging and resource locks; they never publish a partial set. Reconcile
an uncertain operation with `operation reconcile OPERATION_ID`. A complete,
matching published artifact can reconcile after coordinator restart, even if the
original ISO is no longer available. Reconciliation never recreates disks or copies
media. Unpublished failed staging requires a future explicit disposition workflow;
successful staging is retained until you remove it with `import discard
OPERATION_ID`, which VM setup can do after creation ([ADR 0058](adr/0058-remove-prepared-copies.md)).

Use the successful preparation operation ID in the separate
[VM creation workflow](vm-creation.md), with
[the installation mapping example](../examples/creation/prepared-installation.json).
Choose real reviewed pool/hardware values. Map every blank disk and every medium
exactly once. Media uses SATA or SCSI, is attached read-only and gets its own
`virmill-UUID-media-NNN.iso` managed volume. The example puts the installer first
and guest disks second/third. Positive boot orders must be unique and complete
across disks and media; media order zero attaches a nonboot candidate explicitly.
One SATA controller has six total disk/media slots. No source hardware or original
NICs can be inferred; new NICs require `sourceIndex: -1`.

Creation adds `attach-readonly-media` to its other acknowledgements. All writable
disks and read-only media must pass readback before definition. Media allocation or
transfer failure retains the entire identified partial set. Reviewed definition
recovery reverifies media as well as disks and never reuploads them. Native raw/ISO
volume metadata is distinguished from the QEMU raw CD-ROM driver; guest disks must
still report qcow2. Creating a definition is not evidence of an installed guest.

Generated nonbootable ISO and actual confined empty-disk tests exercise local files;
libvirt test-driver XML checks exercise only simulation. No native host storage,
KVM boot, firmware/TPM or installer behavior has been verified for this path.
