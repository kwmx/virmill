package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"virmill.local/core/internal/backend/xmlpatch"
)

// ADR 0068: boot order and media edits are offered while a VM runs on ADR
// 0061's terms, and change only the saved definition.
func TestBootEditWhileRunningChecksOnlyTheSavedDefinition(t *testing.T) {
	ctx := context.Background()
	s, p, _ := hardwareFixture(t)
	p.vm.State = "running"
	p.vm.LiveXML = p.vm.PersistentXML
	plan, err := s.planVM(ctx, 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: hardwareInput()})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Operation != "vm.configure-hardware" {
		t.Fatal("unexpected operation", plan.Operation)
	}
	running, ok := plan.Review["runningBoot"].(xmlpatch.BootView)
	after := plan.Review["afterBoot"].(xmlpatch.BootView)
	if !ok || running.Devices[1].MediaPresent == after.Devices[1].MediaPresent {
		t.Fatal("the review hides what the running VM keeps", plan.Review)
	}
	if !strings.Contains(plan.Estimates.Notes, "keeps running") {
		t.Fatal("the estimate does not say the guest keeps running", plan.Estimates)
	}
	nextBoot := false
	for _, risk := range plan.Risks {
		if strings.Contains(risk, "keeps its current boot order and media") {
			nextBoot = true
		}
	}
	if !nextBoot {
		t.Fatal("no risk says the change applies at the next start", plan.Risks)
	}
	stored, raw, err := s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var recipe map[string]any
	if err = json.Unmarshal(raw, &recipe); err != nil {
		t.Fatal(err)
	}
	if recipe["editPrecondition"] != persistentPrecondition || recipe["editBeforePersistentSHA256"] != xmlpatch.Digest(p.vm.PersistentXML) {
		t.Fatal("the plan is not bound to the saved definition", recipe)
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
	p.vm.State, p.vm.HasManagedSave = "running", true
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
}

// The guest-agent channel keeps the stopped-VM rule: it adds a device, and
// offering it on a running VM would suggest the channel can be used now.
func TestGuestAgentChannelStillRequiresAStoppedVM(t *testing.T) {
	s, p, _ := configService(t)
	p.vm.State = "running"
	p.vm.LiveXML = p.vm.PersistentXML
	if _, err := s.planVM(context.Background(), 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: map[string]any{"enableGuestAgent": true}}); err == nil {
		t.Fatal("the guest-agent channel was offered on a running VM")
	}
}
