package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

func autoNetworkDocument(t *testing.T, kind string) string {
	t.Helper()
	path := protectedNetworkDocument(t, kind, kind != "guest-only", true)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "10.197.238.0/24", "auto", 1))
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func autoNetworkPlan(t *testing.T, s *Service, kind string) (domain.Plan, networkRecipe) {
	t.Helper()
	p, err := s.planNetworkCreation(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "create", Path: autoNetworkDocument(t, kind)})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := s.Engine.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := parseNetworkRecipe(p, raw)
	if err != nil {
		t.Fatal(err)
	}
	return p, r
}

func TestAutomaticNetworkUsesHostNativeAndPlannedOccupancy(t *testing.T) {
	s, p, h := cidrCheckService(t)
	s.NetworkAllocation = func(context.Context) (network.AllocationConfig, error) {
		return network.AllocationConfig{Version: 1, Ranges: []network.AllocationRange{{CIDR: "10.88.0.0/16", PrefixLength: 24}}, Planned: []network.PlannedAllocation{{ID: "future-lab", CIDR: "10.88.4.0/24"}}}, nil
	}
	h.inventory.Prefixes = []domain.HostNetworkPrefix{{CIDR: "0.0.0.0/0", Source: "route"}, {CIDR: "10.88.0.0/24", Source: "address"}, {CIDR: "10.88.1.0/24", Source: "route", Table: 901}}
	p.networks = []domain.VirtualNetwork{cidrFixtureNetwork("both", `<network><ip address="10.88.2.1" prefix="24"/></network>`, `<network><ip address="10.88.3.1" prefix="24"/></network>`)}
	cidr, review, err := s.allocateNetworkCIDR(context.Background(), "qemu:///system")
	if err != nil || cidr != "10.88.5.0/24" || review == nil || review.RequestedCIDR != "auto" || len(review.ObservedDigest) != 64 || p.listCalls != 1 || h.calls != 1 {
		t.Fatal(cidr, review, err, p.listCalls, h.calls)
	}
}

func TestAutomaticNetworkProfilesFreezeSelectionAndReserveAtExecution(t *testing.T) {
	for _, kind := range []string{"nat", "lab", "guest-only"} {
		t.Run(kind, func(t *testing.T) {
			s, n, _ := networkService(t)
			p, r := autoNetworkPlan(t, s, kind)
			if r.Definition.IPv4CIDR != "10.0.0.0/24" || r.Allocation == nil {
				t.Fatal(r)
			}
			records, err := s.networkRecords()
			if err != nil || len(records) != 0 {
				t.Fatal("planning reserved subnet", records, err)
			}
			s.NetworkAllocation = func(context.Context) (network.AllocationConfig, error) {
				return network.AllocationConfig{Version: 1, Ranges: []network.AllocationRange{{CIDR: "172.16.0.0/12", PrefixLength: 26}}}, nil
			}
			job := awaitConfig(t, s, applyConfig(t, s, p).ID)
			if job.State != "succeeded" || n.defined == nil || *n.defined != r.Definition {
				t.Fatal(job, n.defined)
			}
			records, err = s.networkRecords()
			if err != nil || len(records) != 1 || records[0].Definition != r.Definition {
				t.Fatal(records, err)
			}
			result, err := s.networkCreationResult(context.Background(), 1000, Request{Connection: "qemu:///system", ID: job.ID})
			if err != nil || result.(map[string]any)["subnetReserved"] != true || !reflect.DeepEqual(result.(map[string]any)["allocation"], r.Allocation) {
				t.Fatal(result, err)
			}
			// Another selection observes the existing guest-only logical reservation too.
			s.NetworkAllocation = nil
			next, _, err := s.allocateNetworkCIDR(context.Background(), "qemu:///system")
			if err != nil || next != "10.0.1.0/24" {
				t.Fatal(next, err)
			}
		})
	}
}

