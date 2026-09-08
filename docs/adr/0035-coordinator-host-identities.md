# ADR 0035: Preserve host identities in the user coordinator

Status: accepted defect correction, 8 September 2026.

Native import preview on the installed Fedora 44 user service rejected the
root-owned, mode-0755, RPM-verified /usr/bin/qemu-img. The running coordinator's
uid_map and gid_map each contained only `1000 1000 1`. Read-only nsenter inspection
confirmed that it saw that executable as UID/GID 65534. The user unit's
PrivateTmp=yes caused an implicit user namespace, despite PrivateUsers=no in
systemctl's displayed properties.

Disable PrivateTmp for the ordinary-user coordinator so it observes host UIDs and
authenticated peer identities correctly. Keep NoNewPrivileges=yes and UMask=0077.
The separate privileged helper unit is unchanged. Untrusted parsers, image tools
and plugins retain their existing individual sandbox policies and private work
directories. Do not weaken root-ownership checks or accept overflow UID 65534.

This corrects a packaging choice against the specification's existing identity
and confinement requirements; it does not change locked scope or service inputs.
Updating an installed unit requires a user daemon-reload and coordinator restart.
On the authorized disposable VM, restart only after recording guests and jobs and
confirming no nonterminal jobs. Startup Recover marks nonterminal jobs uncertain;
it does not replay effects. Verify existing guests/jobs and root tool ownership
afterward. Native preview evidence remains separate from guest installation.
