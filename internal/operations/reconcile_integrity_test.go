package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
)

type integrityObservation struct{ executions, observations int }

func (*integrityObservation) Validate(context.Context, domain.Plan, []byte) error { return nil }
func (h *integrityObservation) Execute(context.Context, domain.Plan, []byte, domain.Step) error {
	h.executions++
	return errors.New("this fixture has no executable effect")
}
func (h *integrityObservation) Reconcile(context.Context, domain.Plan, []byte, domain.Step) (bool, error) {
	h.observations++
	return true, nil // Synthetic observation only; no filesystem or native effect.
}

func TestReconcileRequiresImmutableRecipeButNotUnexpiredAuthorization(t *testing.T) {
	for _, scenario := range []string{"expired unchanged", "changed plan", "changed recipe", "duplicate recipe keys", "malformed recipe"} {
		t.Run(scenario, func(t *testing.T) {
			e, _ := openEngine(t, t.TempDir())
			h := &integrityObservation{}
			e.Handlers["fixture.effect"] = h
			p := makePlan(t, e)
			// Construct a synthetic previously accepted, now-expired journal
			// entry through Store. This is not a new Apply of an expired plan.
			p.CreatedAt = time.Now().Add(-2 * time.Hour)
			p.ExpiresAt = p.CreatedAt.Add(15 * time.Minute)
			var err error
			p.Digest, err = PlanDigest(p)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.Store.DB.Exec("UPDATE plans SET body=? WHERE id=?", body, p.ID); err != nil {
				t.Fatal(err)
			}
			j, err := e.Store.Accept(p, "synthetic-prior-acceptance", "synthetic-request")
			if err != nil {
				t.Fatal(err)
			}
			j.State = "recovery-required"
			if err = e.Store.Update(j, "synthetic interrupted observation"); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "changed plan":
				p.Operation = "changed-operation"
				body, err = json.Marshal(p)
				if err == nil {
					_, err = e.Store.DB.Exec("UPDATE plans SET body=? WHERE id=?", body, p.ID)
				}
			case "changed recipe", "duplicate recipe keys", "malformed recipe":
				changed := map[string]string{"changed recipe": `{"intent":"different"}`, "duplicate recipe keys": `{"intent":"local test file","intent":"local test file"}`, "malformed recipe": `{"intent":`}[scenario]
				_, err = e.Store.DB.Exec("UPDATE plans SET input=? WHERE id=?", []byte(changed), p.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := e.Store.Job(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Reconcile(context.Background(), j.ID)
			if scenario == "expired unchanged" {
				if err != nil || result.State != "succeeded" || h.observations != 1 {
					t.Fatal("unchanged expired recipe cannot be reconciled", result, err, h.observations)
				}
			} else {
				var failure *domain.Error
				if !errors.As(err, &failure) || failure.Code != "SOURCE_CHANGED" || h.observations != 0 {
					t.Fatal("corrupt plan reached recovery handler", result, err, h.observations)
				}
				after, readErr := e.Store.Job(j.ID)
				beforeBytes, _ := json.Marshal(before)
				afterBytes, _ := json.Marshal(after)
				if readErr != nil || string(beforeBytes) != string(afterBytes) {
					t.Fatal("refusal rewrote uncertain job", readErr)
				}
			}
			locks, lockErr := e.Store.ResourceJobs("test-resource")
			wantLocks := 1
			if scenario == "expired unchanged" {
				wantLocks = 0
			}
			if lockErr != nil || len(locks) != wantLocks || (wantLocks == 1 && locks[0] != j.ID) || h.executions != 0 {
				t.Fatal("unexpected lock disposition or effect replay", locks, lockErr, h.executions)
			}
		})
	}
}
