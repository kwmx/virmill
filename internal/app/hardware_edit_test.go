package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

func hardwareFixture(t *testing.T) (*Service, *configProvider, string) {
	t.Helper()
	s, p, path := configService(t)
	p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "</domain>", `<os><type arch='x86_64'>hvm</type></os><devices><disk type='file' device='disk'><source file='/never-opened/system.qcow2'/><target dev='vda' bus='virtio'/><boot order='2'/></disk><disk type='volume' device='cdrom'><driver name='qemu' type='raw'/><source pool='private-fixture' volume='installer.iso'/><target dev='sda' bus='sata'/><readonly/><boot order='1'/></disk><interface type='network'><mac address='52:54:00:11:22:33'/><source network='existing'/></interface></devices></domain>`, 1)
	return s, p, path
}
func hardwareInput() map[string]any {
	return map[string]any{"applyMode": "next-boot", "memoryMiB": float64(512), "ejectMedia": "sda", "bootOrder": []xmlpatch.BootChoice{{Kind: "disk", ID: "vda"}, {Kind: "interface", ID: "52:54:00:11:22:33"}}}
}
func planHardware(t *testing.T, s *Service) domain.Plan {
	t.Helper()
	plan, err := s.planVM(context.Background(), 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: hardwareInput()})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func TestHardwareCombinedPlanReviewAndDurableExecution(t *testing.T) {
	s, p, _ := hardwareFixture(t)
	plan := planHardware(t, s)
	if plan.Operation != "vm.configure-hardware" || len(plan.Steps) != 1 {
		t.Fatal("combined edit was split or uses legacy operation", plan)
	}
	before := plan.Review["beforeBoot"].(xmlpatch.BootView)
	after := plan.Review["afterBoot"].(xmlpatch.BootView)
	if before.Devices[1].SourceType != "volume" || after.Devices[1].SourceType != "file" || after.Devices[1].MediaPresent || after.Devices[1].Order != 0 || after.Devices[0].Order != 1 || after.Devices[2].Order != 2 {
		t.Fatal("review hides boot/media changes", plan.Review)
	}
	for _, needed := range []string{"host-mutation", "exclusive-configuration-writer", "replace-boot-order", "eject-retain-media"} {
		found := false
		for _, ack := range plan.Acknowledgements {
			if ack == needed {
				found = true
			}
		}
		if !found {
			t.Fatal("missing acknowledgement", needed)
		}
	}
	_, input, err := s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(input), "/never-opened") || strings.Contains(string(input), "<domain>") {
		t.Fatal("full XML journaled")
	}
	job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls != 1 || !strings.Contains(p.vm.PersistentXML, "preserved") || strings.Contains(p.vm.PersistentXML, "installer.iso") || !strings.Contains(p.vm.PersistentXML, "/never-opened/system.qcow2") {
		t.Fatal("unexpected synthetic effect", p.vm, p.calls)
	}
	t.Log("synthetic shared coordinator: one reviewed CPU/RAM/boot/media definition, hashes only, no file or storage deletion")
}
func TestHardwareRefusalAndStalePlanDoNotExecute(t *testing.T) {
	for _, test := range []string{"missing-boot-replacement", "empty-candidate", "duplicate", "live-mode", "saved", "running", "unknown-field"} {
		t.Run(test, func(t *testing.T) {
			s, p, _ := hardwareFixture(t)
			input := hardwareInput()
			switch test {
			case "missing-boot-replacement":
				delete(input, "bootOrder")
			case "empty-candidate":
				input["bootOrder"] = []xmlpatch.BootChoice{{Kind: "disk", ID: "sda"}}
			case "duplicate":
				input["bootOrder"] = []xmlpatch.BootChoice{{Kind: "disk", ID: "vda"}, {Kind: "disk", ID: "vda"}}
			case "live-mode":
				input["applyMode"] = "both"
			case "saved":
				p.vm.HasManagedSave = true
			case "running":
				p.vm.State = "running"
			case "unknown-field":
				input["password"] = "hardware-dummy-secret"
			}
			_, err := s.planVM(context.Background(), 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "set", Input: input})
			if err == nil || strings.Contains(err.Error(), "hardware-dummy-secret") || p.calls != 0 {
				t.Fatal("unsafe plan accepted or leaked", err, p.calls)
			}
		})
	}
	s, p, _ := hardwareFixture(t)
	plan := planHardware(t, s)
	p.vm.Fingerprint = "external-change"
	if _, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "stale-hardware", Acknowledgements: plan.Acknowledgements}); err == nil || p.calls != 0 {
		t.Fatal("stale edit applied", err, p.calls)
	}
}
func TestHardwareLostAcknowledgementReopensWithoutRedefining(t *testing.T) {
	s, p, path := hardwareFixture(t)
	p.fail = "ack"
	plan := planHardware(t, s)
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
		t.Fatal("redefinition replayed", p.calls)
	}
}
func TestHardwareUnknownOperationRegistryFailsClosed(t *testing.T) {
	s, p, _ := hardwareFixture(t)
	plan := planHardware(t, s)
	delete(s.Engine.Handlers, "vm.configure-hardware")
	if _, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "old-hardware-registry", Acknowledgements: plan.Acknowledgements}); err == nil || p.calls != 0 {
		t.Fatal("old registry applied new hardware operation", err, p.calls)
	}
}
func TestBootInspectionKeepsLiveAndPersistentSeparate(t *testing.T) {
	s, p, _ := hardwareFixture(t)
	p.vm.State = "running"
	var err error
	p.vm.LiveXML, err = xmlpatch.EditHardware(p.vm.PersistentXML, xmlpatch.HardwareEdit{EjectMedia: "sda", BootOrder: []xmlpatch.BootChoice{{Kind: "disk", ID: "vda"}}})
	if err != nil {
		t.Fatal(err)
	}
	response := s.Call(context.Background(), 1000, "vm.boot.get", Request{Connection: "fixture", ID: "vm-fixture"})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	data := response.Data.(map[string]any)
	if data["persistent"].(xmlpatch.BootView).Devices[1].MediaPresent != true || data["live"].(xmlpatch.BootView).Devices[1].MediaPresent || data["guestBootVerified"] != false || p.calls != 0 {
		t.Fatal("layers conflated or read mutated", data)
	}
	if response = s.Call(context.Background(), 1000, "vm.boot.get", Request{}); response.Error == nil {
		t.Fatal("missing identity accepted")
	}
}
func TestHardwareExampleSchema(t *testing.T) {
	b, err := os.ReadFile("../../examples/configuration/finish-installation.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("vm-hardware-edit-input", b); err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err = json.Unmarshal(b, &request); err != nil {
		t.Fatal(err)
	}
	if _, err = xmlpatch.ParseHardwareInput(request); err != nil {
		t.Fatal(err)
	}
}

