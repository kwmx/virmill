# ADR 0028: Protected network profiles and separate helper authority

Status: accepted implementation decision; native and full acceptance evidence
remain separate. This extends ADR 0026 without reducing any v1 requirement.

The normative networking behavior and schemas take precedence over example CLI
syntax and engineering sequencing. Services-only NAT/lab and guest-only are
mandatory. They now use the existing declaration, plan, authorization, durable
definition/filter/activation, reconciliation and reviewed recovery workflow.
Existing network configuration is never rewritten through the creation model.

The Network schema has a DHCP switch but no independent managed DNS switch.
For these new protected profiles, that switch explicitly controls the paired
managed DHCP/DNS service. Both are enabled together or disabled together; the
preview and documentation disclose this choice. Services-only permits only the
declared DHCP/DNS traffic and required protocol control traffic. Disabling DHCP
must not leave libvirt's default DNS service implicitly enabled. Native XML
therefore includes an explicit DNS enable setting. Version 1 allowed-host XML
and intent hashes remain unchanged. See the [libvirt network format](https://libvirt.org/formatnetwork.html).

Guest-only has no host IP, DHCP, DNS or forwarding configuration. An optional
explicit private IPv4 CIDR is a logical guest-subnet reservation only: it is not
emitted as a native host address. Without a CIDR, no address allocation exists,
but the exact network and host allocation locks and retained-record integrity
checks still apply. Guests need explicit static addressing or a separately
declared router/DHCP guest. No guest configuration is inferred by creation.

The public declaration API remains `virmill/v1`. Protected internal recipes,
native intent markers, helper requests and ownership journals use version 2.
The existing SQLite metadata collection name remains `network-creation-v1` so
old and new retained allocations are inspected together; each body's version
must exactly match its definition's policy family. This is an additive record
format, not a rewrite or SQL migration. Old version 1 records and failed jobs
retain their exact meaning. Downgrading a protected record is corruption.

Protected requests use the bounded `network.policy-filter` helper operation
and an independent `protectedNetworks` actor/key/network-UUID grant. Existing
`networks` grants authorize only the version 1 IPv6 filter; neither grant
implicitly authorizes the other family. The complete definition is signed and
bound to the original durable plan and resource owner. Storage authority,
arbitrary firewall syntax, deletion, global reload and physical interfaces are
outside both operations.

An ordinary IPv4 filter INPUT direct rule does not establish host isolation:
firewalld's iptables backend can accept established connections earlier, and
libvirt can insert service accept rules in that filter layer. Conversely, an
unconditional bridge INPUT IPv4 drop also prevents NAT routing through the
bridge gateway. The protected services-only rule set therefore uses the earlier
IPv4 mangle INPUT hook, which sees host-local packets while preserving the
separate forwarding path. Exact DHCP/DNS exceptions precede the final ingress
drop for the reviewed generated bridge. The helper checks the actual kernel
chain's reachability and ordered rule projection, rather than inferring runtime
enforcement from the configured firewall backend. Unknown bypasses, jumps,
accepts or ambiguous output cause refusal. See the
[firewalld direct ordering rules](https://firewalld.org/documentation/man-pages/firewalld.direct.html)
and the [implementation review](../reviews/protected-network-filter-design.md).

Guest-only additionally drops IPv4 and ARP at bridge INPUT/OUTPUT, preserving
guest-to-guest IPv4/ARP FORWARD traffic. Both protected profiles retain the four
bridge IPv6 drops from version 1. The new bridge retains the explicitly selected
native trusted zone; the separate scoped earlier drops supply its host-access
boundary. This supersedes ADR 0026's anticipated zone/policy implementation
choice, not the required isolation behavior. Creating permanent-only zones or
policies and reloading the owner's whole firewall is unnecessary for this path.
The direct interface is deprecated; removal or missing capability remains a
capability failure, never permission to silently weaken the profile.

The helper adds identical runtime/permanent entries, with fsynced owner, job
and per-rule intent before every effect. It neither flushes tables nor rewrites
foreign chains. Version 1 keeps its eight exact arguments and journal indices;
version 2 has a bounded indexed rule set and independently versioned owner
records. Exact scoped additional DROP shapes are understood for coexistence,
without granting version 1 authority to create protected rules. Same-job apply
cannot replay. A new reviewed recovery job may finish only identically bound
owned rules while the network is still inactive. Drift retains resources and
locks for reviewed recovery.

Version 2 uses a 240-second overall helper bound, including repeated native,
inventory and kernel-order checks; each external command retains its five-second
bound. Version 1 retains 90 seconds. Client, socket and executor limits agree,
and the signed request expiry also limits execution. Coordinator jobs remain
durable after a client detaches. A privileged external writer can still change
network state between observations; exclusive-writer acknowledgement and
separate reload/reboot and packet qualification remain required.

The helper always reports `packetVerified: false`. Strict XML, rule inventory,
in-memory libvirt and injected crash tests do not prove real guest routing or
hardware isolation. The parent alone runs the disposable-host packet fixtures,
preserving pre-existing guests, networks and supplied media. All 71 acceptance
scenarios, including multi-NIC, IPv6, host recovery and complete restore, remain
required for release.
