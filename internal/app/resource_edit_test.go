package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

type configProvider struct {
	mu               sync.Mutex
	vm               domain.VM
	calls            int
	fail             string
	entered, release chan struct{}
}

func (p *configProvider) List(ctx context.Context, uri string) ([]domain.VM, error) {
	v, e := p.Get(ctx, uri, "")
	return []domain.VM{v}, e
}
func (p *configProvider) Get(context.Context, string, string) (domain.VM, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vm, nil
}
func (p *configProvider) Capabilities(context.Context, string) ([]domain.Capability, error) {
	return nil, nil
}
func (p *configProvider) CheckConfiguration(context.Context, string, string, map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail == "preflight" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "fixture preservation unavailable")
	}
	return nil
}
func (p *configProvider) ObserveConfiguration(_ context.Context, _, _ string, input map[string]any) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return xmlpatch.Digest(p.vm.PersistentXML) == input["xmlSHA256"] && p.vm.State == "stopped" && !p.vm.HasManagedSave, nil
}
func (p *configProvider) Execute(ctx context.Context, _, _, action string, input map[string]any) error {
	if p.entered != nil {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if action != "set" {
		return errors.New("unexpected fixture action")
	}
	if p.fail == "define" {
		return errors.New("synthetic define failure")
	}
	edit, err := resourceEdit(input)
	if err != nil {
		return err
	}
	xml, err := xmlpatch.EditResources(p.vm.PersistentXML, edit)
	if err != nil {
		return err
	}
	p.vm.PersistentXML = xml
	h := sha256.Sum256([]byte(p.vm.PersistentXML))
	p.vm.Fingerprint = hex.EncodeToString(h[:])
	if p.fail == "ack" {
		return errors.New("synthetic lost define acknowledgement")
	}
	return nil
}
func configService(t *testing.T) (*Service, *configProvider, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	p := &configProvider{vm: domain.VM{Key: domain.ResourceKey{ProviderID: "fixture", ConnectionID: "fixture", Kind: "vm", UUID: "vm-fixture"}, State: "stopped", Fingerprint: "before", PersistentXML: `<domain><name>fixture</name><memory unit='KiB'>262144</memory><currentMemory unit='KiB'>262144</currentMemory><vcpu>2</vcpu><metadata><x:opaque xmlns:x='urn:fixture' keep='yes'>preserved</x:opaque></metadata></domain>`}}
	s := New(p, engine)
	t.Cleanup(func() { s.Engine.Close(); s.Engine.Store.Close() })
	return s, p, path
}
func planConfig(t *testing.T, s *Service) domain.Plan {
	t.Helper()
	p, err := s.planVM(context.Background(), 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: map[string]any{"vcpus": float64(4), "memoryMiB": float64(512), "applyMode": "next-boot"}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func applyConfig(t *testing.T, s *Service, p domain.Plan) domain.Job {
	t.Helper()
	j, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func awaitConfig(t *testing.T, s *Service, id string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := s.Engine.Store.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		switch j.State {
		case "succeeded", "recovery-required", "failed", "canceled":
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("configuration fixture timeout")
	return domain.Job{}
}
func TestResourcePlanRefusesLiveSavedAdvancedAndSensitiveInputsBeforeJournal(t *testing.T) {
	for _, test := range []string{"running", "saved", "transient", "topology", "preflight", "now", "secret", "fraction", "empty"} {
		t.Run(test, func(t *testing.T) {
			s, p, _ := configService(t)
			input := map[string]any{"vcpus": float64(4), "memoryMiB": float64(512), "applyMode": "next-boot"}
			switch test {
			case "running":
				p.vm.State = "running"
			case "saved":
				p.vm.HasManagedSave = true
			case "transient":
				p.vm.PersistentXML = ""
			case "topology":
				p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "</domain>", `<cpu><topology cores='2' sockets='1' threads='1'/></cpu></domain>`, 1)
			case "preflight":
				p.fail = "preflight"
			case "now":
				input["applyMode"] = "now"
			case "secret":
				input["password"] = "fixture-secret-value"
			case "fraction":
				input["memoryMiB"] = 512.5
			case "empty":
				input = map[string]any{}
			}
			_, err := s.planVM(context.Background(), 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: input})
			if err == nil || strings.Contains(err.Error(), "fixture-secret-value") {
				t.Fatal("unsafe request accepted or leaked", err)
			}
			var count int
			if e := s.Engine.Store.DB.QueryRow("SELECT COUNT(*) FROM plans").Scan(&count); e != nil || count != 0 || p.calls != 0 {
				t.Fatal("rejected request journaled or executed", count, p.calls, e)
			}
		})
	}
}
func TestResourcePlanApplyRechecksDriftAndPreservesOpaqueConfiguration(t *testing.T) {
	s, p, _ := configService(t)
	plan := planConfig(t, s)
	if plan.Review["requiresShutdown"] != true || plan.Review["persistentEdit"] != true || p.calls != 0 {
		t.Fatal(plan)
	}
	p.mu.Lock()
	p.vm.Fingerprint = "external-change"
	p.mu.Unlock()
	if _, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "stale", Acknowledgements: plan.Acknowledgements}); err == nil {
		t.Fatal("stale plan accepted")
	}
	p.mu.Lock()
	p.vm.Fingerprint = "before"
	p.mu.Unlock()
	j := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls != 1 || !strings.Contains(p.vm.PersistentXML, "<memory unit='KiB'>524288</memory>") || !strings.Contains(p.vm.PersistentXML, `<x:opaque xmlns:x='urn:fixture' keep='yes'>preserved</x:opaque>`) {
		t.Fatal("incorrect effect", p.vm, p.calls)
	}
}
func TestResourceLostAcknowledgementRecoversAfterJournalReopenWithoutReplay(t *testing.T) {
	s, p, path := configService(t)
	p.fail = "ack"
	plan := planConfig(t, s)
	j := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	s.Engine.Close()
	if err := s.Engine.Store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(db)
	updated := New(p, s.Engine)
	s.Provider = updated.Provider
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	j, err = s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || j.State != "succeeded" {
		t.Fatal(j, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls != 1 {
		t.Fatal("configuration replayed", p.calls)
	}
	t.Log("synthetic configuration backend and real SQLite reopen only; no native guest evidence")
}
func TestResourceFailureKeepsUncertaintyAndCancellationDoesNotUndoACompletedDefine(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		s, p, _ := configService(t)
		p.fail = "define"
		j := awaitConfig(t, s, applyConfig(t, s, planConfig(t, s)).ID)
		if j.State != "recovery-required" {
			t.Fatal(j)
		}
		if r, err := s.Engine.Reconcile(context.Background(), j.ID); err == nil && r.State == "succeeded" {
			t.Fatal("missing effect fabricated")
		}
	})
	t.Run("cancel-in-flight", func(t *testing.T) {
		s, p, _ := configService(t)
		p.entered = make(chan struct{})
		p.release = make(chan struct{})
		j := applyConfig(t, s, planConfig(t, s))
		select {
		case <-p.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("not executing")
		}
		if _, err := s.Engine.Cancel(j.ID); err != nil {
			t.Fatal(err)
		}
		close(p.release)
		j = awaitConfig(t, s, j.ID)
		if j.State != "succeeded" || !j.CancelRequested {
			t.Fatal("completed effect misreported as undone", j)
		}
	})
}

func TestResourceOperationCompatibilityFailsClosed(t *testing.T) {
	s, p, _ := configService(t)
	plan := planConfig(t, s)
	if plan.Operation != "vm.configure-resources" {
		t.Fatal("new contract uses legacy operation", plan.Operation)
	}
	_, input, err := s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy := plan
	legacy.Operation = "vm.set"
	h := s.Engine.Handlers["vm.set"]
	if err = h.Validate(context.Background(), legacy, input); err == nil {
		t.Fatal("legacy unsafe edit accepted")
	}
	if ok, err := h.Reconcile(context.Background(), legacy, input, legacy.Steps[0]); err == nil || ok {
		t.Fatal("legacy configuration certified", ok, err)
	}
	// Simulate the old handler registry: an existing newer plan cannot be applied
	// merely because SQLite schema 3 itself remains readable by the older program.
	delete(s.Engine.Handlers, "vm.configure-resources")
	if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "old-registry", Acknowledgements: plan.Acknowledgements}); err == nil {
		t.Fatal("old registry executed new operation")
	}
	if p.calls != 0 {
		t.Fatal("compatibility refusal had effect")
	}
}

func TestResourcePlanStoresHashesWithoutOpaqueXMLValues(t *testing.T) {
	s, p, _ := configService(t)
	p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "preserved", "opaque-credential-fixture-sentinel", 1)
	plan := planConfig(t, s)
	_, input, err := s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(input), "opaque-credential-fixture-sentinel") || strings.Contains(string(input), "<domain>") || !strings.Contains(string(input), "xmlSHA256") {
		t.Fatal("opaque XML value journaled", string(input))
	}
	j := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !strings.Contains(p.vm.PersistentXML, "opaque-credential-fixture-sentinel") {
		t.Fatal("opaque value lost")
	}
}
