# ADR 0054: Explicit selected-disk removal

Status: implemented; amended 2026-09-16 (files outside storage pools); native evidence recorded separately.

Specification 03 requires definition-only removal by default, with an explicit
disk list and separate acknowledgement for deletion. Specification 07 requires
native and persisted dependency reconciliation. The beta previously implemented
only the retain-storage default. This addition preserves that operation and adds
`vm.remove-disks-v1`; an older binary cannot execute the new recipe as keep-only.

CLI `vm remove UUID --delete-disk vda --delete-disk vdb` and the removal form's
unchecked disk toggles call the same `vm.remove` service. Each target resolves to
an exact registered file volume, key, pool, path, raw/qcow2 format, filesystem
birth generation and metadata fingerprint. The immutable review lists files,
capacity/allocation, kept sources and the separate `data-loss-delete-disks`
acknowledgement. `--yes` does not grant it. Backups are never selected.

The existing stopped BIOS removal guard remains. Read-only media, shared devices,
selected backing parents, hardlink/symlink aliases, unknown volume state, live
images, inaccessible/incomplete pool inventories and uncertain references refuse
deletion. The graph reconciles every registered pool image with confined image
metadata plus VM/saved-state/snapshot/checkpoint XML. Only the exact revalidated
owner definition is omitted from graph references; its saved/snapshot state is
still checked. This refactoring leaves failed-creation cleanup behavior unchanged.
All persisted metadata and unresolved operation recipes are scanned too. Exact
matching entries in this VM's verified allocation history are descriptive history;
source, preparation, backup, snapshot and unknown records receive no exemption.

A single durable engine step records a separate CAS intent/acknowledgement for
undefine and each selected disk deletion. Native and persisted references are
rechecked before each file effect. No wipe, directory deletion, recursive unlink,
auxiliary-state discard or forced guest stop occurs. Cancellation after undefine,
a lost acknowledgement or partial deletion preserves remaining files and locks.
Reconciliation requires complete bound receipts and exact absence; it never
repeats deletion. A partial operation needs an explicit recovery disposition,
not an automatic retry. No SQLite schema migration is needed.

Libvirt has no atomic cross-client compare-and-delete. Exact held-handle checks
and generation observations precede each call; users must coordinate other VM
and storage writers and acknowledge this explicitly. The complete v1 graph,
firmware/TPM removal and recovery qualification remains mandatory. A conservative
unsupported profile is not recorded as a passed support claim.

## Amendment 2026-09-16: files outside storage pools

Native run `new-vm-system-native-002` showed that the graph refused every disk
deletion on a host where any other guest, firmware file or snapshot names a
plain file outside the registered pools. Such hosts are common, so a person
could not remove a VM they had just created together with its disks.

The graph no longer refuses such a reference by itself. It collects each
absolute file named outside the pools, with the format the XML declares for it,
and decides by object identity:

- A selected volume is a single-link regular file (filesystem identity refuses
  hard-linked or special files), so its only directory entry is its pool path.
  Any other name that reaches it goes through a symlink, `..`, repeated
  separators, a magic link or a bind mount, and so resolves to the same device
  and inode. Each outside path is resolved as QEMU opens it, following links,
  and deletion refuses if it reaches a selected volume's device and inode.
- A path that does not exist (`ENOENT`, `ENOTDIR`) reaches no object, as for a
  UEFI VM that never started or a removed installer ISO.
- Any other failure to observe a path refuses, including permission denied on
  a directory: an unreadable directory could hold a symlink to the volume.
- A source declared `raw` (or `iso`) is opened by QEMU without reading a
  backing file, so its identity is enough. A `qcow2` source, and one with no
  declared format whose header is a qcow2 header, is read under the existing
  confined metadata inspector. Its backing file is resolved and checked the same
  way, up to 16 levels, stopping at a reconciled pool volume, whose chain the
  pool graph already inspects. An unreadable header, another image format, a
  non-regular file without a raw declaration, a remote or protocol
  backing name, or a cycle refuses.
- The observed objects and chains enter the graph digest, so a change between
  review and each deletion is `STALE_PLAN`, and every check repeats before each
  file effect.

Exact path references, pool-volume references, shared directories, block and
network sources and active guests keep their earlier refusals. As before,
libvirt has no atomic compare-and-delete: a link created after the last recheck
is an external writer race that the acknowledgement already covers. A view of
the volume through a different filesystem (FUSE, NFS re-export, overlay) has a
different identity and is not detected; such setups need their own adapter.
