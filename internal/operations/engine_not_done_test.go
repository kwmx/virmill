package operations

import (
	"context"
	"sync/atomic"
	"testing"

	"virmill.local/core/internal/domain"
)

// unanswered does nothing on its first execution, like a guest that ignores a
// shutdown request, and takes effect on the second.
type unanswered struct{ calls atomic.Int32 }

func (h *unanswered) Validate(context.Context, domain.Plan, []byte) error { return nil }
func (h *unanswered) Execute(context.Context, domain.Plan, []byte, domain.Step) error {
	if h.calls.Add(1) == 1 {
		return NotDone(domain.Fail("WAIT_TIMEOUT", "guest did not answer"))
	}
	return nil
}
func (h *unanswered) Reconcile(context.Context, domain.Plan, []byte, domain.Step) (bool, error) {
	return h.calls.Load() > 1, nil
}

func TestNotDoneStepFailsAndFreesItsResources(t *testing.T) {
	e, _ := openEngine(t, t.TempDir())
	e.Handlers["fixture.effect"] = &unanswered{}
	first := makePlan(t, e)
	j, err := e.Apply(context.Background(), 1000, ApplyRequest{first.ID, first.Digest, "first", []string{"fixture-effect"}})
	if err != nil {
		t.Fatal(err)
	}
	if j = waitJob(t, e, j.ID); j.State != "failed" || j.Error == nil || j.Error.Code != "WAIT_TIMEOUT" || j.Error.Message != "guest did not answer" {
		t.Fatal(j.State, j.Error)
	}
	second := makePlan(t, e)
	j, err = e.Apply(context.Background(), 1000, ApplyRequest{second.ID, second.Digest, "second", []string{"fixture-effect"}})
	if err != nil {
		t.Fatal("the failed job kept its resource locked:", err)
	}
	if j = waitJob(t, e, j.ID); j.State != "succeeded" {
		t.Fatal(j.State, j.Error)
	}
}
