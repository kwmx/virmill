//go:build linux && amd64 && cgo

package creating

import (
	"context"
	"encoding/json"
	"slices"
	"sort"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

// ADR 0062: close an unresolved disk addition. A job left needing recovery
// keeps this VM and its pool locked, so every addition needs a reviewed way
// out. Accepting records that the reviewed disk is in the saved definition;
// deleting removes the new volume, and only while nothing names it.
const diskAddDisposeOperation = "vm.disk.add.dispose-v1"
const diskAddDispositionKind = "vm-disk-add-disposition"

type diskAddDisposeInput struct {
	Version           int                      `json:"version"`
	ParentOperationID string                   `json:"parentOperationID"`
	AdditionPlanID    string                   `json:"additionPlanID"`
	Plan              domain.DiskAdditionPlan  `json:"plan"`
	Disposition       string                   `json:"disposition"`
	Observation       domain.AddedDiskDisposal `json:"observation"`
}

type diskAddDisposition struct {
	Version           int                      `json:"version"`
	PlanID            string                   `json:"planID"`
	OperationID       string                   `json:"operationID"`
	ParentOperationID string                   `json:"parentOperationID"`
	AdditionPlanID    string                   `json:"additionPlanID"`
	Disposition       string                   `json:"disposition"`
	Observation       domain.AddedDiskDisposal `json:"observation"`
}

type diskAddDisposeHandler struct {
	s       *Service
	backend domain.DiskAdditionDisposalBackend
}

// requireUndisposedAddition refuses a second disposition for one addition.
func (h *diskAddDisposeHandler) requireUndisposedAddition(planID string) error {
	b, err := h.s.Store.MetadataBytes(diskAddDispositionKind, planID)
	if err != nil {
		return err
	}
	if b != nil {
		return domain.Fail("RECOVERY_REQUIRED", "this disk addition already has a durable disposition; it cannot be closed twice")
	}
	return nil
}

// addition re-derives the original addition recipe and checks its binding.
func (h *diskAddDisposeHandler) addition(p domain.Plan, planID string) (diskAddInput, error) {
	var in diskAddInput
	original, encoded, err := h.s.Store.Plan(planID)
	if err != nil {
		return in, err
	}
	if original.Operation != diskAddOperation || original.ActorUID != p.ActorUID || original.ConnectionID != p.ConnectionID {
		return in, domain.Fail("PERMISSION_DENIED", "a disposition needs the original actor's disk addition on this connection")
	}
	digest, err := operations.PlanDigest(original)
	if err != nil || digest != original.Digest {
		return in, domain.Fail("SOURCE_CHANGED", "the original disk addition plan changed")
	}
	if in, err = parseDiskAdd(original, encoded); err != nil {
		return in, err
	}
	return in, h.requireUndisposedAddition(planID)
}

func (h *diskAddDisposeHandler) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	b, err := json.Marshal(r.Input)
	if err != nil {
		return empty, err
	}
	if validation.Schema("vm-disk-add-dispose-input", b) != nil {
		return empty, domain.Fail("INVALID_INPUT", `Choose how to close the disk addition, for example {"disposition":"accept"}.`)
	}
	var request struct {
		Disposition string `json:"disposition"`
	}
	if err = wire.Decode(b, &request); err != nil {
		return empty, err
	}
	parent, err := h.s.Store.Job(r.ID)
	if err != nil {
		return empty, err
	}
	prior, _, err := h.s.Store.Plan(parent.PlanID)
	if err != nil {
		return empty, err
	}
	if prior.Operation != diskAddOperation {
		return empty, domain.Fail("INVALID_INPUT", "this operation is not an unresolved disk addition")
	}
	if parent.State != "recovery-required" && parent.State != "interrupted" {
		return empty, domain.Fail("INVALID_INPUT", "only an unresolved disk addition needs a disposition")
	}
	addition, err := h.addition(prior, prior.ID)
	if err != nil {
		return empty, err
	}
	observation, err := h.backend.InspectAddedDiskDisposal(ctx, addition.Plan, request.Disposition == "delete")
	if err != nil {
		return empty, err
	}
	if request.Disposition == "accept" && !observation.Referenced {
		return empty, domain.Fail("INVALID_INPUT", "this VM's saved definition does not name the new disk, so there is nothing to accept; delete the unused volume instead")
	}
	if request.Disposition == "keep" && observation.Referenced {
		return empty, domain.Fail("INVALID_INPUT", "this VM's saved definition names the new disk, so accept it instead")
	}
	if request.Disposition == "delete" && observation.Referenced {
		return empty, domain.Fail("RESOURCE_BUSY", "this VM's saved definition names the new disk, so its volume cannot be deleted here; accept it, or remove the disk with VM disk removal first")
	}
	in := diskAddDisposeInput{Version: 1, ParentOperationID: parent.ID, AdditionPlanID: prior.ID, Plan: addition.Plan, Disposition: request.Disposition, Observation: observation}
	resources := append([]string{}, prior.ResourceIDs...)
	for _, resource := range observation.ResourceIDs {
		if !slices.Contains(resources, resource) {
			resources = append(resources, resource)
		}
	}
	sort.Strings(resources)
	acks := []string{"inherit-recovery-resources", "close-disk-addition"}
	risks := []string{
		"Closes the unresolved disk addition and releases this VM's locks; the addition never becomes a successful operation.",
		"Nothing is uploaded and no definition is changed by this disposition.",
	}
	switch {
	case request.Disposition == "accept":
		risks = append(risks, "The new disk stays in the saved definition with its verified volume; it is empty until the guest formats it.")
	case request.Disposition == "keep":
		// Keeping deletes nothing, so it needs no dependency proof. It is the way
		// out where that proof cannot be read, such as root-only pool images.
		risks = append(risks, "Nothing is deleted: the unused volume "+addition.Plan.Target.VolumeName+" stays in pool "+addition.Plan.Target.PoolName+" until you remove it.")
	case observation.VolumeState != "present":
		// Interrupted before its volume existed: nothing is left to delete, and
		// without this the addition would hold the VM and pool with no way out.
		risks = append(risks, "The new volume was never created, so nothing is deleted; if it appears before this runs, the plan is refused.")
	default:
		acks = append(acks, "host-mutation", "data-loss-delete-disks")
		risks = append(risks, "Permanently deletes the new volume that no definition names; deletion cannot be undone.")
	}
	step := domain.Step{ID: "dispose", Action: diskAddDisposeOperation,
		Preconditions:       []string{"the original actor's unresolved disk addition and every inherited lock", "a disposition that matches what the host shows now"},
		Idempotency:         "reconcile-before-retry",
		Compensation:        "Retain the disk, its volume and the inherited locks after any uncertainty",
		Reconciliation:      "Observe the durable disposition record; never delete or define again",
		CompletionPredicate: "A durable disposition closes the addition, and a deleted volume is absent"}
	return h.s.Engine.Plan(ctx, uid, r.Connection, diskAddDisposeOperation, resources, map[string]string{"disk-addition-plan": prior.ID}, in, []domain.Step{step}, acks, risks)
}

