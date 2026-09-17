# Phased plan to 1.0

This plan continues [the roadmap](roadmap.md) and does not replace it. Phases 1
and 2 are the ones with native evidence today; the phases below are what is
left, in the order that keeps Virmill usable at every step. Each phase ends with
native evidence on the authorized test host, and all 71 acceptance scenarios
stay mandatory for 1.0.

Judge each phase by a real screen and a real host, not by software tests.

## Phase 2b — Finish everyday lifecycle (next)

Phase 2 is usable but uneven: storage edits are qualified on your own session
connection only, and one host configuration still needs a manual step.

Done first, ahead of the list: **New VM** (ADR 0065). The owner could not create
or import a VM in beta.5. From a file to a running VM with its display open now
takes one settings page and one confirmation, natively on a host with no pools
([record](evidence/new-vm-run.md)), on both connections.

1. **Done: start session VMs with no manual libvirt step** (ADR 0064). The
   packages ship a per-user libvirt socket that the coordinator's service starts
   before itself, so the daemon is always started by the user's systemd and
   never inherits the coordinator's hardening. Natively on Fedora 44, a VM
   started through Virmill after the daemon's own idle exit
   ([record](evidence/session-socket-run.md)). Distributions that ship their own
   per-user socket are software-tested only.
2. **Done: add, move and the addition disposition on `qemu:///system`.**
   Natively on Fedora 44 with an Ubuntu guest: an added disk and a moved disk
   both booted, and Move deleted the original pool volume. Closing an addition
   interrupted by a coordinator crash passed in all three states it can be left
   in. Two ways to be left with a locked VM were found and fixed: an addition
   whose volume never existed, and one whose unused volume Virmill cannot prove
   safe to delete because root-only pool images cannot be read. The new **keep**
   choice releases the VM without deleting anything
   ([record](evidence/disk-system-run.md)). Deleting such a volume still needs a
   privileged, read-only proof, a design decision shared with VM removal.
3. **Done: move a guest's own boot disk, then boot it.** Natively on the system
   connection, the Ubuntu 24.04 guest's `sda` moved to another pool and back,
   and each time the guest booted from it and shut down gracefully; libvirt
   counted the guest's reads and writes on the moved disk
   ([record](evidence/disk-system-run.md)). Move needs a stopped VM; moving the
   disk of a running VM is not planned before 1.0.
4. **Done: full clones** (ADR 0066). `vm clone` copies every writable disk with
   Move's verified copy and defines an independent VM with a new UUID and MAC
   addresses; the original is never changed. Natively on the system connection
   an Ubuntu guest with nine disks was cloned and the clone booted from its
   copies ([record](evidence/vm-clone-run.md)). **Open decision:** STO-02 also
   names linked clones; whether they stay in the 1.0 scope or move after 1.0 is
   the owner's call.
5. **Live CPU and memory, and boot edits while running.** The next-boot path
   exists; the live path needs its own reviewed operation and refusals.
6. **Console access (SPICE and serial)**, which DEV-04 needs and which makes
   every other guest problem diagnosable.
7. **Autostart across a host restart**, and stale plans after someone edits a
   VM outside Virmill, with unknown XML preserved on adopted VMs.

Scenarios: CORE-01…06, STO-01, STO-02, DEV-04 (console), UX-02, UX-03.

## Phase 3 — Networking that just works

Unchanged from the roadmap: verified NAT internet and DNS, lab,
services-only and guest-only isolation, IPv6 policy, CIDR overlap detection,
dual-homed warnings, an existing LAN bridge, Wi-Fi guidance, port forwards.

Networking comes before protection because a VM with no network is not yet
useful, and because backup and restore over a network exercise it.

Scenarios: NET-01…07, NET-10…12.

## Phase 4 — Protection

Cold snapshots and revert including firmware and TPM identity; encrypted
backups with restore, retention, pruning, schedules and test restores.

Scenarios: SNAP-01, SNAP-03, BAK-01, BAK-04…06, STO-03.

## Phase 5 — Guests and devices

Cloud-init users and keys, guest-agent readiness, guest recipes with honest
guidance when access is missing; USB attach and detach with stable identities;
read-only PCI discovery.

Scenarios: GUEST-01…03, DEV-01…03.

## Phase 6 — Reliability, security and the 1.0 release

Jobs surviving client exits, crashes and reboots; idempotent retries;
serialized concurrent clients. Security boundaries: client identity, helper
actions, secrets, hostile input. RPM and DEB install, upgrade and uninstall
that keep user data; documentation that matches the commands; the full suite on
a frozen support matrix.

Scenarios: JOB-01…04, SEC-01…03, UX-01, REL-01, REL-03, REL-04.

## Rules this plan keeps

- A scenario is accepted only with evidence at its stated level. Nothing is
  promoted to make a release possible, and `make release-check` is never
  weakened.
- Every phase ships as an owner-test pre-release so the work is usable and
  reviewable before 1.0.
- Each operation that can fail part way ships with a way out: either its effect
  can be proven by observation, or it has a reviewed disposition. An operation
  without one can strand a VM, which happened twice this cycle.
- A failure must name its cause. Two releases' worth of native runs were spent
  on refusals that reported one shared message.

## After 1.0

Extensions, labs, templates and linked clones, managed bridge creation with
rollback, and live snapshots and backups with guest quiescing, as the roadmap
lists.
