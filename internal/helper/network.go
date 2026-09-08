package helper

import (
	"errors"
	"time"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
)

// NetworkPermission grants addition/observation of fixed policy rules for one
// reviewed newly managed network. Policy.Networks authorizes only version 1;
// Policy.ProtectedNetworks separately authorizes version 2 host-access rules.
// There are no wildcard resource, actor or key grants or storage authority.
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

func networkOperation(version int) string {
	switch version {
	case 1:
		return "network.ipv6-filter"
	case 2:
		return "network.policy-filter"
	default:
		return ""
	}
}

func networkTimeout(version int) time.Duration {
	if version == 2 {
		return 240 * time.Second
	}
	return 90 * time.Second
}

func authorizeNetwork(r Request, p Policy) error {
	if r.Network == nil || networkOperation(r.Network.Version) == "" || r.Operation != networkOperation(r.Network.Version) || r.Network.Definition.PolicyVersion() != r.Network.Version || r.Access != nil || r.Auxiliary != nil || r.RootID != "" || (r.Mode != "check" && r.Mode != "apply" && r.Mode != "observe") || r.Network.Definition.UUID != r.ResourceID || networkxml.Validate(r.Network.Definition) != nil {
		return errors.New("exact typed network policy request required")
	}
	grants := p.Networks
	if r.Network.Version == 2 {
		grants = p.ProtectedNetworks
	}
	for _, grant := range grants {
		if grant.ActorUID == r.ActorUID && grant.KeyID == r.KeyID && grant.ResourceID == r.ResourceID {
			return nil
		}
	}
	return errors.New("network policy requires explicit administrator approval for this policy family, actor, key and network UUID")
}