func TestAutomaticNetworkNewConflictRefusesWithoutReallocation(t *testing.T) {
	for _, source := range []string{"route", "planned", "invalid-settings"} {
		t.Run(source, func(t *testing.T) {
			s, n, _ := networkService(t)
			p, r := autoNetworkPlan(t, s, "lab")
			if source == "route" {
				s.HostPrefixes = func(context.Context) (domain.HostNetworkPrefixes, error) {
					return domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{{CIDR: r.Definition.IPv4CIDR, Source: "route", Table: 999}}}, nil
				}
			} else {
				s.NetworkAllocation = func(context.Context) (network.AllocationConfig, error) {
					c := network.DefaultAllocationConfig()
					if source == "invalid-settings" {
						c.Version = 2
					} else {
						c.Planned = []network.PlannedAllocation{{ID: "new-lab", CIDR: r.Definition.IPv4CIDR}}
					}
					return c, nil
				}
			}
			_, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "stale-auto", Acknowledgements: p.Acknowledgements})
			if err == nil || n.definitions != 0 || n.activations != 0 {
				t.Fatal("stale selection mutated", err, n.definitions)
			}
			records, e := s.networkRecords()
			if e != nil || len(records) != 0 {
				t.Fatal(records, e)
			}
		})
	}
}

