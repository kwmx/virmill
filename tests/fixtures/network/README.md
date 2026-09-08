# Disposable namespace packet fixture

`network_packet_fixture.py` is a single-use, parent-operated observer for a newly
created, otherwise unused Virmill network on the explicitly authorized disposable
host. Import and parser tests perform no native operations. The existing setup,
recording, veth identity, udev settlement, STP waiting and conditional cleanup
contract is described in `docs/network-packet-fixtures.md`; the protected-profile
extension below supersedes that document's original allow-only profile list.

This fixture contributes packet-test prerequisites to NET-01/02/03/04/05/06. In
particular, NET-03 requires real services-only lab traffic and NET-04 requires a
guest-only segment without host L3/DHCP and working guest static addressing. A
namespace is not a real VM, and these local parser tests are not packet evidence.
All reports retain `fullNetworkAcceptance: false`,
`guestOrHardwareQualification: false` and the existing narrower qualification
limits. Parent-owned execution and evidence attribution remain separate.

## Selected profiles

| Kind | Host access | DHCP/DNS | IPv4 peer | Arbitrary host TCP | Explicit external TCP |
| --- | --- | --- | --- | --- | --- |
| nat | allow | Existing on/off behavior | connected | connected | connected if requested |
| lab | allow | Existing on/off behavior | connected | connected | blocked if requested |
| nat | services-only | Paired on/on or off/off | connected | blocked | connected; required |
| lab | services-only | Paired on/on or off/off | connected | blocked | blocked; required |
| guest-only | deny | off/off | connected using explicit static addresses | blocked | blocked; required |

Other combinations are refused. All profiles declare IPv6 disabled. A successful
IPv6 peer/link-local probe or observed router advertisement contradicts that
declaration and prevents the overall scoped result passing. Lack of traffic in
the bounded probe is not a complete IPv6 isolation proof.

Allow profiles retain the version-1 native creation marker. Protected profiles
require version 2 and exactly one empty native `dns` element with explicit
`enable="yes"` iff DHCP is enabled, otherwise `enable="no"`. Guest-only XML must
have no `ip`, `route`, `dhcp` or `forward` configuration. Known equivalent libvirt
IPv6-disabled omission remains accepted. These observations do not replace the
parent's complete definition/intent matcher or firewall qualification.

## Invocation additions

All existing identity and single-use arguments remain required, including
`--execute-reviewed`, `--confirm-new-unused-network`, `--root`, `--output`,
`--run-id`, `--recipe-sha256`, `--network-id`, `--network-xml-sha256` and the exact
UUID-bound `--bridge vm[12 lowercase hex]`. Output must be exactly
`ROOT/network-packet-RUN_UUID`; ROOT must be an existing ordinary, symlink-free
direct child of `/home/virmill-test/virmill-tests`. The parent supplies the frozen
root-owned script and all hashes. Existing output is never reused or removed.

The new arguments are:

- `--kind nat|lab|guest-only` and
  `--host-access allow|services-only|deny`, in only the combinations above.
- `--static-cidr4 CIDR`, required only for guest-only. It is a canonical RFC1918
  network from /8 through /29, approved by the parent as the new network's logical
  segment. Both `--static-a4` and `--static-b4` must be distinct usable addresses
  inside it. No address is assigned to the host bridge. Existing assigned host
  addresses overlapping the logical subnet are refused.
- `--host4-target IPV4`, required only for guest-only. It must be a canonical,
  already assigned, ready global-scope IPv4 on exactly one other UP host
  interface, outside the logical segment. The observer records its
  interface/index/MAC/address/prefix binding and rechecks it. It never creates or
  changes a host address. NAT/lab use their exact native bridge gateway.
- Existing `--forward4-target IPV4 --forward4-port PORT` are mandatory for all
  protected profiles. The target must be outside the logical segment, distinct
  from the guest-only host target, and not any assigned host address. Loopback,
  unspecified, multicast and link-local targets are refused. The parent must
  select and authorize an actual reachable endpoint; this README supplies none.
- Existing `--dns-name NAME` is mandatory for services-only with DHCP on and
  refused with DHCP off. The A query goes only to the selected bridge gateway;
  it may cause explicitly selected upstream DNS traffic. Successful resolution
  and advertisement of that gateway by the own DHCP ACK are required.

