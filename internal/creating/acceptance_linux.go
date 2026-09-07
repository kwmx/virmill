//go:build linux && amd64

package creating

import (
	"context"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const acceptanceOperation = "vm.create.accept-devices-v1"

type acceptanceInput struct {
	Version           int                                  `json:"schemaVersion"`
	ParentOperationID string                               `json:"parentOperationID"`
	CreationPlanID    string                               `json:"creationPlanID"`
	Receipt           Receipt                              `json:"receipt"`
	DevicePolicy      *domain.CreationDevicePolicy         `json:"devicePolicy"`
	Observation       domain.CreationAcceptanceObservation `json:"observation"`
}
type acceptanceProof struct {
	Version        int                                  `json:"schemaVersion"`
	PlanID         string                               `json:"planID"`
	InputDigest    string                               `json:"inputDigest"`
	OperationID    string                               `json:"operationID"`
	CreationPlanID string                               `json:"creationPlanID"`
	Observation    domain.CreationAcceptanceObservation `json:"observation"`
}
type acceptanceHandler struct {
	s       *Service
	backend domain.CreationAcceptanceBackend
}

func acceptanceOriginalID(prior domain.Plan, encoded []byte) (string, error) {
	switch prior.Operation {
	case "vm.create", "vm.create.devices-v1":
		return prior.ID, nil
	case "vm.create.resume":
		var old resumeInput
		if err := wire.Decode(encoded, &old); err != nil {
			return "", err
		}
		return old.CreationPlanID, nil
	case acceptanceOperation:
		var old acceptanceInput
		if err := wire.Decode(encoded, &old); err != nil {
			return "", err
		}
		if old.Version != 1 {
			return "", domain.Fail("RECOVERY_REQUIRED", "unsupported acceptance input version")
		}
		return old.CreationPlanID, nil
	default:
		return "", domain.Fail("INVALID_INPUT", "acceptance requires an uncertain creation, definition resume or device acceptance operation")
	}
}

func (h *acceptanceHandler) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	b, err := operations.Canonical(r.Input)
	if err != nil {
		return empty, err
	}
	if err = validation.Schema("vm-creation-accept-input", b); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	var request struct {
		DevicePolicy *domain.CreationDevicePolicy `json:"devicePolicy"`
	}
	if err = wire.Decode(b, &request); err != nil {
		return empty, err
	}
	parent, err := h.s.Store.Job(r.ID)
	if err != nil {
		return empty, err
	}
	prior, encoded, err := h.s.Store.Plan(parent.PlanID)
	if err != nil {
		return empty, err
	}
	if prior.ActorUID != uid || prior.ConnectionID != r.Connection {
		return empty, domain.Fail("PERMISSION_DENIED", "acceptance requires the original actor and connection")
	}
	id, err := acceptanceOriginalID(prior, encoded)
	if err != nil {
		return empty, err
	}
	receipt, err := h.s.load(id)
	if err != nil {
		return empty, err
	}
	in := acceptanceInput{Version: 1, ParentOperationID: parent.ID, CreationPlanID: id, Receipt: receipt, DevicePolicy: request.DevicePolicy}
	original, recipe, volumes, err := h.original(prior, in)
	if err != nil {
		return empty, err
	}
	in.Observation, err = h.backend.InspectCreationAcceptance(ctx, r.Connection, recipe.Target, in.DevicePolicy, volumes, receipt.Binding)
	if err != nil {
		return empty, err
	}
	acks := []string{"inherit-recovery-resources", "accept-observed-devices", "exclusive-external-writer"}
	if in.DevicePolicy.WatchdogAction == "reset" {
		acks = append(acks, "watchdog-reset")
	}
	step := domain.Step{ID: "accept", Action: acceptanceOperation, Preconditions: []string{"original actor, recipe, complete receipt and every inherited lock", "stopped persistent BIOS VM without autostart, saved state, snapshots or checkpoints", "exact reviewed device policy and remaining original configuration", "all retained bytes reverified under cooperative read guards"}, Idempotency: "reconcile-before-retry", Compensation: "Retain all files, definition and inherited locks on uncertainty", Reconciliation: "Reverify observed configuration and retained bytes; complete only the separate acceptance catalog record", CompletionPredicate: "Durable acceptance proof and matching managed ownership; original creation remains partial"}
	return h.s.Engine.Plan(ctx, uid, r.Connection, acceptanceOperation, append([]string{}, prior.ResourceIDs...), map[string]string{"creation-plan": original.ID, "vm": in.Observation.VMFingerprint}, in, []domain.Step{step}, acks, []string{"Accepts the explicitly listed existing chipset devices; no VM definition or storage content is changed", "Original creation stays partial and links to this recovery; it is never relabeled successful", "Cooperative QEMU read guards do not exclude arbitrary external writers; keep the VM and its files exclusively offline throughout acceptance", "A reset watchdog can reset a guest after a later separately authorized boot", "Guest boot, setup, networking and firmware/TPM recovery are not verified"})
}

