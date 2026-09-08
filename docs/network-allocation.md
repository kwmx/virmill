# Automatic private IPv4 subnet selection

Set `spec.ipv4.cidr: auto` in a Network declaration and use the existing network
creation workflow. The shared service selects a concrete subnet during planning;
both CLI and TUI show that same subnet, configuration snapshot and observation
digest. A preview reserves no subnet and changes no host networking.

The [automatic lab example](../examples/networks/auto-lab.yaml) requests a new
isolated lab with managed DHCP/DNS, services-only host access, no DHCP default
route, no egress and disabled IPv6:

```sh
virmill network create examples/networks/auto-lab.yaml --plan --output json --non-interactive
virmill plan show PLAN_ID --output json --non-interactive
```

`PATH` is a positional argument, including for filenames containing spaces. Quote
such paths in a shell. No new allocation command or `--file` flag is required.
Use `--output ndjson` for one clean machine-readable preview record. These
commands return a plan; they do not wait for a hidden approval prompt.

In the TUI, open **Networks**, select **network create**, and enter the declaration
path. Page through the result at 80×24 using PgUp/PgDn. Review `allocation`, its
`requestedCIDR: auto`, configuration and `observedDigest`, together with the exact
selected `definition.ipv4CIDR`, native XML, host policy, risks and required
acknowledgements. `plan show` retrieves the stored review in either interface.
Press `a` to open approval and enter the full plan digest. Esc cancels an
unsubmitted path or approval form without creating a job or reservation.

An administrator must still grant the exact planned network UUID through the
appropriate [helper policy](network-helper.md). Automatic selection grants no
permission. The example requires the separate `protectedNetworks` policy family.
Apply the reviewed plan ID, digest and every returned acknowledgement:

```sh
virmill plan apply PLAN_ID --digest PLAN_DIGEST \
  --idempotency-key UNIQUE_REQUEST_ID --ack host-mutation \
  --ack network-host-access --ack network-firewall \
  --ack exclusive-network-writer --wait --timeout 300s
virmill network creation result OPERATION_ID --output json
```

## Allocation settings

The ordinary coordinator reads
`$XDG_CONFIG_HOME/virmill/network-allocation.json`, falling back to
`$HOME/.config/virmill/network-allocation.json` when `XDG_CONFIG_HOME` is unset.
This location comes from the coordinator's environment. The CLI's reserved
`--config` flag does not select this file. An absent file uses these ordered
search pools, selecting /24 subnets: `10.0.0.0/8`, `172.16.0.0/12`, then
`192.168.0.0/16`. Defaults are search preferences, not an assumption that a
private range is unused.

Adapt [the allocation settings example](../examples/network-allocation.json)
and save it at that location. Its `external-lab` entry is illustrative; replace
it with the real external allocations that must remain unavailable:

```json
{
  "version": 1,
  "ranges": [
    {"cidr": "10.0.0.0/8", "prefixLength": 24},
    {"cidr": "172.16.0.0/12", "prefixLength": 24},
    {"cidr": "192.168.0.0/16", "prefixLength": 24}
  ],
  "planned": [{"id": "external-lab", "cidr": "10.9.0.0/24"}]
}
```

Settings use strict JSON version 1 with the exact field spellings shown above.
Unknown fields, duplicate keys, null values, malformed JSON and invalid settings
are refused. `ranges` contains 1–16 disjoint, canonical RFC1918 IPv4 CIDRs in
search order. Each `prefixLength` is between the pool's prefix length and /30,
with an overall minimum of /8. The first available aligned subnet in the first
eligible range is selected. Public IPv4, IPv6 and host-bit CIDRs are not pools.

The optional `planned` array has at most 256 entries. Each ID is unique and
matches `[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}`. Its CIDR is canonical RFC1918 IPv4;
/31 and /32 reservations also exclude any candidate containing them. Planned
entries may overlap each other or lie outside the search pools. These entries
describe allocations to avoid; they do not create networks or reserve resources
in another coordinator. Keep them current for declarations, labs or subnets
whose intended addresses cannot yet be observed natively.

