# Open inputs and release blockers

- U001: Missing `START-HERE-AGENT-PROMPT.md` from supplied package; other indexed
  members verified. Normative documents suffice to continue (ADR 0001).
- U002: The owner authorized a remote disposable VM for installation and guest
  tests on 2026-09-07. SSH key authentication and read-only system libvirt inventory
  work. Passwordless sudo and expanded storage are ready. Development packages
  were installed/upgraded and the first native run is recorded in
  `evidence/disposable-first-run.md`. The local development host and discovered production
  inventory remain unauthorized for host mutations. Local connection details stay
  in ignored `.virmill-local/test-host.json`.
- U003: The remote test VM exposes `/dev/kvm` and reports KVM virtualization.
  One Virmill-created Kali Q35/BIOS clone reached its graphical login screen,
  QMP confirmed KVM enabled, and graceful shutdown passed. Firmware/TPM,
  isolation and physical USB support remain unverified. See `evidence/device-policy-run.md`.
- U004: Six owner-supplied samples are inventoried with exact hashes. The Windows
  OVA's provided checksums validate after a parser fix; one Kali QCOW2 passed
  independent preparation and native disk readback. Guest compatibility and the full required fixture matrix
  remains incomplete. Preserve original media and do not redistribute it.
- U005: Restic is absent on the local build host; the test VM has
  `restic-0.19.1-1.fc44.x86_64` and `virt-v2v-2.12.0-1.fc44.x86_64`.
  Availability is recorded, but adapters and full qualification remain required.
- U006: Signing identity/publication destination unset. Build local reviewable
  artifacts; publication and real release signing require owner review.
- U007: The optional shell-profile recipe source is not supplied. Keep the audited
  digest as a source requirement; never fetch-and-execute an unpinned replacement.
- U008: Native Q35 creation adds unreviewed controllers, inputs, audio, balloon and
  reset-watchdog settings. Strict comparison retains the failed operation and
  powered-off VM. Versioned explicit device intent passed the separate Q35/BIOS native
  creation/boot/shutdown test. Compatible acceptance of the retained definition
  and other native profiles remain required. Old recipes still refuse changed semantics.

- U009: During initial validation, `vm creation result` suggests recovery before a
  receipt exists, although the job is still running. Report active stages without
  premature recovery guidance; preserve uncertainty errors for actual failure.

- U010: Native lifecycle plans inherit the engine’s zero-valued downtime estimate,
  including `requiresDowntime: false` for explicit stop. Human-readable risks exist,
  corrected by the operation-specific estimator in ADR 0015. Focused service,
  engine and CLI/TUI tests pass; the earlier runtime evidence is unchanged. Exact
  capacity/duration estimation for the full matrix remains required.

Unimplemented code is tracked separately in the requirements tracker and is not
classified as a hardware limitation. None of these unknowns reduces 1.0 scope.
