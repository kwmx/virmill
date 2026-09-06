# Open inputs and release blockers

- U001: Missing `START-HERE-AGENT-PROMPT.md` from supplied package; other indexed
  members verified. Normative documents suffice to continue (ADR 0001).
- U002: No explicitly authorized disposable libvirt/network/storage/USB test host.
  Host-mutation tests must not run against discovered production inventory.
- U003: `/dev/kvm` is hidden in the default sandbox but exists and is accessible on the outer host. A read-only probe confirmed this without opening it. Guest/hardware claims still need an explicitly authorized disposable target and fixtures.
- U004: Guest/source media and their exact digests unavailable. Proprietary media
  must come from the operator's legitimate source; do not redistribute it.
- U005: Restic missing; record/build exact dependency before repository integration.
- U006: Signing identity/publication destination unset. Build local reviewable
  artifacts; publication and real release signing require owner review.
- U007: The optional shell-profile recipe source is not supplied. Keep the audited
  digest as a source requirement; never fetch-and-execute an unpinned replacement.

Unimplemented code is tracked separately in the requirements tracker and is not
classified as a hardware limitation. None of these unknowns reduces 1.0 scope.