func (h *diskAddDisposeHandler) RecoveryParent(_ context.Context, _ domain.Plan, b []byte) (string, error) {
	var in diskAddDisposeInput
	if err := wire.Decode(b, &in); err != nil {
		return "", err
	}
	return in.ParentOperationID, nil
}

func parseDiskAddDispose(p domain.Plan, b []byte) (diskAddDisposeInput, error) {
	var in diskAddDisposeInput
	if wire.Decode(b, &in) != nil || in.Version != 1 || p.Operation != diskAddDisposeOperation ||
		in.ParentOperationID == "" || in.AdditionPlanID == "" ||
		(in.Disposition != "accept" && in.Disposition != "delete" && in.Disposition != "keep") ||
		len(p.Before) != 1 || p.Before["disk-addition-plan"] != in.AdditionPlanID {
		return in, domain.Fail("INVALID_INPUT", "Invalid disk addition disposition binding.")
	}
	return in, nil
}

func (h *diskAddDisposeHandler) Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error) {
	return domain.Estimates{RequiresDowntime: true, Notes: "The VM must already be stopped. Closing the addition changes no definition; a deleted volume frees its file's space."}, nil
}

func (h *diskAddDisposeHandler) Review(_ context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := parseDiskAddDispose(p, b)
	if err != nil {
		return nil, err
	}
	t := in.Plan.Target
	return map[string]any{"action": "dispose-disk-addition", "resource": t.VM, "parentOperationID": in.ParentOperationID,
		"additionPlanID": in.AdditionPlanID, "disposition": in.Disposition, "target": t.Target, "pool": t.PoolName,
		"volume": t.VolumeName, "referenced": in.Observation.Referenced, "volumeState": in.Observation.VolumeState,
		"diskDeletion": in.Disposition == "delete" && in.Observation.VolumeState == "present", "keepsUnusedVolume": in.Disposition == "keep" && in.Observation.VolumeState == "present",
		"changesDefinition": false, "uploadsVolumes": false}, nil
}