func TestAutomaticNetworkRecoveryRetainsSelectionAfterJournalReopen(t *testing.T) {
	s, n, path := networkService(t)
	n.fault = "lost-define"
	p, r := autoNetworkPlan(t, s, "guest-only")
	job := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if job.State != "recovery-required" {
		t.Fatal(job)
	}
	s.Engine.Close()
	s.Engine.Store.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(db)
	s.Engine.Handlers["network.create"] = &networkCreationHandler{s: s}
	s.Engine.Handlers["network.creation.resume"] = &networkResumeHandler{networkCreationHandler{s: s}}
	next, err := s.planNetworkResume(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "resume", ID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, _ := s.Engine.Store.Plan(next.ID)
	resumed, err := parseNetworkRecipe(next, raw)
	if err != nil || !reflect.DeepEqual(r.Allocation, resumed.Allocation) || r.Definition != resumed.Definition {
		t.Fatal(resumed, err)
	}
	done := awaitConfig(t, s, applyConfig(t, s, next).ID)
	if done.State != "succeeded" || n.definitions != 1 || n.activations != 1 {
		t.Fatal(done, n.definitions, n.activations)
	}
}

func TestAutomaticNetworkMissingForeignLogicalReservationFailsClosed(t *testing.T) {
	for _, renamed := range []bool{false, true} {
		t.Run(map[bool]string{false: "name", true: "marker"}[renamed], func(t *testing.T) {
			s, p, _ := cidrCheckService(t)
			d := domain.NetworkDefinition{UUID: "d05031cb-3d7b-4b3d-a54e-d43720a0ca4a", Name: "virmill-d05031cb-3d7b-4b3d-a54e-d43720a0ca4a", Bridge: "vmd05031cb3d7b", Type: "guest-only", IPv4CIDR: "10.0.0.0/24", IPv6Mode: "disabled", HostAccess: "deny", Egress: "none"}
			raw, err := networkxml.Render(d)
			if err != nil {
				t.Fatal(err)
			}
			n := cidrFixtureNetwork(d.UUID, "", raw)
			n.Name = d.Name
			if renamed {
				n.Name = "foreign-name"
			}
			p.networks = []domain.VirtualNetwork{n}
			got, review, err := s.allocateNetworkCIDR(context.Background(), "qemu:///system")
			var de *domain.Error
			if !errors.As(err, &de) || de.Code != "UNRESOLVED_ALLOCATION" || got != "" || review != nil {
				t.Fatal(got, review, err)
			}
		})
	}
}

func TestAutomaticNetworkCancellationAndInvalidReview(t *testing.T) {
	s, _, _ := networkService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, review, err := s.allocateNetworkCIDR(ctx, "qemu:///system"); !errors.Is(err, context.Canceled) || got != "" || review != nil {
		t.Fatal(got, review, err)
	}
	_, r := autoNetworkPlan(t, s, "lab")
	for _, mutate := range []func(*networkRecipe){func(r *networkRecipe) { r.Allocation.Version = 2 }, func(r *networkRecipe) { r.Allocation.RequestedCIDR = "manual" }, func(r *networkRecipe) { r.Allocation.ObservedDigest = "bad" }, func(r *networkRecipe) { r.Definition.IPv4CIDR = "172.20.0.0/25" }, func(r *networkRecipe) {
		r.Allocation.Config.Planned = []network.PlannedAllocation{{ID: "conflict", CIDR: r.Definition.IPv4CIDR}}
	}} {
		raw, _ := json.Marshal(r)
		var changed networkRecipe
		json.Unmarshal(raw, &changed)
		mutate(&changed)
		if validateNetworkAllocation(changed) == nil {
			t.Fatal("corrupt selection accepted", changed)
		}
	}
}

func TestAutomaticForeignAllocationRequiresExactProfileOrPlannedUUID(t *testing.T) {
	for _, tc := range []struct {
		name, kind, cidr, plannedID, plannedCIDR, drift string
		success                                         bool
	}{
		{name: "empty guest", kind: "guest-only", success: true},
		{name: "declared guest", kind: "guest-only", cidr: "10.0.0.0/24", plannedID: "uuid", plannedCIDR: "10.0.0.0/24", success: true},
		{name: "wrong ID", kind: "guest-only", cidr: "10.0.0.0/24", plannedID: "different", plannedCIDR: "10.0.0.0/24"},
		{name: "wrong subnet", kind: "guest-only", cidr: "10.0.0.0/24", plannedID: "uuid", plannedCIDR: "10.1.0.0/24"},
		{name: "hidden by route", kind: "guest-only", cidr: "10.0.0.0/24", drift: `<route address="172.20.0.0" prefix="16" gateway="172.20.0.1"/>`},
		{name: "hidden by IP", kind: "guest-only", cidr: "10.0.0.0/24", drift: `<ip address="172.20.0.1" prefix="24"/>`},
		{name: "visible lab", kind: "lab", cidr: "10.0.0.0/24", success: true},
		{name: "visible NAT", kind: "nat", cidr: "10.0.0.0/24", success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, _ := cidrCheckService(t)
			d := domain.NetworkDefinition{UUID: "d05031cb-3d7b-4b3d-a54e-d43720a0ca4a", Name: "virmill-d05031cb-3d7b-4b3d-a54e-d43720a0ca4a", Bridge: "vmd05031cb3d7b", Type: tc.kind, IPv4CIDR: tc.cidr, IPv6Mode: "disabled", HostAccess: "deny", Egress: "none"}
			if tc.kind != "guest-only" {
				d.HostAccess = "services-only"
				d.DHCPEnabled = true
			}
			if tc.kind == "nat" {
				d.Egress = "any"
				d.AdvertiseDefaultRoute = true
			}
			raw, err := networkxml.Render(d)
			if err != nil {
				t.Fatal(err)
			}
			raw = strings.Replace(raw, "</network>", tc.drift+"</network>", 1)
			n := cidrFixtureNetwork(d.UUID, raw, raw)
			n.Name = d.Name
			p.networks = []domain.VirtualNetwork{n}
			if tc.plannedID != "" {
				s.NetworkAllocation = func(context.Context) (network.AllocationConfig, error) {
					c := network.DefaultAllocationConfig()
					id := tc.plannedID
					if id == "uuid" {
						id = d.UUID
					}
					c.Planned = []network.PlannedAllocation{{ID: id, CIDR: tc.plannedCIDR}}
					return c, nil
				}
			}
			got, review, err := s.allocateNetworkCIDR(context.Background(), "qemu:///system")
			if tc.success {
				want := "10.0.1.0/24"
				if tc.cidr == "" {
					want = "10.0.0.0/24"
				}
				if err != nil || got != want || review == nil {
					t.Fatal(got, review, err)
				}
			} else if err == nil || got != "" || review != nil {
				t.Fatal("foreign intent mismatch accepted", got, review, err)
			}
		})
	}
}
