# Automatic allocation integration review

Read-only source review of `internal/app/network_allocation.go`, the allocation
changes in `network_creation.go`, and the shared observation extraction in
`host_inventory.go`. Scope is NET-06 and JOB-02 prerequisites. No production or
test code was edited, and no native, remote or privileged operation was run.

## Finding corrected during review

The first foreign-network guard treated any IPv4 prefix returned by
`DefinedPrefixes` as a visible allocation. That parser intentionally includes
static routes. A foreign managed guest-only network whose intent hash binds
logical `10.0.0.0/24`, with this added direct child, could therefore escape the
unknown-allocation refusal while leaving its guest subnet unobserved:

```xml
<route address="172.20.0.0" prefix="16" gateway="172.20.0.1"/>
```

This was a source finding, not an executed pre-fix reproducer. It was reported
to the parent before widening foreign-network support. The revised
`matchesForeignNetworkAllocation` at `host_inventory.go:254` closes it by
requiring an exact supported profile match, including the native intent hash.
Added route/IP drift cannot qualify merely by contributing an IPv4 prefix.

The revised handling accepts an empty guest-only allocation only when the
exact empty-CIDR definition matches. A nonempty guest-only allocation needs a
planned entry whose ID equals the native UUID and whose CIDR reconstructs the
exact definition. That entry is independently included in occupied prefixes.
Visible NAT/lab networks require one observed IPv4 prefix plus an exact match
to the supported profile. Each present native XML layer is checked. A failed
match returns `UNRESOLVED_ALLOCATION`; matching supplies allocation facts and
does not transfer ownership or authorize mutation.

The parent's `TestAutomaticForeignAllocationRequiresExactProfileOrPlannedUUID`
contains the added-route and added-IP regressions, wrong planned ID/subnet
cases, and valid empty/declared/visible profiles. This review inspected those
tests; their execution evidence belongs to the parent.

## Selection and recovery binding

`planNetworkCreation` replaces `auto` with a concrete CIDR before constructing
the immutable recipe. The recipe carries the allocation version, original
request, complete selection settings and observation digest. The existing
engine canonical input digest and plan digest bind that recipe and review.
`validateNetworkAllocation` checks the recorded range/prefix relationship and
rejects a selection overlapping its recorded planned allocations. A recipe
without the optional allocation field retains its previous meaning.

Apply checks the selected subnet against fresh native, host and reservation
observations. It never calls the allocator to substitute another subnet.
Current settings contribute new planned conflicts; changing only search
ranges does not rewrite an accepted selection. Invalid current settings cause
refusal. Planning still creates no subnet reservation; the define step stores
the original job/plan-bound reservation before the native definition effect.

`recordForRecipe` at `network_creation.go:195` validates the original plan and
input digests and exact definition, metadata, actor and job identity. Its new
allocation comparison requires the recovery recipe to retain the original
allocation object. Resume copies that recipe and changes only its existing
recovery linkage; it omits the define step. Existing lock inheritance and
partial-effect retention are unchanged. No additional binding defect was
found in this scoped review.

## Observation, cancellation and bounds

Explicit checks and automatic selection now use the same occupancy collector.
It retains host addresses, non-default routes from all observed tables, live
and persistent native declarations, verified local reservations, and supplied
planned allocations. Only host route `/0` defaults are excluded. Configured
network or request-planned `/0` prefixes remain occupied. The observation
digest is informational and grants no reservation or permission.

The collector limits host entries to 65,536, native networks to 4,096, total
native XML to 16 MiB, and the combined result to 65,536 occupied entries. The
existing XML parser additionally bounds each document and its declarations.
Explicit reports cap candidates and total emitted conflicts. The combined
occupancy cap is checked after enumeration, so it is a result bound rather
than a strict peak-allocation limit.

Cancellation reaches native/host calls, is checked between network documents,
and is checked before a selected CIDR or successful public report is returned.
The pure allocator also checks cancellation. Individual XML parses, sorting,
and the preexisting synchronous metadata-store enumeration are not immediately
interruptible; this review does not claim a hard wall-clock cancellation bound.
No success path introduced by the extraction was found to discard a reported
context cancellation.

The reviewed tests also cover frozen selection, new conflicts without
reallocation, journal reopen/recovery, and cancellation/invalid review.
Allocator and loader unit matrices were intentionally not duplicated. Source
review and generated tests do not certify native route completeness, packet
behavior or isolation; all 71 acceptance scenarios remain required.
