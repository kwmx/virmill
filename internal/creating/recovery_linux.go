//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

type resumeInput struct {
	ParentOperationID string  `json:"parentOperationID"`
	CreationPlanID    string  `json:"creationPlanID"`
	Receipt           Receipt `json:"receipt"`
}
type resumeHandler struct{ s *Service }

type resumeProof struct {
	Version        int    `json:"schemaVersion"`
	PlanID         string `json:"planID"`
	InputDigest    string `json:"inputDigest"`
	OperationID    string `json:"operationID"`
	CreationPlanID string `json:"creationPlanID"`
}

func (s *Service) PlanResume(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	if len(r.Input) != 0 {
		return empty, domain.Fail("INVALID_INPUT", "resume definition accepts an operation ID only; hardware changes require a separate workflow")
	}
	parent, err := s.Store.Job(r.ID)
	if err != nil {
		return empty, err
	}
	prior, encoded, err := s.Store.Plan(parent.PlanID)
	if err != nil {
		return empty, err
	}
	if prior.ActorUID != uid || prior.ConnectionID != r.Connection {
		return empty, domain.Fail("PERMISSION_DENIED", "recovery must use the original actor and connection")
	}
	originalID := prior.ID
	switch prior.Operation {
	case "vm.create":
	case "vm.create.resume":
		var old resumeInput
		if err = wire.Decode(encoded, &old); err != nil {
			return empty, err
		}
		originalID = old.CreationPlanID
	default:
		return empty, domain.Fail("INVALID_INPUT", "operation is not a VM creation or its definition recovery")
	}
	receipt, err := s.load(originalID)
	if err != nil {
		return empty, err
	}
	in := resumeInput{ParentOperationID: parent.ID, CreationPlanID: originalID, Receipt: receipt}
	resources := append([]string{}, prior.ResourceIDs...)
	step := domain.Step{ID: "define", Action: "vm.create.resume", Preconditions: []string{"exact durable complete volume receipt", "every original resource lock retained", "unchanged hardware, firmware, networks and source", "VM UUID/name absent", "every retained volume unattached and reverified"}, Idempotency: "reconcile-before-retry", Compensation: "Retain original and copied disks; preserve inherited resource locks on failure or cancellation", Reconciliation: "Observe exact original definition without allocating or uploading again", CompletionPredicate: "Original reviewed VM definition matches all reverified retained volumes"}
	acks := []string{"host-mutation", "inherit-recovery-resources", "define-retained-volumes"}
	if receipt.Defined {
		return empty, domain.Fail("RECOVERY_REQUIRED", "definition was already observed; reconcile the uncertain operation instead of resuming")
	}
	return s.Engine.Plan(ctx, uid, r.Connection, "vm.create.resume", resources, map[string]string{"creation-plan": originalID}, in, []domain.Step{step}, acks, []string{"Transfers all original resource locks to this recovery operation; original operation remains partial and links to its recovery", "Reverifies retained copied volumes and defines the original powered-off VM; allocation and upload are never replayed", "Partial/unverified volume sets cannot be resumed by this adapter and remain retained for a separate recovery disposition"})
}

func (h *resumeHandler) original(p domain.Plan, in resumeInput) (domain.Plan, input, Receipt, error) {
	original, recipe, receipt, err := h.s.originalReceipt(p, in)
	if err == nil {
		err = h.s.requireUndisposed(original.ID)
	}
	if err == nil {
		_, err = readyVolumes(receipt, recipe)
	}
	return original, recipe, receipt, err
}

func (s *Service) requireUndisposed(id string) error {
	b, err := s.Store.MetadataBytes("vm-creation-disposition", id)
	if err != nil {
		return err
	}
	if b != nil {
		return domain.Fail("RECOVERY_REQUIRED", "creation has a durable recovery disposition; its old recipe cannot be resumed or reconciled as a created VM")
	}
	return nil
}

