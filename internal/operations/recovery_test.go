package operations

import (
	"context"
	"errors"
	"testing"
	"virmill.local/core/internal/domain"
)

type inheritedEffect struct {
	parent, failure string
	calls           int
}

func (h *inheritedEffect) RecoveryParent(context.Context, domain.Plan, []byte) (string, error) {
	return h.parent, nil
}
func (h *inheritedEffect) Validate(ctx context.Context, _ domain.Plan, _ []byte) error {
	if h.failure == "validation" && OperationID(ctx) != "" {
		return errors.New("fixture precondition changed after recovery acceptance")
	}
	return nil
}
func (h *inheritedEffect) Execute(context.Context, domain.Plan, []byte, domain.Step) error {
	h.calls++
	return ErrCanceledSafely
}
func (h *inheritedEffect) Reconcile(context.Context, domain.Plan, []byte, domain.Step) (bool, error) {
	return false, nil
}

func TestInheritedUncertainLocksSurviveEarlyRecoveryExit(t *testing.T) {
	for _, failure := range []string{"queued-cancellation", "validation", "safe-cancellation"} {
		t.Run(failure, func(t *testing.T) {
			e, _ := openEngine(t, t.TempDir())
			prior := makePlan(t, e)
			parent, err := e.Store.Accept(prior, "parent-fixture", "parent-digest")
			if err != nil {
				t.Fatal(err)
			}
			parent.State = "recovery-required"
			if err = e.Store.Update(parent, "synthetic prior uncertain effect"); err != nil {
				t.Fatal(err)
			}
			h := &inheritedEffect{parent: parent.ID, failure: failure}
			e.Handlers["fixture.recovery"] = h
			p, err := e.Plan(context.Background(), 1000, prior.ConnectionID, "fixture.recovery", prior.ResourceIDs, nil, map[string]any{"parent": parent.ID}, prior.Steps, []string{"review-recovery"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var child domain.Job
			if failure == "queued-cancellation" {
				child, err = e.Store.AcceptRecovery(p, "child", "digest", parent.ID)
				if err != nil {
					t.Fatal(err)
				}
				child.CancelRequested = true
				if err = e.Store.Update(child, "fixture queued cancellation"); err != nil {
					t.Fatal(err)
				}
				_, b, err := e.Store.Plan(p.ID)
				if err != nil {
					t.Fatal(err)
				}
				e.run(child, p, b, h)
			} else {
				child, err = e.Apply(context.Background(), 1000, ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "child", Acknowledgements: p.Acknowledgements})
				if err != nil {
					t.Fatal(err)
				}
			}
			child = waitJob(t, e, child.ID)
			if child.State != "recovery-required" {
				t.Fatal("early recovery exit released uncertain predecessor", child)
			}
			for _, resource := range prior.ResourceIDs {
				owners, err := e.Store.ResourceJobs(resource)
				if err != nil || len(owners) != 1 || owners[0] != child.ID {
					t.Fatal("inherited lock released", owners, err)
				}
			}
			if failure != "safe-cancellation" && h.calls != 0 {
				t.Fatal("early exit executed an effect")
			}
		})
	}
}
