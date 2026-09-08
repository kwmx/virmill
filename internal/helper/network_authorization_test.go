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

func TestNetworkFilterRequiresSeparateExactAdministratorGrant(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	id := domain.ID()
	d := domain.NetworkDefinition{UUID: id, Name: "virmill-" + id, Bridge: "vm" + strings.ReplaceAll(id, "-", "")[:12], Type: "lab", IPv4CIDR: "10.197.238.0/24", IPv6Mode: "disabled", HostAccess: "allow", Egress: "none"}
	r := Request{APIVersion: domain.APIVersion, ActorUID: 1000, Operation: "network.ipv6-filter", ResourceID: id, PlanDigest: strings.Repeat("a", 64), JobID: domain.ID(), ExpiresAt: now.Add(time.Minute), KeyID: "test-key", Mode: "check", Network: &NetworkRequest{Version: 1, Definition: d}}
	sign := func(r *Request) {
		b, e := SignedBytes(*r)
		if e != nil {
			t.Fatal(e)
		}
		r.Signature = hex.EncodeToString(ed25519.Sign(key, b))
	}
	p := Policy{APIVersion: domain.APIVersion, Actors: []uint32{1000}, Keys: map[string]string{"test-key": hex.EncodeToString(pub)}, Roots: map[string]string{"old-root": "/unused"}}
	sign(&r)
	if Authorize(1000, r, p, now) == nil {
		t.Fatal("legacy storage grant authorized firewall")
	}
	p.Networks = []NetworkPermission{{ActorUID: 1000, KeyID: "test-key", ResourceID: id}}
	if e := Authorize(1000, r, p, now); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"check", "apply", "observe"} {
		copy := r
		copy.Mode = mode
		sign(&copy)
		if e := Authorize(1000, copy, p, now); e != nil {
			t.Fatal(mode, e)
		}
	}
	for _, change := range []func(*Request){func(r *Request) { r.RootID = "old-root" }, func(r *Request) { r.ResourceID = domain.ID() }, func(r *Request) { r.ActorUID = 1001 }, func(r *Request) { r.KeyID = "other" }, func(r *Request) { r.Access = &AccessRequest{} }, func(r *Request) { r.Auxiliary = &AuxiliaryRequest{} }, func(r *Request) { r.Mode = "remove" }, func(r *Request) { r.Operation = "storage.prepare-directory" }, func(r *Request) {
		r.Network = &NetworkRequest{Version: 1, Definition: d}
		r.Network.Definition.Bridge = "ens18"
	}} {
		copy := r
		change(&copy)
		sign(&copy)
		if Authorize(1000, copy, p, now) == nil {
			t.Fatal("unapproved or mixed authority accepted", copy.Operation, copy.Mode)
		}
	}
	changed := r
	changed.Network = &NetworkRequest{Version: 1, Definition: d}
	changed.Network.Definition.IPv4CIDR = "10.197.237.0/24"
	if Authorize(1000, changed, p, now) == nil {
		t.Fatal("signature did not bind network definition")
	}
}