func (h *acceptanceHandler) original(p domain.Plan, in acceptanceInput) (domain.Plan, input, []domain.CreatedVolume, error) {
	original, recipe, receipt, err := h.s.originalReceipt(p, resumeInput{CreationPlanID: in.CreationPlanID, Receipt: in.Receipt})
	if err != nil {
		return original, recipe, nil, err
	}
	if in.Version != 1 || in.DevicePolicy == nil {
		return original, recipe, nil, domain.Fail("RECOVERY_REQUIRED", "explicit supported acceptance input required")
	}
	if err = in.DevicePolicy.Validate(recipe.Target.Spec.Machine); err != nil {
		return original, recipe, nil, err
	}
	if receipt.Defined {
		return original, recipe, nil, domain.Fail("RECOVERY_REQUIRED", "original definition is already confirmed; use ordinary operation reconciliation")
	}
	if recipe.Target.Spec.Firmware.Mode != "bios" || recipe.Target.Spec.Firmware.TPM {
		return original, recipe, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "firmware/TPM acceptance needs the auxiliary-state recovery workflow")
	}
	if err = h.s.requireUndisposed(original.ID); err != nil {
		return original, recipe, nil, err
	}
	volumes, err := readyVolumes(receipt, recipe)
	return original, recipe, volumes, err
}

func (h *acceptanceHandler) RecoveryParent(ctx context.Context, p domain.Plan, b []byte) (string, error) {
	var in acceptanceInput
	if err := wire.Decode(b, &in); err != nil {
		return "", err
	}
	return in.ParentOperationID, nil
}

