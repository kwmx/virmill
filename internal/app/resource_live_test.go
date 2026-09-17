package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

// liveProvider is a running VM whose live definition Virmill can read, with a
// balloon and spare CPU slots, and a synthetic live writer.
type liveProvider struct {
	configProvider
	writes   int
	refuse   string
	partial  bool
	writeErr error
	wmu      sync.Mutex
}

func (p *liveProvider) SetLiveResources(_ context.Context, _, _ string, input map[string]any) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	p.writes++
	if p.refuse != "" {
		return domain.Fail(p.refuse, "synthetic live refusal")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if vcpus, ok := input["vcpus"].(float64); ok {
		p.vm.LiveXML = strings.Replace(p.vm.LiveXML, "current='2'", "current='"+itoa(uint64(vcpus))+"'", 1)
	}
	if p.partial {
		return p.writeErr
	}
	if memory, ok := input["memoryMiB"].(float64); ok {
		p.vm.LiveXML = strings.Replace(p.vm.LiveXML, "<currentMemory unit='KiB'>1048576</currentMemory>",
			"<currentMemory unit='KiB'>"+itoa(uint64(memory)<<10)+"</currentMemory>", 1)
	}
	return p.writeErr
}
func (p *liveProvider) ObserveLiveResources(_ context.Context, _, _ string, input map[string]any) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now, err := readLiveResources(p.vm)
	if err != nil {
		return false, err
	}
	if vcpus, ok := input["vcpus"].(float64); ok && uint64(vcpus) != now.VCPUs {
		return false, nil
	}
	if memory, ok := input["memoryMiB"].(float64); ok && uint64(memory)<<20 != now.MemoryBytes {
		return false, nil
	}
	return true, nil
}
func itoa(n uint64) string {
	out := ""
	for n > 0 {
		out, n = string(rune('0'+n%10))+out, n/10
	}
	if out == "" {
		out = "0"
	}
	return out
}

// liveRunningXML is a running domain with 2 of 4 CPUs plugged in, 1 GiB of 2 GiB
// in use, and a virtio balloon.
func liveRunningXML() string {
	return `<domain type='kvm'><name>fixture</name><memory unit='KiB'>2097152</memory><currentMemory unit='KiB'>1048576</currentMemory>` +
		`<vcpu placement='static' current='2'>4</vcpu><devices><memballoon model='virtio'/></devices></domain>`
}

func liveService(t *testing.T) (*Service, *liveProvider) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dir + "/journal.db")
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	p := &liveProvider{configProvider: configProvider{vm: domain.VM{
		Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///session", Kind: "vm", UUID: "11111111-2222-4333-8444-555555555555"},
		Name: "fixture", State: "running", Fingerprint: "before",
		PersistentXML: `<domain type='kvm'><name>fixture</name><memory unit='KiB'>2097152</memory><currentMemory unit='KiB'>2097152</currentMemory><vcpu placement='static'>4</vcpu><devices><memballoon model='virtio'/></devices></domain>`,
		LiveXML:       liveRunningXML()}}}
	s := New(p, engine)
	t.Cleanup(func() { s.Engine.Close(); s.Engine.Store.Close() })
	return s, p
}

func planLive(t *testing.T, s *Service, p *liveProvider, input map[string]any) (domain.Plan, error) {
	t.Helper()
	input["applyMode"] = "now"
	return s.planVM(context.Background(), 1000, Request{Connection: "qemu:///session", ID: p.vm.Key.UUID, Action: "set", Input: input})
}

