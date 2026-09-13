# Parent-operated native network run

`create_native.py` is an execution recipe for the owner-authorized disposable
`<test-vm-login>` VM. It must run as that ordinary user, in a fresh direct child of
`~/virmill-tests`, with passwordless sudo already authorized. The parent alone
coordinates mutations; concurrent fixture runs are prohibited. Never run it on
the development host or infer another target from discovered libvirt resources.

The fresh run directory contains the three frozen binaries in `bin/`, their
SHA-256 map in `binaries.json`, and the pinned `network_packet_fixture.py`.
The recipe verifies those bytes before use. It runs a private coordinator and
temporarily selects a root-owned copy of the helper using a runtime systemd
drop-in, after requiring the installed helper service/socket be inactive.

Select unused, nonoverlapping private /24 subnets. The ordinary allowed-host
recipe retains its default behavior:

```sh
python3 create_native.py --execute-disposable --root RUN_DIRECTORY \
  --lab-cidr LAB_CIDR --nat-cidr NAT_CIDR
```

The protected run creates services-only lab and NAT segments plus guest-only:

```sh
python3 create_native.py --execute-disposable --root RUN_DIRECTORY \
  --profile protected --lab-cidr LAB_CIDR --nat-cidr NAT_CIDR \
  --guest-cidr GUEST_CIDR --host4-target EXISTING_HOST_IPV4
```

`--dhcp off` selects explicit static addresses subnet+10 and subnet+11 for the
lab/NAT packet endpoints. Guest-only always uses those explicit static endpoints,
has no host IP, and requires the already assigned host address as a negative
probe target. No host address is created or moved. The packet fixture independently
checks the selected target and complete profile contract.

`--kinds nat guest-only` selects dependency-ready profiles for one run. It does
not waive the omitted profile's required qualification. A packet discrepancy
with verified endpoint cleanup is retained while the next selected profile is
tested; an unverified cleanup or failed creation stops further mutations.

The recipe explicitly uses `1.1.1.1:443` for bounded external TCP reachability
and `example.com` for the NAT managed DNS query. Protected isolated labs use
`--dns-own-lease`: role A resolves role B's unique DHCP hostname and must receive
exactly role B's leased IPv4 address. They do not require upstream DNS forwarding. These are packet-test targets, not
publication destinations. Before running, the parent must review and authorize
those network probes. A failed control prevents a passing isolation result.

Each network first receives an unapproved apply attempt, which must fail before
definition. The recipe then grants only its fresh UUID under the correct helper
policy family and submits the exact reviewed plan. It retains plans, jobs, live
XML hashes, command results and the packet report. Packet execution has a
270-second outer limit, covering the fixture's separate work and cleanup budgets.
Client detachment is not treated as job cancellation or permission to retry.

Cleanup stops test writers, deactivates only this run's networks, removes only
their exact definition-bound runtime/permanent rules, and restores the original
helper policy and runtime service configuration. It never undefines existing
networks or guests, edits source media, flushes a table, or reloads firewalld.
New inactive definitions, journals and private evidence remain for inspection.
Before/after guest XML/state, existing network XML/state, firewall inventory and
active-zone comparisons remain strict. An unrelated dynamic change can fail
preservation; it is recorded as a failure rather than silently excluded.

A successful namespace packet run is real kernel packet evidence. It does not
qualify real guest OS routing, multi-NIC topology, reload/reboot restoration,
physical bridges or the entire IPv6 matrix. All mandatory acceptance remains
subject to its own evidence requirements.