DHCP-on obtains two independent own-MAC leases and configures only those returned
addresses. DHCP-off requires explicit static endpoints. Protected DHCP-off also
sends its own DHCPDISCOVER in a five-second observation window; it does not send
a DHCPREQUEST, adopt an offered address or install a DHCP default. A matching
reply, malformed frame or exhausted 512-frame bound makes that absence check
fail/inconclusive. At most eight bounded frame samples and sixteen malformed
reasons/replies are retained. A quiet window remains `absenceProven: false`.

## Negative controls and mutation boundary

Protected host probes create a temporary TCP listener bound only to the selected
existing IPv4 and an OS-assigned ephemeral port. Unlike the legacy allow probe,
it is not constrained with `SO_BINDTODEVICE`: the host-local positive control
must reach the same socket through its local route. The listener sends only a
run-specific public token, accepts at most four connections, and closes after
the probe. Before and after controls must complete the token exchange. Listener
address/port, controls and accepted source addresses are journaled. This creates
temporary exposure of this benign listener on that selected address; it does not
alter a host service, firewall rule or address.

Before the protected host probe, the observer adds a /32 on-link route and
installs/replaces the exact selected bridge-MAC permanent neighbor **inside its
held fresh namespace only**. Replacing that one generated-namespace entry avoids
an incidental learned ARP entry causing `EEXIST`. Route and neighbor readback
must match. Guest-only forwarding uses the same deliberate L2 delivery for the
explicit external /32 target, since there is no host gateway address on that
bridge. NAT/lab forwarding uses a target-only route via the actual gateway; the
protected host probe has already installed that gateway's explicit neighbor.
No default route or host neighbor is added.

External protected forwarding controls perform bounded TCP connects from the
host to the identical target/port before and after the namespace probe. Either
control failure prevents a pass. A negative result must be a bounded network
timeout/refusal/unreachable/permission outcome, not any failed subprocess or
arbitrary socket error. A completed TCP handshake followed by a token timeout
remains **connected**, so it cannot manufacture a blocked-host result.

These controls remove an unavailable listener and missing ARP as explanations
for a negative probe; they do not identify which filtering layer caused it.
`negativeResultProvesFiltering` stays false. The fixture neither installs nor
repairs enforcement. It does not test arbitrary ports/protocols/destinations,
reload/reboot persistence, dual-homed guests, real guest route configuration or
all IPv6 paths.

Before and after packet phases, the observer rechecks the pinned live XML,
bridge index/MAC/MTU and full IPv4/IPv6 address list, plus both endpoints' held
identity/aliases/MAC/master. Guest-only requires the bridge address list remain
empty at each checkpoint, including final cleanup. This is checkpoint evidence,
not continuous monitoring between reads. The original new-unused bridge,
durable object receipts, explicit alias after udev settle, read-only STP
forwarding wait and refusal to delete replaced/unacknowledged objects remain.
Cleanup deletes only the exact generated veth pair/namespaces and verifies the
selected network remains unchanged; it never removes the network or leases.

## Prerequisites, bounds and local checks

Parent-only native execution needs the already installed trusted `ip`, `virsh`,
`udevadm`, `ping` and `python3` binaries, namespace/veth/AF_PACKET kernel support,
and the authorized disposable-host root capabilities for these operations. The
script discovers exact binary paths, hashes and versions in `prerequisites.json`;
there are no downloads or package installs. No additional tool is required by
the protected extension.

Existing bounds remain: 240 commands, 256 KiB per command stream, 8 MiB command
journal payload, 180-second run deadline and a separate 60-second cleanup
opportunity. DHCP acquisition remains 18 seconds per phase with a 40-second
worker timeout; other workers keep 24 seconds. TCP socket operations use
three-second timeouts; the DHCP absence and RA windows each last at most five
seconds. An exceeded deadline or missing prerequisite is retained as failure,
not a skipped success.

Executed locally with Python 3.14.6:

```text
python3 -B -m unittest discover -s tests/fixtures/network -p test_packet_parsers.py
Ran 55 tests in 0.068s — OK
```

The 41 existing tests remain and 14 new tests cover protected profile/marker/DNS
confusion, static addresses, assigned host identity, exact namespace route and
neighbor commands, before/after listener controls, external target controls,
connected-but-stalled token handling, L3 drift, protected result aggregation and
DHCP quiet/reply/malformed/saturated windows. All sockets, commands, threads and
time used by new execution-path tests are mocked. No SSH, native packet,
namespace, VM, IPC or host mutation was performed for this implementation.
