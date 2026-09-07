# Read-only host prefix observation

`internal/platform/linux.ObserveHostNetworkPrefixes(ctx)` supplies the shared
network planner with occupied address networks and route destinations. It is a
read-only Linux adapter for NET-06. It contributes address-family and capability
facts to NET-05, CORE-06 and UX-01; it does not implement or certify those complete
acceptance scenarios.

The adapter uses the pinned `golang.org/x/sys/unix` dependency and four rtnetlink
dump requests: IPv4 addresses, IPv6 addresses, IPv4 routes and IPv6 routes. Route
requests use `RT_TABLE_UNSPEC`, without interface, table or protocol filters.
There are no shell commands, `ip` subprocesses, downloads, helper requests,
multicast subscriptions, route/interface changes or firewall changes.

## Returned facts and allocation rules

Each prefix is masked to its canonical CIDR and carries `source` (`address` or
`route`), the interface index and the routing table. Address entries have table
zero. Identical entries are deduplicated by all four fields. Results sort by CIDR
text, source, numeric table and numeric interface index; warnings are deduplicated
and sorted. Distinct interface or table provenance is preserved.

- Assigned address networks are retained regardless of tentative, deprecated or
  other address flags. Both `IFA_LOCAL` and `IFA_ADDRESS` are retained: on a
  point-to-point interface these can describe different local and peer networks.
- Every IPv4/IPv6 route destination is retained, including local, link-local,
  blackhole, unreachable and source-specific routes. The collector does not
  attempt to decide which policy-routing rule wins. `RTA_TABLE` overrides the
  compact route-header table, including table numbers above 255.
- IPv4 `0.0.0.0/0` and IPv6 `::/0` routes remain explicit facts. The shared planner
  may exclude **route** defaults from overlap checks. An assigned `/0` address or
  a planned `/0` allocation must not receive that exception.
- Inline output/input interfaces and every inline multipath interface are
  retained. Dead or link-down paths remain potential overlap evidence. Interface
  index zero means the route has no resolved interface in this observation.
- Nexthop-object routes (`RTA_NH_ID`) retain their destination and table with an
  interface-zero entry and the stable warning
  `HOST_PREFIX_NEXTHOP_INTERFACE_UNRESOLVED`. The separate nexthop-object graph is
  not resolved; retaining the destination still lets allocation reject overlap.

The service must combine these observations with active and inactive libvirt
networks, planned lab networks and existing application allocations. The adapter
does not enumerate those sources itself.

## Bounded failure behavior

The entire four-dump observation has a five-second deadline, or the caller's
earlier cancellation/deadline. Nonblocking receives poll at most 50 milliseconds
before checking cancellation again. Each datagram is at most 1 MiB; each dump is
at most 16 MiB and 65,536 messages. There are at most 131,072 entries per dump and
across the combined result, 4,096 attributes per attribute sequence, 4,096 inline
multipath entries per route and eight nested attribute levels.

The adapter checks the kernel sender, request sequence, recipient port and
multipart framing. Every dump needs `NLMSG_DONE` with a successful status.
Truncation, missing completion, kernel errors/overruns, receive-buffer loss,
filtered or interrupted dumps, cancellation and resource limits discard the
whole observation. A zero error acknowledgement cannot replace completion.

Message, attribute and multipath lengths/alignment are checked before decoding.
Conflicting duplicate attributes, invalid known field sizes/encodings, invalid
families/prefix lengths/interfaces and ambiguous nested multipath layouts fail.
Identical duplicate attributes are permitted. Unused ancillary attributes remain
opaque after envelope validation; attributes marked nested are structurally
validated with bounded depth. The collector never infers another prefix from
unknown ancillary payloads.

## Scope and evidence limits

This observes the caller's **current network namespace**. It covers all routing
tables there, including tables used for VPNs and VRFs. It does not enter other
network namespaces. Deployment must place the observer in the namespace of the
host being planned; local facts cannot describe a remote libvirt host.

Separate dumps are not an atomic host snapshot. Kernel-reported interrupted dumps
are rejected, but a valid completed observation can immediately become stale.
The application must reobserve relevant state when checking a plan/apply boundary.
No result proves that a route is reachable or that firewall, IPv6 RA, guest
routing or packet isolation works. Read restrictions or unavailable family dumps
are errors, not empty successful inventories.

Synthetic fixtures in [network-prefixes](../tests/fixtures/network-prefixes/README.md)
exercise decoding without containing host addresses. The opt-in ordinary-user
smoke test invokes the real read-only collector and reports only prefix/warning
counts. It is a current-namespace observation, not hardware, VPN, policy routing
or packet qualification. Run it with:

```sh
VIRMILL_HOST_PREFIX_SMOKE=1 GOPROXY=off GOSUMDB=off ./scripts/go test ./internal/platform/linux -run '^TestHostPrefixReadOnlySmoke$' -count=1 -v
```

The implementation follows the Linux UAPI `if_addr.h`, `rtnetlink.h` and
`netlink.h` layouts and the kernel's
[Netlink message and dump documentation](https://www.kernel.org/doc/html/latest/userspace-api/netlink/intro.html).
