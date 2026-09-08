package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

type rebootFixture struct {
	fixtureProvider
	reboots atomic.Int32
	fault   string
}

func (p *rebootFixture) Reboot(ctx context.Context, uri, id, fingerprint string) (domain.RebootObservation, error) {
	p.reboots.Add(1)
	if uri != p.VM.Key.ConnectionID || id != p.VM.Key.UUID || fingerprint != p.VM.Fingerprint {
		return domain.RebootObservation{}, domain.Fail("STALE_PLAN", "fixture mismatch")
	}
	if p.fault == "lost-ack" {
		return domain.RebootObservation{}, domain.Fail("RECOVERY_REQUIRED", "generated lost acknowledgement")
	}
	r := domain.RebootObservation{Resource: p.VM.Key, ObservedAt: time.Now(), Evidence: "libvirt-reboot-event"}
	if p.fault == "wrong-vm" {
		r.Resource.UUID = "other"
	}
	if p.fault == "old-event" {
		r.ObservedAt = r.ObservedAt.Add(-time.Hour)
	}
	return r, nil
}
func rebootService(t *testing.T) (*Service, *rebootFixture, string) {
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
	p := &rebootFixture{fixtureProvider: fixtureProvider{VM: domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///session", Kind: "vm", UUID: "0d98a1a3-e8fb-4e88-bfa0-ac466a733e39"}, State: "running", Fingerprint: "before"}}}
	s := New(p, operations.New(db))
	t.Cleanup(func() { s.Engine.Close(); s.Engine.Store.Close() })
	return s, p, path
}
func rebootPlan(t *testing.T, s *Service, p *rebootFixture) domain.Plan {
	t.Helper()
	r, err := s.planVM(context.Background(), 1000, Request{Connection: p.VM.Key.ConnectionID, ID: p.VM.Key.UUID, Action: "reboot"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestRebootReceiptsDeduplicateAndReconcileAfterReopen(t *testing.T) {
	s, p, path := rebootService(t)
	plan := rebootPlan(t, s, p)
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Schema("operation-plan", raw); err != nil {
		t.Fatal("reboot plan violates public schema", err)
	}
	if !plan.Estimates.RequiresDowntime || plan.Review["hardStopFallback"] != false {
		t.Fatal("incomplete review", plan)
	}
	request := operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "one-reboot", Acknowledgements: plan.Acknowledgements}
	job, err := s.Engine.Apply(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	job = awaitConfig(t, s, job.ID)
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	repeated, err := s.Engine.Apply(context.Background(), 1000, request)
	if err != nil || repeated.ID != job.ID || p.reboots.Load() != 1 {
		t.Fatal("replayed reboot", repeated, err)
	}
	// Simulate crash after durable event receipt but before terminal acknowledgement.
	s.Engine.Close()
	job.State = "recovery-required"
	if err := s.Engine.Store.Update(job, "generated lost terminal acknowledgement"); err != nil {
		t.Fatal(err)
	}
	s.Engine.Store.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(db)
	s.Engine.Handlers["vm.reboot"] = &rebootHandler{s: s}
	got, err := s.Engine.Reconcile(context.Background(), job.ID)
	if err != nil || got.State != "succeeded" || p.reboots.Load() != 1 {
		t.Fatal("receipt recovery replayed", got, err)
	}
}
func TestRebootUncertaintyNeverUsesRunningStateOrReplays(t *testing.T) {
	for _, fault := range []string{"lost-ack", "wrong-vm", "old-event"} {
		t.Run(fault, func(t *testing.T) {
			s, p, _ := rebootService(t)
			p.fault = fault
			plan := rebootPlan(t, s, p)
			job := awaitConfig(t, s, applyConfig(t, s, plan).ID)
			if job.State != "recovery-required" || p.VM.State != "running" {
				t.Fatal(job)
			}
			if _, err := s.Engine.Reconcile(context.Background(), job.ID); err == nil || p.reboots.Load() != 1 {
				t.Fatal("unproven reboot accepted/replayed", err)
			}
			var count int
			if err := s.Engine.Store.DB.QueryRow("SELECT count(*) FROM locks WHERE job_id=?", job.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("uncertainty lost lock", count, err)
			}
		})
	}
}
func TestRebootStalePlanAndMissingAcknowledgementRefuseBeforeEffect(t *testing.T) {
	s, p, _ := rebootService(t)
	plan := rebootPlan(t, s, p)
	r := operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "bad"}
	if _, err := s.Engine.Apply(context.Background(), 1000, r); err == nil {
		t.Fatal("missing acknowledgements accepted")
	}
	r.Acknowledgements = plan.Acknowledgements
	p.VM.Fingerprint = "external-change"
	if _, err := s.Engine.Apply(context.Background(), 1000, r); err == nil || p.reboots.Load() != 0 {
		t.Fatal("stale reboot executed", err)
	}
}
func TestRebootRejectedInputsCreateNoPlan(t *testing.T) {
	for _, fault := range []string{"stopped", "secret", "unsupported"} {
		t.Run(fault, func(t *testing.T) {
			s, p, _ := rebootService(t)
			r := Request{Connection: p.VM.Key.ConnectionID, ID: p.VM.Key.UUID, Action: "reboot"}
			switch fault {
			case "stopped":
				p.VM.State = "stopped"
			case "secret":
				r.Input = map[string]any{"password": "fixture-secret"}
			case "unsupported":
				s.Provider = &p.fixtureProvider
			}
			if _, err := s.planVM(context.Background(), 1000, r); err == nil {
				t.Fatal("invalid reboot accepted")
			}
			var count int
			if err := s.Engine.Store.DB.QueryRow("SELECT count(*) FROM plans").Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected input stored", count, err)
			}
		})
	}
}
