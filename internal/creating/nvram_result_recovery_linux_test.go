//go:build linux && amd64

package creating

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// The real recovery coordinator uses the original creation binding while the
// returned operation is the child. Both immutable recipes must be checked.
// All backend observations and disk contents here are generated fixtures.
func TestNVRAMRecoveryResultRequiresUnchangedChildAndOriginalRecipes(t *testing.T) {
	for _, changed := range []string{"child plan", "child recipe"} {
		t.Run(changed, func(t *testing.T) {
			s, f, request, _ := nvramLifecycleFixture(t)
			f.fail = "define"
			original, err := s.Plan(context.Background(), 1000, request)
			if err != nil {
				t.Fatal(err)
			}
			parent := awaitCreation(t, s, applyCreation(t, s, original).ID)
			if parent.State != "recovery-required" {
				t.Fatal("expected synthetic failed definition", parent)
			}
			f.mu.Lock()
			f.fail = ""
			f.mu.Unlock()
			plan, err := s.PlanResume(context.Background(), 1000, app.Request{Connection: request.Connection, ID: parent.ID})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Review["nvramDeclarationVersion"] != 1 || plan.Review["nvramInitializationVerified"] != false {
				t.Fatal("recovery review omitted the original declaration policy", plan.Review)
			}
			child := awaitCreation(t, s, applyCreation(t, s, plan).ID)
			if child.State != "succeeded" || child.RecoveryOf != parent.ID {
				t.Fatal("fixture did not complete original declaration through recovery", child)
			}
			nvramLifecycleResult(t, s, child, true, true, "declaration-bound")
			plan, recipe, err := s.Store.Plan(plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			var body []byte
			if err = s.Store.DB.QueryRow("SELECT body FROM plans WHERE id=?", plan.ID).Scan(&body); err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "child plan":
				corrupt := plan
				corrupt.Risks = append(append([]string(nil), plan.Risks...), "unreviewed changed child plan")
				encoded, marshalErr := json.Marshal(corrupt)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				_, err = s.Store.DB.Exec("UPDATE plans SET body=? WHERE id=?", encoded, plan.ID)
			case "child recipe":
				var corrupt resumeInput
				if err = json.Unmarshal(recipe, &corrupt); err != nil {
					t.Fatal(err)
				}
				corrupt.ParentOperationID = domain.ID()
				encoded, marshalErr := json.Marshal(corrupt)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				_, err = s.Store.DB.Exec("UPDATE plans SET input=? WHERE id=?", encoded, plan.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			proofBefore, err := s.Store.MetadataBytes(nvramDeclarationKind, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			observations, inspections := f.observations, f.inspections
			allocations, uploads, definitions := f.allocated, f.populated, f.definitions
			f.mu.Unlock()
			data, err := s.Result(context.Background(), 1000, child.ID)
			var failure *domain.Error
			if data != nil || !errors.As(err, &failure) || failure.Code != "SOURCE_CHANGED" {
				t.Fatalf("changed recovery identity produced result data: data=%v error=%v", data, err)
			}
			proofAfter, proofErr := s.Store.MetadataBytes(nvramDeclarationKind, original.ID)
			if proofErr != nil || !bytes.Equal(proofBefore, proofAfter) {
				t.Fatal("result refusal changed original declaration", proofErr)
			}
			got, jobErr := s.Store.Job(child.ID)
			if jobErr != nil || !same(got, child) {
				t.Fatal("result refusal rewrote child", got, jobErr)
			}
			nvramLifecycleLocks(t, s, original, "")
			f.mu.Lock()
			unchanged := f.observations == observations && f.inspections == inspections && f.allocated == allocations && f.populated == uploads && f.definitions == definitions
			f.mu.Unlock()
			if !unchanged {
				t.Fatal("result invoked native observation or replayed effects")
			}
			if _, err = s.Store.DB.Exec("UPDATE plans SET body=?,input=? WHERE id=?", body, recipe, plan.ID); err != nil {
				t.Fatal(err)
			}
			nvramLifecycleResult(t, s, child, true, true, "declaration-bound")
		})
	}
}
