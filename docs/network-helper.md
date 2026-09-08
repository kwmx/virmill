# Network helper setup

Managed NAT/lab and guest-only creation with disabled IPv6 require the ordinary-user
coordinator, the authenticated root helper, running firewalld, its `firewall-cmd`
client and working bridge-family direct-rule support. The helper does not start
firewalld or reload it. NAT also requires IPv4 forwarding to be enabled already.
Unsupported hosts fail capability checks before definition.

Complete the existing [helper setup](managed-volume-access.md) to establish
the coordinator's private Ed25519 key, root-owned policy and system socket.
The private key must remain mode 0600 and must never be copied into policy or
sent in a message. `virmill host helper identity` reports its public key, key ID
and actor UID. Existing `actors` and `keys` entries remain necessary.

Create the network preview first. For a protected version 2 profile, add an entry
to `protectedNetworks` in `/etc/virmill/helper-policy.json`, using the exact
values from that preview and the coordinator's public identity. This object
illustrates the additional array; merge its entry into the existing policy:

```json
{
  "protectedNetworks": [
    {
      "actorUID": 1000,
      "keyID": "THE_COORDINATOR_PUBLIC_KEY_ID",
      "resourceID": "THE_NEW_NETWORK_UUID_FROM_THE_PREVIEW"
    }
  ]
}
```

Version 2 applies to NAT/lab with `hostAccess: services-only` and guest-only with
`hostAccess: deny`. It uses helper operation `network.policy-filter` and request
version 2. The original allowed-host NAT/lab profiles continue to use the
`networks` array, operation `network.ipv6-filter` and request version 1. A
`networks` entry grants no protected-profile authority; a `protectedNetworks`
entry grants no version 1 authority. Moving between them requires a new reviewed
definition and the matching administrator grant, not editing a persisted plan.

An administrator maintains this root-owned file and preserves its other entries.
No storage
root, wildcard UUID, different actor or signing key grants network authority.
The fresh preview remains reviewable before the network grant exists; apply
checks the grant and firewalld before accepting a host mutation. Grant revocation
blocks subsequent helper calls but does not delete existing protective rules.

The original version 1 rule vectors remain unchanged: four IPv6 DROP rules for
the generated bridge, covering logical ingress in INPUT/FORWARD and logical
egress in OUTPUT/FORWARD. Each has a runtime and permanent entry.

The protected version 2 path retains those IPv6 rules and adds a fixed policy:

- Services-only uses IPv4 `mangle INPUT` for host-destined traffic. When DHCP is
  enabled, narrowly declared DHCP requests and TCP/UDP DNS to the bridge service
  address precede the drop for other guest-to-host IPv4 traffic. Disabling DHCP
  removes both service allowances and disables managed DNS in network XML.
  Routing through NAT remains distinct from access to host services.
- Guest-only adds bridge-family IPv4 and ARP drops for traffic entering or
  leaving the host, without blocking the intended guest-to-guest IPv4/ARP bridge
  path. Its native XML contains no host IP, DHCP, DNS service or forwarding.
  A declared CIDR is only a logical guest-subnet reservation; guests require
  explicit static addressing.

These protected rule paths are under implementation and native qualification.
The helper must verify supported chain ordering and exact observed configuration
before activation is admitted. It refuses unknown or potentially bypassing
rules, passthroughs or relevant custom chains instead of rewriting foreign
configuration. It cannot accept a shell command, different interface, arbitrary
rule, removal request, reload or storage path. Matching runtime/permanent entries
do not establish packet isolation.

Every filter mutation has a fsynced root-owned intent in
`/var/lib/virmill-host-helper`. Resource ownership binds actor, key, network UUID
and the complete reviewed definition, including its policy version. Job and per-rule records separately bind
the approved plan and operation. Duplicate apply is refused. A fresh reviewed
recovery job may finish the same fixed rule set when prior durable intent proves
ownership; it does not adopt matching foreign rules or delete unrelated entries.

Firewalld's direct interface is deprecated. The bridge-family path addresses
frames between guests and the host that an IP-only zone does not describe; the
protected services-only path also checks IPv4 mangle INPUT ordering explicitly.
Native behavior, reload/reboot restoration, and the full IPv6 matrix still need
release evidence on each supported distribution. Presence in firewalld is a
configuration observation; helper responses always keep `packetVerified: false`.

Version 2 has a 240-second overall helper limit; version 1 retains its 90-second
limit. Both retain bounded 5-second child commands. Native observation of the
original version 1 operation found that 93 separately acknowledged firewalld
calls exceeded its initial 30-second limit. A client can use `--timeout 300s` for
a protected operation or `--timeout 180s` for version 1. A client wait limit is
separate from the helper's deadline: detachment never cancels or silently retries
the durable job.
