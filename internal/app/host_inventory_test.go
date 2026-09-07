package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

type cidrInventoryFixture struct {
	resourceFixture
	networks  []domain.VirtualNetwork
	err       error
	listCalls int
	onList    func(context.Context)
}

func (p *cidrInventoryFixture) ListNetworks(ctx context.Context, uri string) ([]domain.VirtualNetwork, error) {
	p.connection = uri
	p.listCalls++
	if p.onList != nil {
		p.onList(ctx)
	}
	return p.networks, p.err
}

type cidrHostFixture struct {
	inventory domain.HostNetworkPrefixes
	err       error
	calls     int
	onObserve func(context.Context)
}

func (h *cidrHostFixture) observe(ctx context.Context) (domain.HostNetworkPrefixes, error) {
	h.calls++
	if h.onObserve != nil {
		h.onObserve(ctx)
	}
	return h.inventory, h.err
}

func cidrCheckService(t *testing.T) (*Service, *cidrInventoryFixture, *cidrHostFixture) {
	t.Helper()
	s, _, _ := configService(t)
	p, h := &cidrInventoryFixture{}, &cidrHostFixture{}
	s.Provider, s.HostPrefixes = p, h.observe
	t.Cleanup(func() {
		if p.calls != 0 {
			t.Error("read-only CIDR check executed a provider mutation")
		}
		for _, table := range []string{"plans", "jobs", "events", "dedup", "locks", "metadata"} {
			var count int
			if err := s.Engine.Store.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
				t.Errorf("CIDR check changed journal table %s: count=%d, error=%v", table, count, err)
			}
		}
	})
	return s, p, h
}

func cidrCheckRequest(candidates ...string) Request {
	return Request{Connection: "qemu:///system", Input: map[string]any{"candidates": candidates}}
}

func cidrCheckResult(t *testing.T, s *Service, r Request) cidrReport {
	t.Helper()
	response := s.Call(context.Background(), 1000, "network.cidr.check", r)
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	report, ok := response.Data.(cidrReport)
	if !ok {
		t.Fatalf("CIDR method returned unexpected type %T", response.Data)
	}
	digest, err := hex.DecodeString(report.ObservedDigest)
	if err != nil || len(digest) != 32 || report.Reserved || report.IsolationVerified {
		t.Fatalf("invalid digest or unjustified reservation/isolation claim: %#v", report)
	}
	if len(report.Warnings) == 0 {
		t.Fatal("observation scope limitations are missing")
	}
	return report
}

func cidrFixtureNetwork(id, live, persistent string) domain.VirtualNetwork {
	return domain.VirtualNetwork{
		Key:    domain.ResourceKey{ProviderID: "fixture", ConnectionID: "qemu:///system", Kind: "network", UUID: id},
		Active: live != "", Persistent: persistent != "", LiveXML: live, PersistentXML: persistent,
	}
}

