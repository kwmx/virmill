package helper

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
)

func TestProtectedNetworkAuthorityCannotReuseOrDowngradeLegacyGrant(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, kind := range []string{"nat", "lab", "guest-only"} {
		t.Run(kind, func(t *testing.T) {
			id := domain.ID()
			d := domain.NetworkDefinition{UUID: id, Name: "virmill-" + id, Bridge: "vm" + strings.ReplaceAll(id, "-", "")[:12], Type: kind, IPv4CIDR: "10.197.224.0/24", IPv6Mode: "disabled", HostAccess: "services-only", Egress: "none"}
			if kind == "nat" {
				d.Egress = "any"
			}
			if kind == "guest-only" {
				d.HostAccess = "deny"
			}
			r := Request{APIVersion: domain.APIVersion, ActorUID: 1000, Operation: "network.policy-filter", ResourceID: id, PlanDigest: strings.Repeat("a", 64), JobID: domain.ID(), ExpiresAt: now.Add(5 * time.Minute), KeyID: "fixture-key", Mode: "check", Network: &NetworkRequest{Version: 2, Definition: d}}
			sign := func(r *Request) {
				b, e := SignedBytes(*r)
				if e != nil {
					t.Fatal(e)
				}
				r.Signature = hex.EncodeToString(ed25519.Sign(key, b))
			}
			grant := NetworkPermission{ActorUID: r.ActorUID, KeyID: r.KeyID, ResourceID: id}
			p := Policy{APIVersion: domain.APIVersion, Actors: []uint32{1000}, Keys: map[string]string{r.KeyID: hex.EncodeToString(pub)}, Networks: []NetworkPermission{grant}}
			sign(&r)
			if Authorize(1000, r, p, now) == nil {
				t.Fatal("legacy grant authorized protected policy")
			}
			p.ProtectedNetworks = []NetworkPermission{grant}
			for _, mode := range []string{"check", "apply", "observe"} {
				copy := r
				copy.Mode = mode
				sign(&copy)
				if err := Authorize(1000, copy, p, now); err != nil {
					t.Fatal(mode, err)
				}
			}
			for _, change := range []func(*Request){
				func(r *Request) { r.Operation = "network.ipv6-filter" },
				func(r *Request) { r.Network.Version = 1 },
				func(r *Request) { r.Operation = "network.ipv6-filter"; r.Network.Version = 1 },
				func(r *Request) { r.RootID = "root" },
				func(r *Request) { r.Access = &AccessRequest{} },
				func(r *Request) { r.Auxiliary = &AuxiliaryRequest{} },
				func(r *Request) { r.Network.Definition.HostAccess = "allow" },
				func(r *Request) { r.Mode = "remove" },
				func(r *Request) { r.ResourceID = domain.ID() },
			} {
				copy := r
				copy.Network = &NetworkRequest{Version: 2, Definition: d}
				change(&copy)
				sign(&copy)
				if Authorize(1000, copy, p, now) == nil {
					t.Fatal("mixed/downgraded authority accepted", copy)
				}
			}
			copy := r
			copy.Network = &NetworkRequest{Version: 2, Definition: d}
			copy.Network.Definition.IPv4CIDR = "10.197.223.0/24"
			if Authorize(1000, copy, p, now) == nil {
				t.Fatal("signature failed to bind protected definition")
			}
			p.ProtectedNetworks = nil
			if Authorize(1000, r, p, now) == nil {
				t.Fatal("revocation ignored")
			}
		})
	}
}

func TestProtectedGrantDoesNotAuthorizeLegacyAllowedHostNetwork(t *testing.T) {
	id := domain.ID()
	d := domain.NetworkDefinition{UUID: id, Name: "virmill-" + id, Bridge: "vm" + strings.ReplaceAll(id, "-", "")[:12], Type: "lab", IPv4CIDR: "10.197.224.0/24", IPv6Mode: "disabled", HostAccess: "allow", Egress: "none"}
	r := Request{ActorUID: 1000, KeyID: "key", ResourceID: id, Mode: "check", Operation: "network.ipv6-filter", Network: &NetworkRequest{Version: 1, Definition: d}}
	p := Policy{ProtectedNetworks: []NetworkPermission{{ActorUID: 1000, KeyID: "key", ResourceID: id}}}
	if authorizeNetwork(r, p) == nil {
		t.Fatal("protected grant authorized allowed host policy")
	}
}
