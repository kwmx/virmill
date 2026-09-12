# ADR 0050: Reviewed VM definition removal retaining storage

Status: implemented baseline, native qualification recorded separately.

The CLI contract in specification document 03 requires `vm remove` to retain
storage by default. This does not authorize reusing failed-creation cleanup,
discarding firmware state, or deleting disk files. Complete CORE-02 remains open.

A shared `vm.remove` service produces the versioned durable operation
`vm.remove-definition-v1`. Both interfaces use it; the TUI reads the selected VM
again and requires its exact name before Preview. Apply has a separate review.
The persisted recipe binds the local connection, UUID, name, complete observed
fingerprint, persistent XML digest and sorted declared storage sources. Raw XML
is excluded from the recipe. This operation creates no configuration backup.

The provider accepts a stopped persistent x86 BIOS VM with automatic startup off,
no managed save, snapshots or checkpoints, and supported file-backed disks.
It refuses protected XML, auxiliary firmware/TPM state, unrecognized devices and
unsupported storage layouts. Unknown opaque metadata is bound by the XML digest.
Source paths are declarations, not a verified backing graph or capture.

The service repeats inventory/catalog-integrity checks and the provider repeats
native inspection on a held domain handle immediately before `UndefineFlags(0)`.
No discard flags, storage API, host helper or guest power operation are used.
Libvirt has no atomic compare-and-undefine API: other lifecycle editors must be
coordinated. A concurrently started or recreated domain is not removed again.
Existing managed creation records remain; successful creation receipts continue
to prevent failed-creation cleanup from treating retained disks as garbage.

Intent precedes the effect. A successful native return is recorded in a durable
receipt bound to the operation and plan. Reconciliation requires that receipt and
exact native UUID absence. Connection errors never mean absence. Missing receipts,
lost acknowledgements or replacement domains require recovery without replay.

This adds the safe default workflow. Selected-disk deletion, auxiliary-state
retention and complete deletion-profile qualification remain mandatory 1.0 work;
there is no implicit scope reduction or simulated hardware claim.
