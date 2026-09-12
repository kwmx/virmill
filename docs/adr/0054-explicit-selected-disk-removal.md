# ADR 0054: Explicit selected-disk removal

Status: implemented; native evidence recorded separately.

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
