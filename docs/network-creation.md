# Create a managed network

`virmill network create PATH --plan` reads a `virmill/v1` Network declaration and
returns an immutable preview through the same service used by the TUI. Supported
declarations have these distinct policies:

| Profile | Host access | Egress | Address and service declaration |
| --- | --- | --- | --- |
| NAT | `allow` or `services-only` | `any` | Private IPv4 CIDR or `auto`; optional DHCP |
| Lab | `allow` or `services-only` | `none` | Private IPv4 CIDR or `auto`; optional DHCP; no DHCP default route |
| Guest-only | `deny` | `none` | No host IP, DHCP, DNS or forwarding; optional logical guest-subnet CIDR |

Protected `services-only` and `guest-only` plans use recipe and policy version 2.
The original `allow` NAT/lab profiles remain version 1 with their exact XML,
intent hashes and allowed-host behavior. They remain unsuitable for guests that
require host isolation. A preview describes the requested policy; it does not
prove that packet enforcement or guest configuration has been verified.

Use [protected NAT](../examples/networks/protected-nat.yaml),
[protected lab](../examples/networks/protected-lab.yaml), or
[static protected lab](../examples/networks/protected-static-lab.yaml), after
checking that the chosen subnet is unused. The original
[allowed-host lab](../examples/networks/allowed-host-lab.yaml) and
[allowed-host NAT](../examples/networks/allowed-host-nat.yaml) examples remain
available when broad host access is explicitly intended:

```sh
virmill network cidr check --input '{"candidates":["10.197.241.0/24"]}'
virmill network create examples/networks/protected-lab.yaml --plan
```

The preview shows the retained display metadata, fresh network UUID, native
`virmill-UUID` name, new `vm` bridge identity, exact XML, host-access policy and
required acknowledgements. It reserves nothing and changes no host state.
An administrator must first add the exact network UUID from this preview to
the correct helper policy family as described in [network helper setup](network-helper.md).
Version 2 requires a separate `protectedNetworks` entry bound to the exact
actor UID, signing key ID and network UUID; a version 1 `networks` grant is
insufficient.
Apply the exact returned plan ID/digest and all its acknowledgements:

```sh
virmill plan apply PLAN_ID --digest PLAN_DIGEST \
  --idempotency-key UNIQUE_REQUEST_ID --ack host-mutation \
  --ack network-host-access --ack network-firewall \
  --ack exclusive-network-writer --wait --timeout 300s
virmill network creation result OPERATION_ID
```

In the TUI, open **Networks**, select **network create**, and enter the declaration
path. Review the returned plan and its acknowledgements through the normal plan
approval screen. Page through the complete definition, risks and helper step,
then confirm its exact digest. Esc exits approval without submitting it. The
same section provides creation result and recovery actions. `plan show` retains
the original reviewed policy and digest in both interfaces; it does not convert
an old allowed-host plan into a protected one.

Creation requires `qemu:///system`, `ipv6.mode: disabled`, and a new virtual
bridge. NAT and lab accept `auto` using [configurable allocation](network-allocation.md), or an explicit canonical private IPv4 CIDR with prefix
/8 through /30. Their bridge address is subnet+1;
when enabled, DHCP leases span subnet+2 through the last usable address. NAT DHCP
requires `advertiseDefaultRoute: true`; lab DHCP requires false. A DHCP-disabled
profile also requires false. In version 2 services-only profiles, DHCP controls
the paired managed DHCP/DNS service set: enabled means explicit DNS `yes`, and
disabled means explicit DNS `no`. There is no separate DNS toggle or implied
DNS service when DHCP is disabled. Managed DNS may use the host's upstream
resolver; allowing DNS is not a promise that queries remain local.

[Guest-only with a CIDR](../examples/networks/guest-only.yaml) reserves that
logical subnet without installing a host address. It requires DHCP and default
route advertisement to be false. Guests need explicit static addresses in that
subnet; the command does not assign them. The
[guest-only example without IPv4](../examples/networks/protected-guest-only-no-cidr.yaml)
also creates no logical subnet reservation. Check the intended guest subnet
separately before configuring or attaching guests. Absence of a reservation is
not proof that arbitrary guest addresses are conflict-free.

