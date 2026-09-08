package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/store"
)

type Handler interface {
	Validate(context.Context, domain.Plan, []byte) error
	Execute(context.Context, domain.Plan, []byte, domain.Step) error
	Reconcile(context.Context, domain.Plan, []byte, domain.Step) (bool, error)
}

// PartialEffectHandler opts a cumulative workflow into retaining its complete
// lock set if cancellation or validation stops a later step. Previously verified
// external effects still need an explicit recovery disposition.
type PartialEffectHandler interface{ RetainCompletedEffects() bool }

// PlanReviewer supplies explicit, non-secret effect details for human approval.
// Those details are included in the same immutable digest as execution authority.
type PlanReviewer interface {
	Review(context.Context, domain.Plan, []byte) (map[string]any, error)
}

// PlanEstimator supplies operation-specific estimates before the immutable plan
// is hashed and stored. It must observe only, just like Validate and Review.
type PlanEstimator interface {
	Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error)
}

// RecoveryHandler identifies an uncertain job whose complete lock set must be
// inherited atomically. It is implemented only by explicit reviewed workflows;
// ordinary apply requests cannot select or bypass resource-lock ownership.
type RecoveryHandler interface {
	RecoveryParent(context.Context, domain.Plan, []byte) (string, error)
}
type ApplyRequest struct {
	PlanID           string   `json:"planID"`
	PlanDigest       string   `json:"planDigest"`
	IdempotencyKey   string   `json:"idempotencyKey"`
	Acknowledgements []string `json:"acknowledgements"`
}
type Engine struct {
	Store    *store.Store
	Handlers map[string]Handler
	mu       sync.Mutex
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func New(s *store.Store) *Engine {
	ctx, c := context.WithCancel(context.Background())
	return &Engine{Store: s, Handlers: map[string]Handler{}, ctx: ctx, cancel: c}
}
func (e *Engine) Close() { e.cancel(); e.wg.Wait() }
func (e *Engine) Plan(ctx context.Context, uid uint32, connection, operation string, resources []string, before map[string]string, input any, steps []domain.Step, acks, risks []string) (domain.Plan, error) {
	p := domain.Plan{}
	h, ok := e.Handlers[operation]
	if !ok {
		return p, domain.Fail("NOT_IMPLEMENTED", "operation has no executable workflow")
	}
	b, err := Canonical(input)
	if err != nil {
		return p, err
	}
	digest, err := Digest(input)
	if err != nil {
		return p, err
	}
	sort.Strings(resources)
	if resources == nil {
		resources = []string{}
	}
	if acks == nil {
		acks = []string{}
	}
	if risks == nil {
		risks = []string{}
	}
	if before == nil {
		before = map[string]string{}
	}
	p = domain.Plan{APIVersion: domain.APIVersion, ID: domain.ID(), CreatedAt: time.Now().UTC(), ActorUID: uid, ConnectionID: connection, Operation: operation, ResourceIDs: resources, InputDigest: digest, Before: before, RequiredGrants: []domain.Grant{}, Acknowledgements: acks, Risks: risks, Steps: steps}
	p.ExpiresAt = p.CreatedAt.Add(15 * time.Minute)
	for _, r := range resources {
		p.RequiredGrants = append(p.RequiredGrants, domain.Grant{Operation: operation, ResourceID: r})
	}
	if len(steps) == 0 {
		return p, domain.Fail("INVALID_INPUT", "plan requires an explicit verification step")
	}
	if err = h.Validate(ctx, p, b); err != nil {
		return p, err
	}
	if reviewer, ok := h.(PlanReviewer); ok {
		p.Review, err = reviewer.Review(ctx, p, b)
		if err != nil {
			return p, err
		}
	}
	if estimator, ok := h.(PlanEstimator); ok {
		p.Estimates, err = estimator.Estimate(ctx, p, b)
		if err != nil {
			return p, err
		}
	}
	p.Digest, err = PlanDigest(p)
	if err != nil {
		return p, err
	}
	return p, e.Store.SavePlan(p, b)
}
func (e *Engine) Apply(ctx context.Context, uid uint32, r ApplyRequest) (domain.Job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var empty domain.Job
	if r.IdempotencyKey == "" || len(r.IdempotencyKey) > 128 {
		return empty, domain.Fail("INVALID_INPUT", "idempotency key must contain 1–128 characters")
	}
	sort.Strings(r.Acknowledgements)
	requestHash, err := Digest(struct {
		Actor   uint32
		Request ApplyRequest
	}{uid, r})
	if err != nil {
		return empty, err
	}
	if j, found, err := e.Store.Dedup(r.IdempotencyKey, requestHash); found || err != nil {
		return j, err
	}
	p, input, err := e.Store.Plan(r.PlanID)
	if err != nil {
		return empty, err
	}
	if p.ActorUID != uid {
		return empty, domain.Fail("PERMISSION_DENIED", "plan belongs to a different actor")
	}
	computed, err := PlanDigest(p)
	if err != nil {
		return empty, err
	}
	if p.Digest != r.PlanDigest || p.Digest != computed || !time.Now().Before(p.ExpiresAt) {
		return empty, domain.Fail("STALE_PLAN", "plan hash differs or plan expired; generate a fresh preview")
	}
	var inputValue any
	if err = json.Unmarshal(input, &inputValue); err != nil {
		return empty, err
	}
	d, err := Digest(inputValue)
	if err != nil || d != p.InputDigest {
		return empty, domain.Fail("SOURCE_CHANGED", "stored input digest differs")
	}
	acks := map[string]bool{}
	for _, a := range r.Acknowledgements {
		acks[a] = true
	}
	for _, a := range p.Acknowledgements {
		if !acks[a] {
			return empty, domain.Fail("PERMISSION_REQUIRED", "missing plan acknowledgement: "+a)
		}
	}
	h, ok := e.Handlers[p.Operation]
	if !ok {
		return empty, domain.Fail("NOT_IMPLEMENTED", "handler unavailable")
	}
	if err = h.Validate(ctx, p, input); err != nil {
		return empty, err
	}
	var j domain.Job
	if recovery, ok := h.(RecoveryHandler); ok {
		parent, parentErr := recovery.RecoveryParent(ctx, p, input)
		if parentErr != nil {
			return empty, parentErr
		}
		j, err = e.Store.AcceptRecovery(p, r.IdempotencyKey, requestHash, parent)
	} else {
		j, err = e.Store.Accept(p, r.IdempotencyKey, requestHash)
	}
	if err != nil {
		return empty, err
	}
	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.run(j, p, input, h) }()
	return j, nil
}
func (e *Engine) transition(j *domain.Job, state, message string, err error) error {
	j.State = state
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) {
			copy := *de
			j.Error = &copy
		} else {
			j.Error = domain.Fail("OPERATION_FAILED", err.Error())
		}
		j.Error.OperationID = j.ID
	}
	return e.Store.Update(*j, message)
}
func (e *Engine) run(j domain.Job, p domain.Plan, input []byte, h Handler) {
	ctx := context.WithValue(e.ctx, operationContextKey{}, j.ID)
	for i, step := range p.Steps {
		e.mu.Lock()
		fresh, err := e.Store.Job(j.ID)
		if err != nil {
			e.mu.Unlock()
			return
		}
		j.CancelRequested = fresh.CancelRequested
		j.Step = i
		retainPartial := false
		if cumulative, ok := h.(PartialEffectHandler); ok && i > 0 {
			retainPartial = cumulative.RetainCompletedEffects()
		}
		if j.CancelRequested {
			if j.RecoveryOf != "" || retainPartial {
				_ = e.transition(&j, "recovery-required", "Recovery canceled; inherited uncertain resources remain locked", domain.Fail("RECOVERY_REQUIRED", "review another recovery operation"))
			} else {
				_ = e.transition(&j, "canceled", "Canceled at a safe step boundary", nil)
			}
			e.mu.Unlock()
			return
		}
		if err = e.transition(&j, "validating", "Rechecking preconditions before effect", nil); err != nil {
			e.mu.Unlock()
			return
		}
		e.mu.Unlock()
		if err = h.Validate(ctx, p, input); err != nil {
			if j.RecoveryOf != "" || retainPartial {
				_ = e.transition(&j, "recovery-required", "Recovery preconditions failed; inherited resources remain locked", err)
			} else {
				_ = e.transition(&j, "failed", "Preconditions failed; no new step effect", err)
			}
			return
		}
		if err = e.transition(&j, "running", "Intent persisted: "+step.Action, nil); err != nil {
			return
		}
		if err = h.Execute(ctx, p, input, step); err != nil {
			if errors.Is(err, ErrCanceledSafely) && j.RecoveryOf == "" {
				_ = e.transition(&j, "canceled", "Canceled at a verified safe boundary; no published effect remains", nil)
				return
			}
			_ = e.transition(&j, "recovery-required", "Step may have taken effect; reconcile before any retry", err)
			return
		}
		if err = e.transition(&j, "verifying", "Effect returned; verifying observed state", nil); err != nil {
			return
		}
		ok, err := h.Reconcile(ctx, p, input, step)
		if err != nil || !ok {
			if err == nil {
				err = domain.Fail("RECOVERY_REQUIRED", "completion predicate not confirmed")
			}
			_ = e.transition(&j, "recovery-required", "Effect was not proven complete", err)
			return
		}
	}
	_ = e.transition(&j, "succeeded", "All completion predicates verified", nil)
}
func (e *Engine) Cancel(id string) (domain.Job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, err := e.Store.Job(id)
	if err != nil {
		return j, err
	}
	if domain.Terminal(j.State) {
		return j, domain.Fail("INVALID_INPUT", "job is already terminal")
	}
	j.CancelRequested = true
	err = e.Store.Update(j, "Cancellation requested; active step must reach a safe boundary")
	return j, err
}
func (e *Engine) Reconcile(ctx context.Context, id string) (domain.Job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	j, err := e.Store.Job(id)
	if err != nil {
		return j, err
	}
	if j.State != "recovery-required" && j.State != "interrupted" {
		return j, domain.Fail("INVALID_INPUT", "only interrupted/uncertain jobs may be reconciled")
	}
	p, input, err := e.Store.Plan(j.PlanID)
	if err != nil {
		return j, err
	}
	// Recovery may run long after plan expiry, but it must still observe the
	// exact immutable recipe that was authorized. In particular, a changed
	// recipe version must not downgrade a new completion-proof requirement.
	planDigest, err := PlanDigest(p)
	if err != nil || planDigest != p.Digest {
		return j, domain.Fail("SOURCE_CHANGED", "persisted recovery plan digest differs; retain resources without invoking its handler")
	}
	inputDigest, err := Digest(json.RawMessage(input))
	if err != nil || inputDigest != p.InputDigest {
		return j, domain.Fail("SOURCE_CHANGED", "persisted recovery recipe digest differs; retain resources without invoking its handler")
	}
	h, ok := e.Handlers[p.Operation]
	if !ok {
		return j, domain.Fail("NOT_IMPLEMENTED", "handler unavailable")
	}
	if j.Step < 0 || j.Step >= len(p.Steps) {
		return j, errors.New("journal step invalid")
	}
	// Recovery is another execution boundary for this same accepted job. Bind its
	// identity exactly as run does, so helper observations cannot invent a job ID.
	ctx = context.WithValue(ctx, operationContextKey{}, j.ID)
	ok, err = h.Reconcile(ctx, p, input, p.Steps[j.Step])
	if err != nil || !ok {
		if err == nil {
			err = domain.Fail("RECOVERY_REQUIRED", "effect cannot be proven; preserve resources and create a reviewed recovery plan")
		}
		return j, err
	}
	if j.Step != len(p.Steps)-1 {
		return j, domain.Fail("RECOVERY_REQUIRED", "step complete but later steps need a fresh reviewed plan")
	}
	j.Error = nil
	err = e.transition(&j, "succeeded", "Recovered by observing the original intended effect; no replay", nil)
	return j, err
}

// Recover never replays an uncertain effect. Resource locks remain held until resolved.
func (e *Engine) Recover() error {
	jobs, err := e.Store.PendingJobs()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if !domain.Terminal(j.State) {
			if err = e.transition(&j, "recovery-required", "Coordinator restarted; effects require observation before continuation", domain.Fail("RECOVERY_REQUIRED", fmt.Sprintf("interrupted at step %d", j.Step))); err != nil {
				return err
			}
		}
	}
	return nil
}
