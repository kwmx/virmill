package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

// Deterministic provider failures exercise service/journal guarantees only.
// They do not qualify native undefine, storage preservation or host behavior.
type removalFixture struct {
	fixtureProvider
	definition            domain.DefinitionRemoval
	inspections, removals atomic.Int32
	absent                atomic.Bool
	lostAcknowledgement   bool
}

func (p *removalFixture) InspectDefinitionRemoval(_ context.Context, _, _ string) (domain.DefinitionRemoval, error) {
	p.inspections.Add(1)
	r := p.definition
	r.RetainedSources = append([]string(nil), r.RetainedSources...)
	return r, nil
}
func (p *removalFixture) RemoveDefinition(_ context.Context, r domain.DefinitionRemoval) error {
	p.removals.Add(1)
	if !reflect.DeepEqual(r, p.definition) {
		return domain.Fail("STALE_PLAN", "fixture definition changed")
	}
	p.absent.Store(true)
	if p.lostAcknowledgement {
		return domain.Fail("RECOVERY_REQUIRED", "fixture lost acknowledgement after removal")
	}
	return nil
}
func (p *removalFixture) DefinitionAbsent(_ context.Context, key domain.ResourceKey) (bool, error) {
	if key != p.VM.Key {
		return false, domain.Fail("INVALID_INPUT", "fixture exact resource differs")
	}
	return p.absent.Load(), nil
}

func removalService(t *testing.T) (*Service, *removalFixture, string) {
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
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: "0d98a1a3-e8fb-4e88-bfa0-ac466a733e39"}
	p := &removalFixture{fixtureProvider: fixtureProvider{VM: domain.VM{Key: key, Name: "retained-definition", State: "stopped", Fingerprint: strings.Repeat("a", 64), PersistentXML: "<domain/>"}}}
	p.definition = domain.DefinitionRemoval{Resource: key, Name: p.VM.Name, Fingerprint: p.VM.Fingerprint, DefinitionSHA256: strings.Repeat("b", 64), RetainedSources: []string{"/fixture/retained.qcow2"}}
	s := New(p, operations.New(db))
	t.Cleanup(func() { s.Engine.Close(); s.Engine.Store.Close() })
	return s, p, path
}
func removalPlan(t *testing.T, s *Service, p *removalFixture) domain.Plan {
	t.Helper()
	plan, err := s.planRemoval(context.Background(), 1000, Request{Connection: p.VM.Key.ConnectionID, ID: p.VM.Key.UUID, Action: "remove"})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func removalNoPlans(t *testing.T, s *Service) {
	t.Helper()
	var count int
	if err := s.Engine.Store.DB.QueryRow("SELECT count(*) FROM plans").Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected removal persisted a plan", count, err)
	}
}

func TestRemovalRejectsExtraInputBeforeProviderInspection(t *testing.T) {
	for _, fault := range []string{"input", "path", "after", "apply", "missing-id", "invalid-id", "action", "remote"} {
		t.Run(fault, func(t *testing.T) {
			s, p, _ := removalService(t)
			r := Request{Connection: p.VM.Key.ConnectionID, ID: p.VM.Key.UUID, Action: "remove"}
			switch fault {
			case "input":
				r.Input = map[string]any{"deleteDisks": true}
			case "path":
				r.Path = "/fixture/not-an-input"
			case "after":
				r.After = 1
			case "apply":
				r.Apply = &operations.ApplyRequest{}
			case "missing-id":
				r.ID = ""
			case "invalid-id":
				r.ID = "other"
			case "action":
				r.Action = "delete-disks"
			case "remote":
				r.Connection = "qemu+ssh://other/system"
			}
			if _, err := s.planRemoval(context.Background(), 1000, r); err == nil || p.inspections.Load() != 0 || p.removals.Load() != 0 {
				t.Fatal("invalid removal reached provider", err, p.inspections.Load())
			}
			removalNoPlans(t, s)
		})
	}
}

