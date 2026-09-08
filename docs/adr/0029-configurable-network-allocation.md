# ADR 0029: Configurable automatic IPv4 selection

Status: accepted implementation decision; complete NET-06 and native
qualification remain required.

The normative networking specification requires automatic allocation from
configurable private ranges. The previous explicit-CIDR-only creation restriction
was an implementation gap, not a scope decision. `cidr: auto` now uses the same
shared network creation operation, authorization, locks and recovery path as an
explicit CIDR. The public declaration API stays `virmill/v1`.

Non-secret settings belong in `$XDG_CONFIG_HOME/virmill/network-allocation.json`
under the packaging path contract. Version 1 settings contain ordered `ranges`
with `cidr` and `prefixLength`, and optional named `planned` reservations. In the
absence of a settings file, ordered RFC 1918 pools 10/8, 172.16/12 and 192.168/16
supply /24 candidates. These are candidate pools, never presumed-free networks.
An existing invalid, unreadable, linked or nonregular settings file refuses the
flow. No first-run write, download, helper authority or environment alias is added.

Selection takes one bounded observation of host addresses and all non-default
route tables, active and persistent native network layers, verified durable
application reservations and configured planned allocations. Ordered pool
preference and the lowest available aligned subnet make selection deterministic.
Only host route /0 entries are ignored. Configuration and native /0 declarations
still conflict. Planned user input to the existing CIDR checker remains additive.

The exact chosen subnet, configuration snapshot and observation digest are bound
into an optional version-1 allocation review inside the immutable network
recipe. Existing explicit recipes omit this field and retain their bytes and
policy-version semantics. Plan review reserves nothing. Apply and each effect
recheck the same selected subnet, including current planned reservations; a new
conflict refuses execution. Changing pool preference does not rewrite a reviewed
selection. No helper policy or XML version changes are needed: both already bind
the final exact definition. Recovery preserves the original allocation review
and uses the original durable reservation; it never reruns the allocator.

Guest-only networks may have a logical subnet without any native host address.
Their intent hash cannot reveal that subnet to another coordinator. Foreign
managed networks must exactly match a supported fixed profile, including that
hash and every native layer. Observable NAT/lab prefixes can be checked directly;
a guest-only logical CIDR requires a `planned` entry whose ID equals the native
UUID. The service reconstructs the fixed profile and checks the complete XML
against that exact UUID/CIDR. An exact empty-CIDR guest-only profile proves no
logical allocation exists. These checks observe allocation facts and grant no
mutation authority. Added IP/route fields, renamed definitions, stale hashes and
unknown profiles refuse with `UNRESOLVED_ALLOCATION`; a plausible prefix alone is
insufficient. Verified local journal reservations must also match each observed
native layer. Shared host-wide atomic IPAM and concurrent foreign-writer
coordination remain required engineering work.

Intervals avoid scanning every possible subnet against every occupied prefix.
All collections and settings sizes are bounded; malformed observations and
cancellation fail without network mutation. Native snapshots remain non-atomic,
and the existing exclusive-writer acknowledgement still applies. This slice
makes no hardware, VPN, packet-isolation or complete release claim.
