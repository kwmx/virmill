# Starting your own VMs without a manual libvirt step

Probe: `tests/fixtures/release/session_socket_native.py` ([ADR 0064](../adr/0064-per-user-libvirt-socket.md)).
It runs on the authorized Fedora 44 test VM through the installed packages,
against a stopped `qemu:///session` VM with no guest OS.

## Result

`session-socket-native-001` **passed** on build `ba6f7e6`, which the installer
upgraded from the beta.4 packages with guests, networks, source-media metadata
and the helper policy preserved.

| Check | Observed |
| --- | --- |
| Packaged units installed | `virmill-virtqemud.socket` and `.service` under `/usr/lib/systemd/user` |
| Socket starts with the coordinator | active, with no step by the user |
| Daemon's idle exit | observed: the daemon exited by itself after 120 seconds with no client |
| Start through Virmill, no daemon of this user running beforehand | the job succeeded and the VM ran |
| Daemon that served the start | pid 87433 in `virmill-virtqemud.service`, `NoNewPrivs` 0 |
| Daemons inside `virmilld.service` | none |
| `virmill doctor` | libvirt ready, no per-user socket warning |
| Force off through Virmill | stopped |
| Other VMs, prior jobs, source media | unchanged |

This is the case that used to fail. Before this change, the coordinator was the
first program to reach `qemu:///session` after the daemon's idle exit. It
forked the daemon itself, the daemon inherited no-new-privileges, and the start
failed with `cannot execute binary …: Operation not permitted`. Beta.5 turned
that into a clear refusal that still needed a manual step.

## Debian and Ubuntu packages

`session-socket-deb-container-001` **passed** in Debian 13 and Ubuntu 24.04
containers, on the DEBs built from `ba6f7e6`: apt installation, `dpkg --verify`,
`systemd-analyze verify` of all five packaged units including the two new ones,
`virmill version` as an ordinary user, and a purge that left nothing behind.

The check needed two changes, and neither weakens the units. Virmill's packages
do not depend on the libvirt daemon, so the containers do not install it.
`systemd-analyze verify` still refuses a unit whose command is missing, so an
earlier run failed on `/usr/sbin/virtqemud`. Where the daemon is absent, the
check now uses a stand-in executable for verification and removes it again. On
a real host without libvirt the socket skips itself through its own condition.
Debian 13's newer systemd also runs `man` for every `Documentation=` page. The
only pages these units name are libvirt's `virtqemud(8)`, and Virmill ships no
manual pages of its own, so the check passes `--man=no`, which hides nothing
Virmill owns. Ubuntu 24.04's older systemd checks neither, which is why it
passed first.

## How the choice was made

The ADR records the experiments that preceded this run: a control that
reproduced the failure with `setpriv --no-new-privs` alone, the two candidate
fixes, and a first run of the chosen one whose per-process check wrongly
reported the system libvirt daemon before it was corrected. Those were
exploratory and are not ledger entries; this run is.

## Not covered

Only `qemu:///session` on Fedora 44, where libvirt ships no per-user socket
unit. A distribution that ships its own `/usr/lib/systemd/user/virtqemud.socket`
makes Virmill's socket skip itself, and that path is covered by software tests
only. Hosts without a systemd user manager keep the plan-time refusal.