func TestRemovalRejectsUnsupportedProviderAndMismatchedInspection(t *testing.T) {
	for _, fault := range []string{"unsupported", "uuid", "connection", "provider", "kind", "fingerprint", "definition-hash"} {
		t.Run(fault, func(t *testing.T) {
			s, p, _ := removalService(t)
			switch fault {
			case "unsupported":
				s.Provider = &p.fixtureProvider
			case "uuid":
				p.definition.Resource.UUID = "1d98a1a3-e8fb-4e88-bfa0-ac466a733e39"
			case "connection":
				p.definition.Resource.ConnectionID = "qemu:///session"
			case "provider":
				p.definition.Resource.ProviderID = "other"
			case "kind":
				p.definition.Resource.Kind = "network"
			case "fingerprint":
				p.definition.Fingerprint = ""
			case "definition-hash":
				p.definition.DefinitionSHA256 = "not-a-digest"
			}
			if _, err := s.planRemoval(context.Background(), 1000, Request{Connection: p.VM.Key.ConnectionID, ID: p.VM.Key.UUID, Action: "remove"}); err == nil {
				t.Fatal("unsupported/mismatched removal inspection accepted")
			}
			removalNoPlans(t, s)
			if p.removals.Load() != 0 {
				t.Fatal("preview performed removal")
			}
		})
	}
}

func TestRemovalPlanSchemaDigestAndFreshInspection(t *testing.T) {
	for _, changed := range []string{"fingerprint", "definition-hash", "retained-sources", "name"} {
		t.Run(changed, func(t *testing.T) {
			s, p, _ := removalService(t)
			plan := removalPlan(t, s, p)
			raw, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			if err := validation.Schema("operation-plan", raw); err != nil {
				t.Fatal(err)
			}
			digest, err := operations.PlanDigest(plan)
			if err != nil || digest != plan.Digest {
				t.Fatal("removal plan digest mismatch", err)
			}
			if plan.Operation != "vm.remove-definition-v1" || !reflect.DeepEqual(plan.ResourceIDs, []string{p.VM.Key.String()}) || plan.Before[p.VM.Key.String()] != p.definition.Fingerprint {
				t.Fatal("plan resource binding differs", plan)
			}
			switch changed {
			case "fingerprint":
				p.definition.Fingerprint = strings.Repeat("c", 64)
			case "definition-hash":
				p.definition.DefinitionSHA256 = strings.Repeat("c", 64)
			case "retained-sources":
				p.definition.RetainedSources = append(p.definition.RetainedSources, "/fixture/extra.qcow2")
			case "name":
				p.definition.Name = "renamed"
			}
			_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "stale-removal", Acknowledgements: plan.Acknowledgements})
			if err == nil || p.removals.Load() != 0 || p.inspections.Load() < 2 {
				t.Fatal("stale definition removed", err)
			}
		})
	}
}

func TestRemovalRecipeResourceAndFingerprintBindingsRefuseBeforeInspection(t *testing.T) {
	for _, fault := range []string{"resource", "fingerprint", "version"} {
		t.Run(fault, func(t *testing.T) {
			s, p, _ := removalService(t)
			plan := removalPlan(t, s, p)
			_, raw, err := s.Engine.Store.Plan(plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			var recipe removalRecipe
			if err := json.Unmarshal(raw, &recipe); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "resource":
				recipe.Definition.Resource.UUID = "1d98a1a3-e8fb-4e88-bfa0-ac466a733e39"
			case "fingerprint":
				recipe.Definition.Fingerprint = strings.Repeat("c", 64)
			case "version":
				recipe.Version = 0
			}
			raw, err = json.Marshal(recipe)
			if err != nil {
				t.Fatal(err)
			}
			before := p.inspections.Load()
			if err := (&removalHandler{s: s}).Validate(context.Background(), plan, raw); err == nil || p.inspections.Load() != before || p.removals.Load() != 0 {
				t.Fatal("unbound recipe reached provider", err)
			}
		})
	}
}

func TestRemovalSharedDispatchProducesRetainedStorageReview(t *testing.T) {
	s, p, _ := removalService(t)
	response := s.Call(context.Background(), 1000, "vm.remove", Request{Connection: p.VM.Key.ConnectionID, ID: p.VM.Key.UUID})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	raw, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	var plan domain.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Operation != "vm.remove-definition-v1" || plan.Review["diskDeletion"] != false || plan.Review["backupsDeleted"] != false || plan.Review["backupCreated"] != false || plan.Review["automaticStop"] != false || plan.Review["requiresStopped"] != true || plan.Review["configurationRemoved"] != true {
		t.Fatal("shared removal review obscures retention or mutation scope", plan)
	}
	if p.removals.Load() != 0 {
		t.Fatal("shared preview executed removal")
	}
}

