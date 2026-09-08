package app

import (
	"context"
	"encoding/hex"
	"net/netip"

	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/domain"
)

// The selection and the configuration that produced it belong to the immutable
// recipe. Host observations are informational; apply independently rechecks the
// selected subnet and can never substitute a new one.
type networkAllocationReview struct {
	Version        int                      `json:"version"`
	RequestedCIDR  string                   `json:"requestedCIDR"`
	Config         network.AllocationConfig `json:"config"`
	ObservedDigest string                   `json:"observedDigest"`
}

func (s *Service) allocationConfig(ctx context.Context) (network.AllocationConfig, error) {
	if err := ctx.Err(); err != nil {
		return network.AllocationConfig{}, err
	}
	cfg := network.DefaultAllocationConfig()
	if s.NetworkAllocation != nil {
		var err error
		cfg, err = s.NetworkAllocation(ctx)
		if err != nil {
			return network.AllocationConfig{}, err
		}
	}
	if err := network.ValidateAllocationConfig(cfg); err != nil {
		return network.AllocationConfig{}, domain.Fail("INVALID_INPUT", "invalid network allocation settings: "+err.Error())
	}
	return cfg, ctx.Err()
}

func validateNetworkAllocation(r networkRecipe) error {
	a := r.Allocation
	if a == nil {
		return nil
	} // Pre-allocation recipes keep their exact meaning.
	digest, err := hex.DecodeString(a.ObservedDigest)
	if a.Version != 1 || a.RequestedCIDR != "auto" || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != a.ObservedDigest || network.ValidateAllocationConfig(a.Config) != nil {
		return domain.Fail("INVALID_INPUT", "invalid automatic subnet selection binding")
	}
	selected, err := canonicalPrefix(r.Definition.IPv4CIDR)
	if err != nil {
		return err
	}
	allowed := false
	for _, scope := range a.Config.Ranges {
		pool, _ := netip.ParsePrefix(scope.CIDR)
		allowed = allowed || selected.Bits() == scope.PrefixLength && pool.Contains(selected.Addr())
	}
	for _, planned := range a.Config.Planned {
		p, _ := netip.ParsePrefix(planned.CIDR)
		if p.Overlaps(selected) {
			allowed = false
			break
		}
	}
	if !allowed {
		return domain.Fail("INVALID_INPUT", "selected subnet is outside its reviewed allocation settings")
	}
	return nil
}

func (s *Service) allocateNetworkCIDR(ctx context.Context, uri string) (string, *networkAllocationReview, error) {
	cfg, err := s.allocationConfig(ctx)
	if err != nil {
		return "", nil, err
	}
	occupied, report, err := s.observeCIDROccupancy(ctx, uri, cidrInput{Planned: cfg.Planned}, "")
	if err != nil {
		return "", nil, err
	}
	prefixes := make([]netip.Prefix, 0, len(occupied))
	for _, item := range occupied {
		p, err := canonicalPrefix(item.CIDR)
		if err != nil {
			return "", nil, err
		}
		prefixes = append(prefixes, p)
	}
	chosen, err := network.AllocateConfigured(ctx, cfg, prefixes)
	if err != nil {
		return "", nil, err
	}
	if err = ctx.Err(); err != nil {
		return "", nil, err
	}
	return chosen.String(), &networkAllocationReview{Version: 1, RequestedCIDR: "auto", Config: cfg, ObservedDigest: report.ObservedDigest}, nil
}
