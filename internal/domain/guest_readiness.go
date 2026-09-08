package domain

import (
	"context"
	"time"
)

// GuestReadiness is a point-in-time guest-agent observation for a running VM.
// State is absent, unknown, disconnected, unresponsive or responsive; it is not
// the native VM state. Only a successful fixed guest-ping proves responsiveness.
// Neither channel connectivity nor responsiveness proves application readiness.
type GuestReadiness struct {
	Resource        ResourceKey `json:"resource"`
	State           string      `json:"state"`
	AgentConnected  bool        `json:"agentConnected"`
	AgentResponsive bool        `json:"agentResponsive"`
	Evidence        string      `json:"evidence"`
	ObservedAt      time.Time   `json:"observedAt"`
}

// GuestReadinessProvider observes an explicitly selected VM without changing
// guest configuration, installing an agent or invoking caller-supplied commands.
type GuestReadinessProvider interface {
	InspectGuestReadiness(context.Context, string, string) (GuestReadiness, error)
}
