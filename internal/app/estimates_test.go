package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
)

func TestVMPlansReportInterruptionAndStoppedEditRequirement(t *testing.T) {
	for _, test := range []struct {
		action, state string
		downtime      bool
	}{
		{"start", "stopped", false}, {"restore-saved", "stopped", false}, {"resume", "paused", false},
		{"autostart", "stopped", false}, {"stop", "running", true}, {"hard-stop", "running", true},
		{"pause", "running", true}, {"save", "running", true},
	} {
		t.Run(test.action, func(t *testing.T) {
			s, p, _ := configService(t)
			p.vm.State = test.state
			p.vm.HasManagedSave = test.action == "restore-saved"
			input := map[string]any{}
			if test.action == "autostart" {
				input["enabled"] = true
			}
			plan, err := s.planVM(context.Background(), 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: test.action, Input: input})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Estimates.RequiresDowntime != test.downtime || !strings.Contains(plan.Estimates.Notes, "not estimated") || p.calls != 0 {
				t.Fatal("incorrect or mutating estimate", plan.Estimates, p.calls)
			}
			stored, _, err := s.Engine.Store.Plan(plan.ID)
			if err != nil || stored.Estimates != plan.Estimates {
				t.Fatal("estimate not persisted", err)
			}
			digest, err := operations.PlanDigest(plan)
			if err != nil || digest != plan.Digest {
				t.Fatal("estimate not included in original digest", err)
			}
			plan.Estimates.RequiresDowntime = !plan.Estimates.RequiresDowntime
			altered, err := operations.PlanDigest(plan)
			if err != nil || altered == digest {
				t.Fatal("downtime substitution did not alter digest", err)
			}
		})
	}
	for _, hardware := range []bool{false, true} {
		s, _, _ := hardwareFixture(t)
		plan := planConfig(t, s)
		if hardware {
			plan = planHardware(t, s)
		}
		if !plan.Estimates.RequiresDowntime || !strings.Contains(plan.Estimates.Notes, "already stopped") {
			t.Fatal("stopped edit prerequisite absent", plan.Estimates)
		}
		data, err := json.Marshal(plan)
		if err != nil || validation.Schema("operation-plan", data) != nil {
			t.Fatal("estimated plan violates existing schema", err)
		}
	}
	// Force off also ends a paused VM, but a stopped one has nothing to force.
	for state, allowed := range map[string]bool{"running": true, "paused": true, "stopped": false} {
		s, p, _ := configService(t)
		p.vm.State = state
		plan, err := s.planVM(context.Background(), 1000, Request{Connection: "fixture", ID: "vm-fixture", Action: "hard-stop", Input: map[string]any{}})
		if !allowed {
			if err == nil || !strings.Contains(err.Error(), "hard-stop requires running state, observed stopped") {
				t.Fatal("stopped VM was offered a force off", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(state, err)
		}
		stored, input, err := s.Engine.Store.Plan(plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = (&vmHandler{s: s, action: "hard-stop"}).Validate(context.Background(), stored, input); err != nil {
			t.Fatal(state, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&vmHandler{action: "stop"}).Estimate(ctx, domain.Plan{}, nil); err == nil {
		t.Fatal("canceled estimate succeeded")
	}
	if _, err := (&vmHandler{action: "future-unimplemented"}).Estimate(context.Background(), domain.Plan{}, nil); err == nil {
		t.Fatal("unknown VM action received a fabricated estimate")
	}
}