func TestCIDRCheckIncludesHostPolicyTablesBothNetworkLayersAndPlanned(t *testing.T) {
	s, p, h := cidrCheckService(t)
	h.inventory = domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{
		{CIDR: "192.0.2.0/24", Source: "address", InterfaceIndex: 2},
		{CIDR: "10.90.0.0/16", Source: "route", InterfaceIndex: 8, Table: 90000},
		{CIDR: "2001:db8:90::/48", Source: "route", InterfaceIndex: 9, Table: 901},
		{CIDR: "0.0.0.0/0", Source: "route", InterfaceIndex: 2, Table: 254},
		{CIDR: "::/0", Source: "route", InterfaceIndex: 2, Table: 254},
	}, Warnings: []string{"fixture host observation warning"}}
	p.networks = []domain.VirtualNetwork{
		cidrFixtureNetwork("network-both", `<network><ip address="10.20.0.1" prefix="24"/></network>`, `<network><ip address="10.21.0.1" netmask="255.255.255.0"/></network>`),
		cidrFixtureNetwork("network-inactive", "", `<network><ip address="10.22.0.1" prefix="24"/></network>`),
		cidrFixtureNetwork("network-transient", `<network><ip family="ipv6" address="2001:db8:30::1" prefix="64"/></network>`, ""),
	}
	r := cidrCheckRequest("192.0.2.128/25", "10.90.3.0/24", "2001:db8:90:1::/64", "10.20.0.0/24", "10.21.0.0/24", "10.22.0.0/24", "2001:db8:30::/64", "10.23.0.0/24", "2001:db8:40::/64", "198.51.100.0/24")
	r.Input["planned"] = []map[string]any{{"id": "lab-v4", "cidr": "10.23.0.0/24"}, {"id": "lab-v6", "cidr": "2001:db8:40::/64"}}
	report := cidrCheckResult(t, s, r)
	want := []prefixConflict{
		{CIDR: "192.0.2.0/24", Source: "address", InterfaceIndex: 2},
		{CIDR: "10.90.0.0/16", Source: "route", InterfaceIndex: 8, Table: 90000},
		{CIDR: "2001:db8:90::/48", Source: "route", InterfaceIndex: 9, Table: 901},
		{CIDR: "10.20.0.0/24", Source: "network-live", ID: "network-both"},
		{CIDR: "10.21.0.0/24", Source: "network-persistent", ID: "network-both"},
		{CIDR: "10.22.0.0/24", Source: "network-persistent", ID: "network-inactive"},
		{CIDR: "2001:db8:30::/64", Source: "network-live", ID: "network-transient"},
		{CIDR: "10.23.0.0/24", Source: "planned", ID: "lab-v4"},
		{CIDR: "2001:db8:40::/64", Source: "planned", ID: "lab-v6"},
	}
	if len(report.Candidates) != len(want)+1 || report.IgnoredDefaultRoutes != 2 {
		t.Fatalf("missing candidates or route-default accounting: %#v", report)
	}
	for i, conflict := range want {
		if !reflect.DeepEqual(report.Candidates[i].Conflicts, []prefixConflict{conflict}) {
			t.Fatalf("candidate %d lost provenance: %#v", i, report.Candidates[i])
		}
	}
	if len(report.Candidates[len(want)].Conflicts) != 0 || report.Warnings[0] != "fixture host observation warning" {
		t.Fatal("default routes blocked an unused candidate or adapter warning was lost")
	}
	if h.calls != 1 || p.listCalls != 1 || p.connection != "qemu:///system" {
		t.Fatal("shared method did not perform one fresh inventory observation")
	}
}

func TestCIDRCheckExcludesOnlyRouteDefaults(t *testing.T) {
	for _, family := range []struct{ name, candidate, all, xml string }{
		{"ipv4", "198.51.100.0/24", "0.0.0.0/0", `<network><ip address="192.0.2.1" prefix="0"/></network>`},
		{"ipv6", "2001:db8:7::/64", "::/0", `<network><ip family="ipv6" address="2001:db8::1" prefix="0"/></network>`},
	} {
		for _, source := range []string{"route", "address", "planned", "network-live", "network-persistent"} {
			t.Run(family.name+"/"+source, func(t *testing.T) {
				s, p, h := cidrCheckService(t)
				r := cidrCheckRequest(family.candidate)
				switch source {
				case "route", "address":
					h.inventory.Prefixes = []domain.HostNetworkPrefix{{CIDR: family.all, Source: source, InterfaceIndex: 2}}
				case "planned":
					r.Input["planned"] = []map[string]any{{"id": "whole-family", "cidr": family.all}}
				case "network-live":
					p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("whole-family", family.xml, "")}
				case "network-persistent":
					p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("whole-family", "", family.xml)}
				}
				report := cidrCheckResult(t, s, r)
				if source == "route" {
					if report.IgnoredDefaultRoutes != 1 || len(report.Candidates[0].Conflicts) != 0 {
						t.Fatal("route default was treated as universal overlap")
					}
				} else if report.IgnoredDefaultRoutes != 0 || len(report.Candidates[0].Conflicts) != 1 || report.Candidates[0].Conflicts[0].Source != source {
					t.Fatal("non-route /0 was incorrectly excluded")
				}
			})
		}
	}
}

