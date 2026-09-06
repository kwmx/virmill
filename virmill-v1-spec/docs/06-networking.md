# 06 — Networking and multi-NIC behavior

## Product model

Networks and NICs are separate resources. A VM can have zero or many NICs, each connected to one network. A NIC can be changed without redefining unrelated interfaces. Network membership does not itself configure routing inside an already-installed guest.

Do not overload the word “bridge”: a libvirt NAT network normally uses an internal host bridge too. In the UI, **LAN bridge** specifically means connection to an existing physical-LAN bridge, not simply the presence of a Linux bridge device. Libvirt documents NAT, routed, isolated and existing-bridge configurations separately. [S15]

## Network types

| Type | Egress | Host access | DHCP/DNS ownership | Typical use |
|---|---|---|---|---|
| `nat` | Host-routed NAT, subject to explicit egress policy | Explicit services-only or host-access policy | Managed by backend for this network | Routine internet-capable guests, including Wi-Fi hosts |
| `bridge` | Whatever the physical LAN provides | Same-LAN exposure, not controlled isolation | External by default | VM appears as a LAN device |
| `lab` | No host forwarding outside segment by default | `services-only`, `allow`, or explicitly supported deny policy | Optional managed DHCP/DNS | Multiple lab guests with controlled host services |
| `guest-only` | No host routing | No host IP on this segment; defense-in-depth filtering | No host DHCP/DNS in baseline; static guest addresses or a user-created router/DHCP VM | Private guest segment without host L3 presence |

A routed network is an advanced configurable extension of the network service when tested; the mandatory user flows do not require a routed external subnet. Existing VLAN-aware bridges may be used with capability checks; do not promise all VLAN tagging modes on every bridge/backend.

### Host access is not the same as internet isolation

Libvirt's ordinary isolated network prevents forwarding to other networks but may still allow access to the host. Its default firewalld zone can permit host services. `port isolated` isolates guest ports from one another, not guests from the host. The product must encode and enforce the intended host policy rather than infer it from “no NAT.” [S15]

`services-only` permits only the explicitly configured network DHCP/DNS services and necessary protocol control traffic. It does not automatically expose host SSH, dashboards or arbitrary host services. `guest-only` removes host L3 addressing on that bridge and adds appropriate ingress/forwarding restrictions; it is not a claim the hypervisor cannot observe guest traffic or that VM escape is impossible.

## Multi-NIC route intent

For each NIC store `defaultRoute`, address mode, DNS intent, route metric and optional static routes. Exactly one IPv4 default route is the normal profile; IPv6 route intent is separately validated. Multiple defaults require an explicit advanced policy and a guest configuration method known to support it.

For managed cloud images, generate renderer-compatible network configuration keyed by MAC/stable NIC identity. Prefer the internet NIC for the default route and DNS. Lab NICs receive connected routes but no default gateway. Suppress default-route advertisement on managed lab DHCP networks; verify this with packet/guest tests instead of assuming backend behavior.

Cloud-init's version-2 support is not identical across all guests/renderers. Use a tested renderer adapter or a compatible supported format, and do not blindly write Netplan-only settings to every Linux image. [S16]

For imported guests without a supported provisioning channel, attach the NICs and provide a concrete guest configuration task. Report `guest-routing-unverified` until checked. Never silently reconfigure a guest simply because a virtual NIC was attached.

## Example intended topology

```text
Internet/LAN
    |
host uplink -- NAT network -------- workstation NIC 1 (default route)
                                   workstation NIC 2 ---- research lab
                                   workstation NIC 3 ---- targets segment
                                                          target VM NIC 1
```

The workstation can access several segments and may become a router if configured inside the guest. Attaching a VM to both a protected segment and an internet/LAN network therefore raises a **dual-homed guest** warning. The tool must not describe downstream guests as securely isolated against a compromised dual-homed router. Provide an isolation audit that finds those paths. Libvirt explicitly notes that a guest router can connect an otherwise isolated network elsewhere. [S15]

## Address allocation

Offer automatic CIDR allocation from configurable private IPv4 ranges. Compare against non-default host routes, active and inactive libvirt networks, planned lab networks, VPN/tunnel routes and existing application allocations. Do not treat the default route as an overlap with every possible subnet. Never assume a hard-coded private range is unused.

