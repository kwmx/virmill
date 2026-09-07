//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type cleanupHandler struct {
	s       *Service
	backend domain.CreationCleanupBackend
}
type cleanupInput struct {
	ParentOperationID string                 `json:"parentOperationID"`
	CreationPlanID    string                 `json:"creationPlanID"`
	Receipt           Receipt                `json:"receipt"`
	Disposition       string                 `json:"disposition"`
	Observation       domain.CreationCleanup `json:"observation"`
}
type cleanupProof struct {
	Version        int    `json:"schemaVersion"`
	PlanID         string `json:"planID"`
	OperationID    string `json:"operationID"`
	InputDigest    string `json:"inputDigest"`
	CreationPlanID string `json:"creationPlanID"`
	Intents        []bool `json:"deleteIntents"`
	Absent         []bool `json:"confirmedAbsent"`
}
type creationDisposition struct {
	Version         int                    `json:"schemaVersion"`
	PlanID          string                 `json:"planID"`
	OperationID     string                 `json:"operationID"`
	CreationPlanID  string                 `json:"creationPlanID"`
	Disposition     string                 `json:"disposition"`
	Observation     domain.CreationCleanup `json:"observation"`
	SourceDirectory string                 `json:"preservedSourceDirectory"`
}

func candidatesOf(r Receipt) []domain.CleanupCandidate {
	out := []domain.CleanupCandidate{}
	for _, v := range r.Volumes {
		out = append(out, domain.CleanupCandidate{Intent: v.Intent, Allocated: v.Allocated})
	}
	return out
}
func (h *cleanupHandler) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	b, err := json.Marshal(r.Input)
	if err != nil {
		return empty, err
	}
	if err = validation.Schema("vm-creation-cleanup-input", b); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	var request struct {
		Disposition string `json:"disposition"`
	}
	if err = wire.Decode(b, &request); err != nil {
		return empty, err
	}
	if request.Disposition != "retain" && request.Disposition != "delete" {
		return empty, domain.Fail("INVALID_INPUT", "choose an explicit retain or delete disposition")
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
		return empty, domain.Fail("PERMISSION_DENIED", "cleanup must use the original actor and connection")
	}
	originalID := prior.ID
	switch prior.Operation {
	case "vm.create", "vm.create.devices-v1":
	case "vm.create.resume":
		var old resumeInput
		if err = wire.Decode(encoded, &old); err != nil {
			return empty, err
		}
		originalID = old.CreationPlanID
	case "vm.create.cleanup":
		var old cleanupInput
		if err = wire.Decode(encoded, &old); err != nil {
			return empty, err
		}
		originalID = old.CreationPlanID
	default:
		return empty, domain.Fail("INVALID_INPUT", "cleanup needs an unresolved creation or its recovery operation")
	}
	receipt, err := h.s.load(originalID)
	if err != nil {
		return empty, err
	}
	in := cleanupInput{ParentOperationID: parent.ID, CreationPlanID: originalID, Receipt: receipt, Disposition: request.Disposition}
	_, recipe, _, err := h.original(prior, in)
	if err != nil {
		return empty, err
	}
	in.Observation, err = h.backend.InspectCreationCleanup(ctx, r.Connection, recipe.Target.Spec, candidatesOf(receipt), in.Disposition == "delete")
	if err != nil {
		return empty, err
	}
	resources := append([]string{}, prior.ResourceIDs...)
	additional := append([]string{}, in.Observation.ResourceIDs...)
	if in.Disposition == "delete" {
		for _, v := range candidatesOf(receipt) {
			if v.Allocated != nil && v.Allocated.Path != "" {
				additional = append(additional, "local-file|"+v.Allocated.Path)
			}
		}
	}
	for _, resource := range additional {
		found := false
		for _, old := range resources {
			found = found || old == resource
		}
		if !found {
			resources = append(resources, resource)
		}
	}
	sort.Strings(resources)
	acks := []string{"inherit-recovery-resources", "abandon-creation"}
	risks := []string{"Original creation remains partial and links to this disposition; it does not become a successful VM creation", "Original media and prepared source files are retained", "This permanently closes the original creation recipe; a new creation needs a new plan"}
	if in.Disposition == "delete" {
		acks = append(acks, "host-mutation", "delete-new-volumes")
		risks = append(risks, "Deletes only the reviewed newly allocated file generations; deletion cannot be undone", "External tools can race observations; any error retains inherited locks and requires observation or a fresh recovery plan", "Deletion currently requires inactive guests and a complete locally inspectable file-pool graph")
	} else {
		acks = append(acks, "retain-partial-volumes")
		risks = append(risks, "Records durable pins for every candidate, including unidentified or uncertain volumes; no file is deleted", "Pins remain protected from automatic collection; manual disposition of retained resources is a separate workflow")
	}
	step := domain.Step{ID: "dispose", Action: "vm.create.cleanup", Preconditions: []string{"original actor, receipt and all inherited locks", "intended VM UUID and name absent", "explicit retention or generation-bound deletion"}, Idempotency: "reconcile-before-retry", Compensation: "Retain originals, undeleted candidates and inherited locks after any uncertainty", Reconciliation: "Observe committed retention record or absence after each durable delete intent; never replay deletion", CompletionPredicate: "Original recipe closed with an explicit durable retention or verified deletion disposition"}
	return h.s.Engine.Plan(ctx, uid, r.Connection, "vm.create.cleanup", resources, map[string]string{"creation-plan": originalID}, in, []domain.Step{step}, acks, risks)
}
func (h *cleanupHandler) original(p domain.Plan, in cleanupInput) (domain.Plan, input, Receipt, error) {
	original, recipe, receipt, err := h.s.originalReceipt(p, resumeInput{CreationPlanID: in.CreationPlanID, Receipt: in.Receipt})
	if err != nil {
		return original, recipe, receipt, err
	}
	if err = h.s.requireUndisposed(original.ID); err != nil {
		return original, recipe, receipt, err
	}
	if receipt.Defined {
		return original, recipe, receipt, domain.Fail("RECOVERY_REQUIRED", "definition was observed; reconcile the creation instead of disposing its volumes")
	}
	if in.Disposition != "retain" && in.Disposition != "delete" {
		return original, recipe, receipt, domain.Fail("INVALID_INPUT", "explicit cleanup disposition required")
	}
	for _, candidate := range candidatesOf(receipt) {
		if a := candidate.Allocated; a != nil {
			if a.Path == recipe.Directory || strings.HasPrefix(a.Path, recipe.Directory+string(filepath.Separator)) {
				return original, recipe, receipt, domain.Fail("RECOVERY_REQUIRED", "allocation receipt overlaps preserved prepared source")
			}
		}
	}
	return original, recipe, receipt, nil
}
func (h *cleanupHandler) RecoveryParent(ctx context.Context, p domain.Plan, b []byte) (string, error) {
	var in cleanupInput
	if err := wire.Decode(b, &in); err != nil {
		return "", err
	}
	return in.ParentOperationID, nil
}
func (h *cleanupHandler) checkLocks(ctx context.Context, p domain.Plan, in cleanupInput) error {
	parent, err := h.s.Store.Job(in.ParentOperationID)
	if err != nil {
		return err
	}
	prior, _, err := h.s.Store.Plan(parent.PlanID)
	if err != nil {
		return err
	}
	if prior.ActorUID != p.ActorUID || prior.ConnectionID != p.ConnectionID {
		return domain.Fail("PERMISSION_DENIED", "cleanup parent authority differs")
	}
	owner := parent.ID
	accepted := parent.State == "partial" && parent.RecoveryOperationID != "" && parent.RecoveryOperationID == operations.OperationID(ctx)
	if accepted {
		owner = parent.RecoveryOperationID
	} else if parent.State != "recovery-required" || parent.RecoveryOperationID != "" {
		return domain.Fail("STALE_PLAN", "cleanup parent is no longer unresolved")
	}
	inherited := map[string]bool{}
	for _, id := range prior.ResourceIDs {
		inherited[id] = true
	}
	for _, resource := range p.ResourceIDs {
		owners, err := h.s.Store.ResourceJobs(resource)
		if err != nil {
			return err
		}
		if !accepted && !inherited[resource] && len(owners) == 0 {
			continue
		}
		if len(owners) != 1 || owners[0] != owner {
			return domain.Fail("RESOURCE_BUSY", "cleanup does not own the required storage/domain lock set")
		}
	}
	for resource := range inherited {
		found := false
		for _, r := range p.ResourceIDs {
			found = found || r == resource
		}
		if !found {
			return domain.Fail("RESOURCE_BUSY", "cleanup dropped an inherited lock")
		}
	}
	return nil
}
func (h *cleanupHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	var in cleanupInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	_, recipe, receipt, err := h.original(p, in)
	if err != nil {
		return err
	}
	if err = h.checkLocks(ctx, p, in); err != nil {
		return err
	}
	if in.Disposition == "delete" {
		for _, v := range candidatesOf(receipt) {
			if v.Allocated == nil || v.Allocated.Path == "" {
				continue
			}
			found := false
			for _, resource := range p.ResourceIDs {
				found = found || resource == "local-file|"+v.Allocated.Path
			}
			if !found {
				return domain.Fail("STALE_PLAN", "cleanup needs source-file concurrency locks; review a fresh plan")
			}
		}
	}
	current, err := h.backend.InspectCreationCleanup(ctx, p.ConnectionID, recipe.Target.Spec, candidatesOf(receipt), in.Disposition == "delete")
	if err != nil {
		return err
	}
	if !same(current, in.Observation) {
		return domain.Fail("STALE_PLAN", "cleanup identities or dependency graph changed; review a fresh plan")
	}
	if len(current.Volumes) != len(receipt.Volumes) {
		return domain.Fail("RECOVERY_REQUIRED", "backend omitted cleanup candidates")
	}
	for i, v := range current.Volumes {
		if !same(v.Candidate, candidatesOf(receipt)[i]) || (v.State != "present" && v.State != "absent" && v.State != "unknown") {
			return domain.Fail("RECOVERY_REQUIRED", "backend changed cleanup candidate identity")
		}
		if in.Disposition == "delete" && (v.State == "unknown" || (v.State == "present" && (v.Candidate.Allocated == nil || v.Candidate.Allocated.Generation == ""))) {
			return domain.Fail("RECOVERY_REQUIRED", "cleanup deletion requires exact allocation generations")
		}
	}
	if in.Disposition == "delete" {
		if current.GraphDigest == "" {
			return domain.Fail("RECOVERY_REQUIRED", "complete dependency proof missing")
		}
		return h.checkReferences(p, in)
	}
	return nil
}
func (h *cleanupHandler) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in cleanupInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	_, recipe, _, err := h.original(p, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"parentOperationID": in.ParentOperationID, "originalCreationPlanID": in.CreationPlanID, "disposition": in.Disposition, "volumes": in.Observation.Volumes, "dependencyGraphDigest": in.Observation.GraphDigest, "preservedSourceDirectory": recipe.Directory, "deletesVM": false, "deletesFirmware": false, "createsVM": false, "closesOriginalRecipe": true}, nil
}

