# ADR 0060: Import in about one copy of disk space

Status: accepted; native evidence pending.

## Context

An OVA import held up to three full copies of each disk at once: the members
unpacked from the archive, the converted qcow2 and the new pool volume. Both
space checks also reserved each disk's virtual size plus 25%. The owner's 35 GB
appliance has one streamOptimized VMDK with a 120 GiB virtual disk and 34 GiB of
data. Preparation asked for about 184 GiB free, creation for 150 GiB more in the
pool, and a host with 62 GiB free could not import it. ADR 0057 and ADR 0058
deferred single-copy import.

Writing the converted image straight into a system pool is not possible for the
unprivileged coordinator. System pool files belong to root, and only libvirt
writes them, by upload. Letting libvirt convert (`vol-create-from`) would parse
untrusted images outside the sandbox of ADR 0003 and ADR 0005. A root-helper
write grant would add privileged surface for every user.

## Decision

Three changes bring the peak to about one copy. Each keeps the existing
confinement, digests and recovery.

### 1. Space checks use measured sizes

Preparation measures each disk with sandboxed
`qemu-img measure -O qcow2 -o compat=1.1`, on the same source exposure the
conversion uses. The budget is the measured size plus 1% and 16 MiB, capped at
the old worst case (virtual size plus 25% and 16 MiB). The same figure is the
converter's output-file limit, so an image that writes more than it declared
fails closed instead of filling the disk.

Creation reserves each prepared disk's file size plus 16 MiB instead of its
virtual size plus 25%. The review says the disk can grow to its virtual size as
the guest writes. New recipes record `spaceBudget: "file-bytes-v1"`. Recipes
without it keep the old rule, so plans and recovery from older builds decode
and check unchanged.

### 2. OVA disks are converted in place

When the descriptor declares a self-contained image, the plan records the
member's byte range in the archive, found in the same pass that hashes it.
Self-contained images are a streamOptimized VMDK, qcow2, VDI, VHDX, VPC or raw.

Execution checks the archive as before: its identity and full SHA-256 before
and after, and each member's digest and byte range in one pass. It unpacks only
the other, small members.

The confined `qemu-img` sees only the held, read-guarded archive, at
`/source/archive`. It opens the disk through a raw node limited to that range.
Inspection must find one image with no backing file, whose every node reads that
range of that file; a VMDK extent anywhere else is refused. Other disks are
unpacked and converted as before.

### 3. Creation can hand over the prepared copy

With `preparedCopy: "hand-over"`, the creation plan asks for
`hand-over-prepared-copy`. This is VM setup's default when **Prepared copy:
Remove after creation** is chosen.

- **Recording the hand-over.** Before the first disk upload, creation durably
  records that the preparation is handed over.
- **Releasing space while copying.** While uploading each disk, it releases
  (punches holes in) the prepared file behind the bytes already sent.
- **Verification is unchanged.** Every new volume is still verified by a full
  read-back against the prepared digest.
- **Pool budget.** When the prepared folder and the pool share a filesystem
  (compared by device at plan and apply) and that filesystem can release file
  ranges (probed at plan), the pool budget no longer counts each disk's file
  size. Otherwise the full size stays reserved.
- **What is not handed over.** ISO media and cloud-init seeds are copied as
  before.

After the record, creation, resume and acceptance compare the stored preparation
receipt instead of re-reading the released files. The preparation is no longer
offered for new VMs. If copying stops after the hand-over began, the prepared
copy is incomplete. The job needs recovery as it does today, and the user imports
the original again; the original is never modified.

## Consequences

- The owner's appliance needs about 34 GiB for preparation and, on the same
  filesystem, little more for creation, instead of about 102 GiB held at once.
- Each disk is still written twice, by the conversion and by the upload. Only
  the peak space drops.
- A failure during upload now costs a new import rather than a retry from the
  prepared copy. To create several VMs from one import, choose **Keep**.
- Measured budgets trust `qemu-img`'s allocation map. The output-file limit
  enforces it.
- In-place conversion keeps the archive open and read-guarded for the whole
  conversion. Changing the archive during preparation fails the preparation, as
  it does for disk sets (ADR 0008).