Reject overlapping attached networks in the normal VM profile. Overlaps across independent labs require an explicitly supported routing-domain isolation feature; do not pretend namespaces exist when they do not. Validate DHCP bounds, static reservations, network/broadcast addresses, MAC uniqueness and address-family compatibility.

IPv6 is explicit per network: disabled, local-only or a supported external mode. Disabling IPv6 for a newly managed network means enforce that segment's intended behavior; never disable it globally on the host. Existing external bridges are observed rather than silently rewritten. Test link-local and router-advertisement paths; IPv4 firewall success is not IPv6 isolation evidence.

## NAT egress and forwards

Normal NAT allows external communication through the host; it does not inherently prohibit private LAN access. Offer distinct `any`, `internet-only`, and `none` policies. `internet-only` applies explicit private/local destination restrictions with carefully scoped DHCP/DNS exceptions and both address families. Document its limitations and dual-homed bypass paths.

Port-forward objects contain protocol, host bind address/port, destination VM/NIC/port and lifecycle policy. Default host bind is loopback; binding all interfaces or a LAN address requires a visible exposure acknowledgement. Resolve DHCP-backed targets with stable reservations; do not silently point a forward to a different VM after an address changes. Check conflicts, restore rules after restart, and remove only rules owned by the application.

Do not manipulate router UPnP, external firewalls or public DNS automatically. A bridge connection also does not guarantee the physical network has internet service.

## Creating a LAN bridge safely

Use an existing bridge with no host reconfiguration whenever possible. Guided bridge creation is included for certified **NetworkManager** and **Netplan/systemd-networkd** configurations. Other host stacks may use existing bridges but are not automatically claimed as supported bridge-creation stacks.

1. Inspect uplink, manager, current addresses/routes/DNS, VLAN/bond membership and whether the control session depends on it.
2. Reject ordinary Wi-Fi client bridging as a default solution; offer NAT. Special Wi-Fi configurations are not assumed. [S17]
3. Produce a plan that moves the host's L3 configuration to the bridge where appropriate, preserves management connectivity and names every changed object/file.
4. Save a recoverable checkpoint and arm an independent rollback watchdog before applying. NetworkManager provides checkpoint APIs. Netplan's `try` has documented rollback caveats, so it is not sufficient proof that the prior state was restored. [S18] [S19]
5. Apply through the owning manager, not a mix of transient `ip` mutations and persistent files that disagree.
6. Verify addresses, routes, DNS and local connectivity; request positive confirmation before committing the checkpoint. On timeout/failure, restore and verify the original configuration.
7. If rollback cannot be proven, mark recovery-required with concrete local-console instructions. Never flush all host firewall rules to “repair” it.

When called over SSH, detect the remote session and the interface carrying it. Host-network changes still count as local management, but the interruption risk must be disclosed. Creating a bridge on an external production host is not authorized by this specification.

## Firewall ownership

Use libvirt for its own network/firewall semantics. Add only separately identifiable application rules via the certified firewall adapter: firewalld on managed hosts or an owned nftables table on supported non-firewalld hosts. Do not edit libvirt-generated rules directly, flush global tables, or fight Docker/VPN firewall managers.

Test actual packet flow and ordering, not merely presence of an nftables rule. Firewall reload/reboot must preserve or reconcile policy. If the active host firewall stack cannot enforce a promised policy safely, refuse that policy before attaching the VM. Existing L2 traffic on a LAN bridge may not pass host IP-forward rules; do not claim routed firewall rules isolate it.

## Mutation and diagnostics

Label each network field as hot-updatable or requiring network/VM disruption. Never restart a network carrying running guests without approval. Refuse network deletion while NICs reference it unless an explicit detachment plan names those VMs and effects.

Diagnostics show bridge/link status, membership, DHCP leases, duplicate IP/MAC findings, route intent versus observation, DNS checks, gateway reachability, exposure policy and dual-homed paths. Packet capture is opt-in, scoped and time-limited; do not collect unrelated host traffic. Internet connectivity tests require explicit network access and a documented endpoint or operator-selected target.
