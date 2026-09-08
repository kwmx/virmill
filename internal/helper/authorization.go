// Package helper defines the bounded privileged request/grant contract.
package helper

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

type Request struct {
	APIVersion string            `json:"apiVersion"`
	ActorUID   uint32            `json:"actorUID"`
	Operation  string            `json:"operation"`
	ResourceID string            `json:"resourceID"`
	RootID     string            `json:"rootID"`
	PlanDigest string            `json:"planDigest"`
	JobID      string            `json:"jobID"`
	ExpiresAt  time.Time         `json:"expiresAt"`
	KeyID      string            `json:"keyID"`
	Signature  string            `json:"signature"`
	Mode       string            `json:"mode,omitempty"`
	Access     *AccessRequest    `json:"access,omitempty"`
	Auxiliary  *AuxiliaryRequest `json:"auxiliary,omitempty"`
	Network    *NetworkRequest   `json:"network,omitempty"`
}
type AccessRequest struct {
	Mapping            domain.ManagedFileVolume `json:"mapping"`
	RelativePath       string                   `json:"relativePath"`
	Before             json.RawMessage          `json:"before"`
	ActorGroups        []uint32                 `json:"actorGroups"`
	OriginalGrantJobID string                   `json:"originalGrantJobID,omitempty"`
}
type Response struct {
	APIVersion string             `json:"apiVersion"`
	Success    bool               `json:"success"`
	Error      string             `json:"error,omitempty"`
	Access     json.RawMessage    `json:"access,omitempty"`
	Auxiliary  *AuxiliaryResponse `json:"auxiliary,omitempty"`
	ErrorCode  string             `json:"errorCode,omitempty"`
	Network    *NetworkResponse   `json:"network,omitempty"`
}
type Policy struct {
	APIVersion        string                `json:"apiVersion"`
	Keys              map[string]string     `json:"keys"`
	Roots             map[string]string     `json:"roots"`
	Actors            []uint32              `json:"actors"`
	Auxiliary         []AuxiliaryPermission `json:"auxiliary,omitempty"`
	Networks          []NetworkPermission   `json:"networks,omitempty"`
	ProtectedNetworks []NetworkPermission   `json:"protectedNetworks,omitempty"`
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
	switch r.Operation {
	case "network.ipv6-filter", "network.policy-filter":
		if err := authorizeNetwork(r, p); err != nil {
			return err
		}
	case "state.auxiliary":
		if r.Network != nil {
			return errors.New("auxiliary operation cannot carry network authority")
		}
		if err := authorizeAuxiliary(r, p); err != nil {
			return err
		}
	case "storage.prepare-directory":
		if r.Mode != "" || r.Access != nil || r.Auxiliary != nil || r.Network != nil {
			return errors.New("legacy directory operation cannot carry access authority")
		}
	case "storage.grant-read", "storage.revoke-read":
		if r.Network != nil || r.Auxiliary != nil || r.Access == nil || (r.Mode != "check" && r.Mode != "apply" && r.Mode != "observe") {
			return errors.New("typed access request and explicit mode required")
		}
		if r.Access.Mapping.VMID != r.ResourceID || !uuid.MatchString(r.Access.Mapping.PoolID) {
			return errors.New("native VM/pool identity differs")
		}
		if r.Operation == "storage.grant-read" && r.Access.OriginalGrantJobID != "" {
			return errors.New("grant cannot reference another grant")
		}
		if r.Operation == "storage.revoke-read" && (!uuid.MatchString(r.Access.OriginalGrantJobID) || r.Access.OriginalGrantJobID == r.JobID) {
			return errors.New("revoke requires a distinct original grant job")
		}
		if len(r.Access.ActorGroups) == 0 || len(r.Access.ActorGroups) > 4096 {
			return errors.New("bounded actual peer groups required")
		}
		for i, g := range r.Access.ActorGroups {
			if g == ^uint32(0) || (i > 0 && r.Access.ActorGroups[i-1] >= g) {
				return errors.New("canonical peer group inventory required")
			}
		}
	default:
		return errors.New("helper operation not implemented or allowlisted")
	}
	if (!(r.Operation == "state.auxiliary" && auxiliaryID(r.ResourceID)) && !uuid.MatchString(r.ResourceID)) || !uuid.MatchString(r.JobID) {
		return errors.New("resource/job must be stable UUIDs")
	}
	digest, e := hex.DecodeString(r.PlanDigest)
	if e != nil || len(digest) != 32 {
		return errors.New("invalid plan digest")
	}
	if !now.Before(r.ExpiresAt) || r.ExpiresAt.Sub(now) > 15*time.Minute {
		return errors.New("grant expired or duration exceeds limit")
	}
	if r.Operation != "network.ipv6-filter" && r.Operation != "network.policy-filter" && p.Roots[r.RootID] == "" {
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