func (h *acceptanceHandler) validate(ctx context.Context, p domain.Plan, in acceptanceInput, ownerID string) error {
	original, recipe, volumes, err := h.original(p, in)
	if err != nil {
		return err
	}
	parent, err := h.s.Store.Job(in.ParentOperationID)
	if err != nil {
		return err
	}
	prior, encoded, err := h.s.Store.Plan(parent.PlanID)
	if err != nil {
		return err
	}
	id, err := acceptanceOriginalID(prior, encoded)
	if err != nil {
		return err
	}
	if id != original.ID || prior.ActorUID != p.ActorUID || prior.ConnectionID != p.ConnectionID {
		return domain.Fail("RECOVERY_REQUIRED", "acceptance parent identity differs")
	}
	owner := parent.ID
	if parent.State == "partial" && ownerID != "" && parent.RecoveryOperationID == ownerID {
		owner = ownerID
	} else if parent.State != "recovery-required" || parent.RecoveryOperationID != "" {
		return domain.Fail("STALE_PLAN", "acceptance parent is no longer unresolved")
	}
	if !same(p.ResourceIDs, prior.ResourceIDs) {
		return domain.Fail("RECOVERY_REQUIRED", "acceptance must inherit the complete original lock set")
	}
	for _, resource := range p.ResourceIDs {
		jobs, err := h.s.Store.ResourceJobs(resource)
		if err != nil {
			return err
		}
		if len(jobs) != 1 || jobs[0] != owner {
			return domain.Fail("RESOURCE_BUSY", "acceptance does not own every inherited lock")
		}
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: in.Receipt.VMID}
	catalog, err := h.s.Store.MetadataBytes("created-vm", key.String())
	if err != nil {
		return err
	}
	if catalog != nil {
		var existing ownershipRecord
		if err = wire.Decode(catalog, &existing); err != nil {
			return err
		}
		if existing.Version != 2 || existing.AcceptancePlanID != p.ID || ownerID == "" || existing.OperationID != ownerID {
			return domain.Fail("RECOVERY_REQUIRED", "ownership is already recorded; reconcile its acceptance instead of creating another plan")
		}
	}
	if err = h.s.checkSource(ctx, original, recipe); err != nil {
		return err
	}
	got, err := h.backend.InspectCreationAcceptance(ctx, p.ConnectionID, recipe.Target, in.DevicePolicy, volumes, in.Receipt.Binding)
	if err != nil {
		return err
	}
	if !same(got, in.Observation) {
		return domain.Fail("STALE_PLAN", "retained definition, files or acceptance environment changed")
	}
	return nil
}
func (h *acceptanceHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	var in acceptanceInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	return h.validate(ctx, p, in, operations.OperationID(ctx))
}
func (h *acceptanceHandler) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in acceptanceInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	_, recipe, volumes, err := h.original(p, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"parentOperationID": in.ParentOperationID, "originalCreationPlanID": in.CreationPlanID, "originalHardware": recipe.Target.Spec, "acceptedDevicePolicy": in.DevicePolicy, "retainedVolumes": volumes, "observation": in.Observation, "reverifyRetainedBytes": true, "changesVM": false, "startsVM": false, "allocatesVolumes": false, "uploadsVolumes": false, "deletesFiles": false, "guestBootVerified": false}, nil
}
func (h *acceptanceHandler) Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error) {
	return domain.Estimates{RequiresDowntime: true, Notes: "VM must already be stopped and remain stopped for full retained-file readback. No stop/start is performed. No new disk allocation; readback duration is not estimated."}, nil
}
func (h *acceptanceHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	if err := h.Validate(ctx, p, b); err != nil {
		return err
	}
	var in acceptanceInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	_, recipe, volumes, err := h.original(p, in)
	if err != nil {
		return err
	}
	if err = operations.Note(ctx, h.s.Store, "Acceptance intent: read all retained bytes and verify the reviewed stopped definition without changing it"); err != nil {
		return err
	}
	if err = h.s.cancellable(ctx, func(worker context.Context) error {
		return h.backend.VerifyCreationAcceptance(worker, p.ConnectionID, recipe.Target, in.DevicePolicy, volumes, in.Receipt.Binding, in.Observation)
	}); err != nil {
		return err
	}
	canceled, err := operations.CancellationRequested(ctx, h.s.Store)
	if err != nil {
		return err
	}
	if canceled {
		return domain.Fail("RECOVERY_REQUIRED", "acceptance canceled before proof commit; inherited locks retained")
	}
	proof := acceptanceProof{Version: 1, PlanID: p.ID, InputDigest: p.InputDigest, OperationID: operations.OperationID(ctx), CreationPlanID: in.CreationPlanID, Observation: in.Observation}
	return h.s.Store.ComparePut("vm-creation-acceptance", p.ID, nil, proof)
}

