# Network helper setup

Managed NAT/lab creation with disabled IPv6 requires the ordinary-user
coordinator, the authenticated root helper, running firewalld, its `firewall-cmd`
client and working bridge-family direct-rule support. The helper does not start
firewalld or reload it. NAT also requires IPv4 forwarding to be enabled already.
Unsupported hosts fail capability checks before definition.

Complete the existing [helper setup](managed-volume-access.md) to establish
the coordinator's private Ed25519 key, root-owned policy and system socket.
The private key must remain mode 0600 and must never be copied into policy or
sent in a message. `virmill host helper identity` reports its public key, key ID
and actor UID. Existing `actors` and `keys` entries remain necessary.

Create the network preview first. Add one entry to a `networks` array in
`/etc/virmill/helper-policy.json`, using the exact values from that preview and
public identity:

```json
{
  "actorUID": 1000,
  "keyID": "THE_COORDINATOR_PUBLIC_KEY_ID",
  "resourceID": "THE_NEW_NETWORK_UUID_FROM_THE_PREVIEW"
}
```

This is an additional entry inside the existing policy, not a replacement for
its other settings. An administrator maintains this root-owned file. No storage
root, wildcard UUID, different actor or signing key grants network authority.
The fresh preview remains reviewable before the network grant exists; apply
checks the grant and firewalld before accepting a host mutation. Grant revocation
blocks subsequent helper calls but does not delete existing protective rules.

The helper can only add or observe four fixed IPv6 DROP rules for the generated
bridge: logical ingress in INPUT/FORWARD and logical egress in OUTPUT/FORWARD.
It maintains matching runtime and permanent firewalld entries. It cannot accept
a shell command, different interface, arbitrary firewall rule, removal request,
reload or storage path. Foreign potentially bypassing bridge rules cause refusal.

Every filter mutation has a fsynced root-owned intent in
`/var/lib/virmill-host-helper`. Resource ownership binds actor, key, network UUID
and the complete reviewed definition. Job and per-rule records separately bind
the approved plan and operation. Duplicate apply is refused. A fresh reviewed
recovery job may finish the same fixed rule set when prior durable intent proves
ownership; it does not adopt matching foreign rules or delete unrelated entries.

Firewalld's direct interface is deprecated. This adapter uses its bridge family
only because ordinary zones cannot block IPv6 frames bridged between guests.
Native behavior, reload/reboot restoration, and the full IPv6 matrix still need
release evidence on each supported distribution. Presence in firewalld is a
configuration observation; helper responses always keep `packetVerified: false`.