func (s *Service) originalReceipt(p domain.Plan, in resumeInput) (domain.Plan, input, Receipt, error) {
	var recipe input
	original, encoded, err := s.Store.Plan(in.CreationPlanID)
	if err != nil {
		return original, recipe, Receipt{}, err
	}
	if original.Operation != "vm.create" || original.ActorUID != p.ActorUID || original.ConnectionID != p.ConnectionID {
		return original, recipe, Receipt{}, domain.Fail("PERMISSION_DENIED", "recovery creation identity differs")
	}
	digest, err := operations.PlanDigest(original)
	if err != nil || digest != original.Digest {
		return original, recipe, Receipt{}, domain.Fail("SOURCE_CHANGED", "original creation plan changed")
	}
	var value any
	if err = json.Unmarshal(encoded, &value); err != nil {
		return original, recipe, Receipt{}, err
	}
	digest, err = operations.Digest(value)
	if err != nil || digest != original.InputDigest {
		return original, recipe, Receipt{}, domain.Fail("SOURCE_CHANGED", "original creation input changed")
	}
	if err = wire.Decode(encoded, &recipe); err != nil {
		return original, recipe, Receipt{}, err
	}
	receipt, err := s.load(original.ID)
	if err != nil {
		return original, recipe, receipt, err
	}
	core := receipt
	core.Defined = in.Receipt.Defined
	if !same(core, in.Receipt) {
		return original, recipe, receipt, domain.Fail("SOURCE_CHANGED", "retained volume receipt differs from reviewed recovery")
	}
	binding, err := operations.Digest([]string{original.ID, original.InputDigest})
	if err != nil {
		return original, recipe, receipt, err
	}
	if receipt.PlanID != original.ID || receipt.Binding != binding || receipt.VMID != recipe.Target.Spec.UUID || receipt.Connection != original.ConnectionID {
		return original, recipe, receipt, domain.Fail("RECOVERY_REQUIRED", "original creation receipt binding differs")
	}
	job, err := s.Store.Job(receipt.OperationID)
	if err != nil {
		return original, recipe, receipt, err
	}
	if job.PlanID != original.ID {
		return original, recipe, receipt, domain.Fail("RECOVERY_REQUIRED", "creation receipt operation differs")
	}
	if len(receipt.Volumes) != len(recipe.Volumes) || receipt.GuestBootVerified {
		return original, recipe, receipt, domain.Fail("RECOVERY_REQUIRED", "creation volume receipt is incomplete or has incompatible semantics")
	}
	for i, v := range receipt.Volumes {
		if !same(v.Intent, recipe.Volumes[i]) || (v.Allocated != nil && !same(v.Allocated.Intent, v.Intent)) {
			return original, recipe, receipt, domain.Fail("RECOVERY_REQUIRED", "creation volume receipt differs from original intent")
		}
	}
	return original, recipe, receipt, nil
}
func (h *resumeHandler) RecoveryParent(ctx context.Context, p domain.Plan, b []byte) (string, error) {
	var in resumeInput
	if err := wire.Decode(b, &in); err != nil {
		return "", err
	}
	return in.ParentOperationID, nil
}
func (h *resumeHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	var in resumeInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	original, recipe, receipt, err := h.original(p, in)
	if err != nil {
		return err
	}
	if receipt.Defined {
		return domain.Fail("RECOVERY_REQUIRED", "VM definition was observed; use operation reconcile")
	}
	parent, err := h.s.Store.Job(in.ParentOperationID)
	if err != nil {
		return err
	}
	owner := parent.ID
	if parent.State == "partial" && parent.RecoveryOperationID != "" && parent.RecoveryOperationID == operations.OperationID(ctx) {
		owner = parent.RecoveryOperationID
	} else if parent.State != "recovery-required" || parent.RecoveryOperationID != "" {
		return domain.Fail("STALE_PLAN", "recovery parent is no longer unresolved")
	}
	for _, resource := range p.ResourceIDs {
		jobs, e := h.s.Store.ResourceJobs(resource)
		if e != nil {
			return e
		}
		if len(jobs) != 1 || jobs[0] != owner {
			return domain.Fail("RESOURCE_BUSY", "recovery no longer owns the complete resource lock set")
		}
	}
	if err = h.s.checkSource(ctx, original, recipe); err != nil {
		return err
	}
	return h.s.checkTarget(ctx, original, recipe)
}
func (h *resumeHandler) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in resumeInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	_, recipe, receipt, err := h.original(p, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"parentOperationID": in.ParentOperationID, "originalCreationPlanID": in.CreationPlanID, "target": recipe.Target, "retainedVolumes": receipt.Volumes, "reverifyBeforeDefine": true, "allocatesVolumes": false, "uploadsVolumes": false, "startsVM": false, "guestBootVerified": false}, nil
}
func (h *resumeHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	if err := h.Validate(ctx, p, b); err != nil {
		return err
	}
	var in resumeInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	original, recipe, receipt, err := h.original(p, in)
	if err != nil {
		return err
	}
	volumes, err := readyVolumes(receipt, recipe)
	if err != nil {
		return err
	}
	for _, volume := range volumes {
		canceled, err := operations.CancellationRequested(ctx, h.s.Store)
		if err != nil {
			return err
		}
		if canceled {
			return domain.Fail("RECOVERY_REQUIRED", "definition recovery canceled; inherited volumes and locks retained")
		}
		if err = operations.Note(ctx, h.s.Store, "Reverify retained volume before recovery definition: "+volume.Intent.Name); err != nil {
			return err
		}
		if err = h.s.cancellable(ctx, func(worker context.Context) error {
			return h.s.Backend.VerifyCreatedVolume(worker, p.ConnectionID, volume)
		}); err != nil {
			return err
		}
	}
	if err = h.s.checkTarget(ctx, original, recipe); err != nil {
		return err
	}
	canceled, err := operations.CancellationRequested(ctx, h.s.Store)
	if err != nil {
		return err
	}
	if canceled {
		return domain.Fail("RECOVERY_REQUIRED", "definition recovery canceled; retained resources remain locked")
	}
	proof := resumeProof{Version: 1, PlanID: p.ID, InputDigest: p.InputDigest, OperationID: operations.OperationID(ctx), CreationPlanID: original.ID}
	if err = h.s.Store.ComparePut("vm-creation-recovery", p.ID, nil, proof); err != nil {
		return err
	}
	if err = operations.Note(ctx, h.s.Store, "Recovery intent persisted: define original VM using every reverified retained volume"); err != nil {
		return err
	}
	_, err = h.s.Backend.DefineCreatedVM(ctx, p.ConnectionID, recipe.Target, volumes, receipt.Binding)
	return err
}
func (h *resumeHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	var in resumeInput
	if err := wire.Decode(b, &in); err != nil {
		return false, err
	}
	encodedProof, err := h.s.Store.MetadataBytes("vm-creation-recovery", p.ID)
	if err != nil {
		return false, err
	}
	var proof resumeProof
	if err = wire.Decode(encodedProof, &proof); err != nil {
		return false, domain.Fail("RECOVERY_REQUIRED", "this recovery has no durable completed volume verification")
	}
	if proof.Version != 1 || proof.PlanID != p.ID || proof.InputDigest != p.InputDigest || proof.CreationPlanID != in.CreationPlanID {
		return false, domain.Fail("RECOVERY_REQUIRED", "recovery verification receipt differs")
	}
	job, err := h.s.Store.Job(proof.OperationID)
	if err != nil {
		return false, err
	}
	if job.PlanID != p.ID {
		return false, domain.Fail("RECOVERY_REQUIRED", "recovery verification operation differs")
	}
	original, _, _, err := h.original(p, in)
	if err != nil {
		return false, err
	}
	_, encoded, err := h.s.Store.Plan(original.ID)
	if err != nil {
		return false, err
	}
	return h.s.Reconcile(ctx, original, encoded, original.Steps[0])
}

var _ operations.RecoveryHandler = (*resumeHandler)(nil)
