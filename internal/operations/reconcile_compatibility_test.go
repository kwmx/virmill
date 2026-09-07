package operations

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
)

var errCompatibilityObserved = errors.New("synthetic observer reached; effect remains unproven")

// This observer never reports effect completion. Tests exercise integrity,
// dispatch and persistence only; Execute must never be reached.
type compatibilityObserver struct {
	executions, observations int
	plan                     domain.Plan
	input                    []byte
	step                     domain.Step
	operationID              string
}

func (*compatibilityObserver) Validate(context.Context, domain.Plan, []byte) error { return nil }
func (h *compatibilityObserver) Execute(context.Context, domain.Plan, []byte, domain.Step) error {
	h.executions++
	return errors.New("compatibility fixture cannot execute an effect")
}
func (h *compatibilityObserver) Reconcile(ctx context.Context, p domain.Plan, input []byte, step domain.Step) (bool, error) {
	h.observations++
	h.plan, h.input, h.step, h.operationID = p, append([]byte{}, input...), step, OperationID(ctx)
	return false, errCompatibilityObserved
}

func compatibilityFixture(t *testing.T) (*Engine, *compatibilityObserver, domain.Plan, domain.Job) {
	t.Helper()
	e, _ := openEngine(t, t.TempDir())
	h := &compatibilityObserver{}
	e.Handlers["fixture.effect"] = h
	p := makePlan(t, e)
	j, err := e.Store.Accept(p, "compatibility-accepted", "synthetic-request")
	if err != nil {
		t.Fatal(err)
	}
	j.State = "recovery-required"
	j.Error = domain.Fail("RECOVERY_REQUIRED", "synthetic uncertain prior observation")
	if err = e.Store.Update(j, "synthetic uncertainty; no effect executed"); err != nil {
		t.Fatal(err)
	}
	return e, h, p, j
}

// Compare raw rows so a malformed body remains checkable, and so event order,
// digests, metadata and exact lock owners cannot change unnoticed on refusal.
func compatibilitySnapshot(t *testing.T, e *Engine) map[string][][]string {
	t.Helper()
	out := map[string][][]string{}
	for name, query := range map[string]string{
		"plans":    "SELECT id,digest,body,input FROM plans ORDER BY id",
		"jobs":     "SELECT id,plan_id,body FROM jobs ORDER BY id",
		"events":   "SELECT job_id,seq,body FROM events ORDER BY job_id,seq",
		"locks":    "SELECT resource,job_id FROM locks ORDER BY resource",
		"dedup":    "SELECT key,request_digest,job_id,created_at FROM dedup ORDER BY key",
		"metadata": "SELECT kind,id,body FROM metadata ORDER BY kind,id",
	} {
		rows, err := e.Store.DB.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]string, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err = rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			out[name] = append(out[name], values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func compatibilityUnchanged(t *testing.T, e *Engine, before map[string][][]string) {
	t.Helper()
	if after := compatibilitySnapshot(t, e); !reflect.DeepEqual(after, before) {
		for table := range before {
			if !reflect.DeepEqual(after[table], before[table]) {
				t.Errorf("reconcile refusal changed %s rows", table)
			}
		}
		t.FailNow()
	}
}

func TestReconcileCompatibilityNegativeJournalStep(t *testing.T) {
	e, h, _, j := compatibilityFixture(t)
	j.Step = -1
	if err := e.Store.Update(j, "synthetic corrupt step index"); err != nil {
		t.Fatal(err)
	}
	before := compatibilitySnapshot(t, e)
	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.Errorf("negative journal step panicked instead of returning a bounded error: %v", panicValue)
		}
		if h.observations != 0 || h.executions != 0 {
			t.Error("invalid journal step reached a handler")
		}
		compatibilityUnchanged(t, e, before)
	}()
	if _, err := e.Reconcile(context.Background(), j.ID); err == nil {
		t.Fatal("negative journal step accepted")
	}
}

func TestReconcileCompatibilityCanonicalRepresentationChanges(t *testing.T) {
	e, h, p, j := compatibilityFixture(t)
	// Canonical integrity binds values, not JSON whitespace or escape spelling.
	// Keep the original stored digest and feed the actual raw bytes to dispatch.
	input := []byte("\n { \"intent\" : \"local\\u0020test file\" } \n")
	digest, err := Digest(json.RawMessage(input))
	if err != nil || digest != p.InputDigest {
		t.Fatal("fixture changed semantic input", digest, err)
	}
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.DB.Exec("UPDATE plans SET body=?,input=? WHERE id=?", body, input, p.ID); err != nil {
		t.Fatal(err)
	}
	before := compatibilitySnapshot(t, e)
	got, err := e.Reconcile(context.Background(), j.ID)
	if !errors.Is(err, errCompatibilityObserved) || got.ID != j.ID || got.State != "recovery-required" || h.observations != 1 || h.executions != 0 || !reflect.DeepEqual(h.input, input) || h.plan.Digest != p.Digest || h.operationID != j.ID {
		t.Fatal("canonical-equivalent input was rejected or authority changed", got, err)
	}
	compatibilityUnchanged(t, e, before)
}

