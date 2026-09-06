// Package network validates intent. Its reports do not certify packet filtering.
package network

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

type IPv4 struct {
	CIDR string `json:"cidr"`
	DHCP struct {
		Enabled               bool `json:"enabled"`
		AdvertiseDefaultRoute bool `json:"advertiseDefaultRoute"`
	} `json:"dhcp"`
}
type Spec struct {
	Type      string `json:"type"`
	BridgeRef string `json:"bridgeRef"`
	IPv4      *IPv4  `json:"ipv4"`
	IPv6      struct {
		Mode string `json:"mode"`
	} `json:"ipv6"`
	HostAccess string `json:"hostAccess"`
	Egress     string `json:"egress"`
}
type NIC struct {
	ID           string `json:"id"`
	NetworkRef   string `json:"networkRef"`
	DefaultRoute bool   `json:"defaultRoute"`
	RouteMetric  int    `json:"routeMetric"`
	IPv4         struct {
		Mode    string   `json:"mode"`
		Address string   `json:"address"`
		Gateway string   `json:"gateway"`
		DNS     []string `json:"dns"`
	} `json:"ipv4"`
}

func Validate(s Spec) error {
	switch s.Type {
	case "nat", "lab", "bridge", "guest-only":
	default:
		return errors.New("unknown network type")
	}
	if s.Type == "bridge" && (s.BridgeRef == "" || s.HostAccess != "allow" || s.Egress != "any") {
		return errors.New("external bridge intent cannot enforce host isolation")
	}
	if s.Type == "guest-only" && (s.HostAccess != "deny" || s.Egress != "none") {
		return errors.New("guest-only must deny host access and egress")
	}
	if s.Type == "lab" && s.Egress != "none" {
		return errors.New("lab egress must be none")
	}
	if s.IPv4 != nil {
		if s.Type == "guest-only" || s.Type == "bridge" {
			if s.IPv4.DHCP.Enabled {
				return errors.New("guest-only/external bridge cannot own host DHCP")
			}
		}
		if (s.Type == "lab" || s.Type == "guest-only") && s.IPv4.DHCP.AdvertiseDefaultRoute {
			return errors.New("lab DHCP must not advertise a default route")
		}
		if s.IPv4.CIDR != "auto" {
			p, e := netip.ParsePrefix(s.IPv4.CIDR)
			if e != nil || !p.Addr().Is4() || p != p.Masked() || p.Bits() > 30 {
				return errors.New("expected canonical usable IPv4 subnet")
			}
		}
	}
	return nil
}
func ValidateNICs(nics []NIC, nets map[string]Spec) ([]string, error) {
	seen := map[string]bool{}
	defaults := 0
	warnings := []string{}
	attached := []netip.Prefix{}
	protected, external := false, false
	for _, n := range nics {
		if seen[n.ID] || n.ID == "" {
			return nil, errors.New("duplicate/missing NIC ID")
		}
		seen[n.ID] = true
		if n.DefaultRoute {
			defaults++
		}
		if n.IPv4.Gateway != "" && !n.DefaultRoute {
			return nil, errors.New("non-default NIC cannot declare default gateway")
		}
		if n.DefaultRoute && n.IPv4.Mode == "none" {
			return nil, errors.New("default route requires addressing")
		}
		if n.IPv4.Mode == "static" {
			p, e := netip.ParsePrefix(n.IPv4.Address)
			if e != nil || !p.Addr().Is4() {
				return nil, errors.New("invalid static address")
			}
			if p.Addr() == p.Masked().Addr() {
				return nil, errors.New("network address is not a host address")
			}
			if n.IPv4.Gateway != "" {
				g, e := netip.ParseAddr(n.IPv4.Gateway)
				if e != nil || !p.Contains(g) {
					return nil, errors.New("gateway outside NIC subnet")
				}
			}
		}
		for _, dns := range n.IPv4.DNS {
			if _, e := netip.ParseAddr(dns); e != nil {
				return nil, errors.New("invalid DNS address")
			}
		}
		if nets != nil {
			s, ok := nets[n.NetworkRef]
			if !ok {
				return nil, fmt.Errorf("unknown network %s", n.NetworkRef)
			}
			if s.Type == "lab" || s.Type == "guest-only" {
				protected = true
				if n.DefaultRoute {
					return nil, errors.New("protected network cannot supply normal default route")
				}
			}
			if s.Type == "nat" || s.Type == "bridge" {
				external = true
			}
			if s.IPv4 != nil && s.IPv4.CIDR != "auto" {
				p, e := netip.ParsePrefix(s.IPv4.CIDR)
				if e != nil {
					return nil, e
				}
				for _, other := range attached {
					if p.Overlaps(other) {
						return nil, errors.New("overlapping attached subnets")
					}
				}
				attached = append(attached, p)
				if n.IPv4.Mode == "static" {
					a, _ := netip.ParsePrefix(n.IPv4.Address)
					if !p.Contains(a.Addr()) {
						return nil, errors.New("static NIC address outside selected network")
					}
				}
			}
		}
	}
	if defaults > 1 {
		return nil, errors.New("multiple normal IPv4 default routes")
	}
	if protected && external {
		warnings = append(warnings, "DUAL_HOMED_GUEST: guest forwarding can bypass the protected segment boundary")
	}
	warnings = append(warnings, "guest-routing-unverified: intent validation is not observed guest routing")
	return warnings, nil
}

// Allocate excludes default routes only when they represent host routes. Planned
// allocations and network prefixes must never pass through that exclusion.
func Allocate(candidates, hostRoutes, occupied []netip.Prefix) (netip.Prefix, error) {
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].String() < candidates[j].String() })
	for _, c := range candidates {
		if !c.IsValid() || !c.Addr().IsPrivate() || !c.Addr().Is4() || c != c.Masked() {
			continue
		}
		conflict := false
		for _, r := range hostRoutes {
			if r.Bits() > 0 && c.Overlaps(r) {
				conflict = true
			}
		}
		for _, r := range occupied {
			if c.Overlaps(r) {
				conflict = true
			}
		}
		if !conflict {
			return c, nil
		}
	}
	return netip.Prefix{}, errors.New("no non-overlapping private subnet available")
}
