package app

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type cidrInput struct {
	Candidates []string `json:"candidates"`
	Planned    []struct {
		ID   string `json:"id"`
		CIDR string `json:"cidr"`
	} `json:"planned"`
}
type prefixConflict struct {
	CIDR           string `json:"cidr"`
	Source         string `json:"source"`
	ID             string `json:"id,omitempty"`
	InterfaceIndex uint32 `json:"interfaceIndex,omitempty"`
	Table          uint32 `json:"table,omitempty"`
}
type cidrCandidate struct {
	CIDR      string           `json:"cidr"`
	Conflicts []prefixConflict `json:"conflicts"`
}
type cidrReport struct {
	Candidates           []cidrCandidate `json:"candidates"`
	ObservedDigest       string          `json:"observedDigest"`
	IgnoredDefaultRoutes int             `json:"ignoredDefaultRoutes"`
	Reserved             bool            `json:"reserved"`
	IsolationVerified    bool            `json:"isolationVerified"`
	Warnings             []string        `json:"warnings"`
}

func canonicalPrefix(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil || p.Addr().Is4In6() || p != p.Masked() || p.String() != s {
		return netip.Prefix{}, domain.Fail("INVALID_INPUT", "expected canonical IPv4 or IPv6 CIDR")
	}
	return p, nil
}

// checkCIDRs observes all host tables and selected local libvirt network layers.
// It grants no reservation and performs no network or journal mutation.
func (s *Service) checkCIDRs(ctx context.Context, r Request) (any, error) {
	return s.checkCIDRsExcept(ctx, r, "")
}