func compatibilityExpireBeforeAcceptance(t *testing.T, e *Engine, p domain.Plan) domain.Plan {
	t.Helper()
	// Synthetic historic acceptance: build an already-expired immutable record
	// before Store.Accept/AcceptRecovery. Never bypass expiry through Apply.
	p.CreatedAt = time.Now().UTC().Add(-2 * time.Hour)
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
	if _, err = e.Store.DB.Exec("UPDATE plans SET digest=?,body=? WHERE id=?", p.Digest, body, p.ID); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReconcileCompatibilityExpiredRecoveryKeepsOriginalReferenceAndChildIdentity(t *testing.T) {
	e, _ := openEngine(t, t.TempDir())
	originalHandler, resumeHandler := &compatibilityObserver{}, &compatibilityObserver{}
	e.Handlers["fixture.effect"], e.Handlers["fixture.resume"] = originalHandler, resumeHandler
	original := compatibilityExpireBeforeAcceptance(t, e, makePlan(t, e))
	parent, err := e.Store.Accept(original, "historical-parent", "synthetic-parent-request")
	if err != nil {
		t.Fatal(err)
	}
	parent.State = "recovery-required"
	if err = e.Store.Update(parent, "synthetic original uncertainty"); err != nil {
		t.Fatal(err)
	}
	// The generic engine dispatches the accepted child's recipe. Resolving and
	// validating the referenced original is the operation-specific handler's
	// responsibility; creation lifecycle tests exercise that production adapter.
	input := map[string]any{"parentOperationID": parent.ID, "originalPlanID": original.ID, "originalInputDigest": original.InputDigest}
	childPlan, err := e.Plan(context.Background(), original.ActorUID, original.ConnectionID, "fixture.resume", original.ResourceIDs, map[string]string{"original-plan": original.ID}, input, []domain.Step{{ID: "observe-original", Action: "fixture.resume", Idempotency: "reconcile-before-retry", CompletionPredicate: "fixture never establishes completion"}}, []string{"inherit-recovery-resources"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	childPlan = compatibilityExpireBeforeAcceptance(t, e, childPlan)
	child, err := e.Store.AcceptRecovery(childPlan, "historical-child", "synthetic-child-request", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	child.State, child.CancelRequested = "recovery-required", true
	if err = e.Store.Update(child, "synthetic interrupted recovery with retained cancellation request"); err != nil {
		t.Fatal(err)
	}
	before := compatibilitySnapshot(t, e)
	// An inherited outer operation context must not replace the accepted child.
	ctx := context.WithValue(context.Background(), operationContextKey{}, parent.ID)
	got, err := e.Reconcile(ctx, child.ID)
	if !errors.Is(err, errCompatibilityObserved) || got.ID != child.ID || got.State != "recovery-required" || got.RecoveryOf != parent.ID || !got.CancelRequested || resumeHandler.operationID != child.ID || resumeHandler.plan.ID != childPlan.ID || resumeHandler.plan.Digest != childPlan.Digest || resumeHandler.step.ID != "observe-original" || resumeHandler.observations != 1 || originalHandler.observations != 0 || resumeHandler.executions != 0 || originalHandler.executions != 0 {
		t.Fatal("expired recovery lost child authority or replayed original work", got, err)
	}
	var observedReference struct {
		ParentOperationID   string `json:"parentOperationID"`
		OriginalPlanID      string `json:"originalPlanID"`
		OriginalInputDigest string `json:"originalInputDigest"`
	}
	if err = json.Unmarshal(resumeHandler.input, &observedReference); err != nil || observedReference.ParentOperationID != parent.ID || observedReference.OriginalPlanID != original.ID || observedReference.OriginalInputDigest != original.InputDigest {
		t.Fatal("recovery dispatch rewrote original recipe references", observedReference, err)
	}
	storedOriginal, originalInput, err := e.Store.Plan(observedReference.OriginalPlanID)
	if err != nil || storedOriginal.Digest != original.Digest {
		t.Fatal("referenced original plan did not remain available", err)
	}
	if d, err := Digest(json.RawMessage(originalInput)); err != nil || d != observedReference.OriginalInputDigest {
		t.Fatal("referenced original recipe changed", err)
	}
	storedParent, err := e.Store.Job(parent.ID)
	if err != nil || storedParent.State != "partial" || storedParent.RecoveryOperationID != child.ID {
		t.Fatal("original uncertainty was relabeled successful", storedParent, err)
	}
	owners, err := e.Store.ResourceJobs("test-resource")
	if err != nil || !reflect.DeepEqual(owners, []string{child.ID}) {
		t.Fatal("inconclusive child observation lost inherited ownership", owners, err)
	}
	compatibilityUnchanged(t, e, before)
}

func TestReconcileCompatibilityReadAndDispatchFailuresPreserveJournal(t *testing.T) {
	for _, scenario := range []string{"missing-job", "malformed-job", "missing-plan", "malformed-plan", "missing-handler", "step-out-of-range"} {
		t.Run(scenario, func(t *testing.T) {
			e, h, p, j := compatibilityFixture(t)
			id := j.ID
			var err error
			switch scenario {
			case "missing-job":
				id = "absent-operation"
			case "malformed-job":
				_, err = e.Store.DB.Exec("UPDATE jobs SET body=? WHERE id=?", []byte(`{"id":`), j.ID)
			case "missing-plan":
				j.PlanID = "absent-plan"
				err = e.Store.Update(j, "synthetic missing plan reference")
			case "malformed-plan":
				_, err = e.Store.DB.Exec("UPDATE plans SET body=? WHERE id=?", []byte(`{"id":`), p.ID)
			case "missing-handler":
				delete(e.Handlers, p.Operation)
			case "step-out-of-range":
				j.Step = len(p.Steps)
				err = e.Store.Update(j, "synthetic invalid step upper bound")
			}
			if err != nil {
				t.Fatal(err)
			}
			before := compatibilitySnapshot(t, e)
			if _, err = e.Reconcile(context.Background(), id); err == nil || h.observations != 0 || h.executions != 0 {
				t.Fatal("read/dispatch failure reached an observer or reported success", err)
			}
			compatibilityUnchanged(t, e, before)
		})
	}
}

func TestReconcileCompatibilityRejectedIntegrityPreservesRetryIdentityAndBusyResource(t *testing.T) {
	e, _ := openEngine(t, t.TempDir())
	h := &compatibilityObserver{}
	e.Handlers["fixture.effect"] = h
	p := compatibilityExpireBeforeAcceptance(t, e, makePlan(t, e))
	r := ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "historic-retry", Acknowledgements: p.Acknowledgements}
	requestDigest, err := Digest(struct {
		Actor   uint32
		Request ApplyRequest
	}{p.ActorUID, r})
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.Store.Accept(p, r.IdempotencyKey, requestDigest)
	if err != nil {
		t.Fatal(err)
	}
	j.State = "recovery-required"
	if err = e.Store.Update(j, "synthetic accepted prior effect"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.DB.Exec("UPDATE plans SET input=? WHERE id=?", []byte(`{"intent":"changed externally"}`), p.ID); err != nil {
		t.Fatal(err)
	}
	before := compatibilitySnapshot(t, e)
	_, err = e.Reconcile(context.Background(), j.ID)
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "SOURCE_CHANGED" {
		t.Fatal("fixture did not reach integrity refusal", err)
	}
	// The exact accepted request remains a read of the original uncertain job,
	// even after expiry/corruption. Dedup does not re-authorize its recipe.
	got, err := e.Apply(context.Background(), p.ActorUID, r)
	if err != nil || got.ID != j.ID || got.State != "recovery-required" {
		t.Fatal("accepted retry lost original uncertainty", got, err)
	}
	changed := r
	changed.PlanDigest = "changed-reviewed-digest"
	_, err = e.Apply(context.Background(), p.ActorUID, changed)
	if !errors.As(err, &failure) || failure.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("changed request reused accepted key", err)
	}
	compatibilityUnchanged(t, e, before)
	// An independently reviewed contender cannot acquire the uncertain resource.
	contender := makePlan(t, e)
	before = compatibilitySnapshot(t, e)
	_, err = e.Apply(context.Background(), contender.ActorUID, ApplyRequest{PlanID: contender.ID, PlanDigest: contender.Digest, IdempotencyKey: "contending-client", Acknowledgements: contender.Acknowledgements})
	if !errors.As(err, &failure) || failure.Code != "RESOURCE_BUSY" || h.observations != 0 || h.executions != 0 {
		t.Fatal("integrity refusal weakened retry or resource ownership", err)
	}
	compatibilityUnchanged(t, e, before)
}