func TestCIDRCheckInactiveConfiguredStaticRoutesRemainOccupied(t *testing.T) {
	for _, tc := range []struct {
		name, candidate, destination, xml string
	}{
		{
			"ipv4-static-route", "198.51.100.128/25", "198.51.100.0/24",
			`<network><ip address="192.0.2.1" prefix="24"/><route address="198.51.100.0" netmask="255.255.255.0" gateway="192.0.2.254"/></network>`,
		},
		{
			"ipv6-static-route", "2001:db8:99:1::/64", "2001:db8:99::/48",
			`<network><ip family="ipv6" address="2001:db8:1::1" prefix="64"/><route family="ipv6" address="2001:db8:99::" prefix="48" gateway="2001:db8:1::2"/></network>`,
		},
		{
			"ipv4-configured-default", "198.51.100.0/24", "0.0.0.0/0",
			`<network><ip address="192.0.2.1" prefix="24"/><route address="0.0.0.0" prefix="0" gateway="192.0.2.254"/></network>`,
		},
		{
			"ipv6-configured-default", "2001:db8:99::/64", "::/0",
			`<network><ip family="ipv6" address="2001:db8:1::1" prefix="64"/><route family="ipv6" address="::" prefix="0" gateway="2001:db8:1::2"/></network>`,
		},
		{
			"ipv4-implicit-configured-default", "198.51.100.0/24", "0.0.0.0/0",
			`<network><ip address="192.0.2.1" prefix="24"/><route gateway="192.0.2.254"/></network>`,
		},
		{
			"ipv6-implicit-configured-default", "2001:db8:99::/64", "::/0",
			`<network><ip family="ipv6" address="2001:db8:1::1" prefix="64"/><route family="ipv6" gateway="2001:db8:1::2"/></network>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := cidrCheckService(t)
			p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("inactive-static-route", "", tc.xml)}
			h.inventory.Prefixes = []domain.HostNetworkPrefix{
				{CIDR: "0.0.0.0/0", Source: "route", InterfaceIndex: 2, Table: 254},
				{CIDR: "::/0", Source: "route", InterfaceIndex: 2, Table: 254},
			}
			report := cidrCheckResult(t, s, cidrCheckRequest(tc.candidate))
			want := []prefixConflict{{CIDR: tc.destination, Source: "network-persistent", ID: "inactive-static-route"}}
			if len(report.Candidates) != 1 || !reflect.DeepEqual(report.Candidates[0].Conflicts, want) {
				t.Fatalf("inactive configured route was missed or lost provenance: %#v", report.Candidates)
			}
			if report.IgnoredDefaultRoutes != 2 {
				t.Fatal("host defaults and configured network routes were not distinguished")
			}
		})
	}
}

func TestCIDRCheckRejectsInvalidInputsBeforeObservation(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Request)
	}{
		{"missing", func(r *Request) { r.Input = nil }},
		{"empty", func(r *Request) { r.Input = map[string]any{} }},
		{"no-candidates", func(r *Request) { r.Input["candidates"] = []string{} }},
		{"duplicate-candidates", func(r *Request) { r.Input["candidates"] = []string{"192.0.2.0/24", "192.0.2.0/24"} }},
		{"unknown-key", func(r *Request) { r.Input["allocate"] = true }},
		{"host-bits", func(r *Request) { r.Input["candidates"] = []string{"192.0.2.1/24"} }},
		{"invalid-cidr", func(r *Request) { r.Input["candidates"] = []string{"192.0.2.0/33"} }},
		{"mapped-ipv6", func(r *Request) { r.Input["candidates"] = []string{"::ffff:192.0.2.0/120"} }},
		{"noncanonical-ipv6", func(r *Request) { r.Input["candidates"] = []string{"2001:0db8::/32"} }},
		{"wrong-type", func(r *Request) { r.Input["candidates"] = "192.0.2.0/24" }},
		{"unknown-planned-key", func(r *Request) {
			r.Input["planned"] = []map[string]any{{"id": "lab", "cidr": "192.0.2.0/24", "approved": true}}
		}},
		{"invalid-planned-id", func(r *Request) { r.Input["planned"] = []map[string]any{{"id": "bad\nlabel", "cidr": "192.0.2.0/24"}} }},
		{"missing-planned-cidr", func(r *Request) { r.Input["planned"] = []map[string]any{{"id": "lab"}} }},
		{"planned-host-bits", func(r *Request) { r.Input["planned"] = []map[string]any{{"id": "lab", "cidr": "192.0.2.1/24"}} }},
		{"duplicate-planned-id", func(r *Request) {
			r.Input["planned"] = []map[string]any{{"id": "lab", "cidr": "192.0.2.0/24"}, {"id": "lab", "cidr": "198.51.100.0/24"}}
		}},
		{"id-field", func(r *Request) { r.ID = "unexpected" }},
		{"path-field", func(r *Request) { r.Path = "/tmp/unexpected" }},
		{"action-field", func(r *Request) { r.Action = "allocate" }},
		{"after-field", func(r *Request) { r.After = 1 }},
		{"apply-field", func(r *Request) { r.Apply = &operations.ApplyRequest{} }},
		{"remote-connection", func(r *Request) { r.Connection = "qemu+ssh://unrelated.example/system" }},
		{"unknown-connection", func(r *Request) { r.Connection = "test:///default" }},
		{"connection-query", func(r *Request) { r.Connection = "qemu:///system?socket=/tmp/other" }},
		{"too-many-candidates", func(r *Request) {
			var values []string
			for i := 0; i < 65; i++ {
				values = append(values, fmt.Sprintf("10.0.%d.0/24", i))
			}
			r.Input["candidates"] = values
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := cidrCheckService(t)
			r := cidrCheckRequest("192.0.2.0/24")
			tc.edit(&r)
			response := s.Call(context.Background(), 1000, "network.cidr.check", r)
			if response.Error == nil || response.Data != nil || h.calls != 0 || p.listCalls != 0 {
				t.Fatalf("invalid input produced success or reached adapters: %#v, host=%d, networks=%d", response, h.calls, p.listCalls)
			}
		})
	}
}

func TestCIDRCheckFailsClosedOnMissingCapabilitiesAndIncompleteInventory(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
		edit func(*Service, *cidrInventoryFixture, *cidrHostFixture)
	}{
		{"no-host-adapter", "UNSUPPORTED_CAPABILITY", func(s *Service, _ *cidrInventoryFixture, _ *cidrHostFixture) { s.HostPrefixes = nil }},
		{"no-network-inventory", "UNSUPPORTED_CAPABILITY", func(s *Service, _ *cidrInventoryFixture, _ *cidrHostFixture) { s.Provider = &fixtureProvider{} }},
		{"incomplete-host-dump", "OPERATION_FAILED", func(_ *Service, _ *cidrInventoryFixture, h *cidrHostFixture) {
			h.inventory.Prefixes = []domain.HostNetworkPrefix{{CIDR: "192.0.2.0/24", Source: "address", InterfaceIndex: 2}}
			h.err = errors.New("synthetic interrupted host dump")
		}},
		{"incomplete-network-list", "OPERATION_FAILED", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("partial", "", `<network/>`)}
			p.err = errors.New("synthetic incomplete network enumeration")
		}},
		{"invalid-host-prefix", "INVALID_STATE", func(_ *Service, _ *cidrInventoryFixture, h *cidrHostFixture) {
			h.inventory.Prefixes = []domain.HostNetworkPrefix{{CIDR: "192.0.2.1/24", Source: "address"}}
		}},
		{"unknown-host-source", "INVALID_STATE", func(_ *Service, _ *cidrInventoryFixture, h *cidrHostFixture) {
			h.inventory.Prefixes = []domain.HostNetworkPrefix{{CIDR: "0.0.0.0/0", Source: "unknown"}}
		}},
		{"missing-network-id", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("", "", `<network/>`)}
		}},
		{"duplicate-network-id", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("duplicate", "", `<network/>`), cidrFixtureNetwork("duplicate", "", `<network/>`)}
		}},
		{"missing-live-layer", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			n := cidrFixtureNetwork("incomplete", "", `<network/>`)
			n.Active = true
			p.networks = []domain.VirtualNetwork{n}
		}},
		{"missing-persistent-layer", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			n := cidrFixtureNetwork("incomplete", `<network/>`, "")
			n.Persistent = true
			p.networks = []domain.VirtualNetwork{n}
		}},
		{"no-observed-state", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			p.networks = []domain.VirtualNetwork{{Key: domain.ResourceKey{UUID: "incomplete"}}}
		}},
		{"truncated-network-xml", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("truncated", "", `<network><ip address="192.0.2.1" prefix="24">`)}
		}},
		{"ambiguous-network-prefix", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("ambiguous", "", `<network><ip address="192.0.2.1"/></network>`)}
		}},
		{"host-entry-limit", "INVALID_STATE", func(_ *Service, _ *cidrInventoryFixture, h *cidrHostFixture) {
			h.inventory.Prefixes = make([]domain.HostNetworkPrefix, 65537)
		}},
		{"network-entry-limit", "INVALID_STATE", func(_ *Service, p *cidrInventoryFixture, _ *cidrHostFixture) {
			p.networks = make([]domain.VirtualNetwork, 4097)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := cidrCheckService(t)
			tc.edit(s, p, h)
			response := s.Call(context.Background(), 1000, "network.cidr.check", cidrCheckRequest("192.0.2.0/24"))
			if response.Error == nil || response.Error.Code != tc.code || response.Data != nil {
				t.Fatalf("incomplete observation returned success or wrong failure: %#v", response)
			}
			if tc.name == "incomplete-host-dump" && p.listCalls != 0 {
				t.Fatal("continued network observation after failed host dump")
			}
		})
	}
}

func TestCIDRCheckCancellationCannotReturnEmptySuccess(t *testing.T) {
	for _, phase := range []string{"before-call", "host-observation", "network-observation"} {
		t.Run(phase, func(t *testing.T) {
			s, p, h := cidrCheckService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch phase {
			case "before-call":
				cancel()
			case "host-observation":
				h.onObserve = func(context.Context) { cancel() }
			case "network-observation":
				p.onList = func(context.Context) { cancel() }
			}
			// Adapters deliberately return empty inventories despite cancellation;
			// the shared service must independently refuse a success response.
			response := s.Call(ctx, 1000, "network.cidr.check", cidrCheckRequest("192.0.2.0/24"))
			if response.Error == nil || response.Data != nil || !strings.Contains(response.Error.Message, "canceled") {
				t.Fatalf("canceled observation returned empty success: %#v", response)
			}
			if phase == "before-call" && (h.calls != 0 || p.listCalls != 0) {
				t.Fatal("pre-canceled request reached adapters")
			}
		})
	}
}

func TestCIDRCheckDeterministicAcrossEnumerationOrderAndReobserves(t *testing.T) {
	s, p, h := cidrCheckService(t)
	h.inventory.Prefixes = []domain.HostNetworkPrefix{
		{CIDR: "192.0.2.0/24", Source: "route", InterfaceIndex: 7, Table: 1000},
		{CIDR: "192.0.2.0/24", Source: "address", InterfaceIndex: 2},
		{CIDR: "192.0.2.0/24", Source: "route", InterfaceIndex: 8, Table: 1001},
	}
	p.networks = []domain.VirtualNetwork{
		cidrFixtureNetwork("a", `<network><ip address="192.0.2.1" prefix="24"/></network>`, `<network><ip address="192.0.2.2" prefix="24"/></network>`),
		cidrFixtureNetwork("b", "", `<network><ip address="192.0.2.3" prefix="24"/></network>`),
	}
	r := cidrCheckRequest("192.0.2.0/24", "198.51.100.0/24")
	planned := []map[string]any{{"id": "lab-z", "cidr": "192.0.2.0/24"}, {"id": "lab-a", "cidr": "192.0.2.128/25"}}
	r.Input["planned"] = planned
	first := cidrCheckResult(t, s, r)
	if len(first.Candidates[0].Conflicts) != 8 {
		t.Fatal("overlap provenance was deduplicated across distinct sources/interfaces/tables")
	}
	h.inventory.Prefixes[0], h.inventory.Prefixes[2] = h.inventory.Prefixes[2], h.inventory.Prefixes[0]
	p.networks[0], p.networks[1] = p.networks[1], p.networks[0]
	planned[0], planned[1] = planned[1], planned[0]
	second := cidrCheckResult(t, s, r)
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("enumeration order changed report or observed digest")
	}
	h.inventory.Prefixes = append(h.inventory.Prefixes, domain.HostNetworkPrefix{CIDR: "198.51.100.0/24", Source: "route", Table: 2000})
	third := cidrCheckResult(t, s, r)
	if third.ObservedDigest == second.ObservedDigest || len(third.Candidates[1].Conflicts) != 1 || h.calls != 3 || p.listCalls != 3 {
		t.Fatal("CIDR check cached stale host facts instead of reobserving")
	}
}

func TestCIDRCheckBoundedConflictResponseHasNoPartialReport(t *testing.T) {
	s, _, h := cidrCheckService(t)
	for i := 0; i < 129; i++ {
		h.inventory.Prefixes = append(h.inventory.Prefixes, domain.HostNetworkPrefix{CIDR: "10.0.0.0/8", Source: "route", Table: uint32(500 + i)})
	}
	var candidates []string
	for i := 0; i < 64; i++ {
		candidates = append(candidates, fmt.Sprintf("10.%d.0.0/16", i))
	}
	response := s.Call(context.Background(), 1000, "network.cidr.check", cidrCheckRequest(candidates...))
	if response.Error == nil || response.Error.Code != "INVALID_STATE" || response.Data != nil {
		t.Fatal("excessive conflict set returned a partial report or success")
	}
}

func TestCIDRCheckAllowsOnlyLocalConnectionSelections(t *testing.T) {
	for _, selection := range []string{"", "qemu:///system", "qemu:///session"} {
		t.Run(selection, func(t *testing.T) {
			s, p, _ := cidrCheckService(t)
			r := cidrCheckRequest("192.0.2.0/24")
			r.Connection = selection
			cidrCheckResult(t, s, r)
			want := selection
			if want == "" {
				want = "qemu:///system"
			}
			if p.connection != want {
				t.Fatal("network inventory did not receive the selected local connection")
			}
		})
	}
}
