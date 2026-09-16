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
- Done: removing the prepared copy after import (ADR 0058), natively for OVA and
  ISO; guest boot and disk order shown on the serial console for the generated
  BIOS OVA.
- Done: guided cloud-image setup (ADR 0059), natively with Ubuntu 24.04's cloud
  image: one approval to a running guest that took a NAT address and accepted
  the reviewed SSH key, with passwordless sudo and cloud-init done.
- Done: `virmill doctor` says when firewalld blocks a NAT network's DHCP, which
  had kept the first cloud guest offline on the test host.
- Done: Create storage pool and Start pool inside VM setup, natively on a
  private session with no pools, each followed by one approval to a started VM
  ([record](evidence/streamlined-setup-run.md#pools-inside-vm-setup)).
- Done: one-copy import (ADR 0060). OVA disks are converted in place, space
  budgets are measured, and the prepared copy is handed over to the pool while
  copying. Natively, the owner's UEFI appliance with a 34 GiB disk imported in one
  approval to a started VM with a peak use of 34.4 GiB
  ([record](evidence/one-approval-import-run.md#owner-appliance-in-one-copy-of-space)).

Phase 1 is complete.

Scenarios: IMP-01, IMP-02, IMP-03, IMP-04, IMP-07, IMP-08, STO-01 (creation).

## Phase 2 — Everyday VM lifecycle

Running, changing and removing VMs is dependable on system and session
connections.

- Done: native start, graceful stop, force off (also from paused), restart,
  pause, resume, save and restore-saved on system and session connections, with
  stale plans refused ([record](evidence/power-run.md)). A shutdown the guest
  ignores fails plainly and leaves the VM free for Force off, now also in the TUI.
- Done: next-boot CPU and memory edits while a VM runs (ADR 0061), natively
  ([record](evidence/running-edits-run.md)).
- Done: growing a stopped VM's disk (ADR 0062), natively on both connections
  ([record](evidence/disk-grow-run.md)).
- Done: removing the VMs Virmill creates and UEFI VMs while keeping the disks,
  the firmware settings file and an emulated TPM's state (ADR 0063), natively
  on both connections ([record](evidence/removal-created-run.md)).
- Done: adding one empty disk to a stopped VM (ADR 0062), natively on the
  session connection ([record](evidence/disk-add-run.md)), with a reviewed
  disposition for an addition that stops before it finishes.
- Done: moving a disk to another storage pool (ADR 0062), natively on the
  session connection in both directions, with the VM booted from the moved disk
  afterwards ([record](evidence/disk-move-run.md)).
- Next: full clones; live CPU and memory changes and boot edits while running;
  stale plans after external edits and unknown XML kept after edits on adopted
  VMs; autostart across a host restart; console access (SPICE and serial) on
  supported desktops; Add, Move and the addition disposition on the system
  connection.
- Done: starting your own VMs without a manual libvirt step (ADR 0064),
  natively on Fedora 44 ([record](evidence/session-socket-run.md)).

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