func (s *Service) acceptanceProof(p domain.Plan, in acceptanceInput) (acceptanceProof, bool, error) {
	var proof acceptanceProof
	digest, err := operations.Digest(in)
	planDigest, planErr := operations.PlanDigest(p)
	if err != nil || planErr != nil || p.Operation != acceptanceOperation || in.Version != 1 || digest != p.InputDigest || planDigest != p.Digest {
		return proof, false, domain.Fail("RECOVERY_REQUIRED", "acceptance plan or input binding differs")
	}
	b, err := s.Store.MetadataBytes("vm-creation-acceptance", p.ID)
	if err != nil || b == nil {
		return proof, false, err
	}
	if err = wire.Decode(b, &proof); err != nil {
		return proof, true, err
	}
	if proof.Version != 1 || proof.PlanID != p.ID || proof.InputDigest != p.InputDigest || proof.CreationPlanID != in.CreationPlanID || !same(proof.Observation, in.Observation) {
		return proof, true, domain.Fail("RECOVERY_REQUIRED", "acceptance proof binding or version differs")
	}
	j, err := s.Store.Job(proof.OperationID)
	if err != nil {
		return proof, true, err
	}
	if j.PlanID != p.ID || j.RecoveryOf != in.ParentOperationID {
		return proof, true, domain.Fail("RECOVERY_REQUIRED", "acceptance proof operation differs")
	}
	return proof, true, nil
}
func acceptedOwnership(p domain.Plan, in acceptanceInput, proof acceptanceProof, volumes []domain.CreatedVolume) ownershipRecord {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: in.Receipt.VMID}
	return ownershipRecord{Version: 2, Key: key, CreationPlanID: in.CreationPlanID, OperationID: proof.OperationID, Binding: in.Receipt.Binding, Volumes: volumes, AcceptancePlanID: p.ID}
}
func (h *acceptanceHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	var in acceptanceInput
	if err := wire.Decode(b, &in); err != nil {
		return false, err
	}
	proof, present, err := h.s.acceptanceProof(p, in)
	if err != nil {
		return false, err
	}
	if !present {
		return false, domain.Fail("RECOVERY_REQUIRED", "acceptance has no durable completed verification; review a new acceptance without replay")
	}
	if err = h.validate(ctx, p, in, proof.OperationID); err != nil {
		return false, err
	}
	_, recipe, volumes, err := h.original(p, in)
	if err != nil {
		return false, err
	}
	if err = h.backend.VerifyCreationAcceptance(ctx, p.ConnectionID, recipe.Target, in.DevicePolicy, volumes, in.Receipt.Binding, in.Observation); err != nil {
		return false, err
	}
	record := acceptedOwnership(p, in, proof, volumes)
	old, err := h.s.Store.MetadataBytes("created-vm", record.Key.String())
	if err != nil {
		return false, err
	}
	if old != nil {
		var existing ownershipRecord
		if err = wire.Decode(old, &existing); err != nil {
			return false, err
		}
		if !same(record, existing) {
			return false, domain.Fail("RECOVERY_REQUIRED", "acceptance ownership catalog conflicts; retain locks")
		}
		return true, nil
	}
	return true, h.s.Store.ComparePut("created-vm", record.Key.String(), nil, record)
}

func (s *Service) acceptanceResult(j domain.Job, p domain.Plan, b []byte) (any, error) {
	var in acceptanceInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	if in.Version != 1 {
		return nil, domain.Fail("RECOVERY_REQUIRED", "unsupported acceptance result version")
	}
	proof, present, err := s.acceptanceProof(p, in)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"operation": j, "acceptanceProof": nil, "acceptanceProofAvailable": present, "complete": false, "originalCreationPlanID": in.CreationPlanID, "guestBootVerified": false, "setupVerified": false, "connectivityVerified": false}
	if present {
		result["acceptanceProof"] = proof
	}
	if creationInProgress(j.State) {
		result["nextActions"] = []string{"operation watch " + j.ID, "operation show " + j.ID}
		return result, nil
	}
	if j.State != "succeeded" || !present || proof.OperationID != j.ID {
		return result, domain.Fail("RECOVERY_REQUIRED", "acceptance is incomplete; retained resources require reconciliation or fresh review")
	}
	_, recipe, receipt, err := s.originalReceipt(p, resumeInput{CreationPlanID: in.CreationPlanID, Receipt: in.Receipt})
	if err != nil {
		return result, err
	}
	volumes, err := readyVolumes(receipt, recipe)
	if err != nil {
		return result, err
	}
	want := acceptedOwnership(p, in, proof, volumes)
	b, err = s.Store.MetadataBytes("created-vm", want.Key.String())
	if err != nil {
		return result, err
	}
	var got ownershipRecord
	if err = wire.Decode(b, &got); err != nil {
		return result, domain.Fail("RECOVERY_REQUIRED", "acceptance ownership record unavailable")
	}
	if !same(got, want) {
		return result, domain.Fail("RECOVERY_REQUIRED", "acceptance ownership record differs")
	}
	result["complete"] = true
	return result, nil
}

var _ operations.RecoveryHandler = (*acceptanceHandler)(nil)
