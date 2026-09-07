package operations

import (
	"context"
	"errors"
	"testing"
	"virmill.local/core/internal/domain"
)

type estimatedEffect struct {
	*effect
	estimates int
	fail      bool
}

func (h *estimatedEffect) Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error) {
	h.estimates++
	if h.fail {
		return domain.Estimates{}, errors.New("fixture estimate unavailable")
	}
	return domain.Estimates{RequiresDowntime: true, Notes: "fixture estimate"}, nil
}
func TestEstimateFailureDoesNotPersistPlanOrExecute(t *testing.T) {
	e, h := openEngine(t, t.TempDir())
	estimated := &estimatedEffect{effect: h, fail: true}
	e.Handlers["fixture.effect"] = estimated
	_, err := e.Plan(context.Background(), 1000, "fixture", "fixture.effect", nil, nil, map[string]any{}, []domain.Step{{ID: "effect"}}, nil, nil)
	if err == nil {
		t.Fatal("failed estimate accepted")
	}
	var count int
	if err = e.Store.DB.QueryRow("SELECT count(*) FROM plans").Scan(&count); err != nil || count != 0 || h.calls.Load() != 0 {
		t.Fatal("failed estimate had a durable side effect", count, err)
	}
}
func TestNewEstimatorDoesNotRewritePreviouslyReviewedPlan(t *testing.T) {
	e, h := openEngine(t, t.TempDir())
	old := makePlan(t, e)
	if old.Estimates != (domain.Estimates{}) {
		t.Fatal("fixture does not represent legacy default estimates")
	}
	estimated := &estimatedEffect{effect: h}
	e.Handlers["fixture.effect"] = estimated
	stored, _, err := e.Store.Plan(old.ID)
	if err != nil || stored.Digest != old.Digest || stored.Estimates != old.Estimates {
		t.Fatal("old review changed", err)
	}
	job, err := e.Apply(context.Background(), 1000, ApplyRequest{PlanID: old.ID, PlanDigest: old.Digest, IdempotencyKey: "legacy-estimate", Acknowledgements: old.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitJob(t, e, job.ID); result.State != "succeeded" || estimated.estimates != 0 || h.calls.Load() != 1 {
		t.Fatal("old intent re-estimated or replayed", result, estimated.estimates)
	}
	fresh := makePlan(t, e)
	if !fresh.Estimates.RequiresDowntime || estimated.estimates != 1 {
		t.Fatal("new plan omitted estimate", fresh)
	}
}