On Linux an existing settings file must be an ordinary file of at most 64 KiB.
The reader rejects a symlink at the file itself and checks the held file and
directory entry for change during the bounded read. Parent directory resolution
follows normal configuration-directory semantics. An unreadable, unsupported or
changing existing file causes refusal; it does not silently fall back to
defaults. Reading settings is not an ownership or administrator authorization
check.

## A reviewed selection stays fixed

Planning compares candidate subnets with assigned host address prefixes,
non-default routes from all observed routing tables, VPN/tunnel routes, active
and inactive libvirt network definitions, configured planned allocations and
verified durable Virmill reservations. Only host **route** `/0` entries are
ignored as defaults. An address, defined network or explicit allocation is not
exempt merely because it covers many addresses. An unavailable or invalid
inventory fails the request rather than implying an empty network.

The exact selected CIDR, allocation settings and observation digest are retained
in the immutable plan recipe. Settings are consulted again for subsequent CIDR
checks. Changing search ranges never substitutes a new CIDR into a reviewed
plan. A newly observed or configured conflict with the selected subnet refuses
apply with `CIDR_CONFLICT`; exhausted search pools also refuse a new preview with
`CIDR_CONFLICT`. Inspect the conflict and request a new plan when a different
selection is intended. The old plan digest never approves that new selection.

Approval repeats state, capability and helper checks. The coordinator durably
records the exact network identity and selected subnet before native definition.
Two previews may show the same available subnet because planning reserves
nothing. Later applies must recheck it. Cancellation or failure after a durable
operation starts can retain its reservation and created resources for reviewed
recovery. Use the [creation result and recovery workflow](network-creation.md);
do not assume cancellation deleted a network or released its subnet. Automatic
reservation cleanup and complete allocation deletion lifecycle remain separate
work.

## Observation and qualification limits

Automatic selection is available for the supported new NAT, lab and guest-only
declarations on `qemu:///system`, with their existing host-access, DHCP, egress
and disabled-IPv6 restrictions. For guest-only, an automatic CIDR reserves only
the logical guest subnet: it assigns no host IP or guest addresses and starts no
DHCP/DNS service. Omitting guest-only IPv4 means no logical subnet reservation.
Existing guests are never silently readdressed or attached.

The configured planned list is needed for otherwise invisible source
declarations and external intended allocations. Native IPv4 definitions and
this coordinator's verified journal supply observable reservations. A foreign
managed NAT/lab network must match its complete supported native profile and
intent hash before its observable prefix is accepted. A foreign guest-only
network can be recognized as having no logical CIDR only when its entire native
profile and intent hash match that exact empty-CIDR definition. Otherwise recover
the owning declaration and add a `planned` entry whose `id` is the native network
UUID and whose `cidr` is its exact logical subnet. The whole native XML and
intent hash must match this declaration; an arbitrary UUID/CIDR pair is refused.
Unknown profiles, drift or missing/mismatched declarations refuse checks with
`UNRESOLVED_ALLOCATION`. This explicit reconciliation supplies allocation facts;
it does not transfer network mutation ownership or create cross-coordinator
logical-allocation authority.

Host and native snapshots are not atomic, and application locks do not serialize
foreign network writers. Coordinate other writers: rechecks cannot eliminate a
route or libvirt definition racing the final native operation. Subnet selection
is not proof of DHCP, DNS, reachability, IPv6 filtering, isolation or guest route
configuration. Plans/results retain `packetVerification: not-run` and
`guestRoutingVerified: false` until separate qualification exists.

The CLI/TUI regression tests use the actual shared service, generated temporary
declarations and SQLite stores, with substituted native inventory and helper
checks. They cover exact path/review/approval transfer, clean JSON/NDJSON,
settings changes, conflict refusal and cancellation without reservations. They
contribute to NET-06, UX-01, UX-03 and REL-03 at the software test level; they do
not qualify a native network, VM, distribution or hardware.

```sh
./scripts/go test -mod=vendor -race ./internal/ui/cli ./internal/ui/tui \
  -run '^TestNetworkAllocation' -count=1 -v
```
