//go:build linux && amd64

package creating

import (
	"context"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

func failedDefinition(t *testing.T) (*Service, *fixtureBackend, domain.Plan, domain.Job) {
	t.Helper()
	s, backend, request, _ := creationFixture(t)
	backend.fail = "define"
	p, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal("fixture did not interrupt definition", j)
	}
	return s, backend, p, j
}

func TestReviewedResumeReverifiesAndNeverReallocatesOrUploads(t *testing.T) {
	s, backend, original, parent := failedDefinition(t)
	backend.mu.Lock()
	backend.fail = ""
	backend.mu.Unlock()
	p, err := s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if backend.allocated != 2 || backend.populated != 2 || backend.defined {
		t.Fatal("preview repeated effects")
	}
	child := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if child.State != "succeeded" || child.RecoveryOf != parent.ID {
		t.Fatal("recovery did not complete", child)
	}
	parent, err = s.Store.Job(parent.ID)
	if err != nil || parent.State != "partial" || parent.RecoveryOperationID != child.ID {
		t.Fatal("original falsely completed or unlinked", parent, err)
	}
	if backend.allocated != 2 || backend.populated != 2 || !backend.defined {
		t.Fatal("recovery replayed storage work", backend)
	}
	for _, resource := range original.ResourceIDs {
		owners, err := s.Store.ResourceJobs(resource)
		if err != nil || len(owners) != 0 {
			t.Fatal("successful recovery did not release locks", owners, err)
		}
	}
	if _, err = s.Result(context.Background(), 1000, child.ID); err != nil {
		t.Fatal("recovery result inaccessible", err)
	}
	if _, err = s.Result(context.Background(), 1000, parent.ID); err == nil {
		t.Fatal("partial parent falsely reported successful")
	}
	t.Log("synthetic copied-volume coordinator only; no native stream, hardware, or guest evidence")
}

func TestFailedRecoveryRetainsLocksAndAllowsAnotherReviewedRecovery(t *testing.T) {
	s, backend, original, parent := failedDefinition(t)
	p, err := s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	child := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if child.State != "recovery-required" {
		t.Fatal("expected definition failure", child)
	}
	for _, resource := range original.ResourceIDs {
		owners, err := s.Store.ResourceJobs(resource)
		if err != nil || len(owners) != 1 || owners[0] != child.ID {
			t.Fatal("recovery lost inherited locks", owners, err)
		}
	}
	backend.mu.Lock()
	backend.fail = "ack"
	backend.mu.Unlock()
	p, err = s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: child.ID})
	if err != nil {
		t.Fatal(err)
	}
	grandchild := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if grandchild.State != "recovery-required" {
		t.Fatal("lost acknowledgement not uncertain", grandchild)
	}
	// A restarted coordinator observes its durable re-verification proof and
	// backend definition. The in-memory backend remains explicitly synthetic.
	s.Engine.Close()
	s.Engine = operations.New(s.Store)
	s.Engine.Handlers["vm.create"] = s
	s.Engine.Handlers["vm.create.resume"] = &resumeHandler{s: s}
	t.Cleanup(s.Engine.Close)
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Engine.Reconcile(context.Background(), grandchild.ID)
	if err != nil || recovered.State != "succeeded" {
		t.Fatal("lost acknowledgement did not reconcile", recovered, err)
	}
	if backend.allocated != 2 || backend.populated != 2 || backend.definitions != 3 {
		t.Fatal("reconciliation replayed an effect", backend)
	}
}

func TestPartialSetWrongActorStaleTargetAndTamperedRetainedBytesRefuseRecovery(t *testing.T) {
	s, backend, _, parent := failedDefinition(t)
	if _, err := s.PlanResume(context.Background(), 1001, app.Request{Connection: "fixture", ID: parent.ID}); err == nil {
		t.Fatal("wrong actor got recovery authority")
	}
	p, err := s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.caps = "changed"
	backend.mu.Unlock()
	if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("stale recovery target accepted")
	}
	backend.mu.Lock()
	backend.caps = "fixture-before"
	backend.fail = ""
	for key := range backend.volumes {
		backend.volumes[key][0] = 'X'
	}
	backend.mu.Unlock()
	child := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if child.State != "recovery-required" || backend.defined {
		t.Fatal("tampered volume accepted", child)
	}
	if _, err = s.Engine.Reconcile(context.Background(), child.ID); err == nil {
		t.Fatal("recovery without durable re-verification completed")
	}

	s2, b2, request, _ := creationFixture(t)
	b2.fail = "populate"
	p2, err := s2.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	j2 := awaitCreation(t, s2, applyCreation(t, s2, p2).ID)
	if _, err = s2.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: j2.ID}); err == nil {
		t.Fatal("partly uploaded set accepted for definition")
	}
}