func TestLiveResourceChangeReviewsRunningValuesOnly(t *testing.T) {
	s, p := liveService(t)
	plan, err := planLive(t, s, p, map[string]any{"vcpus": float64(4), "memoryMiB": float64(1536)})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Operation != liveResourceOperation || len(plan.Steps) != 1 {
		t.Fatal("unexpected live operation", plan.Operation, plan.Steps)
	}
	running, _ := plan.Review["running"].(map[string]any)
	after, _ := plan.Review["afterChange"].(map[string]any)
	if running["vcpus"] != float64(2) || after["vcpus"] != float64(4) || running["memoryBytes"] != float64(1<<30) || after["memoryBytes"] != float64(1536<<20) {
		t.Fatal("the review does not show the running and requested values", plan.Review)
	}
	if plan.Review["savedDefinitionUnchanged"] != true || plan.Review["nextBootUnchanged"] != true || plan.Review["memoryBalloon"] != "virtio" {
		t.Fatal("the review does not say the saved definition is untouched", plan.Review)
	}
	if plan.Estimates.RequiresDowntime {
		t.Fatal("a live change claimed downtime", plan.Estimates)
	}
	for _, unwanted := range []string{"guest-resource-pressure"} {
		for _, ack := range plan.Acknowledgements {
			if ack == unwanted {
				t.Fatal("an acknowledgement for taking resources away on a change that adds them")
			}
		}
	}
	// A reviewed live change applies once and leaves the saved definition alone.
	before := p.vm.PersistentXML
	job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	if p.writes != 1 || p.vm.PersistentXML != before {
		t.Fatal("unexpected effect", p.writes, p.vm.PersistentXML)
	}
	now, err := readLiveResources(p.vm)
	if err != nil || now.VCPUs != 4 || now.MemoryBytes != 1536<<20 {
		t.Fatal("the running VM does not hold the reviewed values", now, err)
	}
}

func TestLiveResourceChangeRefusesWhatTheRunningVMCannotDo(t *testing.T) {
	for _, test := range []struct{ name, contains string }{
		{"no-slots", "spare CPU slots"},
		{"above-maximum", "at most"},
		{"same-cpu", "already running"},
		{"no-balloon", "no memory balloon"},
		{"too-small", "at least"},
		{"above-memory-maximum", "at most"},
		{"stopped", "next boot instead"},
		{"paused", "Resume it first"},
		{"saved-state", "saved state"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, p := liveService(t)
			input := map[string]any{}
			switch test.name {
			case "no-slots":
				p.vm.LiveXML = strings.Replace(liveRunningXML(), `<vcpu placement='static' current='2'>4</vcpu>`, `<vcpu placement='static'>2</vcpu>`, 1)
				input["vcpus"] = float64(4)
			case "above-maximum":
				input["vcpus"] = float64(8)
			case "same-cpu":
				input["vcpus"] = float64(2)
			case "no-balloon":
				p.vm.LiveXML = strings.Replace(liveRunningXML(), `<memballoon model='virtio'/>`, `<memballoon model='none'/>`, 1)
				input["memoryMiB"] = float64(1536)
			case "too-small":
				input["memoryMiB"] = float64(128)
			case "above-memory-maximum":
				input["memoryMiB"] = float64(4096)
			case "stopped":
				p.vm.State = "stopped"
				input["memoryMiB"] = float64(1536)
			case "paused":
				p.vm.State = "paused"
				input["memoryMiB"] = float64(1536)
			case "saved-state":
				p.vm.HasManagedSave = true
				input["memoryMiB"] = float64(1536)
			}
			_, err := planLive(t, s, p, input)
			if err == nil {
				t.Fatal("an impossible live change was planned")
			}
			if !strings.Contains(err.Error(), test.contains) {
				t.Fatal("the refusal does not say what to change:", err)
			}
			if p.writes != 0 {
				t.Fatal("a refused plan wrote to the VM")
			}
		})
	}
}

// A refusal that never reached the guest ends the job plainly and frees the VM,
// instead of asking for a recovery decision about nothing.
func TestLiveRefusalBeforeAnyEffectFailsPlainly(t *testing.T) {
	s, p := liveService(t)
	plan, err := planLive(t, s, p, map[string]any{"vcpus": float64(3)})
	if err != nil {
		t.Fatal(err)
	}
	p.refuse = "UNSUPPORTED_CAPABILITY"
	job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if job.State != "failed" {
		t.Fatal("an untouched VM was left needing recovery", job.State)
	}
	if now, err := readLiveResources(p.vm); err != nil || now.VCPUs != 2 {
		t.Fatal("the VM changed", now, err)
	}
}