func TestRemovalInventoryHookFailureAndConflictingRecordsRefuse(t *testing.T) {
	for _, phase := range []string{"plan", "validate"} {
		for _, fault := range []string{"error", "resource", "name", "fingerprint", "running", "transient", "autostart", "managed-save"} {
			t.Run(phase+"/"+fault, func(t *testing.T) {
				s, p, _ := removalService(t)
				var plan domain.Plan
				if phase == "validate" {
					plan = removalPlan(t, s, p)
				}
				hookFailure := domain.Fail("SOURCE_CHANGED", "fixture conflicting ownership journal")
				var calls atomic.Int32
				s.InventoryVM = func(_ context.Context, vm domain.VM) (domain.VM, error) {
					calls.Add(1)
					switch fault {
					case "error":
						return domain.VM{}, hookFailure
					case "resource":
						vm.Key.UUID = "1d98a1a3-e8fb-4e88-bfa0-ac466a733e39"
					case "name":
						vm.Name = "conflicting-name"
					case "fingerprint":
						vm.Fingerprint = strings.Repeat("c", 64)
					case "running":
						vm.State = "running"
					case "transient":
						vm.PersistentXML = ""
					case "autostart":
						vm.Autostart = true
					case "managed-save":
						vm.HasManagedSave = true
					}
					return vm, nil
				}
				var err error
				if phase == "plan" {
					_, err = s.planRemoval(context.Background(), 1000, Request{Connection: p.VM.Key.ConnectionID, ID: p.VM.Key.UUID, Action: "remove"})
					removalNoPlans(t, s)
				} else {
					_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "inventory-conflict", Acknowledgements: plan.Acknowledgements})
				}
				if err == nil || calls.Load() == 0 || p.removals.Load() != 0 {
					t.Fatal("conflicting inventory record accepted or bypassed", err)
				}
				if fault == "error" && !errors.Is(err, hookFailure) {
					t.Fatal("inventory-hook failure was replaced", err)
				}
				var jobs int
				if err := s.Engine.Store.DB.QueryRow("SELECT count(*) FROM jobs").Scan(&jobs); err != nil || jobs != 0 {
					t.Fatal("inventory refusal accepted an operation", jobs, err)
				}
			})
		}
	}
}

func TestRemovalRechecksInventoryAfterValidationBeforeProviderEffect(t *testing.T) {
	for _, fault := range []string{"ownership-error", "autostart-changed"} {
		t.Run(fault, func(t *testing.T) {
			s, p, _ := removalService(t)
			plan := removalPlan(t, s, p)
			var executionReads atomic.Int32
			s.InventoryVM = func(ctx context.Context, vm domain.VM) (domain.VM, error) {
				if id := operations.OperationID(ctx); id != "" {
					job, err := s.Engine.Store.Job(id)
					if err != nil {
						return domain.VM{}, err
					}
					// Apply and worker validation have already accepted the original
					// inventory. Change only the read after durable effect intent.
					if job.State == "running" {
						executionReads.Add(1)
						if fault == "ownership-error" {
							return domain.VM{}, domain.Fail("SOURCE_CHANGED", "fixture ownership changed after validation")
						}
						vm.Autostart = true
					}
				}
				return vm, nil
			}
			job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
			if job.State != "recovery-required" || executionReads.Load() != 1 || p.removals.Load() != 0 || p.absent.Load() {
				t.Fatal("last-boundary inventory drift reached removal", job, executionReads.Load(), p.removals.Load())
			}
			raw, err := s.Engine.Store.MetadataBytes("vm-removal-receipt", job.ID)
			if err != nil || len(raw) != 0 {
				t.Fatal("pre-effect refusal wrote removal receipt", err)
			}
		})
	}
}

func TestRemovalFailedAcknowledgementAndAbsentDefinitionNeverProveSuccess(t *testing.T) {
	s, p, _ := removalService(t)
	p.lostAcknowledgement = true
	plan := removalPlan(t, s, p)
	job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
	if job.State != "recovery-required" || !p.absent.Load() || p.removals.Load() != 1 {
		t.Fatal(job)
	}
	raw, err := s.Engine.Store.MetadataBytes("vm-removal-receipt", job.ID)
	if err != nil || len(raw) != 0 {
		t.Fatal("failed effect wrote a success receipt", string(raw), err)
	}
	if _, err := s.Engine.Reconcile(context.Background(), job.ID); err == nil || p.removals.Load() != 1 {
		t.Fatal("absence accepted as receipt or removal replayed", err)
	}
}

