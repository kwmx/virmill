package app

import (
	"context"
	"virmill.local/core/internal/domain"
)

// NetworkFirewall is the authenticated bounded helper boundary. Check grants no
// mutation authority; Apply persists each privileged intent; Observe never
// replays a rule. These methods cannot accept caller-supplied firewall syntax.
type NetworkFirewall interface {
	Check(context.Context, domain.Plan, domain.NetworkDefinition) error
	Apply(context.Context, domain.Plan, domain.NetworkDefinition, string) error
	Observe(context.Context, domain.Plan, domain.NetworkDefinition, string) error
}
