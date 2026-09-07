# ADR 0009 — Installation artifacts and read-only media

Status: accepted routine implementation decision. The mandatory creation-source,
shared-service, preservation and durable-operation rules take precedence over
interpreting a copied ISO or successful definition as an installed guest. ISO,
cloud, existing-disk and full 1.0 workflows remain locked scope. This decision
resolves the new-source representation without authorizing host mutations.

`import.prepare-install` prepares one explicitly selected ISO and 1–64 explicitly
sized empty disks. It reuses source descriptor guards, canonical plans, resource
locks and private-receipt-before-publication semantics. A bounded header recognizer
reads volume identifiers without parsing an untrusted filesystem as root or
inferring publisher authenticity/bootability. The source remains held read-only
through copying, blank-disk creation and final revalidation. Copies are private,
flushed and digest verified. QEMU runs only through the existing confined worker.

An empty disk has no source chain or OVF hardware. The `PreparedInstallation` kind
therefore records an empty source path/chain and explicit check/zero-map evidence,
while `sourceFiles` records the actual ISO provenance. The separate media list
records raw ISO bytes, recognition level and verification. Existing OVA/disk-set
schemas stay unchanged and reject the new kind. NoCloud can later use explicit
read-only media attachment, but this preparation recipe creates no seed or guest
configuration and does not claim that workflow is implemented.

Creation uses one ordered durable volume set: writable disks first, read-only
media after them. `contentType: cdrom-iso` distinguishes a raw ISO volume from the
legacy empty content type, which continues to mean qcow2 disk. The new field is
omitted from old receipt shapes. Unknown types fail closed. Native definition uses
`device=cdrom`, the raw driver and `readonly`; capability probing positively
requires CD-ROM and the chosen SATA/SCSI bus. Boot orders are reviewed across all
boot candidates, and a media order of zero explicitly excludes it. Native device
metadata probes and test-driver XML validation cover both controller choices;
these do not validate real guest I/O. The
[libvirt domain-capability contract](https://libvirt.org/formatdomaincaps.html#hard-drives-floppy-disks-cdroms)
defines the separate device and bus capability lists.

Libvirt documents `iso` as a CD-ROM storage-volume format in addition to `raw`.
Media readback accepts either metadata representation, with exact capacity,
SHA-256 and no backing metadata. Guest disk readback remains qcow2-only. Cleanup
normalizes ISO to raw solely for its fixed QEMU inspection and backing-edge
comparison, retaining original native XML in the graph digest. The stale raw/ISO
qcow2-header check prevents hiding backing edges. See the
[documented directory-volume formats](https://libvirt.org/storage.html#valid-directory-volume-format-types).
This is a compatibility decision from the documented contract, not an observation
of native pool refresh in the current environment.

Cancellation joins workers before removing only unpublished staging; uncertainty
retains the stage/locks. Reconciliation matches kind, plan/input digest, private
receipt and owning operation after journal reopen, without replaying copy/create.
Creation recovery includes every medium and disk before definition. Incomplete
sets remain recovery-required. Successful preparation does not depend on keeping
original ISO bytes, although provenance remains conservatively referenced until
an explicit reference-disposition workflow exists.

SQLite schema 3 is unchanged: this adds an operation/kind and additive typed
fields, not tables or reinterpretation of old values. Older binaries reject the
unknown handler, manifest kind or strict-decoded media/content fields. Existing
migration/downgrade checks continue to apply. New/legacy schema separation, future
input refusal, source drift, partial failure, cancellation, publication reopen,
media mapping and recovery are tested. Hardware verification and full install
lifecycle remain release blockers. Exact generated-fixture tool versions are
recorded in the dependency contract; no fixture generator is a new runtime dependency.