func (h *diskAddDisposeHandler) validate(ctx context.Context, p domain.Plan, in diskAddDisposeInput) error {
	if _, err := h.addition(p, in.AdditionPlanID); err != nil {
		return err
	}
	observation, err := h.backend.InspectAddedDiskDisposal(ctx, in.Plan, in.Disposition == "delete")
	if err != nil {
		return err
	}
	if observation.Referenced != in.Observation.Referenced || observation.VolumeState != in.Observation.VolumeState || observation.GraphDigest != in.Observation.GraphDigest {
		return domain.Fail("STALE_PLAN", "the disk, its volume or the dependency graph changed since the review")
	}
	if in.Disposition == "delete" && observation.Referenced {
		return domain.Fail("RESOURCE_BUSY", "this VM's saved definition now names the new disk; its volume cannot be deleted")
	}
	return nil
}

func (h *diskAddDisposeHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := parseDiskAddDispose(p, b)
	if err != nil {
		return err
	}
	return h.validate(ctx, p, in)
}

func (h *diskAddDisposeHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := parseDiskAddDispose(p, b)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "dispose" {
		return domain.Fail("INVALID_INPUT", "Durable disposition identity required.")
	}
	if err = h.validate(ctx, p, in); err != nil {
		return err
	}
	record := diskAddDisposition{Version: 1, PlanID: p.ID, OperationID: id, ParentOperationID: in.ParentOperationID,
		AdditionPlanID: in.AdditionPlanID, Disposition: in.Disposition, Observation: in.Observation}
	// The disposition is durable before any deletion, so a lost
	// acknowledgement is reconciled by observation instead of a second delete.
	if err = h.s.Store.ComparePut(diskAddDispositionKind, in.AdditionPlanID, nil, record); err != nil {
		return err
	}
	if in.Disposition != "delete" || in.Observation.VolumeState != "present" {
		return nil
	}
	if err = operations.Note(ctx, h.s.Store, "Intent persisted: delete the unreferenced new disk volume"); err != nil {
		return err
	}
	if err = h.backend.DeleteUnreferencedDisk(ctx, in.Plan, in.Observation); err != nil {
		return err
	}
	after, err := h.backend.InspectAddedDiskDisposal(ctx, in.Plan, false)
	if err != nil {
		return err
	}
	if after.VolumeState != "absent" {
		return domain.Fail("RECOVERY_REQUIRED", "the deletion returned but the volume is still present; do not delete again")
	}
	return nil
}

func (h *diskAddDisposeHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := parseDiskAddDispose(p, b)
	if err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "dispose" {
		return false, domain.Fail("INVALID_INPUT", "Durable disposition identity required.")
	}
	raw, err := h.s.Store.MetadataBytes(diskAddDispositionKind, in.AdditionPlanID)
	if err != nil {
		return false, err
	}
	var record diskAddDisposition
	if len(raw) == 0 || wire.Decode(raw, &record) != nil || record.Version != 1 || record.PlanID != p.ID ||
		record.OperationID != id || record.AdditionPlanID != in.AdditionPlanID || record.Disposition != in.Disposition {
		return false, domain.Fail("RECOVERY_REQUIRED", "no durable disposition for this addition; review a new disposition without replay")
	}
	if in.Disposition != "delete" {
		return true, ctx.Err()
	}
	after, err := h.backend.InspectAddedDiskDisposal(ctx, in.Plan, false)
	if err != nil {
		return false, err
	}
	return after.VolumeState == "absent", ctx.Err()
}
