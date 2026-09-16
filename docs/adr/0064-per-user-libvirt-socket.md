# ADR 0064: Ship a per-user libvirt socket so session VMs start without a manual step

Status: accepted and implemented in `ba6f7e6`, with native evidence
`session-socket-native-001` ([run record](../evidence/session-socket-run.md)) on
Fedora 44.

## Context

`virmilld.service` sets `NoNewPrivileges=yes`, which the owner chose to keep. On a
host with no per-user libvirt socket, the first program to use `qemu:///session`
forks `virtqemud` as its own child. When that program is the coordinator, the
daemon inherits no-new-privileges, cannot transition SELinux domains on exec,
and can never launch QEMU: every VM start fails with
`cannot execute binary …: Operation not permitted`. Fedora 44 ships libvirt's
units for the system instance only, and the daemon exits two minutes after it
is last used, so the condition keeps returning.

Beta.5 refuses such a boot while it is still a plan and names a workaround, but
the user still had to run a libvirt client by hand, again after every idle exit.

## What was tested

On the test host, using the probe VM with no guest OS:

| Case | Daemon parent | `NoNewPrivs` | cgroup | VM start |
| --- | --- | --- | --- | --- |
| Control: `virtqemud` run directly under `setpriv --no-new-privs` | that process | 1 | login session scope | refused, exec not permitted |
| A: the same restricted process runs `systemd-run --user … virtqemud` | user systemd | 0 | the transient service | started |
| B: a user socket unit; the daemon started on connection | user systemd | 0 | `virmill-virtqemud.service` | started |

The first run of B looked successful but was measured wrongly: the check listed
every `virtqemud` on the host and reported the system instance, which an earlier
`virsh -c qemu:///system` had started. The unit's journal showed the socket
starting the service on each connection, including after it was stopped. B was
rerun with daemons selected by owner. Through Virmill, with the coordinator
restarted and no daemon running, `vm start` succeeded. The daemon that served
it ran in `virmill-virtqemud.service` with no-new-privileges off, none ran inside
`virmilld.service`, and Force off stopped the VM.

## Decision

Ship `virmill-virtqemud.socket` and `virmill-virtqemud.service` as user units
in both packages, and have `virmilld.service` want the socket and order itself
after it. The socket listens at `%t/libvirt/virtqemud-sock` and hands its file
descriptor to the daemon under the name libvirt adopts, `virtqemud.socket`. The
daemon runs `virtqemud --timeout 120` as libvirt's own units do, and does not
set no-new-privileges.

The socket skips itself where the distribution ships its own
`/usr/lib/systemd/user/virtqemud.socket`, so the two never contend for one path,
and where `/usr/sbin/virtqemud` is not installed, so it never listens where no
daemon could answer. The coordinator's hardening is unchanged.

Option A was rejected. It fixes the parent, but the daemon still exits when
idle, so the coordinator would have to check it before every connection. If it
exited between that check and the connect, libvirt's client would fork a
restricted daemon again. With a socket there is nothing to race: the socket
outlives the daemon and starts it again on demand. B also serves every libvirt
client for the user, not only Virmill.

## Consequences

- Session VMs start with no manual step on hosts like the test host, including
  after the daemon's idle exit.
- The plan-time refusal stays as a safety net. Its advice is now to restart the
  coordinator, which starts the socket and lets the coordinator decide again.
  `virmill doctor` gives the same command, or the distribution's socket first
  where one ships.
- Installing the packages adds two user units that start with the coordinator.
  Nothing is enabled system-wide, and a user who never runs Virmill gets no
  socket.
- Where the socket unit skips itself because the distribution ships one, that
  unit must be enabled once. Virmill does not enable it for the user.
