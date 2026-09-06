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

func TestGrantBindsPeerActionResourcesAndPlan(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	p := Policy{APIVersion: "virmill/v1", Keys: map[string]string{"admin": hex.EncodeToString(pub)}, Roots: map[string]string{"test": "/disposable-not-executed"}, Actors: []uint32{1000}}
	now := time.Now()
	r := Request{APIVersion: "virmill/v1", ActorUID: 1000, Operation: "storage.prepare-directory", ResourceID: domain.ID(), RootID: "test", PlanDigest: strings.Repeat("a", 64), JobID: domain.ID(), ExpiresAt: now.Add(time.Minute), KeyID: "admin"}
	b, _ := SignedBytes(r)
	r.Signature = hex.EncodeToString(ed25519.Sign(key, b))
	if e := Authorize(1000, r, p, now); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Request){func(r *Request) { r.Operation = "runCommand" }, func(r *Request) { r.ResourceID = "../../etc" }, func(r *Request) { r.PlanDigest = strings.Repeat("b", 64) }, func(r *Request) { r.ActorUID = 1001 }, func(r *Request) { r.JobID = domain.ID() }, func(r *Request) { r.RootID = "other" }} {
		copy := r
		mutate(&copy)
		if Authorize(1000, copy, p, now) == nil {
			t.Fatal("grant substitution accepted")
		}
	}
	if Authorize(1001, r, p, now) == nil || Authorize(1000, r, p, now.Add(time.Hour)) == nil {
		t.Fatal("peer/expiry not enforced")
	}
}
