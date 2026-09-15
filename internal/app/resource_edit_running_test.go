package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"virmill.local/core/internal/backend/xmlpatch"
)

// ADR 0061: a next-boot CPU/RAM edit reviewed while the VM runs is checked
// against the saved definition only.
func TestNextBootEditWhileRunningChecksOnlyTheSavedDefinition(t *testing.T) {
	ctx := context.Background()
	s, p, _ := configService(t)
	p.vm.State = "running"
	p.vm.LiveXML = p.vm.PersistentXML
	plan, err := s.planVM(ctx, 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: map[string]any{"vcpus": float64(4), "memoryMiB": float64(512), "applyMode": "next-boot"}})
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
	if recipe["editPrecondition"] != persistentPrecondition || recipe["editBeforePersistentSHA256"] != xmlpatch.Digest(p.vm.PersistentXML) || !strings.Contains(plan.Estimates.Notes, "keeps running") {
		t.Fatal(recipe, plan.Estimates)
	}
	h := &vmHandler{s: s, action: "set"}
	// The live definition moves as the guest runs, and the VM may stop before
	// the plan is applied; the reviewed saved definition is what matters.
	for _, state := range []string{"running", "paused", "stopped"} {
		p.mu.Lock()
		p.vm.State, p.vm.Fingerprint = state, "live-changed-"+state
		p.mu.Unlock()
		if err := h.Validate(ctx, stored, raw); err != nil {
			t.Fatal(state, err)
		}
	}
	p.mu.Lock()
	p.vm.HasManagedSave = true
	p.mu.Unlock()
	if err := h.Validate(ctx, stored, raw); err == nil {
		t.Fatal("edit applied over managed-save state")
	}
	p.mu.Lock()
	p.vm.HasManagedSave = false
	p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "</domain>", "<on_crash>destroy</on_crash></domain>", 1)
	p.mu.Unlock()
	if err := h.Validate(ctx, stored, raw); err == nil {
		t.Fatal("edit applied over a changed saved definition")
	}
	if p.calls != 0 {
		t.Fatal("validation mutated the VM")
	}

	// Other edits still need a stopped VM.
	s, p, _ = configService(t)
	p.vm.State = "running"
	if _, err := s.planVM(ctx, 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: map[string]any{"enableGuestAgent": true, "applyMode": "next-boot"}}); err == nil {
		t.Fatal("guest-agent channel planned on a running VM")
	}
}
