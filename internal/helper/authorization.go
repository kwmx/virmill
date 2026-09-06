// Package helper defines the bounded privileged request/grant contract.
package helper

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"regexp"
	"time"
	"virmill.local/core/internal/operations"
)

type Request struct {
	APIVersion string    `json:"apiVersion"`
	ActorUID   uint32    `json:"actorUID"`
	Operation  string    `json:"operation"`
	ResourceID string    `json:"resourceID"`
	RootID     string    `json:"rootID"`
	PlanDigest string    `json:"planDigest"`
	JobID      string    `json:"jobID"`
	ExpiresAt  time.Time `json:"expiresAt"`
	KeyID      string    `json:"keyID"`
	Signature  string    `json:"signature"`
}
type Policy struct {
	APIVersion string            `json:"apiVersion"`
	Keys       map[string]string `json:"keys"`
	Roots      map[string]string `json:"roots"`
	Actors     []uint32          `json:"actors"`
}

var uuid = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

func SignedBytes(r Request) ([]byte, error) { r.Signature = ""; return operations.Canonical(r) }
func Authorize(peer uint32, r Request, p Policy, now time.Time) error {
	if r.APIVersion != "virmill/v1" || p.APIVersion != "virmill/v1" {
		return errors.New("unsupported helper contract")
	}
	if r.ActorUID != peer || peer == 0 {
		return errors.New("grant actor differs from kernel peer")
	}
	allowed := false
	for _, uid := range p.Actors {
		if uid == peer {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("peer not allowed by administrator policy")
	}
	if r.Operation != "storage.prepare-directory" {
		return errors.New("helper operation not implemented or allowlisted")
	}
	if !uuid.MatchString(r.ResourceID) || !uuid.MatchString(r.JobID) {
		return errors.New("resource/job must be stable UUIDs")
	}
	digest, e := hex.DecodeString(r.PlanDigest)
	if e != nil || len(digest) != 32 {
		return errors.New("invalid plan digest")
	}
	if now.After(r.ExpiresAt) || r.ExpiresAt.Sub(now) > 15*time.Minute {
		return errors.New("grant expired or duration exceeds limit")
	}
	if p.Roots[r.RootID] == "" {
		return errors.New("unapproved storage root")
	}
	pub, e := hex.DecodeString(p.Keys[r.KeyID])
	if e != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("untrusted grant signing key")
	}
	sig, e := hex.DecodeString(r.Signature)
	if e != nil {
		return e
	}
	b, e := SignedBytes(r)
	if e != nil {
		return e
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), b, sig) {
		return errors.New("grant signature invalid")
	}
	return nil
}