func TestHardwareFailureAndCancellationRetainTruthfulState(t *testing.T) {
	t.Run("failed-definition", func(t *testing.T) {
		s, p, _ := hardwareFixture(t)
		p.fail = "define"
		plan := planHardware(t, s)
		job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
		if job.State != "recovery-required" {
			t.Fatal(job)
		}
		if observed, err := s.Engine.Reconcile(context.Background(), job.ID); err == nil && observed.State == "succeeded" {
			t.Fatal("unperformed edit was certified")
		}
		owners, err := s.Engine.Store.ResourceJobs(plan.ResourceIDs[0])
		if err != nil || len(owners) != 1 || owners[0] != job.ID {
			t.Fatal("uncertain resource unlocked", owners, err)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.calls != 1 {
			t.Fatal("reconciliation redefined the VM")
		}
	})
	t.Run("cancel-during-definition", func(t *testing.T) {
		s, p, _ := hardwareFixture(t)
		p.entered = make(chan struct{})
		p.release = make(chan struct{})
		job := applyConfig(t, s, planHardware(t, s))
		select {
		case <-p.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("definition never entered")
		}
		if _, err := s.Engine.Cancel(job.ID); err != nil {
			t.Fatal(err)
		}
		close(p.release)
		job = awaitConfig(t, s, job.ID)
		if job.State != "succeeded" || !job.CancelRequested {
			t.Fatal("completed edit reported as canceled or undone", job)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.calls != 1 {
			t.Fatal("cancellation replayed or undid definition")
		}
	})
	t.Run("changed-after-lost-ack", func(t *testing.T) {
		s, p, _ := hardwareFixture(t)
		p.fail = "ack"
		plan := planHardware(t, s)
		job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
		p.mu.Lock()
		p.vm.PersistentXML = strings.Replace(p.vm.PersistentXML, "preserved", "externally changed", 1)
		p.mu.Unlock()
		if observed, err := s.Engine.Reconcile(context.Background(), job.ID); err == nil && observed.State == "succeeded" {
			t.Fatal("external metadata change certified")
		}
		owners, err := s.Engine.Store.ResourceJobs(plan.ResourceIDs[0])
		if err != nil || len(owners) != 1 {
			t.Fatal("unconfirmed edit unlocked", owners, err)
		}
	})
}
