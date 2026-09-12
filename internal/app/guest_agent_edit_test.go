package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

// This deterministic backend changes in-memory XML only. Real SQLite exercises
// shared-service planning, approval and reconciliation; no native guest or guest
// software installation is simulated as hardware evidence.
type guestChannelProvider struct {
	*configProvider
	checks int
}

func (p *guestChannelProvider) CheckConfiguration(ctx context.Context, uri, id string, input map[string]any) error {
	if input["editVersion"] != float64(3) {
		return p.configProvider.CheckConfiguration(ctx, uri, id, input)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checks++
	if p.fail == "preflight" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "synthetic preservation check unavailable")
	}
	view, err := xmlpatch.ParseGuestAgentInput(input)
	if err != nil {
		return err
	}
	if p.vm.State != "stopped" || p.vm.HasManagedSave || p.vm.Fingerprint != input["editBeforeFingerprint"] {
		return domain.Fail("STALE_PLAN", "synthetic VM state changed")
	}
	after, err := xmlpatch.EnableGuestAgent(p.vm.PersistentXML)
	if err != nil {
		return err
	}
	hash, err := xmlpatch.GuestAgentDigest(after, view.ControllerIndex, view.Port, view.AddsController)
	if err != nil {
		return err
	}
	if hash != input["xmlSHA256"] {
		return domain.Fail("STALE_PLAN", "synthetic XML recipe changed")
	}
	return nil
}
func (p *guestChannelProvider) Execute(ctx context.Context, uri, id, action string, input map[string]any) error {
	if input["editVersion"] != float64(3) {
		return p.configProvider.Execute(ctx, uri, id, action, input)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if action != "set" || input["vmID"] != p.vm.Key.UUID {
		return errors.New("unexpected synthetic guest channel action")
	}
	if _, err := xmlpatch.ParseGuestAgentInput(input); err != nil {
		return err
	}
	if p.fail == "define" {
		return errors.New("synthetic definition failure")
	}
	after, err := xmlpatch.EnableGuestAgent(p.vm.PersistentXML)
	if err != nil {
		return err
	}
	p.vm.PersistentXML = after
	p.vm.Fingerprint = xmlpatch.Digest(after)
	if p.fail == "ack" {
		return errors.New("synthetic lost acknowledgement")
	}
	return nil
}
func (p *guestChannelProvider) ObserveConfiguration(ctx context.Context, uri, id string, input map[string]any) (bool, error) {
	if input["editVersion"] != float64(3) {
		return p.configProvider.ObserveConfiguration(ctx, uri, id, input)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	view, err := xmlpatch.ParseGuestAgentInput(input)
	if err != nil {
		return false, err
	}
	hash, err := xmlpatch.GuestAgentDigest(p.vm.PersistentXML, view.ControllerIndex, view.Port, view.AddsController)
	if err != nil {
		return false, err
	}
	return hash == input["xmlSHA256"] && p.vm.State == "stopped" && !p.vm.HasManagedSave, nil
}

const guestChannelVMID = "570c4866-5b5e-4583-817c-602538d19b9c"

func guestChannelFixture(t *testing.T) (*Service, *guestChannelProvider, string) {
	t.Helper()
	s, base, path := configService(t)
	base.vm.Key = domain.ResourceKey{ProviderID: "fixture", ConnectionID: "qemu:///system", Kind: "vm", UUID: guestChannelVMID}
	base.vm.Name = "Guest channel fixture"
	base.vm.Fingerprint = strings.Repeat("a", 64)
	base.vm.PersistentXML = strings.Replace(base.vm.PersistentXML, "</domain>", `<uuid>`+guestChannelVMID+`</uuid><os><type arch="x86_64">hvm</type></os><devices><disk type="file" device="disk"><driver name="qemu" type="raw"/><source file="/never-opened/guest.raw"/><target dev="hda" bus="ide"/></disk><serial type="pty"><target port="0"/></serial><console type="pty"><target type="serial" port="0"/></console></devices></domain>`, 1)
	p := &guestChannelProvider{configProvider: base}
	s.Provider = p
	return s, p, path
}
func guestChannelRequest(input map[string]any) Request {
	return Request{Connection: "qemu:///system", ID: guestChannelVMID, Action: "set", Input: input}
}
func guestChannelPlan(t *testing.T, s *Service) domain.Plan {
	t.Helper()
	response := s.Call(context.Background(), 1000, "vm.plan", guestChannelRequest(map[string]any{"enableGuestAgent": true}))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	plan, ok := response.Data.(domain.Plan)
	if !ok {
		t.Fatalf("expected shared plan: %T", response.Data)
	}
	return plan
}
func TestGuestChannelServicePreviewAndDurableExecution(t *testing.T) {
	s, p, _ := guestChannelFixture(t)
	before := p.vm.PersistentXML
	plan := guestChannelPlan(t, s)
	if plan.Operation != "vm.configure-guest-agent" || len(plan.Steps) != 1 || p.calls != 0 || p.vm.PersistentXML != before {
		t.Fatal("preview mutated or wrong durable operation", plan.Operation, p.calls)
	}
	_, raw, err := s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if input["editVersion"] != float64(3) || input["enableGuestAgent"] != true || input["applyMode"] != "next-boot" || input["agentController"] != float64(0) || input["agentPort"] != float64(1) || input["agentAddsController"] != true || input["vmID"] != guestChannelVMID {
		t.Fatal("wrong frozen recipe", input)
	}
	if strings.Contains(string(raw), "/never-opened") || strings.Contains(string(raw), "<domain>") || strings.Contains(string(raw), "preserved") {
		t.Fatal("opaque domain content journaled")
	}
	if plan.Review["persistentEdit"] != true || plan.Review["requiresShutdown"] != true || plan.Review["diskDeletion"] != false {
		t.Fatal("review obscures persistent-only scope", plan.Review)
	}
	requested, ok := plan.Review["requested"].(map[string]any)
	if !ok || requested["enableGuestAgent"] != true {
		t.Fatal("review omits requested guest agent", plan.Review)
	}
	for _, required := range []string{"host-mutation", "exclusive-configuration-writer", "guest-agent-host-access"} {
		found := false
		for _, ack := range plan.Acknowledgements {
			if ack == required {
				found = true
			}
		}
		if !found {
			t.Fatal("missing explicit consent", required)
		}
	}
	job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	configured, err := xmlpatch.InspectGuestAgent(p.vm.PersistentXML)
	if err != nil || !configured.Present || p.calls != 1 || !strings.Contains(p.vm.PersistentXML, "preserved") || !strings.Contains(p.vm.PersistentXML, "/never-opened/guest.raw") {
		t.Fatal("synthetic effect lost original config", configured, p.calls, err)
	}
	if p.checks < 2 {
		t.Fatal("preservation preflight was not repeated before apply")
	}
	t.Log("shared service and real SQLite only: exactly one in-memory XML change; no guest installation/readiness claim")
}
func TestGuestChannelServiceInspectionDoesNotPlanOrExecute(t *testing.T) {
	s, p, _ := guestChannelFixture(t)
	response := s.Call(context.Background(), 1000, "vm.guest-agent.show", Request{Connection: "qemu:///system", ID: guestChannelVMID})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	view, ok := response.Data.(GuestAgentConnection)
	if !ok || view.Present || !view.CanEnable || !view.AddsController || view.Reason == "" || view.State != p.vm.State || view.HasManagedSave != p.vm.HasManagedSave {
		t.Fatalf("unhelpful configuration view: %#v", response.Data)
	}
	if p.calls != 0 || p.checks != 0 {
		t.Fatal("read-only inspection invoked effect checks", p.calls, p.checks)
	}
	var count int
	if err := s.Engine.Store.DB.QueryRow("SELECT COUNT(*) FROM plans").Scan(&count); err != nil || count != 0 {
		t.Fatal("read-only inspection planned", count, err)
	}
	for _, request := range []Request{{Connection: "qemu:///system"}, {Connection: "qemu:///system", ID: guestChannelVMID, Input: map[string]any{"enableGuestAgent": true}}, {Connection: "qemu:///system", ID: guestChannelVMID, Action: "set"}} {
		if response = s.Call(context.Background(), 1000, "vm.guest-agent.show", request); response.Error == nil {
			t.Fatal("inspection accepted mutation-shaped request")
		}
	}
}
func TestGuestChannelServiceRefusesUnsafePreviewBeforeJournal(t *testing.T) {
	for _, name := range []string{"running", "saved", "transient", "existing", "external", "duplicate", "preflight", "live", "false", "mixed", "secret", "allocation-injection"} {
		t.Run(name, func(t *testing.T) {
			s, p, _ := guestChannelFixture(t)
			input := map[string]any{"enableGuestAgent": true}
			switch name {
			case "running":
				p.vm.State = "running"
			case "saved":
				p.vm.HasManagedSave = true
			case "transient":
				p.vm.PersistentXML = ""
			case "existing", "external", "duplicate":
				enabled, err := xmlpatch.EnableGuestAgent(p.vm.PersistentXML)
				if err != nil {
					t.Fatal(err)
				}
				p.vm.PersistentXML = enabled
				if name == "external" {
					p.vm.PersistentXML = strings.Replace(enabled, `<channel type="unix">`, `<channel type="unix"><source path="/external/socket" mode="bind"/>`, 1)
				}
				if name == "duplicate" {
					p.vm.PersistentXML = strings.Replace(enabled, "</devices>", `<channel type="unix"><target type="virtio" name="org.qemu.guest_agent.0"/><address type="virtio-serial" controller="0" bus="0" port="2"/></channel></devices>`, 1)
				}
			case "preflight":
				p.fail = "preflight"
			case "live":
				input["applyMode"] = "live"
			case "false":
				input["enableGuestAgent"] = false
			case "mixed":
				input["memoryMiB"] = float64(512)
			case "secret":
				input["password"] = "guest-channel-secret-marker"
			case "allocation-injection":
				input["agentPort"] = float64(4)
			}
			response := s.Call(context.Background(), 1000, "vm.plan", guestChannelRequest(input))
			if response.Error == nil || strings.Contains(response.Error.Error(), "guest-channel-secret-marker") || p.calls != 0 {
				t.Fatal("unsafe preview accepted or leaked", response.Error, p.calls)
			}
			var count int
			if err := s.Engine.Store.DB.QueryRow("SELECT COUNT(*) FROM plans").Scan(&count); err != nil || count != 0 {
				t.Fatal("refusal journaled a plan", count, err)
			}
		})
	}
}
func TestGuestChannelServiceStaleApprovalAndRegistryRefuseEffects(t *testing.T) {
	for _, name := range []string{"fingerprint", "acknowledgement", "digest", "missing-handler"} {
		t.Run(name, func(t *testing.T) {
			s, p, _ := guestChannelFixture(t)
			plan := guestChannelPlan(t, s)
			request := operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: domain.ID(), Acknowledgements: append([]string(nil), plan.Acknowledgements...)}
			switch name {
			case "fingerprint":
				p.vm.Fingerprint = strings.Repeat("b", 64)
			case "acknowledgement":
				request.Acknowledgements = nil
				for _, ack := range plan.Acknowledgements {
					if ack != "guest-agent-host-access" {
						request.Acknowledgements = append(request.Acknowledgements, ack)
					}
				}
			case "digest":
				request.PlanDigest = strings.Repeat("b", 64)
			case "missing-handler":
				delete(s.Engine.Handlers, "vm.configure-guest-agent")
			}
			if _, err := s.Engine.Apply(context.Background(), 1000, request); err == nil || p.calls != 0 {
				t.Fatal("invalid authorization executed", err, p.calls)
			}
		})
	}
}
func TestGuestChannelServiceLostAcknowledgementReconcilesWithoutReplay(t *testing.T) {
	s, p, path := guestChannelFixture(t)
	p.fail = "ack"
	plan := guestChannelPlan(t, s)
	job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if job.State != "recovery-required" {
		t.Fatal(job)
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
	New(p, s.Engine)
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	job, err = s.Engine.Reconcile(context.Background(), job.ID)
	if err != nil || job.State != "succeeded" {
		t.Fatal(job, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls != 1 {
		t.Fatal("durable recovery replayed channel definition", p.calls)
	}
}
func TestGuestChannelServiceLegacyRecipesRemainDistinct(t *testing.T) {
	s, _, _ := guestChannelFixture(t)
	for _, tc := range []struct {
		input     map[string]any
		operation string
		version   float64
	}{
		{map[string]any{"memoryMiB": float64(512)}, "vm.configure-resources", 1},
		{map[string]any{"bootOrder": []xmlpatch.BootChoice{{Kind: "disk", ID: "hda"}}}, "vm.configure-hardware", 2},
	} {
		response := s.Call(context.Background(), 1000, "vm.plan", guestChannelRequest(maps.Clone(tc.input)))
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		plan := response.Data.(domain.Plan)
		if plan.Operation != tc.operation {
			t.Fatal("legacy operation remapped", plan.Operation)
		}
		_, raw, err := s.Engine.Store.Plan(plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		var input map[string]any
		if err = json.Unmarshal(raw, &input); err != nil {
			t.Fatal(err)
		}
		if input["editVersion"] != tc.version || input["enableGuestAgent"] != nil || input["agentPort"] != nil {
			t.Fatal("legacy recipe polluted", input)
		}
	}
}

func TestGuestChannelServiceUncertainDefinitionRetainsLockAndNeverReplays(t *testing.T) {
	for _, mode := range []string{"define", "changed-after-ack"} {
		t.Run(mode, func(t *testing.T) {
			s, p, _ := guestChannelFixture(t)
			p.fail = "define"
			if mode == "changed-after-ack" {
				p.fail = "ack"
			}
			plan := guestChannelPlan(t, s)
			job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
			if job.State != "recovery-required" {
				t.Fatal("uncertain definition did not require recovery", job)
			}
			if mode == "changed-after-ack" {
				p.mu.Lock()
				p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "preserved", "external change", 1)
				p.mu.Unlock()
			}
			reconciled, err := s.Engine.Reconcile(context.Background(), job.ID)
			if err == nil && reconciled.State == "succeeded" {
				t.Fatal("unproven definition certified")
			}
			owners, err := s.Engine.Store.ResourceJobs(plan.ResourceIDs[0])
			if err != nil || len(owners) != 1 || owners[0] != job.ID {
				t.Fatal("unverified resource unlocked", owners, err)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.calls != 1 {
				t.Fatal("recovery replayed an uncertain guest channel mutation", p.calls)
			}
		})
	}
}
