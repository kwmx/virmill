# ADR 0067: Linked clones move after 1.0

Status: accepted. Owner-approved scope amendment, 2026-09-17.

## Context

Acceptance scenario STO-02 in the v1 specification reads: "Full clone is
independent; linked clone retains declared immutable base dependencies." Full
clones are implemented and natively qualified (ADR 0066,
[run record](../evidence/vm-clone-run.md)). Linked clones, which share a
read-only base image with the VMs made from it, need a dependency model for the
base: which VMs use it, that it is never written or deleted while they do, and
how removal, move and backup treat it. None of that exists yet.

The roadmap already lists linked clones with labs and templates after 1.0, and
the phased plan asked the owner to decide.

## Decision

The owner decided on 2026-09-17 to move linked clones after 1.0. For 1.0, STO-02
is satisfied by full clones: a full clone is independent of the VM it was copied
from.

This follows ADR 0001's precedence, where owner decisions come first, and the
ship checklist's provision for an explicit owner-approved scope amendment.

## Consequences

- The specification is not edited: its STO-02 text and the traceability matrix
  that checks requirement records against it stay as they are, and
  `make release-check` is not weakened. The requirement record for STO-02 notes
  this amendment, and a 1.0 release must cite it when STO-02 is accepted on full
  clones alone.
- Linked clones join labs and templates in the roadmap's after-1.0 work.
- Nothing already built depends on linked clones.
