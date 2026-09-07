# ADR 0017 — Reviewed acceptance of retained creation devices

Status: accepted implementation decision under documents 02, 05 and 10 and ADR
0013. This resolves an implementation recovery gap without changing locked scope.

Automatic define reconciliation must still match the original plan exactly. A
separate `vm creation accept` operation may review explicit versioned chipset
device choices for an existing stopped definition. Its original identity, disk,
NIC, firmware and remaining hardware intent are unchanged. Unknown devices or
configuration remain refused; this is not general-purpose adoption. Neither the
old plan nor its incomplete receipt becomes a successful creation.

The new operation inherits all parent locks atomically. Review binds the original
actor, connection, complete volume receipt, explicit device policy and fresh
observations of the VM, pool, capabilities and file generations. Secure and public
XML must agree. No raw XML or secret values are put into the acceptance record.
Acceptance requires a persistent stopped VM with no managed-save image, snapshots
or checkpoints. Fresh UEFI/TPM auxiliary state is not proved by a disk hash; this
adapter refuses it pending the complete auxiliary-state recovery adapter.

Execution holds read-only QEMU permission guards for the entire disk set, verifies
the complete retained bytes and repeats state/generation/configuration checks.
Missing file-read permissions or guards fail closed without privilege escalation.
External tools outside the cooperative lock protocol remain an explicit reviewed
constraint. Acceptance writes only durable verification and ownership records; it
does not define, start, detach, upload, allocate or delete. Failure/cancellation
retains inherited locks. Recovery rechecks the exact observation and retained
bytes before completing catalog registration, without replaying a host mutation.

The original job stays partial and links to its acceptance child. The child has a
separate versioned proof and result; guest readiness remains false. Ownership
version 2 links the accepted definition to that proof. Older readers reject that
record and unknown operation rather than interpreting the old recipe differently.
SQLite layout remains version 3: inherited lock semantics are unchanged, and the
original receipt/plan are never rewritten. Tests cover this compatibility barrier.

Source, deterministic coordinator, captured XML and local file-guard evidence must
be recorded separately from actual native acceptance. None alone qualifies the
full creation, adoption, crash recovery or guest-support matrix.