// Half of a change that did happen keeps its uncertainty, and reconciliation
// observes it instead of replaying anything.
func TestPartialLiveChangeNeedsRecoveryAndIsReconciledByObservation(t *testing.T) {
	s, p := liveService(t)
	plan, err := planLive(t, s, p, map[string]any{"vcpus": float64(4), "memoryMiB": float64(1536)})
	if err != nil {
		t.Fatal(err)
	}
	p.partial, p.writeErr = true, domain.Fail("OPERATION_FAILED", "synthetic memory failure")
	job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if job.State != "recovery-required" {
		t.Fatal("a partial live change did not keep its uncertainty", job.State)
	}
	if now, err := readLiveResources(p.vm); err != nil || now.VCPUs != 4 || now.MemoryBytes != 1<<30 {
		t.Fatal("unexpected partial effect", now, err)
	}
	if p.writes != 1 {
		t.Fatal("the change was replayed", p.writes)
	}
}

func TestLiveResourceViewOffersOnlyWhatTheVMCanDo(t *testing.T) {
	s, p := liveService(t)
	view, err := s.resourceView(context.Background(), Request{Connection: "qemu:///session", ID: p.vm.Key.UUID})
	if err != nil {
		t.Fatal(err)
	}
	if !view.CanChangeLiveCPU || !view.CanChangeLiveMemory || view.MemoryBalloon != "virtio" {
		t.Fatal("a VM with slots and a balloon was not offered a live change", view)
	}
	found := false
	for _, mode := range view.ApplyModes {
		found = found || mode == "now"
	}
	if !found {
		t.Fatal("apply modes do not offer now", view.ApplyModes)
	}
	p.vm.LiveXML = strings.Replace(strings.Replace(liveRunningXML(),
		`<vcpu placement='static' current='2'>4</vcpu>`, `<vcpu placement='static'>2</vcpu>`, 1),
		`<memballoon model='virtio'/>`, `<memballoon model='none'/>`, 1)
	view, err = s.resourceView(context.Background(), Request{Connection: "qemu:///session", ID: p.vm.Key.UUID})
	if err != nil {
		t.Fatal(err)
	}
	if view.CanChangeLiveCPU || view.CanChangeLiveMemory {
		t.Fatal("a VM without slots or a balloon was offered a live change", view)
	}
	if !strings.Contains(view.LiveCPUReason, "spare CPU slots") || !strings.Contains(view.LiveMemoryReason, "no memory balloon") {
		t.Fatal("the reasons do not say what to change", view.LiveCPUReason, view.LiveMemoryReason)
	}
	for _, mode := range view.ApplyModes {
		if mode == "now" {
			t.Fatal("apply modes offer a live change that is refused")
		}
	}
}

// The plan is bound to what the VM is running with, not to its fingerprint,
// which moves as the guest runs.
func TestLiveChangeIsBoundToTheReviewedRunningValues(t *testing.T) {
	ctx := context.Background()
	s, p := liveService(t)
	plan, err := planLive(t, s, p, map[string]any{"memoryMiB": float64(1536)})
	if err != nil {
		t.Fatal(err)
	}
	stored, raw, err := s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var recipe map[string]any
	if err = json.Unmarshal(raw, &recipe); err != nil {
		t.Fatal(err)
	}
	if recipe["liveVersion"] != float64(1) || recipe["liveBeforeMemoryBytes"] != float64(1<<30) || recipe["liveMaximumMemoryBytes"] != float64(2<<30) {
		t.Fatal("the plan does not record what it was reviewed against", recipe)
	}
	h := &liveResourceHandler{s: s}
	p.vm.Fingerprint = "the guest kept running"
	if err = h.Validate(ctx, stored, raw); err != nil {
		t.Fatal("an unrelated live change refused the plan", err)
	}
	p.vm.LiveXML = strings.Replace(liveRunningXML(), "<currentMemory unit='KiB'>1048576</currentMemory>", "<currentMemory unit='KiB'>1572864</currentMemory>", 1)
	if err = h.Validate(ctx, stored, raw); err == nil {
		t.Fatal("the plan was applied after something else changed the running memory")
	}
	if p.writes != 0 {
		t.Fatal("validation changed the VM")
	}
}