func TestRemovalMissingOrMismatchedReceiptNeverUsesAbsenceOrReplays(t *testing.T) {
	for _, fault := range []string{"missing", "operationID", "planID", "planDigest", "definition-fingerprint", "definition-resource"} {
		t.Run(fault, func(t *testing.T) {
			s, p, _ := removalService(t)
			plan := removalPlan(t, s, p)
			job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
			if job.State != "succeeded" {
				t.Fatal(job)
			}
			s.Engine.Close()
			job.State = "recovery-required"
			if err := s.Engine.Store.Update(job, "fixture lost terminal acknowledgement"); err != nil {
				t.Fatal(err)
			}
			if fault == "missing" {
				if _, err := s.Engine.Store.DB.Exec("DELETE FROM metadata WHERE kind=? AND id=?", "vm-removal-receipt", job.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				raw, err := s.Engine.Store.MetadataBytes("vm-removal-receipt", job.ID)
				if err != nil {
					t.Fatal(err)
				}
				var receipt map[string]any
				if err := json.Unmarshal(raw, &receipt); err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "definition-fingerprint":
					receipt["definition"].(map[string]any)["fingerprint"] = strings.Repeat("c", 64)
				case "definition-resource":
					receipt["definition"].(map[string]any)["resource"].(map[string]any)["resourceUUID"] = "1d98a1a3-e8fb-4e88-bfa0-ac466a733e39"
				default:
					receipt[fault] = "mismatched-fixture-binding"
				}
				if err := s.Engine.Store.ComparePut("vm-removal-receipt", job.ID, raw, receipt); err != nil {
					t.Fatal(err)
				}
			}
			if !p.absent.Load() {
				t.Fatal("fixture should remain absent")
			}
			if _, err := s.Engine.Reconcile(context.Background(), job.ID); err == nil || p.removals.Load() != 1 {
				t.Fatal("invalid durable evidence accepted or removal replayed", err)
			}
		})
	}
}

func TestRemovalReceiptSurvivesReopenWithoutReplayAndRefusesReplacement(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "replacement"}[replacement], func(t *testing.T) {
			s, p, path := removalService(t)
			plan := removalPlan(t, s, p)
			request := operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "one-removal", Acknowledgements: plan.Acknowledgements}
			job, err := s.Engine.Apply(context.Background(), 1000, request)
			if err != nil {
				t.Fatal(err)
			}
			job = awaitConfig(t, s, job.ID)
			if job.State != "succeeded" || p.removals.Load() != 1 {
				t.Fatal(job)
			}
			repeat, err := s.Engine.Apply(context.Background(), 1000, request)
			if err != nil || repeat.ID != job.ID || p.removals.Load() != 1 {
				t.Fatal("idempotency replay", err)
			}
			raw, err := s.Engine.Store.MetadataBytes("vm-removal-receipt", job.ID)
			if err != nil || len(raw) == 0 {
				t.Fatal("missing durable receipt", err)
			}
			var receipt map[string]any
			if err := json.Unmarshal(raw, &receipt); err != nil || receipt["operationID"] != job.ID || receipt["planID"] != plan.ID || receipt["planDigest"] != plan.Digest {
				t.Fatal("receipt job/plan binding differs", receipt, err)
			}
			s.Engine.Close()
			job.State = "recovery-required"
			if err := s.Engine.Store.Update(job, "fixture lost terminal acknowledgement"); err != nil {
				t.Fatal(err)
			}
			s.Engine.Store.Close()
			db, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			s.Engine = operations.New(db)
			s.Engine.Handlers["vm.remove-definition-v1"] = &removalHandler{s: s}
			p.absent.Store(!replacement)
			got, err := s.Engine.Reconcile(context.Background(), job.ID)
			if replacement {
				if err == nil || got.State == "succeeded" {
					t.Fatal("replacement definition falsely declared removed", got, err)
				}
			} else if err != nil || got.State != "succeeded" {
				t.Fatal("durable receipt recovery failed", got, err)
			}
			if p.removals.Load() != 1 {
				t.Fatal("recovery replayed removal")
			}
		})
	}
}
