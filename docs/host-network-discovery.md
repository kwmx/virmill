# PCI inventory and CIDR conflict observation

These read-only commands use the shared coordinator service. They create no
plans, jobs, network allocations or device assignments.

`virmill host pci list --output json` lists native libvirt PCI addresses, vendor
and product IDs, reported drivers, NUMA nodes and IOMMU groups. Choose the same
action in the TUI **Devices** section. Unknown optional topology remains unknown;
incomplete or contradictory group membership fails the observation. A listed
device is not permission or proof that it is safe to detach or pass through.
Bootloader/initramfs setup, exclusive use, physical assignment and restoration
remain separate mandatory work and qualification.

Check candidate subnets before planning networks:

```sh
virmill network cidr check --input '{"candidates":["10.77.0.0/24","fd42:77::/64"],"planned":[{"id":"other-lab","cidr":"10.78.0.0/24"}]}' --output json
```

In the TUI **Networks** section, choose **network cidr check** and enter:

```json
{"input":{"candidates":["10.77.0.0/24","fd42:77::/64"],"planned":[{"id":"other-lab","cidr":"10.78.0.0/24"}]}}
```

Each candidate returns conflicts attributed to host interface addresses, routes
(including policy/VPN tables), live and persistent libvirt network IPs and static-route destinations, or supplied
planned allocations. IPv4 and IPv6 are compared separately. Candidates are
alternatives; they do not reserve space or conflict with one another unless that
space is also in `planned`. Supply every known planned allocation explicitly.
Only universal route `/0` entries are excluded; a configured address, network or
planned `/0` still conflicts. Both local system and session connections are
supported. Observation happens in the coordinator's network namespace.

Use canonical network CIDRs. Unknown fields, duplicate candidates/planned IDs,
invalid prefixes, incomplete native XML, interrupted dumps and response limits
produce an error without a partial clear result. Up to 64 candidates and 256
planned allocations are accepted. Reduce the candidate set if the conflict
response limit is reached. Inspection requires native libvirt permissions and
ordinary-user route-netlink access; it does not request repairs or elevate itself.

An empty conflict list only describes a completed observation. Separate kernel
dumps and libvirt reads are not atomic; routes can change immediately afterward.
`observedDigest` identifies the compared observations and planned allocations,
not a reservation or authorization. Recheck immediately before a reviewed change.
No packet, DHCP, routing or isolation test is performed by these commands.

Adapter details and fixture limitations are in
[PCI discovery](pci-discovery-adapter.md) and
[host prefix observation](host-prefix-observer.md).

Configured static-route defaults follow the [libvirt network XML contract](https://libvirt.org/formatnetwork.html#static-routes):
a missing route destination or mask is its address-family default `/0`; the
gateway is mandatory. Such configured routes remain conflict evidence even
while a network is inactive. Missing masks on interface IP declarations are
refused when a safe prefix cannot be explicitly observed.
