# Create a managed network

`virmill network create PATH --plan` reads a `virmill/v1` Network declaration and
returns an immutable preview. The current executable profiles are NAT with
`hostAccess: allow` and `egress: any`, or lab with `hostAccess: allow` and
`egress: none`. Both explicitly allow access to the host. They do not provide the
restricted host boundary required for untrusted lab guests.

Use [the lab example](../examples/networks/allowed-host-lab.yaml) or
[the NAT example](../examples/networks/allowed-host-nat.yaml), after checking
that the chosen subnet is unused:

```sh
virmill network cidr check --input '{"candidates":["10.197.238.0/24"]}'
virmill network create examples/networks/allowed-host-lab.yaml --plan
```

The preview shows the retained display metadata, fresh network UUID, native
`virmill-UUID` name, new `vm` bridge identity, exact XML, host-access policy and
required acknowledgements. It reserves nothing and changes no host state.
An administrator must first add the exact network UUID from this preview to
the helper policy as described in [network helper setup](network-helper.md).
Apply the exact returned plan ID/digest and all its acknowledgements:

```sh
virmill plan apply PLAN_ID --digest PLAN_DIGEST \
  --idempotency-key UNIQUE_REQUEST_ID --ack host-mutation \
  --ack network-host-access --ack network-firewall \
  --ack exclusive-network-writer --wait
virmill network creation result OPERATION_ID
```

In the TUI, open **Networks**, select **network create**, and enter the declaration
path. Review the returned plan and its acknowledgements through the normal plan
approval screen. The same section provides creation result and recovery actions.

Creation currently requires `qemu:///system`, an explicit private IPv4 CIDR,
`ipv6.mode: disabled`, and a new virtual bridge. The bridge address is subnet+1;
when enabled, DHCP leases span subnet+2 through the last usable address. NAT DHCP
requires `advertiseDefaultRoute: true`; lab DHCP requires false. A DHCP-disabled
profile also requires false. Addresses, DNS and routes inside an existing guest
are never silently changed. No guest is attached or started by this command.

NAT requires host IPv4 forwarding to be enabled already. Libvirt owns its bridge,
DHCP/DNS and NAT or isolated-forwarding configuration. The bounded helper adds four IPv6 bridge-frame DROP rules to both firewalld
runtime and permanent configuration before activation. No physical uplink is
moved and no firewall reload is requested. Native network autostart remains disabled.
The implementation rejects unsupported restricted profiles, automatic CIDR
allocation, external bridge creation, network extensions and enabled IPv6 rather
than substituting another policy. Those mandatory workflows remain in the
release tracker.

Apply rechecks every host route table, assigned addresses, both network XML
layers, and durable Virmill subnet reservations. Only host default routes are
excluded from conflict detection. Other authorized network writers must be
coordinated because libvirt definition has no atomic create-only primitive.
Checks can detect ordinary collisions but cannot eliminate an external writer
racing the native definition call.

Before definition, the coordinator durably reserves the exact subnet and network
identity. Definition, IPv6 filtering and activation are separate job steps. A lost acknowledgement
retains resources and reports recovery required. Inspect the result, then use
`virmill operation reconcile OPERATION_ID` for read-only observation. If the
exact inactive definition was retained but activation was not attempted, use:

```sh
virmill network creation resume OPERATION_ID --plan
```

Apply this new preview to finish its exact owned IPv6 rules and activate the retained network. Recovery never defines
it again, inherits all the original locks, and leaves the original operation
partial. Missing or changed definitions are refused. Cancellation or validation failure after a completed step retains its locks as
recovery-required. Cancellation or failure retains any created definition and reservation; there is no implicit deletion or
subnet reuse. Cleanup and complete reservation lifecycle remain outstanding.

The result's `packetVerification: not-run` and `guestRoutingVerified: false`
describe what the application has proven. A defined/active network is not proof
of DHCP options, DNS, internet reachability, guest routes, IPv6 or isolation. The
[packet fixture](network-packet-fixtures.md) records native namespace observations
separately from real guest and hardware qualification.
