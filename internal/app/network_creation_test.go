package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

type networkCreationFixture struct {
	resourceFixture
	mu                       sync.Mutex
	defined                  *domain.NetworkDefinition
	active                   bool
	definitions, activations int
	fault                    string
}

type networkFirewallFixture struct {
	mu           sync.Mutex
	present      bool
	applyError   bool
	applications int
}

func (f *networkFirewallFixture) Check(ctx context.Context, _ domain.Plan, _ domain.NetworkDefinition) error {
	return ctx.Err()
}
func (f *networkFirewallFixture) Apply(ctx context.Context, _ domain.Plan, _ domain.NetworkDefinition, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applications++
	if f.applyError {
		return errors.New("generated firewall partial failure")
	}
	f.present = true
	return ctx.Err()
}
func (f *networkFirewallFixture) Observe(ctx context.Context, _ domain.Plan, _ domain.NetworkDefinition, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.present {
		return errors.New("generated missing runtime rule")
	}
	return ctx.Err()
}

func (p *networkCreationFixture) observed(uri string) domain.VirtualNetwork {
	d := *p.defined
	xml, _ := networkxml.Render(d)
	n := domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "network", UUID: d.UUID}, Name: d.Name, Persistent: true, PersistentXML: xml, Active: p.active, Fingerprint: "fixture", IsolationVerification: "not-run"}
	if n.Active {
		n.LiveXML = xml
	}
	return n
}
func (p *networkCreationFixture) ListNetworks(_ context.Context, uri string) ([]domain.VirtualNetwork, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.defined == nil {
		return []domain.VirtualNetwork{}, nil
	}
	return []domain.VirtualNetwork{p.observed(uri)}, nil
}
func (p *networkCreationFixture) CheckNetworkCreation(ctx context.Context, _ string, _ domain.NetworkDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.defined != nil {
		return errors.New("collision")
	}
	return ctx.Err()
}
func (p *networkCreationFixture) DefineNetwork(ctx context.Context, _ string, d domain.NetworkDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.definitions++
	if p.defined != nil {
		return errors.New("overwrite")
	}
	if p.fault == "missing-definition" {
		return errors.New("before definition")
	}
	p.defined = &d
	if p.fault == "lost-define" {
		return errors.New("lost define acknowledgement")
	}
	return ctx.Err()
}
func (p *networkCreationFixture) ActivateNetwork(ctx context.Context, _ string, d domain.NetworkDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activations++
	if p.defined == nil || *p.defined != d || p.active {
		return errors.New("different definition or replay")
	}
	p.active = true
	if p.fault == "lost-activate" {
		return errors.New("lost activate acknowledgement")
	}
	return ctx.Err()
}
func (p *networkCreationFixture) InspectCreatedNetwork(ctx context.Context, uri string, d domain.NetworkDefinition) (domain.VirtualNetwork, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.defined == nil || *p.defined != d {
		return domain.VirtualNetwork{}, errors.New("absent or drifted definition")
	}
	return p.observed(uri), ctx.Err()
}
func networkService(t *testing.T) (*Service, *networkCreationFixture, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	provider := &networkCreationFixture{}
	s := New(provider, operations.New(db))
	s.NetworkFirewall = &networkFirewallFixture{}
	s.HostPrefixes = func(context.Context) (domain.HostNetworkPrefixes, error) {
		return domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{{CIDR: "0.0.0.0/0", Source: "route"}}}, nil
	}
	t.Cleanup(func() { s.Engine.Close(); s.Engine.Store.Close() })
	return s, provider, path
}
func networkDocument(t *testing.T, kind, host string) string {
	t.Helper()
	d := map[string]any{"apiVersion": "virmill/v1", "kind": "Network", "metadata": map[string]any{"name": "fixture-lab", "displayName": "Fixture network", "tags": []string{"test"}}, "spec": map[string]any{"type": kind, "ipv4": map[string]any{"cidr": "10.197.238.0/24", "dhcp": map[string]any{"enabled": true, "advertiseDefaultRoute": kind == "nat"}}, "ipv6": map[string]any{"mode": "disabled"}, "hostAccess": host, "egress": "none"}}
	if kind == "nat" {
		d["spec"].(map[string]any)["egress"] = "any"
	}
	raw, _ := json.Marshal(d)
	path := filepath.Join(t.TempDir(), "network.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func networkPlan(t *testing.T, s *Service) domain.Plan {
	t.Helper()
	p, e := s.planNetworkCreation(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "create", Path: networkDocument(t, "lab", "allow")})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestNetworkCreationDurableAllocationAndDedup(t *testing.T) {
	s, n, _ := networkService(t)
	p := networkPlan(t, s)
	raw, _ := json.Marshal(p)
	if err := validation.Schema("operation-plan", raw); err != nil {
		t.Fatal(err)
	}
	req := operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "network-once", Acknowledgements: p.Acknowledgements}
	j, err := s.Engine.Apply(context.Background(), 1000, req)
	if err != nil {
		t.Fatal(err)
	}
	j = awaitConfig(t, s, j.ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	again, err := s.Engine.Apply(context.Background(), 1000, req)
	if err != nil || again.ID != j.ID {
		t.Fatal(again, err)
	}
	n.mu.Lock()
	if n.definitions != 1 || n.activations != 1 {
		t.Fatal("replayed effects")
	}
	n.mu.Unlock()
	report := cidrCheckResult(t, s, cidrCheckRequest("10.197.238.0/24"))
	found := false
	for _, c := range report.Candidates[0].Conflicts {
		found = found || c.Source == "application-reservation"
	}
	if !found {
		t.Fatal("durable subnet reservation absent", report)
	}
	out, err := s.networkCreationResult(context.Background(), 1000, Request{Connection: "qemu:///system", ID: j.ID})
	if err != nil || out.(map[string]any)["packetVerification"] != "not-run" {
		t.Fatal(out, err)
	}
}
func TestNetworkLostActivationReconcilesWithoutReplayAfterReopen(t *testing.T) {
	s, n, path := networkService(t)
	n.fault = "lost-activate"
	p := networkPlan(t, s)
	j := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if j.State != "recovery-required" || j.Step != 2 {
		t.Fatal(j)
	}
	s.Engine.Close()
	s.Engine.Store.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(db)
	s.Engine.Handlers["network.create"] = &networkCreationHandler{s: s}
	j, err = s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || j.State != "succeeded" {
		t.Fatal(j, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.activations != 1 || n.definitions != 1 {
		t.Fatal("recovery replayed")
	}
}
func TestNetworkLostDefinitionRequiresReviewedActivationWithInheritedLocks(t *testing.T) {
	s, n, _ := networkService(t)
	n.fault = "lost-define"
	p := networkPlan(t, s)
	j := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if j.State != "recovery-required" || j.Step != 0 {
		t.Fatal(j)
	}
	if _, err := s.Engine.Reconcile(context.Background(), j.ID); err == nil {
		t.Fatal("incomplete creation marked success")
	}
	recovered, err := s.planNetworkResume(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "resume", ID: j.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(recovered)
	if err = validation.Schema("operation-plan", raw); err != nil {
		t.Fatal(err)
	}
	child := awaitConfig(t, s, applyConfig(t, s, recovered).ID)
	if child.State != "succeeded" || child.RecoveryOf != j.ID {
		t.Fatal(child)
	}
	prior, err := s.Engine.Store.Job(j.ID)
	if err != nil || prior.State != "partial" || prior.RecoveryOperationID != child.ID {
		t.Fatal(prior, err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.definitions != 1 || n.activations != 1 {
		t.Fatal("resume redefined", n.definitions, n.activations)
	}
}
func TestNetworkMissingDefinitionAndDriftKeepReservation(t *testing.T) {
	for _, fault := range []string{"missing-definition", "lost-define"} {
		t.Run(fault, func(t *testing.T) {
			s, n, _ := networkService(t)
			n.fault = fault
			p := networkPlan(t, s)
			j := awaitConfig(t, s, applyConfig(t, s, p).ID)
			if j.State != "recovery-required" {
				t.Fatal(j)
			}
			n.mu.Lock()
			if n.defined != nil {
				n.defined.IPv4CIDR = "10.197.237.0/24"
			}
			n.mu.Unlock()
			if _, err := s.planNetworkResume(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "resume", ID: j.ID}); err == nil {
				t.Fatal("unsafe recovery permitted")
			}
			records, err := s.networkRecords()
			if err != nil || len(records) != 1 {
				t.Fatal("reservation lost", records, err)
			}
			var locks int
			if err = s.Engine.Store.DB.QueryRow("SELECT count(*) FROM locks WHERE job_id=?", j.ID).Scan(&locks); err != nil || locks != 2 {
				t.Fatal("locks lost", locks, err)
			}
		})
	}
}
func TestNetworkApplyRechecksNewVPNConflictAndAcknowledgements(t *testing.T) {
	s, n, _ := networkService(t)
	p := networkPlan(t, s)
	if _, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "without-ack"}); err == nil {
		t.Fatal("missing acks accepted")
	}
	s.HostPrefixes = func(context.Context) (domain.HostNetworkPrefixes, error) {
		return domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{{CIDR: "10.197.0.0/16", Source: "route", Table: 901}}}, nil
	}
	if _, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "stale-vpn", Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("new VPN overlap ignored")
	}
	if n.definitions != 0 {
		t.Fatal("mutated before refusing")
	}
}
func TestNetworkRestrictedIntentIsNotWeakened(t *testing.T) {
	for _, host := range []string{"services-only", "deny"} {
		t.Run(host, func(t *testing.T) {
			s, n, _ := networkService(t)
			_, err := s.planNetworkCreation(context.Background(), 1000, Request{Connection: "qemu:///system", Action: "create", Path: networkDocument(t, "lab", host)})
			if err == nil {
				t.Fatal("unsupported isolation weakened")
			}
			if n.definitions != 0 {
				t.Fatal("defined unsupported network")
			}
			var count int
			if e := s.Engine.Store.DB.QueryRow("SELECT count(*) FROM plans").Scan(&count); e != nil || count != 0 {
				t.Fatal(count, e)
			}
		})
	}
}
func TestNetworkReservationCorruptionFailsClosed(t *testing.T) {
	s, _, _ := networkService(t)
	if err := s.Engine.Store.ComparePut(networkRecordKind, "corrupt", nil, map[string]any{"version": 99}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.checkCIDRs(context.Background(), cidrCheckRequest("10.1.2.0/24")); err == nil {
		t.Fatal("corrupt reservation ignored")
	}
}
