//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
	"virmill.local/core/internal/domain"
)

func TestActiveCreationResultAllowsMissingOrPartialReceipt(t *testing.T) {
	for _, state := range []string{"queued", "validating", "running", "verifying"} {
		for _, present := range []bool{false, true} {
			name := state + "/missing"
			if present {
				name = state + "/partial"
			}
			t.Run(name, func(t *testing.T) {
				s, backend, r, _ := creationFixture(t)
				plan, err := s.Plan(context.Background(), 1000, r)
				if err != nil {
					t.Fatal(err)
				}
				job, err := s.Store.Accept(plan, domain.ID(), "fixture-request")
				if err != nil {
					t.Fatal(err)
				}
				job.State = state
				job.CancelRequested = true
				if err = s.Store.Update(job, "synthetic active state"); err != nil {
					t.Fatal(err)
				}
				if present {
					receipt := Receipt{Version: 1, PlanID: plan.ID, OperationID: job.ID, Volumes: []volumeProgress{}}
					if err = s.Store.Put("vm-creation", plan.ID, receipt); err != nil {
						t.Fatal(err)
					}
				}
				result, err := s.Result(context.Background(), 1000, job.ID)
				if err != nil {
					t.Fatal("active observation suggested recovery", err)
				}
				data := result.(map[string]any)
				if data["complete"] != false || data["receiptAvailable"] != present || data["guestBootVerified"] != false || data["setupVerified"] != false || data["connectivityVerified"] != false {
					t.Fatal("progress fabricated completion", data)
				}
				if (data["receipt"] != nil) != present || data["operation"].(domain.Job).State != state || !data["operation"].(domain.Job).CancelRequested {
					t.Fatal("progress lost actual state", data)
				}
				encoded, _ := json.Marshal(data["nextActions"])
				if !strings.Contains(string(encoded), "operation watch") || strings.Contains(string(encoded), "reconcile") {
					t.Fatal("premature recovery advice", string(encoded))
				}
				after, err := s.Store.Job(job.ID)
				if err != nil || after != job || backend.allocated != 0 || backend.definitions != 0 {
					t.Fatal("result mutated operation or backend", err)
				}
				for _, resource := range plan.ResourceIDs {
					owners, err := s.Store.ResourceJobs(resource)
					if err != nil || len(owners) != 1 || owners[0] != job.ID {
						t.Fatal("result changed locks", owners, err)
					}
				}
				if data, err := s.Result(context.Background(), 1001, job.ID); err == nil || data != nil {
					t.Fatal("other actor observed result", data, err)
				}
			})
		}
	}
}

func TestActiveCreationResultRefusesInvalidReceiptAndTerminalInconsistency(t *testing.T) {
	for _, test := range []string{"newer-receipt", "unknown-field", "succeeded-missing", "succeeded-incomplete", "uncertain-missing"} {
		t.Run(test, func(t *testing.T) {
			s, _, r, _ := creationFixture(t)
			plan, err := s.Plan(context.Background(), 1000, r)
			if err != nil {
				t.Fatal(err)
			}
			job, err := s.Store.Accept(plan, domain.ID(), "fixture-request")
			if err != nil {
				t.Fatal(err)
			}
			switch test {
			case "newer-receipt":
				err = s.Store.Put("vm-creation", plan.ID, map[string]any{"schemaVersion": 2})
			case "unknown-field":
				err = s.Store.Put("vm-creation", plan.ID, map[string]any{"schemaVersion": 1, "unknown": true})
			case "succeeded-incomplete":
				job.State = "succeeded"
				err = s.Store.Put("vm-creation", plan.ID, Receipt{Version: 1, PlanID: plan.ID, OperationID: job.ID})
			case "succeeded-missing":
				job.State = "succeeded"
			case "uncertain-missing":
				job.State = "recovery-required"
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Store.Update(job, "synthetic result fixture"); err != nil {
				t.Fatal(err)
			}
			result, err := s.Result(context.Background(), 1000, job.ID)
			if err == nil {
				t.Fatal("invalid or uncertain result reported normal progress", result)
			}
			if result != nil && result.(map[string]any)["complete"] != false {
				t.Fatal("inconsistent terminal state reported complete", result)
			}
		})
	}
}

func TestCreationResultDuringBlockedUploadThenSuccess(t *testing.T) {
	s, backend, r, _ := creationFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	backend.started = started
	backend.release = release
	var once sync.Once
	unblock := func() {
		backend.mu.Lock()
		backend.release = nil
		backend.mu.Unlock()
		once.Do(func() { close(release) })
	}
	t.Cleanup(unblock)
	plan, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	job := applyCreation(t, s, plan)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("synthetic upload did not start")
	}
	for i := 0; i < 3; i++ {
		result, err := s.Result(context.Background(), 1000, job.ID)
		if err != nil {
			t.Fatal("active transfer suggested recovery", err)
		}
		data := result.(map[string]any)
		receipt := data["receipt"].(Receipt)
		if data["complete"] != false || data["receiptAvailable"] != true || data["operation"].(domain.Job).State != "running" || receipt.Defined || receipt.VolumesVerified || receipt.Volumes[0].Allocated == nil {
			t.Fatal("partial receipt misreported", data)
		}
	}
	unblock()
	done := awaitCreation(t, s, job.ID)
	if done.State != "succeeded" {
		t.Fatal(done)
	}
	result, err := s.Result(context.Background(), 1000, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(map[string]any)
	if data["complete"] != true || data["guestBootVerified"] != false || data["setupVerified"] != false || data["connectivityVerified"] != false {
		t.Fatal("definition and guest readiness conflated", data)
	}
	if backend.allocated != 2 || backend.populated != 2 || backend.definitions != 1 {
		t.Fatal("result replayed an effect", backend)
	}
	t.Log("synthetic blocked-upload coordinator only; no native image upload, VM or hardware execution")
}

func TestRecoveredCreationDoesNotMasqueradeAsActiveProgress(t *testing.T) {
	s, backend, r, _ := creationFixture(t)
	plan, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Store.Accept(plan, domain.ID(), "fixture-request")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Result(context.Background(), 1000, job.ID)
	if err != nil || result.(map[string]any)["complete"] != false {
		t.Fatal("queued progress unavailable", result, err)
	}
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	result, err = s.Result(context.Background(), 1000, job.ID)
	if err == nil || result.(map[string]any)["operation"].(domain.Job).State != "recovery-required" || result.(map[string]any)["complete"] != false {
		t.Fatal("recovered uncertainty masked as active progress", result, err)
	}
	if _, present := result.(map[string]any)["nextActions"]; present {
		t.Fatal("uncertain operation received active-work advice", result)
	}
	for _, resource := range plan.ResourceIDs {
		owners, err := s.Store.ResourceJobs(resource)
		if err != nil || len(owners) != 1 || owners[0] != job.ID {
			t.Fatal("recovery result lost uncertainty locks", owners, err)
		}
	}
	if backend.allocated != 0 || backend.populated != 0 || backend.definitions != 0 {
		t.Fatal("read-only result replayed creation", backend)
	}
}
