package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const diskRemovalOperation = "vm.remove-disks-v1"
const diskRemovalReceiptKind = "vm-disk-removal-receipt"

var removalTarget = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

type diskRemovalRecipe struct {
	Version int                `json:"version"`
	Removal domain.DiskRemoval `json:"removal"`
}

// Each externally visible effect has a durable intent and an acknowledged
// completion. Missing acknowledgements are never replaced by replay.
type diskRemovalReceipt struct {
	Version       int      `json:"version"`
	OperationID   string   `json:"operationID"`
	PlanID        string   `json:"planID"`
	PlanDigest    string   `json:"planDigest"`
	RemovalDigest string   `json:"removalDigest"`
	Definition    string   `json:"definition"`
	Disks         []string `json:"disks"`
}

func validDiskRemoval(r domain.DiskRemoval) bool {
	if !validRemoval(r.Definition) || len(r.Disks) < 1 || len(r.Disks) > 64 || !removalDigest(r.GraphDigest) || len(r.ResourceIDs) == 0 || len(r.ResourceIDs) > 16384 || !slices.IsSorted(r.ResourceIDs) || !slices.Contains(r.ResourceIDs, r.Definition.Resource.String()) {
		return false
	}
	paths, generations := map[string]bool{}, map[string]bool{}
	for i, disk := range r.Disks {
		if !removalTarget.MatchString(disk.Target) || (i > 0 && r.Disks[i-1].Target >= disk.Target) || !filepath.IsAbs(disk.Path) || filepath.Clean(disk.Path) != disk.Path || validation.SafeText(disk.Path) != disk.Path || len(disk.Path) > 4096 || !removalUUID.MatchString(disk.PoolID) || disk.PoolID == "00000000-0000-0000-0000-000000000000" || disk.VolumeName == "" || filepath.Base(disk.Path) != disk.VolumeName || disk.VolumeKey == "" || len(disk.VolumeKey) > 4096 || validation.SafeText(disk.VolumeKey) != disk.VolumeKey || disk.Generation == "" || len(disk.Generation) > 512 || validation.SafeText(disk.Generation) != disk.Generation || !removalDigest(disk.Fingerprint) || (disk.Format != "raw" && disk.Format != "qcow2") || disk.CapacityBytes == 0 || paths[disk.Path] || generations[disk.Generation] || !slices.Contains(r.Definition.RetainedSources, disk.Path) {
			return false
		}
		paths[disk.Path], generations[disk.Generation] = true, true
	}
	for i, id := range r.ResourceIDs {
		if id == "" || len(id) > 8192 || validation.SafeText(id) != id || (i > 0 && r.ResourceIDs[i-1] >= id) {
			return false
		}
	}
	return true
}

func (s *Service) planDiskRemoval(ctx context.Context, uid uint32, req Request) (domain.Plan, error) {
	var empty domain.Plan
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: req.Connection, Kind: "vm", UUID: req.ID}
	if !removalIdentity(key) || (req.Action != "" && req.Action != "remove") || req.Path != "" || req.After != 0 || req.Apply != nil {
		return empty, domain.Fail("INVALID_INPUT", "Choose one local VM and explicit disk targets to delete.")
	}
	b, err := json.Marshal(req.Input)
	var input struct {
		DeleteDisks []string `json:"deleteDisks"`
	}
	if err != nil || validation.Schema("vm-removal-input", b) != nil || wire.Decode(b, &input) != nil || len(input.DeleteDisks) < 1 || len(input.DeleteDisks) > 64 {
		return empty, domain.Fail("INVALID_INPUT", "deleteDisks must contain an explicit list of disk targets, such as vda. Omit it to keep every disk.")
	}
	slices.Sort(input.DeleteDisks)
	for i, target := range input.DeleteDisks {
		if !removalTarget.MatchString(target) || (i > 0 && input.DeleteDisks[i-1] == target) {
			return empty, domain.Fail("INVALID_INPUT", "Choose each disk target once; paths, wildcards and empty targets are not accepted.")
		}
	}
	provider, ok := s.Provider.(domain.DiskRemovalProvider)
	if !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "This provider cannot verify safe selected-disk removal.")
	}
	r, err := provider.InspectDiskRemoval(ctx, req.Connection, req.ID, input.DeleteDisks)
	if err != nil {
		return empty, err
	}
	if !validDiskRemoval(r) || r.Definition.Resource != key || !reflect.DeepEqual(diskRemovalTargets(r), input.DeleteDisks) {
		return empty, domain.Fail("INVALID_INPUT", "Disk inspection did not match the selected VM and exact requested disks.")
	}
	if err = s.bindRemovalInventory(ctx, r.Definition); err != nil {
		return empty, err
	}
	if err = s.checkDiskRemovalReferences(ctx, domain.Plan{}, r); err != nil {
		return empty, err
	}
	step := domain.Step{ID: "remove-vm-and-disks", Action: diskRemovalOperation, Preconditions: []string{"unchanged stopped VM and exact selected volume generations", "complete native and persisted dependency graph; no shared or uncertain references"}, Idempotency: "non-repeatable", Compensation: "Keep unselected disks and backups; retain remaining files and locks after any uncertain effect", Reconciliation: "Require per-effect durable receipts and exact VM/selected-volume absence; never replay deletion", CompletionPredicate: "VM definition and only the explicitly selected disks are absent"}
	return s.Engine.Plan(ctx, uid, req.Connection, diskRemovalOperation, slices.Clone(r.ResourceIDs), map[string]string{key.String(): r.Definition.Fingerprint}, diskRemovalRecipe{Version: 1, Removal: r}, []domain.Step{step}, []string{"host-mutation", "remove-vm-definition", "data-loss-delete-disks", "exclusive-lifecycle-writer", "exclusive-storage-writer"}, []string{"Permanent data loss: the selected disks are deleted after the VM definition is removed. No configuration backup or undo copy is created.", "Unselected disks, installer media, backing parents and backups are retained.", "Coordinate other VM and storage editors. A partial or uncertain result keeps remaining files and locks; never repeat deletion blindly."})
}

