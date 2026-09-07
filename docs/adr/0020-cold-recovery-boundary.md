# ADR 0020 — Cold recovery and confidential auxiliary state

Status: architecture selected; complete capture/restore implementation and native
qualification in progress. No scope change and no accepted release case.

Docs 07 and 10 make complete stopped-VM capture the baseline for firmware/TPM
guests. A missing state member is a completeness failure. Readable disk copies,
self-declared manifests, an EFI marker and successful parsing each prove only
their own part of that workflow. Snapshot recovery must preserve the current
branch; independent backup additionally requires encrypted restic storage and
restore without original VM files or the application database.

First resolve native persistent configuration into explicit auxiliary-state facts
without copying or inventing paths. Retain loader format/stateless policy, NVRAM
format/template, TPM source/profile/persistence and external secret UUIDs. Missing
default state paths remain unresolved until an independently checked host adapter
can resolve them. Unsupported external/pass-through TPM, newer unimplemented
varstore and ambiguous secret/path layouts fail visibly. Existing XML remains
authoritative; this view never reconstructs or edits an adopted VM.

Cold capture must bind all disks/backing dependencies, persistent configuration,
firmware code identity, NVRAM and TPM state into a fresh plan. Confirm shutdown,
exclude cooperative writers, retain domain/storage leases, and recheck native
state and file generations before and after copying. Any missing member, changed
source or uncertain completion prevents publication of a complete set. Existing
guests, branches and source artifacts are retained until a separate verified
disposition authorizes deletion.

Confidential auxiliary files stay behind the bounded helper. A generic root file
copy or broad TPM ACL grant is not an acceptable capture API. The selected
mechanism is typed native-identity-bound capture into sealed anonymous file
descriptors, transferred to the authenticated ordinary-user coordinator through
SCM_RIGHTS. No auxiliary bytes enter JSON, logs, plans or helper journals. The
helper independently resolves approved roots, rejects aliases/special files,
checks stopped native state, bounds members/bytes and seals the captured object.
The durable coordinator copies those held artifacts into private staging and
publishes only a complete, reverified manifest. The standalone Linux sealed-FD
transport utility is implemented with adversarial tests; it is not connected to
an authorized helper method. The typed executor, grant binding and publication
still need implementation and native tests. No existing helper operation silently
acquires this authority.

Descriptor sealing fixes content, not the open-file-description offset shared
through SCM_RIGHTS. Downstream consumers must use positional reads from the held
snapshot and close the dedicated transport after its single response. The transport
does not replace peer verification, grant checks or source/metadata binding.

Restore is a separate reviewed operation. Stage and verify every artifact before
definition or reference changes; validate native firmware/CPU/network support.
Default new-identity isolated restore cannot silently reset TPM identity. Recovery
replacement must explicitly preserve required identity, check conflicts and hold
a safety checkpoint before swapping references. Atomic member publication does
not make a multi-file native restore atomic; every externally visible boundary
needs intent, observation and recovery rules.

Legacy recovery-manifest verification continues to check declared metadata/member
integrity. It must reject malformed claims, substitutions, links, special files,
unbounded inputs and cancellation without returning successful verification data.
It cannot infer omitted disks, prove a complete capture, validate repository
encryption, or claim a guest boot. The eventual complete-capture manifest needs a
separate versioned source inventory and provenance contract; old declarations
will not be reinterpreted as that stronger proof.

The versioned `ColdRecoveryPoint` declaration now has a strict bundled schema,
explicit source disk/backing inventory, per-target independent artifact mappings,
firmware code and auxiliary members, secret dispositions and native versions.
The existing CLI/TUI manifest checker dispatches this kind explicitly while
preserving the legacy contract. It checks required declarations and held member
integrity, never capture provenance. Even a self-declared complete inventory must
retain false complete-capture, independent-recovery and boot verification flags.
An auxiliary inventory artifact is a required input to later proof validation;
its mere presence or checksum does not authenticate helper observations.

The source-file utility holds an O_PATH inode pin and a QEMU permission guard.
It checks the previously observed complete file identity before a readable open
and transfers a duplicate of the guarded open file description. Closing a helper
reference must never explicitly unlock that shared description; exclusion lasts
until its final recipient closes. The caller still owes stopped-state, native
resource mapping, root-policy and pre/post-copy checks. Cooperative QEMU locks
do not stop arbitrary writers and do not create privileged read authority. This
utility is not yet connected to a capture endpoint or durable publication job.

The original UEFI guest fixture now records benign TPM NV state and distinguishes
first initialization from subsequent persistence. One exact 4 MiB QCOW2 firmware
tuple reported SEEDED then PRESERVED across two native disposable boots; a separate
2 MiB raw tuple failed TCG2 lookup. Neither set was captured or restored. See the
[native evidence](../evidence/cold-fixes-native-run.md). Actual restored marker
reads can later establish the tested auxiliary identity behavior; they cannot
qualify Windows encryption recovery, guest readiness, full snapshot semantics or
the complete acceptance matrix.

The [native TPM resolver review](../reviews/native-tpm-state-resolution.md) found
that upstream libvirt 12.0.0 intentionally omits implicit emulator storage from
public XML even after initialization. Alternate XML flags are not a complete
inventory API. Explicit state placement for new guests and verified resolution
of existing implicit state are separate integration paths. Neither permits
rebinding the seeded guest to a guessed location. A native launch/process record
can be candidate mapping evidence only when independently tied to its domain,
run, executable, transport and generation; complete stopped membership and
confidential transfer still require the typed host boundary selected above.
