# Phased plan to 1.0

This plan continues [the roadmap](roadmap.md) and does not replace it. Phases 1
and 2 are the ones with native evidence today; the phases below are what is
left, in the order that keeps Virmill usable at every step. Each phase ends with
native evidence on the authorized test host, and all 71 acceptance scenarios
stay mandatory for 1.0.

Judge each phase by a real screen and a real host, not by software tests.

## Release targets

Every phase ships as an owner-test pre-release, so the work is usable before
1.0. One release may hold one phase or half of one, never half a feature.

| Release | Carries | State |
| --- | --- | --- |
| beta.10 | Live CPU and memory, boot edits while running, the virtio balloon default (phase 2b item 5) | ready, unreleased |
| beta.11 | Console access: SPICE viewer and serial, on supported desktops (item 6) | next |
| beta.12 | Autostart across a host restart; stale plans after outside edits with unknown XML kept on adopted VMs (item 7). Closes phase 2b | planned |
| beta.13 | Phase 3a: NAT internet and DNS, lab/services-only/guest-only isolation, IPv6 policy, CIDR overlap, dual-homed warnings | planned |
| beta.14 | Phase 3b: an existing LAN bridge, Wi-Fi guidance with NAT, port forwards | planned |
| beta.15 | Phase 4: cold snapshots and revert; encrypted backups with restore, retention, pruning, schedules and test restores | planned |
| beta.16 | Phase 5: cloud-init users and keys, guest-agent readiness, guest recipes, USB attach and detach, read-only PCI discovery | planned |
| beta.17 | Phase 6a: jobs surviving client exits, crashes and reboots; idempotent retries; serialized concurrent clients | planned |
| 1.0.0-rc.1 | Phase 6b: security boundaries, package install/upgrade/uninstall keeping user data, documentation matching the commands, the full suite on the frozen support matrix | planned |
| 1.0.0 | Every one of the 71 scenarios accepted with evidence at its stated level, `make release-check` green | planned |

The long pole is not the features: 57 of the 71 scenarios are in progress with
work behind them, 12 are not implemented, and 2 are accepted. Acceptance needs
evidence at each scenario's stated level, so phases 3 to 6 each carry the
evidence for their own scenarios rather than leaving a pile of it for the rc.

## Known gaps carried forward

These are not phases; each blocks an acceptance promotion and is named where it
belongs.

- Deleting a disk or an unused volume on a system connection whose pool images
  only root can read is refused with no way through. The design decision is
  shared with VM removal ([note](evidence/removal-created-run.md)).
- Full clones have no native UEFI run, no run into a chosen pool with `--pool`
  and no raw-disk run (ADR 0066).
- Live changes are qualified on the system connection only, spare CPU slots
  cannot be chosen at creation, and a guest with no balloon driver is covered by
  software tests alone (ADR 0068).

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
   copies ([record](evidence/vm-clone-run.md)). The owner moved linked clones,
   which STO-02 also names, to after 1.0 (ADR 0067).
5. **Done: live CPU and memory, and boot edits while running** (ADR 0068).
   `vm set` with `applyMode: "now"` plans `vm.resources-live-v1`, which changes
   what a running VM is running with and leaves its saved settings and next boot
   alone; the TUI offers it as **Change while it runs**. Memory moves through the
   guest's virtio balloon, which new VMs now get by default, and CPUs plug in and
   out within the spare slots a definition carries. Boot order and media follow
   ADR 0061's rule as well: changed while running, applied at the next start.
   Natively on the system connection a Kali guest went 2048 → 1536 → 2048 MiB and
   2 → 3 → 2 CPUs with its definition untouched, an Ubuntu guest's boot order
   changed while it ran and took effect at its next start, and every refusal named
   what to change instead ([record](evidence/live-resources-run.md)). Spare CPU
   slots chosen at creation are not written yet, and the refusal says so.
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