// Every persisted metadata record and active operation is checked for references.
// The current creation receipt and its recovery ancestry are the only exceptions.
// Future manifest adapters must store their actual volume keys/paths, not just a
// private numeric indirection which would bypass this conservative reconciliation.
func (h *cleanupHandler) checkReferences(p domain.Plan, in cleanupInput) error {
	candidates := candidatesOf(in.Receipt)
	names := []string{}
	for _, v := range candidates {
		names = append(names, v.Intent.Name)
		if v.Allocated != nil {
			names = append(names, v.Allocated.Path, v.Allocated.BackendKey)
		}
	}
	var references func(any) bool
	references = func(value any) bool {
		switch x := value.(type) {
		case string:
			for _, name := range names {
				if name != "" && strings.Contains(x, name) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if references(child) {
					return true
				}
			}
		case map[string]any:
			for key, child := range x {
				if references(key) || references(child) {
					return true
				}
			}
		}
		return false
	}
	records, err := h.s.Store.MetadataRecords()
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Kind == "vm-creation" && r.ID == in.CreationPlanID {
			continue
		}
		var value any
		if err = wire.Decode(r.Body, &value); err != nil {
			return err
		}
		if references(value) {
			return domain.Fail("RESOURCE_BUSY", fmt.Sprintf("persisted %s record %s references a cleanup candidate", r.Kind, r.ID))
		}
	}
	ancestry := map[string]bool{}
	id := in.ParentOperationID
	for id != "" {
		if ancestry[id] {
			return domain.Fail("RECOVERY_REQUIRED", "recovery ancestry cycle")
		}
		ancestry[id] = true
		job, e := h.s.Store.Job(id)
		if e != nil {
			return e
		}
		id = job.RecoveryOf
	}
	jobs, err := h.s.Store.StorageReferenceJobs()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if ancestry[job.ID] || job.PlanID == p.ID {
			continue
		}
		switch job.State {
		case "queued", "running", "recovery-required", "interrupted", "partial":
		default:
			continue
		}
		_, encoded, e := h.s.Store.Plan(job.PlanID)
		if e != nil {
			return e
		}
		var value any
		if e = wire.Decode(encoded, &value); e != nil {
			return e
		}
		if references(value) {
			return domain.Fail("RESOURCE_BUSY", "another unresolved operation references a cleanup candidate")
		}
	}
	return nil
}
func (h *cleanupHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	if err := h.Validate(ctx, p, b); err != nil {
		return err
	}
	var in cleanupInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	_, recipe, receipt, err := h.original(p, in)
	if err != nil {
		return err
	}
	proof := cleanupProof{Version: 1, PlanID: p.ID, OperationID: operations.OperationID(ctx), InputDigest: p.InputDigest, CreationPlanID: in.CreationPlanID, Intents: make([]bool, len(receipt.Volumes)), Absent: make([]bool, len(receipt.Volumes))}
	var previous []byte
	save := func() error {
		if err := h.s.Store.ComparePut("vm-creation-cleanup", p.ID, previous, proof); err != nil {
			return err
		}
		var err error
		previous, err = json.Marshal(proof)
		return err
	}
	if err = save(); err != nil {
		return err
	}
	observation := in.Observation
	if in.Disposition == "delete" {
		for i, volume := range observation.Volumes {
			if canceled, e := operations.CancellationRequested(ctx, h.s.Store); e != nil {
				return e
			} else if canceled {
				return domain.Fail("RECOVERY_REQUIRED", "cleanup canceled; inherited candidates and locks retained")
			}
			if volume.State == "absent" {
				proof.Absent[i] = true
				if err = save(); err != nil {
					return err
				}
				continue
			}
			if err = h.checkReferences(p, in); err != nil {
				return err
			}
			proof.Intents[i] = true
			if err = save(); err != nil {
				return err
			}
			if err = operations.Note(ctx, h.s.Store, "Intent persisted: delete exact newly allocated volume generation "+volume.Candidate.Intent.Name); err != nil {
				return err
			}
			if err = h.backend.DeleteCreationVolume(ctx, p.ConnectionID, recipe.Target.Spec, candidatesOf(receipt), observation, i); err != nil {
				return err
			}
			current, e := h.backend.InspectCreationCleanup(ctx, p.ConnectionID, recipe.Target.Spec, candidatesOf(receipt), true)
			if e != nil {
				return e
			}
			expected := observation
			expected.Volumes = append([]domain.CleanupVolume{}, observation.Volumes...)
			expected.Volumes[i].State = "absent"
			expected.Volumes[i].Fingerprint = ""
			if !same(expected, current) {
				return domain.Fail("RECOVERY_REQUIRED", "deletion result or dependency graph differs from expected absence")
			}
			observation = current
			proof.Absent[i] = true
			if err = save(); err != nil {
				return err
			}
		}
	}
	if in.Disposition == "retain" {
		if canceled, e := operations.CancellationRequested(ctx, h.s.Store); e != nil {
			return e
		} else if canceled {
			return domain.Fail("RECOVERY_REQUIRED", "retention canceled; inherited locks remain held")
		}
		current, e := h.backend.InspectCreationCleanup(ctx, p.ConnectionID, recipe.Target.Spec, candidatesOf(receipt), false)
		if e != nil {
			return e
		}
		if !same(current, in.Observation) {
			return domain.Fail("STALE_PLAN", "retention state changed before committing pins")
		}
	} else {
		current, e := h.backend.InspectCreationCleanup(ctx, p.ConnectionID, recipe.Target.Spec, candidatesOf(receipt), false)
		if e != nil {
			return e
		}
		expected := dispositionRecord(p, in, recipe, proof).Observation
		if !same(current.Volumes, expected.Volumes) {
			return domain.Fail("RECOVERY_REQUIRED", "cleanup candidate reappeared or changed before final disposition")
		}
	}
	return h.finish(p, in, recipe, proof)
}
func dispositionRecord(p domain.Plan, in cleanupInput, recipe input, proof cleanupProof) creationDisposition {
	observation := in.Observation
	observation.Volumes = append([]domain.CleanupVolume{}, observation.Volumes...)
	if in.Disposition == "delete" {
		for i := range observation.Volumes {
			observation.Volumes[i].State = "absent"
			observation.Volumes[i].Reason = ""
			observation.Volumes[i].Fingerprint = ""
		}
	}
	return creationDisposition{Version: 1, PlanID: p.ID, OperationID: proof.OperationID, CreationPlanID: in.CreationPlanID, Disposition: in.Disposition, Observation: observation, SourceDirectory: recipe.Directory}
}
func (h *cleanupHandler) finish(p domain.Plan, in cleanupInput, recipe input, proof cleanupProof) error {
	record := dispositionRecord(p, in, recipe, proof)
	return h.s.Store.ComparePut("vm-creation-disposition", in.CreationPlanID, nil, record)
}
func (h *cleanupHandler) proof(p domain.Plan, in cleanupInput) (cleanupProof, error) {
	b, err := h.s.Store.MetadataBytes("vm-creation-cleanup", p.ID)
	if err != nil {
		return cleanupProof{}, err
	}
	return h.s.decodeCleanupProof(p, in, b)
}
func (s *Service) decodeCleanupProof(p domain.Plan, in cleanupInput, b []byte) (cleanupProof, error) {
	var proof cleanupProof
	if err := wire.Decode(b, &proof); err != nil {
		return proof, domain.Fail("RECOVERY_REQUIRED", "cleanup intent proof missing")
	}
	if proof.Version != 1 || proof.PlanID != p.ID || proof.InputDigest != p.InputDigest || proof.CreationPlanID != in.CreationPlanID || len(proof.Intents) != len(in.Receipt.Volumes) || len(proof.Absent) != len(proof.Intents) {
		return proof, domain.Fail("RECOVERY_REQUIRED", "cleanup proof identity or version differs")
	}
	job, err := s.Store.Job(proof.OperationID)
	if err != nil {
		return proof, err
	}
	if job.PlanID != p.ID {
		return proof, domain.Fail("RECOVERY_REQUIRED", "cleanup proof operation differs")
	}
	return proof, nil
}
func (h *cleanupHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	var in cleanupInput
	if err := wire.Decode(b, &in); err != nil {
		return false, err
	}
	proof, err := h.proof(p, in)
	if err != nil {
		return false, err
	}
	committed, err := h.s.Store.MetadataBytes("vm-creation-disposition", in.CreationPlanID)
	if err != nil {
		return false, err
	}
	if committed != nil {
		_, recipe, _, e := h.s.originalReceipt(p, resumeInput{CreationPlanID: in.CreationPlanID, Receipt: in.Receipt})
		if e != nil {
			return false, e
		}
		var record creationDisposition
		if err = wire.Decode(committed, &record); err != nil {
			return false, err
		}
		if !same(record, dispositionRecord(p, in, recipe, proof)) {
			return false, domain.Fail("RECOVERY_REQUIRED", "different cleanup disposition already committed")
		}
		return true, nil // The committed disposition, including retained pins, is durable.
	}
	_, recipe, receipt, err := h.original(p, in)
	if err != nil {
		return false, err
	}
	current, err := h.backend.InspectCreationCleanup(ctx, p.ConnectionID, recipe.Target.Spec, candidatesOf(receipt), false)
	if err != nil {
		return false, err
	}
	if len(current.Volumes) != len(in.Observation.Volumes) {
		return false, domain.Fail("RECOVERY_REQUIRED", "cleanup candidates missing")
	}
	if in.Disposition == "delete" {
		previous, err := h.s.Store.MetadataBytes("vm-creation-cleanup", p.ID)
		if err != nil {
			return false, err
		}
		for i, v := range current.Volumes {
			if !same(v.Candidate, in.Observation.Volumes[i].Candidate) || v.State != "absent" || (in.Observation.Volumes[i].State != "absent" && !proof.Intents[i]) {
				return false, domain.Fail("RECOVERY_REQUIRED", "cleanup has remaining or unproven volume disposition; deletion was not replayed")
			}
			proof.Absent[i] = true
		}
		if err = h.s.Store.ComparePut("vm-creation-cleanup", p.ID, previous, proof); err != nil {
			return false, err
		}
	} else if !same(current, in.Observation) {
		return false, domain.Fail("RECOVERY_REQUIRED", "retention state changed; review a fresh disposition")
	}
	return true, h.finish(p, in, recipe, proof)
}
func (s *Service) cleanupResult(job domain.Job, p domain.Plan, encoded []byte) (any, error) {
	var in cleanupInput
	if err := wire.Decode(encoded, &in); err != nil {
		return nil, err
	}
	proof, err := s.Store.MetadataBytes("vm-creation-cleanup", p.ID)
	if err != nil {
		return nil, err
	}
	disposition, err := s.Store.MetadataBytes("vm-creation-disposition", in.CreationPlanID)
	if err != nil {
		return nil, err
	}
	var recordedProof cleanupProof
	if proof != nil {
		if recordedProof, err = s.decodeCleanupProof(p, in, proof); err != nil {
			return nil, err
		}
		if recordedProof.OperationID != job.ID {
			return nil, domain.Fail("RECOVERY_REQUIRED", "cleanup result proof belongs to another operation")
		}
	}
	if disposition != nil {
		var record creationDisposition
		if err = wire.Decode(disposition, &record); err != nil {
			return nil, err
		}
		if record.Version != 1 || record.PlanID != p.ID || record.OperationID != job.ID || record.CreationPlanID != in.CreationPlanID || record.Disposition != in.Disposition {
			return nil, domain.Fail("RECOVERY_REQUIRED", "cleanup disposition identity or version differs")
		}
	}
	result := map[string]any{"operation": job, "cleanupProof": json.RawMessage(proof), "disposition": json.RawMessage(disposition), "cleanupProofAvailable": proof != nil, "dispositionAvailable": disposition != nil, "complete": false, "vmCreated": false, "guestBootVerified": false}
	if creationInProgress(job.State) {
		result["nextActions"] = []string{"operation watch " + job.ID, "operation show " + job.ID}
		return result, nil
	}
	if job.State != "succeeded" {
		return result, domain.Fail("RECOVERY_REQUIRED", "cleanup is unresolved; inspect candidates and reconcile or review a fresh disposition")
	}
	if proof == nil || disposition == nil {
		return result, domain.Fail("RECOVERY_REQUIRED", "successful cleanup lacks its proof or disposition; inspect without replay")
	}
	if in.Disposition == "delete" {
		for _, absent := range recordedProof.Absent {
			if !absent {
				return result, domain.Fail("RECOVERY_REQUIRED", "successful cleanup has unconfirmed volume deletion; inspect without replay")
			}
		}
	}
	result["complete"] = true
	return result, nil
}

var _ operations.RecoveryHandler = (*cleanupHandler)(nil)
