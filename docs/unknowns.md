# Open inputs and release blockers

- U001: Missing `START-HERE-AGENT-PROMPT.md` from supplied package; other indexed
  members verified. Normative documents suffice to continue (ADR 0001).
- U002: The owner authorized a remote disposable VM for installation and guest
  tests on 2026-09-07. SSH key authentication and read-only system libvirt inventory
  work. Passwordless sudo was subsequently verified. The root filesystem was full
  at intake; storage setup and installation remain pending. The local development host and discovered production
  inventory remain unauthorized for host mutations. Local connection details stay
  in ignored `.virmill-local/test-host.json`.
- U003: The remote test VM exposes `/dev/kvm` and reports KVM virtualization.
  No Virmill guest has been booted there; nested acceleration, firmware/TPM,
  isolation and physical USB support remain unverified.
- U004: One owner-supplied OVA is present on the test VM. Its digest, contents and
  guest compatibility have not been verified. The full required fixture matrix
  remains incomplete. Preserve original media and do not redistribute it.
- U005: Restic missing; record/build exact dependency before repository integration.
- U006: Signing identity/publication destination unset. Build local reviewable
  artifacts; publication and real release signing require owner review.
- U007: The optional shell-profile recipe source is not supplied. Keep the audited
  digest as a source requirement; never fetch-and-execute an unpinned replacement.

Unimplemented code is tracked separately in the requirements tracker and is not
classified as a hardware limitation. None of these unknowns reduces 1.0 scope.
