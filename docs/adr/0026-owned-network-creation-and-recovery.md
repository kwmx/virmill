# ADR 0026: New network creation and retained activation recovery

Status: accepted implementation decision; network acceptance remains open.

The networking document controls the required behavior. All network types,
restricted host access, IPv6 policy, bridge rollback, port forwards and packet
qualification remain mandatory. Engineering slices do not change that scope.

The first executable creation profiles are NAT with explicitly allowed host
access and unrestricted egress, and a lab with explicitly allowed host access
and no external forwarding. Both require an explicit private IPv4 subnet and
explicit disabled IPv6. These are useful declarations permitted by the normative
contract. Services-only, deny, guest-only, external bridges, automatic allocation,
internet-only and local-only IPv6 creation must return a capability error until
their full enforcement path is implemented. The service must never reinterpret
one of these as an allowed-host profile.

Libvirt owns its new virtual bridge, native NAT/isolated forwarding and optional
DHCP/DNS. An explicit trusted zone applies only to these allowed-host profiles.
The service does not modify physical interfaces, global sysctls, unrelated zones
or rules. Packet behavior is unverified until separately measured. Installed
firewalld exposes new-zone/new-policy creation only through permanent
configuration; installing a restrictive profile therefore cannot be treated as
a runtime-only change or implemented by silently reloading the firewall. Restricted host-access zone/policy integration and preservation/reload tests
remain required.

Disabled IPv6 additionally requires bridge-frame filtering. Zones and ordinary
IPv4/IPv6 policies cannot by themselves deny guest-to-guest Ethernet IPv6 frames.
For this supplemental L2 boundary the bounded helper uses firewalld-owned `eb`
direct rules, the documented last resort when zones cannot express a rule. It
adds exactly four scoped DROP rules to runtime and permanent configuration, with
no arbitrary rule syntax, global table edits or reload. The direct API is
deprecated; this adapter is capability-checked and must be replaced if removed.
It does not replace the required zone/policy ownership of host-access controls.
See [firewalld direct options](https://firewalld.org/documentation/direct/options.html)
and [the deprecation notice](https://firewalld.org/documentation/man-pages/firewalld.direct.html).

Each new network requires a separate exact actor/key/UUID administrator grant.
Preview can supply its fresh UUID before that grant exists; apply refuses before
definition if the helper grant or firewalld preflight fails. Storage roots grant
no firewall authority. The root helper independently verifies native XML and
inactive state, records a resource binding, then fsyncs job and per-rule intents
before each request. Same-job apply never replays. A new reviewed recovery job
can finish identical missing rules only when previous durable intents prove
ownership. Unknown bridge-family direct rules refuse the operation; unrelated
IPv4/IPv6 rules remain unchanged. Packet proof is always separate.

A plan allocates a fresh UUID, UUID-based native name and bridge name, while
retaining all declared display metadata. The preview binds the complete typed
definition, metadata and effects. It does not reserve a subnet. Apply takes a
host network allocation lock and a network identity lock, rechecks host addresses,
every routing table, both XML layers and retained application reservations, then
persists its versioned reservation before requesting native definition. Definition, IPv6 filter preparation
and activation have separate durable operation intents. Autostart stays disabled.
The existing SQLite metadata table holds versioned records; no SQL migration is
needed. Records survive failures and coordinator restart.

Libvirt definition is an upsert API without create-only/CAS semantics. Repeated
absence checks prevent ordinary collisions but cannot atomically exclude another
authorized writer. Explicit exclusive-network-writer acknowledgement is required.
No claim of cross-process atomic exclusion is made.

Reconciliation observes the exact retained definition and never replays either
native call. An uncertain definition may have its filter completed and be activated only by a fresh reviewed
recovery plan after exact inactive-state verification and atomic inheritance of
every original resource lock. The original job stays partial. Cancellation or validation failure after a completed step retains the full lock
set as recovery-required. A missing or changed definition retains its reservation
and fails recovery. All reserved prefixes are derived only after validating the
metadata key, original plan/input digest, complete definition/metadata, and
accepted job identity. No deletion or reservation
reuse is implicit. Those workflows remain required subsequent work.

The declaration is read once into the immutable plan. Later edits to that source
file do not alter the approved plan. Namespace/veth probes are real kernel packet
evidence, but cannot qualify real-guest multi-NIC behavior or physical hardware.