Addresses, DNS and routes inside an existing guest are never silently changed.
No guest is attached or started by this command.

NAT requires host IPv4 forwarding to be enabled already. Libvirt owns its bridge,
DHCP/DNS and NAT or isolated-forwarding configuration. Guest-only uses a bridge
without a host layer-three address. The bounded helper must establish the exact
profile's rules in both firewalld runtime and permanent configuration before
activation: version 1 uses the existing IPv6 filter; version 2 adds protected
host-access policy. See the [helper boundary](network-helper.md) for its rule
families and qualification limits. No physical uplink is
moved and no firewall reload is requested. Native network autostart remains disabled.
The implementation still refuses external bridge
creation, network extensions, enabled IPv6, `internet-only` egress and policy
combinations outside the table. These are not silently replaced with an
allowed-host profile. Outstanding mandatory networking, distribution and guest
qualification requirements remain release requirements.

Apply rechecks every host route table, assigned addresses, both network XML
layers, and durable Virmill subnet reservations. Only host default routes are
excluded from conflict detection. Other authorized network writers must be
coordinated because libvirt definition has no atomic create-only primitive.
Checks can detect ordinary collisions but cannot eliminate an external writer
racing the native definition call.

Before definition, the coordinator durably reserves the exact network identity
and any declared subnet. Definition, policy filtering and activation are separate
job steps. A lost acknowledgement
retains resources and reports recovery required. Inspect the result, then use
`virmill operation reconcile OPERATION_ID` for read-only observation. If the
exact inactive definition was retained but activation was not attempted, use:

```sh
virmill network creation resume OPERATION_ID --plan
```

Apply this new preview to finish its exact owned policy rules and activate the retained network. Recovery preserves
the original version 1 or version 2 definition; it never defines
it again, inherits all the original locks, and leaves the original operation
partial. Missing or changed definitions are refused. Cancellation or validation failure after a completed step retains its locks as
recovery-required. Cancellation or failure retains any created definition and reservation; there is no implicit deletion or
subnet reuse. Cleanup and complete reservation lifecycle remain outstanding.

The result's `packetVerification: not-run` and `guestRoutingVerified: false`
describe what the application has proven. A defined/active network is not proof
of DHCP options, DNS, internet reachability, guest routes, IPv6 or isolation. The
[packet fixture](network-packet-fixtures.md) records native namespace observations
separately from real guest and hardware qualification.

Automatic selection and configured planned reservations: [allocation guide](network-allocation.md).

## Guided TUI and inline declarations

Open **Networks → Create network**. Enter a name, choose the purpose and use
**auto** to have the service propose a currently available private subnet.
**Advanced options** exposes host access, DHCP/DNS and default-route advertising.
The purpose summary explains access before preview; changing purpose loads its
visible defaults. Review every page before applying. The operation creates no
VM membership and does not configure guest addresses.

**Export settings** saves a complete Network declaration to a new file without
applying it. **Advanced declaration file** opens the existing file workflow.
Canceling a preview keeps form choices; a service error explains the problem and
leaves them available to correct. Native network and bridge names remain bound
to generated UUIDs; the declaration name/display name is retained as metadata.

CLI users can continue to use a declaration file or supply the identical object:

```sh
virmill network create --input '{"document":{"apiVersion":"virmill/v1","kind":"Network","metadata":{"name":"private-lab"},"spec":{"type":"lab","ipv4":{"cidr":"auto","dhcp":{"enabled":true,"advertiseDefaultRoute":false}},"ipv6":{"mode":"disabled"},"hostAccess":"services-only","egress":"none"}}}' --plan
```

Choose either a path or `input.document`. Unknown fields, mixed sources,
unsupported policy and conflicting subnets are refused by the shared service.
The resulting plan has the same acknowledgement and helper-grant requirements
as a file-based plan. Auto selection is rechecked at apply; it is not a promise
that the host network cannot change after preview.
