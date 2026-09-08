# Managed-network XML declaration contract

`internal/backend/networkxml` is a pure renderer and declaration predicate for
newly owned networks. `Validate`, `Render` and `Match` accept the frozen
`domain.NetworkDefinition`. They do not connect to libvirt, inspect the host,
reserve resources, enforce firewall policy or modify existing definitions.
NET-01, NET-02, NET-05 and NET-06 still require their integration and packet
evidence; these tests are implementation prerequisites only.

The supported profiles are deliberately explicit:

| Property | NAT | Lab |
| --- | --- | --- |
| Type / egress | `nat` / `any` | `lab` / `none` |
| Host access | `allow` | `allow` |
| IPv6 requested policy | `disabled` | `disabled` |
| Native forwarding | NAT, ports 1024–65535 | No forward element |
| DHCP default route | Required when DHCP is enabled | Always false |

DHCP is optional. Static profiles cannot claim an advertised DHCP route. Other
types, egress restrictions, host restrictions and IPv6 modes fail with
`UNSUPPORTED_CAPABILITY`; there is no policy downgrade. NAT egress `any` includes
host LAN/VPN destinations as allowed by the host's routes. It is not an
internet-only profile. Lab host access is allowed, so it is not a host-isolation
boundary or a defense against a host/guest deliberately routing traffic.

Identity validation requires a canonical lowercase nonzero UUID, native name
`virmill-UUID`, and bridge `vm` plus the first twelve unhyphenated UUID digits
(14 bytes). IPv4 must be a canonical masked `/8` through `/30` wholly inside
RFC 1918 space. The bridge address is subnet + 1. DHCP covers subnet + 2 through
broadcast − 1, including a single lease for `/30`. The renderer specifies the
bridge's `trusted` zone, STP on and delay zero. It cannot reference a physical
interface, existing bridge, host device, arbitrary DNS server or XML fragment.
The adapter/service must separately prove identity availability, collect all
applicable address/route/network/planned overlaps and authorize mutation.

The metadata child is `{urn:virmill:v1}networkCreation`, with exact unnamespaced
attributes `apiVersion="virmill/v1"`, `version="1"` and `intent`. The intent is
lowercase SHA-256 of compact Go JSON in this field order:
`{"apiVersion":"virmill/v1","version":1,"definition":<NetworkDefinition>}`.
All typed definition fields participate. A changed field requires both changed
metadata and matching native XML. This reproducible marker is neither an
authentication token nor evidence that an existing network belongs to Virmill.
Durable plan/job ownership remains a separate prerequisite.

`Match` bounds XML to 64 KiB, 32 elements, depth 8 and 16 attributes per element.
It rejects malformed/truncated documents, directives and processing
instructions (including an XML declaration), multiple roots, duplicate expanded
element/attribute names, unsupported namespace declarations and any unknown
attribute/element. Matching never reconstructs or overwrites observed XML.
Mismatches return `SOURCE_CHANGED`; invalid expected definitions retain the
validation error. Error text identifies the differing field, without echoing
arbitrary supplied XML.

The normalization allowance is finite: whitespace outside identity text,
comments, sibling/attribute order, equivalent bound namespace prefixes, omitted
`ipv6="no"`, bridge defaults `stp="on"`/`delay="0"`, explicit
`macTableManager="kernel"`, omitted IPv4 family, equivalent contiguous IPv4
netmask, and omitted NAT forward mode. A canonical unsigned 32-bit live
`connections` counter may vary. One native-assigned, nonzero, six-byte unicast
colon MAC is permitted, including uppercase hex. Its actual bytes are **not
bound** by this definition contract; two observations with different valid
MACs can both match. All name/UUID/bridge/intent bytes remain exact. These
defaults and libvirt's recommendation to assign its own MAC are documented in
the [official network XML format](https://libvirt.org/formatnetwork.html).

The fixed NAT port range must remain present and exact. Custom DNS/dnsmasq
options, additional addresses/routes, DHCP hosts/leases/BOOTP/TFTP, VLANs,
portgroups, MTU changes, hostdev/physical forward devices and future extensions
are refused even if the metadata hash matches. Unsupported native normalization
must fail visibly and receive a source/fixture review before any allowance is
expanded.

Libvirt v12.0.0 `networkDnsmasqConfContents` emits an empty DHCP option 3 and
disables upstream resolver use for isolated networks (lines 1195–1209).
`networkSetIPv6Sysctls` disables IPv6 on the **host bridge** when no IPv6 address
is configured and sets that bridge's RA/autoconf controls (lines 1734–1767).
These are source-level expectations from the
[pinned bridge driver](https://raw.githubusercontent.com/libvirt/libvirt/v12.0.0/src/network/bridge_driver.c),
not observations of a running dnsmasq or firewall.

In particular, `ipv6="no"` plus absence of IPv6 addresses does **not** prove
that guests cannot exchange IPv6 EtherType frames on the bridge. An activation
gate must independently enforce and observe the requested IPv6 policy; packet,
link-local and rogue-RA tests remain required. The renderer also cannot inspect
host hooks, effective firewalld configuration, guest routes or external writers.
No rendered or matching XML alone may promote these properties to verified.

Local verification uses `./scripts/go test ./internal/backend/networkxml`, with
synthetic profile/boundary/normalization, extension drift, XML ambiguity and
fuzz seeds. These tests perform no network, libvirt or firewall operations.
