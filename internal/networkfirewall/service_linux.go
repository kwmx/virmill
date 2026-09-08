//go:build linux && amd64

// Package networkfirewall binds reviewed network operations to the authenticated helper.
package networkfirewall

import (
	"context"
	"path/filepath"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
)

type client struct{ helper helper.Client }

func Register(s *app.Service, config string) {
	s.NetworkFirewall = &client{helper.Client{KeyPath: filepath.Join(config, "helper-key.pem")}}
}
func (c *client) call(ctx context.Context, p domain.Plan, d domain.NetworkDefinition, id, mode string) error {
	version, operation := d.PolicyVersion(), "network.ipv6-filter"
	if version == 2 {
		operation = "network.policy-filter"
	} else if version != 1 {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "network policy family unavailable")
	}
	r := helper.Request{APIVersion: domain.APIVersion, ActorUID: p.ActorUID, Operation: operation, ResourceID: d.UUID, PlanDigest: p.Digest, JobID: id, Mode: mode, Network: &helper.NetworkRequest{Version: version, Definition: d}}
	_, err := c.helper.NetworkFilter(ctx, r)
	return err
}
func (c *client) Check(ctx context.Context, p domain.Plan, d domain.NetworkDefinition) error {
	return c.call(ctx, p, d, p.ID, "check")
}
func (c *client) Apply(ctx context.Context, p domain.Plan, d domain.NetworkDefinition, id string) error {
	return c.call(ctx, p, d, id, "apply")
}
func (c *client) Observe(ctx context.Context, p domain.Plan, d domain.NetworkDefinition, id string) error {
	return c.call(ctx, p, d, id, "observe")
}
