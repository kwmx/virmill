package helper

import (
	"errors"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
)

// NetworkPermission grants only addition/observation of the fixed IPv6 deny
// rules for one reviewed newly managed network. Storage authority never grants
// firewall authority. There are no wildcard resource, actor or key grants.
type NetworkPermission struct {
	ActorUID   uint32 `json:"actorUID"`
	KeyID      string `json:"keyID"`
	ResourceID string `json:"resourceID"`
}
type NetworkRequest struct {
	Version    int                      `json:"version"`
	Definition domain.NetworkDefinition `json:"definition"`
}
type NetworkResponse struct {
	Version          int    `json:"version"`
	ResourceID       string `json:"resourceID"`
	Bridge           string `json:"bridge"`
	PlanDigest       string `json:"planDigest"`
	JobID            string `json:"jobID"`
	RuntimePresent   bool   `json:"runtimePresent"`
	PermanentPresent bool   `json:"permanentPresent"`
	PacketVerified   bool   `json:"packetVerified"`
}

func authorizeNetwork(r Request, p Policy) error {
	if r.Network == nil || r.Network.Version != 1 || r.Access != nil || r.Auxiliary != nil || r.RootID != "" || (r.Mode != "check" && r.Mode != "apply" && r.Mode != "observe") || r.Network.Definition.UUID != r.ResourceID || networkxml.Validate(r.Network.Definition) != nil {
		return errors.New("exact typed network IPv6 filter request required")
	}
	for _, grant := range p.Networks {
		if grant.ActorUID == r.ActorUID && grant.KeyID == r.KeyID && grant.ResourceID == r.ResourceID {
			return nil
		}
	}
	return errors.New("network IPv6 filter requires explicit administrator approval for this actor, key and network UUID")
}
