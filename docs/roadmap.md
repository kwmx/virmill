# Roadmap to a fully working Virmill

Goal: a virtual machine manager that works end to end for someone who just wants
VMs, with advanced settings available on demand. Each phase ends with native
evidence on the test VM, not only software tests. Extensions, labs and templates
come after these phases. All 71 acceptance scenarios stay mandatory for 1.0; the
IDs below link each phase to them.

## Phase 1 — One-step import and creation

A disk image, ISO or OVA becomes a running VM in one review.

- Done: one approval prepares, creates and starts (ADR 0057). The durable
  operations stay separate; a disk is still copied twice. Native disk-image run
  passed ([record](evidence/one-approval-import-run.md)).
- Done: Custom pool (name, folder, autostart) and Start pool for a stopped pool,
  in software tests.
- Done: native one-approval walk-throughs from a disk image, an installer ISO
  (with the connected NAT default) and a generated two-disk OVA, each to a
  started VM.
- Remaining: guest boot verification; in-setup Create and Start pool natively;
  a real owner OVA with a declared format; reclaiming the prepared copy's space.

Scenarios: IMP-01, IMP-02, IMP-03, IMP-04, IMP-07, IMP-08, STO-01 (creation).

## Phase 2 — Everyday VM lifecycle

Running, changing and removing VMs is dependable on system and session
connections.

- Native qualification of start, stop, force stop, reboot, pause, save, restore,
  autostart and delete, and of live versus next-boot CPU, memory and boot edits.
- Refusal of stale plans after external edits; unknown XML kept after edits.
- Disk management: add, grow and move disks; full clones.
- Console access (SPICE and serial) on supported desktops.

Scenarios: CORE-01…06, STO-01, STO-02, DEV-04 (console part), UX-02, UX-03.

## Phase 3 — Networking that just works

- Verified NAT internet and DNS; lab, services-only and guest-only isolation;
  IPv6 policy; CIDR overlap detection; dual-homed warnings.
- Using an existing LAN bridge; Wi-Fi guidance with NAT; port forwards.

Scenarios: NET-01…07, NET-10, NET-11, NET-12.

## Phase 4 — Protection

- Cold snapshots and revert, including firmware/TPM identity.
- Encrypted backups with restore, retention and pruning, schedules, and test
  restores.

Scenarios: SNAP-01, SNAP-03, BAK-01, BAK-04, BAK-05, BAK-06, STO-03.

## Phase 5 — Guests and devices

- Cloud-init users and keys, guest-agent readiness, guest recipes with honest
  guidance when access is missing.
- USB attach and detach with stable identities; read-only PCI discovery.

Scenarios: GUEST-01…03, DEV-01…03.

## Phase 6 — Reliability, security and 1.0 release

- Jobs survive client exits, crashes and reboots; retries stay idempotent;
  concurrent clients are serialized.
- Security boundaries: client identity, helper actions, secrets, hostile input.
- RPM/DEB install, upgrade and uninstall that keep user data; documentation that
  matches the commands; the full suite on the frozen support matrix.

Scenarios: JOB-01…04, SEC-01…03, UX-01, REL-01, REL-03, REL-04.

## After 1.0 — extra features

- Extensions: forms, tables, status cards, mutating actions and providers
  (EXT-01, EXT-03…05, SEC-04, SEC-05).
- Repeatable labs, templates and linked clones (LAB-01…05).
- Managed bridge creation with rollback (NET-08, NET-09).
- Live snapshots and live backups with guest quiescing (SNAP-02, BAK-02, BAK-03).
