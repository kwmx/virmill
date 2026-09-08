package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

func protectedNetworkDocument(t *testing.T, kind string, dhcp, cidr bool) string {
	t.Helper()
	host := "services-only"
	if kind == "guest-only" {
		host = "deny"
	}
	path := networkDocument(t, kind, host)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	spec := doc["spec"].(map[string]any)
	if cidr {
		ip := spec["ipv4"].(map[string]any)
		ip["dhcp"] = map[string]any{"enabled": dhcp, "advertiseDefaultRoute": dhcp && kind == "nat"}
	} else {
		delete(spec, "ipv4")
	}
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func protectedNetworkPlan(t *testing.T, s *Service, kind string, dhcp, cidr bool) domain.Plan {
	t.Helper()
	p, err := s.planNetworkCreation(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "create", Path: protectedNetworkDocument(t, kind, dhcp, cidr)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProtectedNetworkProfilesPersistPolicyAndReservations(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		dhcp, cidr bool
	}{
		{"nat-dhcp", "nat", true, true}, {"nat-static", "nat", false, true},
		{"lab-dhcp", "lab", true, true}, {"lab-static", "lab", false, true},
		{"guest-logical", "guest-only", false, true}, {"guest-addressless", "guest-only", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, n, _ := networkService(t)
			p := protectedNetworkPlan(t, s, tc.kind, tc.dhcp, tc.cidr)
			_, raw, err := s.Engine.Store.Plan(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			r, err := parseNetworkRecipe(p, raw)
			if err != nil || r.Version != 2 || p.Steps[1].Action != "network.policy-filter" {
				t.Fatal(r, p.Steps, err)
			}
			review, err := (&networkCreationHandler{s: s}).Review(context.Background(), p, raw)
			if err != nil || review["packetVerification"] != "not-run" || review["guestRoutingVerified"] != false {
				t.Fatal(review, err)
			}
			xml := review["networkXML"].(string)
			if tc.kind == "guest-only" && (strings.Contains(xml, "<ip ") || strings.Contains(xml, "<forward") || strings.Contains(xml, "<dhcp")) {
				t.Fatal(xml)
			}
			j := awaitConfig(t, s, applyConfig(t, s, p).ID)
			if j.State != "succeeded" {
				t.Fatal(j)
			}
			records, err := s.networkRecords()
			if err != nil || len(records) != 1 || records[0].Version != 2 || records[0].Definition != r.Definition {
				t.Fatal(records, err)
			}
			if n.definitions != 1 || n.activations != 1 {
				t.Fatal(n.definitions, n.activations)
			}
			result, err := s.checkCIDRsExcept(context.Background(), Request{Connection: "qemu:///system", Input: map[string]any{"candidates": []string{"10.197.238.0/24"}}}, "")
			if err != nil {
				t.Fatal(err)
			}
			report := result.(cidrReport)
			reserved := false
			for _, c := range report.Candidates[0].Conflicts {
				if c.Source == "application-reservation" {
					reserved = true
				}
			}
			if reserved != tc.cidr {
				t.Fatal("logical reservation differs", report)
			}
		})
	}
}

func TestProtectedNetworkFailureRetainsPolicyAcrossReviewedRecovery(t *testing.T) {
	for _, kind := range []string{"nat", "lab", "guest-only"} {
		t.Run(kind, func(t *testing.T) {
			s, n, _ := networkService(t)
			p := protectedNetworkPlan(t, s, kind, kind != "guest-only", true)
			fw := s.NetworkFirewall.(*networkFirewallFixture)
			fw.applyError = true
			j := awaitConfig(t, s, applyConfig(t, s, p).ID)
			if j.State != "recovery-required" || n.activations != 0 || n.definitions != 1 {
				t.Fatal(j, n.activations, n.definitions)
			}
			fw.applyError = false
			recovery, err := s.planNetworkResume(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "resume", ID: j.ID})
			if err != nil {
				t.Fatal(err)
			}
			_, raw, err := s.Engine.Store.Plan(recovery.ID)
			if err != nil {
				t.Fatal(err)
			}
			r, err := parseNetworkRecipe(recovery, raw)
			if err != nil || r.Version != 2 || recovery.Steps[0].Action != "network.policy-filter" {
				t.Fatal(r, recovery.Steps, err)
			}
			child := awaitConfig(t, s, applyConfig(t, s, recovery).ID)
			if child.State != "succeeded" || child.RecoveryOf != j.ID || n.definitions != 1 || n.activations != 1 {
				t.Fatal(child, n.definitions, n.activations)
			}
		})
	}
}

func TestNetworkRecipeAndReservationVersionsCannotBeDowngraded(t *testing.T) {
	s, _, _ := networkService(t)
	p := protectedNetworkPlan(t, s, "lab", true, true)
	_, raw, err := s.Engine.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var r networkRecipe
	if err = json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	r.Version = 1
	raw, _ = json.Marshal(r)
	if _, err = parseNetworkRecipe(p, raw); err == nil {
		t.Fatal("protected recipe accepted as legacy")
	}
	j := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	old, err := s.Engine.Store.MetadataBytes(networkRecordKind, r.Definition.UUID)
	if err != nil {
		t.Fatal(err)
	}
	var record networkRecord
	if err = json.Unmarshal(old, &record); err != nil {
		t.Fatal(err)
	}
	record.Version = 1
	if err = s.Engine.Store.ComparePut(networkRecordKind, record.Definition.UUID, old, record); err != nil {
		t.Fatal(err)
	}
	if _, err = s.networkRecords(); err == nil {
		t.Fatal("downgraded reservation accepted")
	}
}

func TestLegacyNetworkRecipeAndStepsRemainVersionOne(t *testing.T) {
	s, _, _ := networkService(t)
	p := networkPlan(t, s)
	_, raw, err := s.Engine.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := parseNetworkRecipe(p, raw)
	if err != nil || r.Version != 1 || p.Steps[1].Action != "network.ipv6-filter" {
		t.Fatal(r, p.Steps, err)
	}
	if !strings.Contains(p.Risks[0], "explicitly permits host access") {
		t.Fatal(p.Risks)
	}
	if _, err = operations.PlanDigest(p); err != nil {
		t.Fatal(err)
	}
}