func diskRemovalTargets(r domain.DiskRemoval) []string {
	out := make([]string, len(r.Disks))
	for i, d := range r.Disks {
		out[i] = d.Target
	}
	return out
}
func parseDiskRemoval(p domain.Plan, b []byte) (diskRemovalRecipe, error) {
	var r diskRemovalRecipe
	if wire.Decode(b, &r) != nil || r.Version != 1 || p.Operation != diskRemovalOperation || !validDiskRemoval(r.Removal) || r.Removal.Definition.Resource.ConnectionID != p.ConnectionID || !reflect.DeepEqual(p.ResourceIDs, r.Removal.ResourceIDs) || len(p.Before) != 1 || p.Before[r.Removal.Definition.Resource.String()] != r.Removal.Definition.Fingerprint {
		return r, domain.Fail("INVALID_INPUT", "Invalid VM and disk removal recipe binding.")
	}
	return r, nil
}

type diskRemovalHandler struct{ s *Service }

func (h *diskRemovalHandler) Estimate(_ context.Context, p domain.Plan, b []byte) (domain.Estimates, error) {
	_, err := parseDiskRemoval(p, b)
	return domain.Estimates{RequiresDowntime: true, Notes: "VM must already be stopped. Deletes selected disk files permanently; no wipe, backup, guest restart or new allocation."}, err
}
func (h *diskRemovalHandler) Review(_ context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := parseDiskRemoval(p, b)
	if err != nil {
		return nil, err
	}
	r := in.Removal
	retained := []string{}
	for _, path := range r.Definition.RetainedSources {
		selected := false
		for _, d := range r.Disks {
			if d.Path == path {
				selected = true
			}
		}
		if !selected {
			retained = append(retained, path)
		}
	}
	return map[string]any{"action": "remove", "resource": r.Definition.Resource, "vmName": r.Definition.Name, "definitionSHA256": r.Definition.DefinitionSHA256, "deleteDisks": diskRemovalTargets(r), "disks": r.Disks, "retainedSources": retained, "diskDeletion": true, "backupsDeleted": false, "configurationRemoved": true, "backupCreated": false, "requiresStopped": true, "automaticStop": false}, nil
}
func (h *diskRemovalHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := parseDiskRemoval(p, b)
	if err != nil {
		return err
	}
	provider, ok := h.s.Provider.(domain.DiskRemovalProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "Disk removal provider unavailable.")
	}
	if err = h.s.bindRemovalInventory(ctx, in.Removal.Definition); err != nil {
		return err
	}
	states, err := provider.CheckDiskRemoval(ctx, in.Removal, false)
	if err != nil {
		return err
	}
	if !diskStatesAre(states, len(in.Removal.Disks), "present") {
		return domain.Fail("STALE_PLAN", "Selected disks must all remain present and unchanged before removal.")
	}
	return h.s.checkDiskRemovalReferences(ctx, p, in.Removal)
}
func diskStatesAre(states []string, n int, want string) bool {
	if len(states) != n {
		return false
	}
	for _, s := range states {
		if s != want {
			return false
		}
	}
	return true
}
func (h *diskRemovalHandler) saveReceipt(id string, previous *[]byte, r diskRemovalReceipt) error {
	if err := h.s.Engine.Store.ComparePut(diskRemovalReceiptKind, id, *previous, r); err != nil {
		return err
	}
	b, err := h.s.Engine.Store.MetadataBytes(diskRemovalReceiptKind, id)
	if err == nil {
		*previous = b
	}
	return err
}
func (h *diskRemovalHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := parseDiskRemoval(p, b)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "remove-vm-and-disks" {
		return domain.Fail("INVALID_INPUT", "Durable disk removal identity required.")
	}
	if err = h.Validate(ctx, p, b); err != nil {
		return err
	}
	provider := h.s.Provider.(domain.DiskRemovalProvider)
	definition, ok := h.s.Provider.(domain.DefinitionRemovalProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "Definition removal provider unavailable.")
	}
	digest, err := operations.Digest(in.Removal)
	if err != nil {
		return err
	}
	r := diskRemovalReceipt{Version: 1, OperationID: id, PlanID: p.ID, PlanDigest: p.Digest, RemovalDigest: digest, Definition: "intent", Disks: make([]string, len(in.Removal.Disks))}
	for i := range r.Disks {
		r.Disks[i] = "pending"
	}
	var previous []byte
	if err = h.saveReceipt(id, &previous, r); err != nil {
		return err
	}
	if err = definition.RemoveDefinition(ctx, in.Removal.Definition); err != nil {
		return err
	}
	r.Definition = "removed"
	if err = h.saveReceipt(id, &previous, r); err != nil {
		return err
	}
	for i := range r.Disks {
		if err = ctx.Err(); err != nil {
			return err
		}
		job, e := h.s.Engine.Store.Job(id)
		if e != nil {
			return e
		}
		if job.CancelRequested {
			return domain.Fail("RECOVERY_REQUIRED", "Removal stopped after definition removal. Remaining disks were kept; inspect this job before any further action.")
		}
		if err = h.s.checkDiskRemovalReferences(ctx, p, in.Removal); err != nil {
			return err
		}
		states, e := provider.CheckDiskRemoval(ctx, in.Removal, true)
		if e != nil {
			return e
		}
		if len(states) != len(r.Disks) {
			return domain.Fail("RECOVERY_REQUIRED", "Incomplete disk state observation.")
		}
		for j, state := range states {
			want := "present"
			if j < i {
				want = "absent"
			}
			if state != want {
				return domain.Fail("RECOVERY_REQUIRED", "Selected disk state differs from recorded effects; preserve remaining files.")
			}
		}
		r.Disks[i] = "intent"
		if err = h.saveReceipt(id, &previous, r); err != nil {
			return err
		}
		if err = provider.DeleteRemovalDisk(ctx, in.Removal, i); err != nil {
			return err
		}
		states, err = provider.CheckDiskRemoval(ctx, in.Removal, true)
		if err != nil {
			return err
		}
		if len(states) != len(r.Disks) || states[i] != "absent" {
			return domain.Fail("RECOVERY_REQUIRED", "Disk deletion returned but exact absence could not be confirmed.")
		}
		r.Disks[i] = "deleted"
		if err = h.saveReceipt(id, &previous, r); err != nil {
			return err
		}
	}
	return nil
}
func (h *diskRemovalHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := parseDiskRemoval(p, b)
	if err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "remove-vm-and-disks" {
		return false, domain.Fail("INVALID_INPUT", "Durable disk removal identity required.")
	}
	raw, err := h.s.Engine.Store.MetadataBytes(diskRemovalReceiptKind, id)
	if err != nil {
		return false, err
	}
	var r diskRemovalReceipt
	digest, err := operations.Digest(in.Removal)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 || wire.Decode(raw, &r) != nil || r.Version != 1 || r.OperationID != id || r.PlanID != p.ID || r.PlanDigest != p.Digest || r.RemovalDigest != digest || r.Definition != "removed" || !diskStatesAre(r.Disks, len(in.Removal.Disks), "deleted") {
		return false, domain.Fail("RECOVERY_REQUIRED", "Deletion is incomplete or an acknowledgement is missing. Preserve remaining files; this job will not replay removal.")
	}
	provider, ok := h.s.Provider.(domain.DiskRemovalProvider)
	if !ok {
		return false, domain.Fail("UNSUPPORTED_CAPABILITY", "Disk removal provider unavailable.")
	}
	states, err := provider.CheckDiskRemoval(ctx, in.Removal, true)
	if err != nil {
		return false, err
	}
	if !diskStatesAre(states, len(in.Removal.Disks), "absent") {
		return false, domain.Fail("RECOVERY_REQUIRED", "A selected volume remains or was recreated. It will not be deleted again.")
	}
	return true, ctx.Err()
}

func diskReferenceError(kind, id string) error {
	return domain.Fail("RESOURCE_BUSY", fmt.Sprintf("Recorded %s (%s) references a selected disk. Resolve that dependency before deletion.", kind, id))
}
func referencesRemoval(value any, r domain.DiskRemoval) bool {
	switch x := value.(type) {
	case string:
		for _, d := range r.Disks {
			for _, needle := range []string{d.Path, d.VolumeKey, d.VolumeName, d.Generation} {
				if needle != "" && strings.Contains(x, needle) {
					return true
				}
			}
		}
	case []any:
		for _, v := range x {
			if referencesRemoval(v, r) {
				return true
			}
		}
	case map[string]any:
		for k, v := range x {
			if referencesRemoval(k, r) || referencesRemoval(v, r) {
				return true
			}
		}
	}
	return false
}
