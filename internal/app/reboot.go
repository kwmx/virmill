package app

import (
	"context"
	"time"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

type rebootRecipe struct {
	Version     int                `json:"version"`
	Resource    domain.ResourceKey `json:"resource"`
	Fingerprint string             `json:"fingerprint"`
}

type rebootReceipt struct {
	Version     int                      `json:"version"`
	OperationID string                   `json:"operationID"`
	PlanID      string                   `json:"planID"`
	PlanDigest  string                   `json:"planDigest"`
	Observation domain.RebootObservation `json:"observation"`
	GuestReady  bool                     `json:"guestReady"`
}

func (s *Service) planReboot(ctx context.Context, uid uint32, r Request) (domain.Plan, error) {
	var empty domain.Plan
	if r.ID == "" || r.Path != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 {
		return empty, domain.Fail("INVALID_INPUT", "reboot requires a stable VM UUID and connection without extra parameters")
	}
	if _, ok := s.Provider.(domain.RebootProvider); !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "selected provider cannot verify a graceful reboot event")
	}
	v, err := s.GetVM(ctx, r.Connection, r.ID)
	if err != nil {
		return empty, err
	}
	if v.State != "running" || v.Fingerprint == "" || v.Key.UUID != r.ID || v.Key.ConnectionID != r.Connection {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "reboot requires the selected running VM with a stable fingerprint")
	}
	key := v.Key.String()
	step := domain.Step{ID: "reboot", Action: "vm.reboot", Preconditions: []string{"unchanged running VM", "reboot event subscription before request"}, Idempotency: "non-repeatable", Compensation: "Preserve the VM and its disks; inspect an uncertain request", Reconciliation: "Read the durable selected-domain reboot event receipt; running state alone proves nothing", CompletionPredicate: "Selected-domain reboot event recorded after the graceful request; guest readiness unverified"}
	return s.Engine.Plan(ctx, uid, r.Connection, "vm.reboot", []string{key}, map[string]string{key: v.Fingerprint}, rebootRecipe{Version: 1, Resource: v.Key, Fingerprint: v.Fingerprint}, []domain.Step{step}, []string{"host-mutation", "guest-reboot", "exclusive-lifecycle-writer"}, []string{"Guest execution is interrupted by one graceful reboot request; no reset or hard-stop fallback", "A reboot event verifies the transition, not guest login, services or network readiness", "Coordinate other lifecycle writers: libvirt events do not carry a request ID; missing events or a lost receipt require recovery without replay"})
}

type rebootHandler struct{ s *Service }

func (h *rebootHandler) Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error) {
	return domain.Estimates{RequiresDowntime: true, Notes: "One graceful reboot; guest outage duration depends on its OS and services. No disk allocation is requested."}, nil
}

func parseReboot(p domain.Plan, raw []byte) (rebootRecipe, error) {
	var r rebootRecipe
	if wire.Decode(raw, &r) != nil || r.Version != 1 || p.Operation != "vm.reboot" || r.Resource.UUID == "" || r.Resource.ConnectionID != p.ConnectionID || r.Fingerprint == "" || len(p.ResourceIDs) != 1 || p.ResourceIDs[0] != r.Resource.String() || len(p.Before) != 1 || p.Before[r.Resource.String()] != r.Fingerprint {
		return r, domain.Fail("INVALID_INPUT", "invalid reboot recipe binding")
	}
	return r, nil
}

func (h *rebootHandler) Review(ctx context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	r, err := parseReboot(p, raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"action": "reboot", "resource": r.Resource, "request": "graceful", "hardStopFallback": false, "diskDeletion": false, "completion": "selected-domain reboot event", "guestReadinessVerified": false}, nil
}

func (h *rebootHandler) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	r, err := parseReboot(p, raw)
	if err != nil {
		return err
	}
	if _, ok := h.s.Provider.(domain.RebootProvider); !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "reboot event provider unavailable")
	}
	v, err := h.s.GetVM(ctx, p.ConnectionID, r.Resource.UUID)
	if err != nil {
		return err
	}
	if v.Key != r.Resource || v.Fingerprint != r.Fingerprint || v.State != "running" {
		return domain.Fail("STALE_PLAN", "VM changed since reboot preview")
	}
	return ctx.Err()
}

func (h *rebootHandler) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	r, err := parseReboot(p, raw)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "reboot" {
		return domain.Fail("INVALID_INPUT", "reboot requires durable operation identity")
	}
	provider, ok := h.s.Provider.(domain.RebootProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "reboot event provider unavailable")
	}
	started := time.Now()
	observed, err := provider.Reboot(ctx, p.ConnectionID, r.Resource.UUID, r.Fingerprint)
	if err != nil {
		return err
	}
	if observed.Resource != r.Resource || observed.Evidence != "libvirt-reboot-event" || observed.ObservedAt.Before(started) || observed.ObservedAt.After(time.Now()) {
		return domain.Fail("RECOVERY_REQUIRED", "reboot observation does not match the selected request; do not replay")
	}
	return h.s.Engine.Store.ComparePut("vm-reboot-receipt", id, nil, rebootReceipt{Version: 1, OperationID: id, PlanID: p.ID, PlanDigest: p.Digest, Observation: observed})
}

func (h *rebootHandler) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	r, err := parseReboot(p, raw)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "reboot" {
		return false, domain.Fail("INVALID_INPUT", "durable reboot operation identity required")
	}
	b, err := h.s.Engine.Store.MetadataBytes("vm-reboot-receipt", id)
	if err != nil {
		return false, err
	}
	if len(b) == 0 {
		return false, domain.Fail("RECOVERY_REQUIRED", "no durable reboot event receipt; running state cannot prove a reboot and the request will not be replayed")
	}
	var receipt rebootReceipt
	if wire.Decode(b, &receipt) != nil || receipt.Version != 1 || receipt.OperationID != id || receipt.PlanID != p.ID || receipt.PlanDigest != p.Digest || receipt.GuestReady || receipt.Observation.Resource != r.Resource || receipt.Observation.Evidence != "libvirt-reboot-event" || receipt.Observation.ObservedAt.Before(p.CreatedAt) || receipt.Observation.ObservedAt.After(time.Now()) {
		return false, domain.Fail("RECOVERY_REQUIRED", "reboot event receipt binding is invalid; preserve VM without replay")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}
