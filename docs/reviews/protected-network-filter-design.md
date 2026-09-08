# Protected network filter executor review

This review covers the version 2 helper implementation and generated-state tests
for NET-03, NET-04, NET-05 and SEC-01 prerequisites. It does not certify packet
isolation. All 71 acceptance scenarios remain required. The parent owns native
testing, release evidence and the shared authority and application contracts.

## Ordering decision

An IPv4 DROP in filter INPUT can be reached after an earlier ACCEPT. In
particular, firewalld's iptables backend places its established-connection
acceptance before direct filter rules. Its nftables backend orders direct rules
differently, and a direct ACCEPT still has to survive later nftables hooks; a
DROP is final. Equal-priority direct rules have no guaranteed relative order.
These distinctions rule out treating a direct filter INPUT listing as proof of
host isolation. [firewalld direct documentation](https://firewalld.org/documentation/man-pages/firewalld.direct.html)

The selected correction uses IPv4 **mangle INPUT**, which precedes filter INPUT
at the local-input hook: the Linux priority definitions assign mangle -150 and
filter 0. It does not filter IPv4 FORWARD. Dropping all IPv4 at bridge INPUT
would also drop frames addressed to the gateway before routing, breaking NAT.
The bridge local-input path passes such frames to the IP stack only afterward.
[Linux v6.12 priority definitions](https://raw.githubusercontent.com/torvalds/linux/v6.12/include/uapi/linux/netfilter_ipv4.h),
[filter hook registration](https://raw.githubusercontent.com/torvalds/linux/v6.12/net/ipv4/netfilter/iptable_filter.c),
[bridge input path](https://raw.githubusercontent.com/torvalds/linux/v6.12/net/bridge/br_input.c)

Libvirt's isolated network mode alone still permits host access. Its network
XML separately controls the bridge address, DHCP and DNS. The helper therefore
requires the fresh, exact reviewed native definition as well as its owned
firewall policy; it does not infer isolation from absence of a forward element.
[libvirt network format](https://libvirt.org/formatnetwork.html)

## Bounded rule plan

Every profile retains the four bridge-scoped IPv6 DROP rules in eb filter
INPUT, OUTPUT and both FORWARD directions. Each runtime rule has an identical
permanent counterpart. The added rules are fixed argv, with only the validated
bridge and calculated bridge IPv4 address substituted:

| Profile | Added runtime rules | Total with permanent rules |
| --- | --- | --- |
| services-only NAT/lab, DHCP enabled | mangle INPUT: UDP source 68 to destination 67 at broadcast /32 or bridge /32; UDP and TCP destination 53 at bridge /32; then bridge-ingress DROP | 18 |
| services-only NAT/lab, DHCP disabled | mangle INPUT bridge-ingress DROP | 10 |
| guest-only | eb INPUT and OUTPUT logical-bridge IPv4 and ARP DROP | 16 |

Service exceptions have distinct priorities -32767 through -32764; the final
DROP is -32763. Every service rule matches `-i <bridge>`. DNS rules do not allow
other host destination addresses, and DHCP rules require client source port
68. Guest-only adds no IPv4 or ARP FORWARD drop, preserving the reviewed peer
bridging policy. No mutation passthrough, global flush, reload, chain deletion
or network activation is performed by this executor.

## Required fresh observations

Version 2 reads both runtime and permanent direct rules, chains and tracked
passthroughs. It refuses every tracked IPv4 passthrough, including a permanent
entry absent from the current kernel. Otherwise a later reload could install an
unobserved INPUT bypass. Custom IPv4 mangle INPUT or INPUT_direct authority is
also refused. Unrelated IPv6 declarations and unused chains are retained as
opaque inventory, never executed as arguments. Direct inventories describe
tracked declarations, not every possible kernel rule; permanent and runtime
configuration require separate operations and reload replaces runtime state
from permanent state. [firewall-cmd documentation](https://firewalld.org/documentation/man-pages/firewall-cmd.html)

The only additional executable read arguments are:

```text
--direct --passthrough ipv4 -t mangle -S INPUT
--direct --passthrough ipv4 -t mangle -S INPUT_direct
```

The second read is issued only for an exact unconditional INPUT trampoline.
Supported layouts are an ACCEPT policy followed directly by the exact managed
body, or the ACCEPT policy and sole INPUT_direct jump followed by that exact
child body. Unknown rules, earlier ACCEPT/RETURN/jumps, duplicate entries,
missing entries and wrong priority order fail. Only reviewed service-rule
shapes may appear in this IPv4 hook, including on other bridges. Disjoint rules
of equal priority may be reordered because their input-interface matches do
not overlap.

Each kernel observation takes two complete snapshots and requires identical
raw output. Output is bounded to 64 KiB, 258 lines and 2,048 bytes per line,
with canonical ASCII spacing and a final newline. The parser accepts the
specific optional `\nsuccess\n` firewall-cmd suffix. The parent reported
firewalld 2.4.4 returning exactly `-P INPUT ACCEPT\n\nsuccess\n` on the
authorized disposable target; the child did not run that native command.
Traces, changed trailers and arbitrary blank lines remain errors.

The implementation repeats inventory and kernel checks before mutation,
between individual additions, and at the end of apply or observe. Native
identity/XML checks remain mandatory; apply requires an inactive persistent
network with autostart disabled. These are bounded observations, not atomic
exclusion of an administrator changing the firewall. The application-owned
exclusive-network-writer acknowledgement remains necessary.

## Persistence and compatibility

Every addition retains its fsynced per-rule intent. Lost acknowledgements,
drift, cancellation and final-write errors preserve partial journal evidence;
they return no successful response. A reviewed resume recognizes already
present owned rules and adds only missing rules. Observation does not repair or
rewrite history. Ownership binds version, actor, key, resource and definition;
version 2 cannot borrow a version 1 grant or silently convert an old owner.

Version 1 retains its eight selected arguments, rule indices, owner and record
bytes, probes and version. Its inventory parser additively recognizes the
exact disjoint IPv4/ARP DROP shapes needed to coexist with version 2. This gives
version 1 no permission to add those rules; same-bridge policy ambiguity and
unknown eb ACCEPT shapes still fail. A frozen-byte regression reads a completed
version 1 journal without modifying it and compares a fresh version 1 apply
against those same bytes.

Permanent presence does not establish uninterrupted filtering across firewall
reload, service stop, reboot or privileged external edits. Firewalld exposes
cleanup and reload behavior independently of these rules. This executor neither
installs a reload guard nor changes those settings.
[firewalld configuration documentation](https://firewalld.org/documentation/man-pages/firewalld.conf.html)

## Source and test limits

Source inspection also checked current upstream firewalld
[`ipXtables.py`](https://raw.githubusercontent.com/firewalld/firewalld/main/src/firewall/core/ipXtables.py)
and [`fw_direct.py`](https://raw.githubusercontent.com/firewalld/firewalld/main/src/firewall/core/fw_direct.py)
for built-in versus INPUT_direct placement and direct backend selection. Those
moving source references explain the two accepted shapes; they are not pinned
source evidence for the installed 2.4.4 build. The Linux v6.12 references above
are also not proof of the parent's reported native 7.1.13 kernel behavior.

Generated tests independently model tracked inventory and kernel output. They
cover both layouts, rule counts, exact service scope, dormant reload bypass,
unknown rules, framing, mid-apply/final drift, lost acknowledgements across the
two-digit index boundary, journal faults, cancellation, grant/owner/history
refusal, v1 coexistence and frozen v1 journal compatibility. No firewall or
guest operation is executed by these tests.

Native evidence still must exercise DHCP/DNS, other host addresses and ports,
existing connections, NAT/lab forwarding, guest peers, IPv6 link-local and RA,
and reload/restart behavior on the qualified backend. Bridge filtering and IP
filtering govern different traffic paths, so one successful IPv4 probe cannot
stand for the complete matrix.
[Linux bridge documentation](https://docs.kernel.org/networking/bridge.html)