// The exclusion is internal to a journaled creation whose exact inactive
// definition has already been verified. Public CIDR checks cannot select it.
func (s *Service) checkCIDRsExcept(ctx context.Context, r Request, excludeNetwork string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "CIDR observation requires an explicit local qemu connection")
	}
	if r.ID != "" || r.Path != "" || r.Action != "" || r.After != 0 || r.Apply != nil {
		return nil, domain.Fail("INVALID_INPUT", "CIDR check accepts only input and a local connection")
	}
	b, err := json.Marshal(r.Input)
	if err != nil {
		return nil, err
	}
	if err = validation.Schema("network-cidr-input", b); err != nil {
		return nil, domain.Fail("INVALID_INPUT", err.Error())
	}
	var in cidrInput
	if err = wire.Decode(b, &in); err != nil {
		return nil, err
	}
	candidates := []netip.Prefix{}
	for _, raw := range in.Candidates {
		p, err := canonicalPrefix(raw)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, p)
	}
	occupied := []prefixConflict{}
	if s.Engine != nil && s.Engine.Store != nil {
		reserved, err := s.networkRecords()
		if err != nil {
			return nil, err
		}
		for _, record := range reserved {
			if record.Connection == r.Connection && record.Definition.UUID != excludeNetwork {
				occupied = append(occupied, prefixConflict{CIDR: record.Definition.IPv4CIDR, Source: "application-reservation", ID: record.Definition.UUID})
			}
		}
	}
	ids := map[string]bool{}
	for _, planned := range in.Planned {
		if ids[planned.ID] {
			return nil, domain.Fail("INVALID_INPUT", "duplicate planned allocation ID")
		}
		ids[planned.ID] = true
		if _, err := canonicalPrefix(planned.CIDR); err != nil {
			return nil, err
		}
		occupied = append(occupied, prefixConflict{CIDR: planned.CIDR, Source: "planned", ID: planned.ID})
	}
	if s.HostPrefixes == nil {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "host prefix observation unavailable")
	}
	inv, ok := s.Provider.(domain.ResourceInventory)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native network inventory unavailable")
	}
	host, err := s.HostPrefixes(ctx)
	if err != nil {
		return nil, err
	}
	if len(host.Prefixes) > 65536 {
		return nil, domain.Fail("INVALID_STATE", "host prefix observation limit")
	}
	report := cidrReport{Candidates: []cidrCandidate{}, Warnings: append([]string{}, host.Warnings...)}
	for _, h := range host.Prefixes {
		p, err := canonicalPrefix(h.CIDR)
		if err != nil {
			return nil, domain.Fail("INVALID_STATE", "invalid observed host prefix")
		}
		if h.Source != "address" && h.Source != "route" {
			return nil, domain.Fail("INVALID_STATE", "unknown observed prefix source")
		}
		if h.Source == "route" && p.Bits() == 0 {
			report.IgnoredDefaultRoutes++
			continue
		}
		occupied = append(occupied, prefixConflict{CIDR: h.CIDR, Source: h.Source, InterfaceIndex: h.InterfaceIndex, Table: h.Table})
	}
	networks, err := inv.ListNetworks(ctx, r.Connection)
	if err != nil {
		return nil, err
	}
	if len(networks) > 4096 {
		return nil, domain.Fail("INVALID_STATE", "network observation limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	xmlBytes := 0
	netIDs := map[string]bool{}
	for _, n := range networks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n.Key.UUID == "" || len(n.Key.UUID) > 128 || netIDs[n.Key.UUID] {
			return nil, domain.Fail("INVALID_STATE", "missing or duplicate network identity")
		}
		netIDs[n.Key.UUID] = true
		if n.Key.UUID == excludeNetwork {
			continue
		}
		if !n.Active && !n.Persistent || n.Active && n.LiveXML == "" || n.Persistent && n.PersistentXML == "" {
			return nil, domain.Fail("INVALID_STATE", "network configuration observation incomplete")
		}
		for _, layer := range []struct{ name, xml string }{{"live", n.LiveXML}, {"persistent", n.PersistentXML}} {
			if layer.xml == "" {
				continue
			}
			xmlBytes += len(layer.xml)
			if xmlBytes > 16<<20 {
				return nil, domain.Fail("INVALID_STATE", "network XML observation limit")
			}
			prefixes, err := network.DefinedPrefixes(layer.xml)
			if err != nil {
				return nil, domain.Fail("INVALID_STATE", "network "+n.Key.UUID+" "+layer.name+": "+err.Error())
			}
			for _, p := range prefixes {
				occupied = append(occupied, prefixConflict{CIDR: p.String(), Source: "network-" + layer.name, ID: n.Key.UUID})
			}
		}
	}
	// Sorting makes attribution/digests stable even when native enumeration order
	// changes. A digest describes this observation; it is never an approval token.
	sort.Slice(occupied, func(i, j int) bool {
		a, b := occupied[i], occupied[j]
		return cmp.Or(cmp.Compare(a.CIDR, b.CIDR), cmp.Compare(a.Source, b.Source), cmp.Compare(a.ID, b.ID), cmp.Compare(a.InterfaceIndex, b.InterfaceIndex), cmp.Compare(a.Table, b.Table)) < 0
	})
	observed, _ := json.Marshal(occupied)
	hash := sha256.Sum256(observed)
	report.ObservedDigest = hex.EncodeToString(hash[:])
	conflicts := 0
	for i, p := range candidates {
		item := cidrCandidate{CIDR: in.Candidates[i], Conflicts: []prefixConflict{}}
		for _, o := range occupied {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			other, _ := netip.ParsePrefix(o.CIDR)
			if p.Overlaps(other) {
				conflicts++
				if conflicts > 8192 {
					return nil, domain.Fail("INVALID_STATE", "CIDR conflict response limit; reduce the candidate set")
				}
				item.Conflicts = append(item.Conflicts, o)
			}
		}
		report.Candidates = append(report.Candidates, item)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report.Warnings = append(report.Warnings, "Read-only observation is not atomic and reserves no CIDR. Recheck immediately before any reviewed network change.", "All host route tables are considered, including inactive policy routes; only route /0 defaults are excluded. Configured network and planned /0 allocations still conflict.", "No packet routing, isolation, passthrough or guest behavior has been verified.")
	return report, nil
}
