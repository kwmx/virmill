package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

// ADR 0062: copy one disk of a stopped VM into another storage pool and point
// the VM at the copy. The original is deleted afterwards unless the review kept
// it, and never during recovery.
const diskMoveOperation = "vm.disk.move-v1"
const diskMoveReceiptKind = "vm-disk-move-receipt"

type diskMoveRecipe struct {
	Version int             `json:"version"`
	Move    domain.DiskMove `json:"move"`
}

// The receipt records the intent, then how far the move actually got.
type diskMoveReceipt struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationID"`
	PlanID      string `json:"planID"`
	PlanDigest  string `json:"planDigest"`
	MoveDigest  string `json:"moveDigest"`
	State       string `json:"state"`
	// OldVolumeKept records that the original survived a move that asked for
	// its deletion, with the reason, so a leftover file is auditable instead of
	// silent. The move itself is complete either way: the VM uses the copy.
	OldVolumeKept   bool   `json:"oldVolumeKept,omitempty"`
	DeletionRefusal string `json:"deletionRefusal,omitempty"`
}

func validDiskMoveRecipe(m domain.DiskMove) bool {
	d := m.Disk
	if !removalIdentity(m.VM) || !removalDigest(m.VMFingerprint) || !removalDigest(m.DefinitionSHA256) ||
		!removalTarget.MatchString(d.Target) || !filepath.IsAbs(d.Path) || filepath.Clean(d.Path) != d.Path ||
		validation.SafeText(d.Path) != d.Path || !removalUUID.MatchString(d.PoolID) ||
		filepath.Base(d.Path) != d.VolumeName || d.VolumeKey == "" || d.Generation == "" ||
		!removalDigest(d.Fingerprint) || d.Format != "qcow2" || d.CapacityBytes == 0 {
		return false
	}
	if !removalUUID.MatchString(m.PoolID) || m.PoolID == d.PoolID || m.PoolName == "" || m.SourcePoolName == "" ||
		m.Volume.PoolID != m.PoolID || m.Volume.Name == "" || m.Volume.ContentType != "" ||
		m.Volume.FileBytes == 0 || m.Volume.VirtualBytes != d.CapacityBytes || !removalDigest(m.Volume.SHA256) ||
		!filepath.IsAbs(m.VolumePath) || filepath.Clean(m.VolumePath) != m.VolumePath ||
		validation.SafeText(m.VolumePath) != m.VolumePath || filepath.Base(m.VolumePath) != m.Volume.Name ||
		m.VolumePath == d.Path {
		return false
	}
	return slices.IsSorted(m.ResourceIDs) && slices.Contains(m.ResourceIDs, m.VM.String()) && len(m.ResourceIDs) == 4
}

func (s *Service) planDiskMove(ctx context.Context, uid uint32, req Request) (domain.Plan, error) {
	var empty domain.Plan
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: req.Connection, Kind: "vm", UUID: req.ID}
	if !removalIdentity(key) || req.Path != "" || req.After != 0 || req.Apply != nil {
		return empty, domain.Fail("INVALID_INPUT", "Choose one local VM, one of its disks and the storage pool to move it to.")
	}
	b, err := json.Marshal(req.Input)
	var input struct {
		Target      string `json:"target"`
		Pool        string `json:"pool"`
		KeepOldCopy bool   `json:"keepOldCopy"`
	}
	if err != nil || validation.Schema("vm-disk-move-input", b) != nil || wire.Decode(b, &input) != nil {
		return empty, domain.Fail("INVALID_INPUT", `Give the disk and the destination pool, for example {"target":"sdb","pool":"archive"}.`)
	}
	provider, ok := s.Provider.(domain.DiskMoveProvider)
	if !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "This provider cannot move disks.")
	}
	m, err := provider.InspectDiskMove(ctx, req.Connection, req.ID, input.Target, input.Pool, input.KeepOldCopy)
	if err != nil {
		return empty, err
	}
	if m.VM != key || m.Disk.Target != input.Target || m.PoolName != input.Pool || m.KeepOldCopy != input.KeepOldCopy {
		return empty, domain.Fail("INVALID_INPUT", "Disk inspection did not match the selected VM, disk and pool.")
	}
	if !validDiskMoveRecipe(m) {
		return empty, domain.Fail("INVALID_INPUT", "Disk inspection returned an incomplete move.")
	}
	acks := []string{"host-mutation", "exclusive-storage-writer", "exclusive-configuration-writer"}
	risks := []string{
		"Copies this disk into another pool and points the VM at the copy; the guest sees the same disk in the same place.",
		fmt.Sprintf("Both copies exist until the move finishes, so %s is needed in %s while it runs.", sizeText(m.Volume.FileBytes), m.PoolName),
		"The VM must already be stopped; nothing inside the guest is changed.",
	}
	if !m.KeepOldCopy {
		acks = append(acks, "data-loss-delete-old-copy")
		risks = append(risks, "The original disk in "+m.SourcePoolName+" is deleted once the VM uses the copy. If it cannot be deleted, the move still completes and the original is reported as kept.")
	}
	if m.Volume.FileBytes > m.DestinationAvailableBytes {
		acks = append(acks, "pool-overcommit")
		risks = append(risks, "The destination pool reports less free space than the copy needs; the copy can fail part way through.")
	}
	step := domain.Step{ID: "move-disk", Action: diskMoveOperation,
		Preconditions:       []string{"unchanged stopped VM and saved definition", "the destination volume name is absent", "no other VM or image uses this disk"},
		Idempotency:         "reconcile-before-retry",
		Compensation:        "Keep both copies and the unchanged VM after any uncertainty; the original is never deleted during recovery",
		Reconciliation:      "Observe the copy and the saved definition; never copy, define or delete twice",
		CompletionPredicate: "The verified copy exists and the saved definition names it instead of the original"}
	return s.Engine.Plan(ctx, uid, req.Connection, diskMoveOperation, slices.Clone(m.ResourceIDs),
		map[string]string{key.String(): m.VMFingerprint}, diskMoveRecipe{Version: 1, Move: m}, []domain.Step{step}, acks, risks)
}

func parseDiskMove(p domain.Plan, b []byte) (diskMoveRecipe, error) {
	var r diskMoveRecipe
	if wire.Decode(b, &r) != nil || r.Version != 1 || p.Operation != diskMoveOperation || !validDiskMoveRecipe(r.Move) ||
		r.Move.VM.ConnectionID != p.ConnectionID || !reflect.DeepEqual(p.ResourceIDs, r.Move.ResourceIDs) ||
		len(p.Before) != 1 || p.Before[r.Move.VM.String()] != r.Move.VMFingerprint {
		return r, domain.Fail("INVALID_INPUT", "Invalid disk move recipe binding.")
	}
	return r, nil
}

type diskMoveHandler struct{ s *Service }

func (h *diskMoveHandler) provider() (domain.DiskMoveProvider, error) {
	provider, ok := h.s.Provider.(domain.DiskMoveProvider)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Disk move provider unavailable.")
	}
	return provider, nil
}

func (h *diskMoveHandler) Estimate(_ context.Context, p domain.Plan, b []byte) (domain.Estimates, error) {
	in, err := parseDiskMove(p, b)
	return domain.Estimates{RequiresDowntime: true, AdditionalBytes: in.Move.Volume.FileBytes,
		Notes: "The VM must already be stopped. The disk is copied into the destination pool, so both copies exist until the move finishes. Nothing inside the guest is changed."}, err
}

func (h *diskMoveHandler) Review(_ context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := parseDiskMove(p, b)
	if err != nil {
		return nil, err
	}
	m := in.Move
	return map[string]any{"action": "move-disk", "resource": m.VM, "target": m.Disk.Target,
		"fromPool": m.SourcePoolName, "fromVolume": m.Disk.VolumeName, "toPool": m.PoolName, "volume": m.Volume.Name,
		"sizeBytes": m.Volume.FileBytes, "capacityBytes": m.Disk.CapacityBytes,
		"destinationAvailableBytes": m.DestinationAvailableBytes, "keepOldCopy": m.KeepOldCopy,
		"diskDeletion": !m.KeepOldCopy, "requiresStopped": true, "automaticStop": false}, nil
}

func (h *diskMoveHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := parseDiskMove(p, b)
	if err != nil {
		return err
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	state, err := provider.CheckDiskMove(ctx, in.Move)
	if err != nil {
		return err
	}
	if state != "before" {
		return domain.Fail("STALE_PLAN", "This disk has already been copied or moved; review again.")
	}
	return nil
}

func (h *diskMoveHandler) saveReceipt(id string, previous *[]byte, r diskMoveReceipt) error {
	if err := h.s.Engine.Store.ComparePut(diskMoveReceiptKind, id, *previous, r); err != nil {
		return err
	}
	b, err := h.s.Engine.Store.MetadataBytes(diskMoveReceiptKind, id)
	if err == nil {
		*previous = b
	}
	return err
}

// unreferencedCopy turns a failure that happened before the definition changed
// into a plain failure that releases this VM's locks, naming the copy so its
// space can be reclaimed. Nothing is in flight at that point: at most a new,
// unreferenced volume exists, exactly as a failed disk addition leaves one.
func unreferencedCopy(m domain.DiskMove, err error) error {
	var refusal *domain.Error
	if !errors.As(err, &refusal) {
		refusal = domain.Fail("OPERATION_FAILED", "the disk could not be copied")
	}
	failure := domain.Fail(refusal.Code, refusal.Message)
	failure.Resource = "local-file|" + m.VolumePath
	failure.SafeNextActions = []string{"Delete the unused copy " + m.Volume.Name + " in " + m.PoolName + " if it was created", "Review the move again"}
	return operations.NotDone(failure)
}

func refusalCode(err error) string {
	var refusal *domain.Error
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return "OPERATION_FAILED"
}

func (h *diskMoveHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := parseDiskMove(p, b)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "move-disk" {
		return domain.Fail("INVALID_INPUT", "Durable disk move identity required.")
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	digest, err := operations.Digest(in.Move)
	if err != nil {
		return err
	}
	r := diskMoveReceipt{Version: 1, OperationID: id, PlanID: p.ID, PlanDigest: p.Digest, MoveDigest: digest, State: "intent"}
	var previous []byte
	if err = h.saveReceipt(id, &previous, r); err != nil {
		return err
	}
	state, err := provider.CheckDiskMove(ctx, in.Move)
	if err != nil {
		return err
	}
	if state == "before" {
		if err = provider.CopyDiskVolume(ctx, in.Move); err != nil {
			// The definition has not changed, so nothing unsafe remains.
			if after, e := provider.CheckDiskMove(ctx, in.Move); e == nil && (after == "before" || after == "copied") {
				return unreferencedCopy(in.Move, err)
			}
			return err
		}
		r.State = "copied"
		if err = h.saveReceipt(id, &previous, r); err != nil {
			return err
		}
		state = "copied"
	}
	if state == "copied" {
		if err = provider.DefineMovedDisk(ctx, in.Move); err != nil {
			return err
		}
		r.State = "retargeted"
		if err = h.saveReceipt(id, &previous, r); err != nil {
			return err
		}
		state = "retargeted"
	}
	if state == "retargeted" && !in.Move.KeepOldCopy {
		if err = provider.DeleteOldVolume(ctx, in.Move); err != nil {
			// The VM already uses the copy, so the move is done. A refused
			// deletion is recorded, never retried here and never hidden.
			r.OldVolumeKept, r.DeletionRefusal = true, refusalCode(err)
			return h.saveReceipt(id, &previous, r)
		}
		r.State = "old-deleted"
		return h.saveReceipt(id, &previous, r)
	}
	if state != "retargeted" && state != "old-deleted" {
		return domain.Fail("RECOVERY_REQUIRED", "The move returned but the VM's disk could not be confirmed.")
	}
	return h.saveReceipt(id, &previous, r)
}

func (h *diskMoveHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := parseDiskMove(p, b)
	if err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "move-disk" {
		return false, domain.Fail("INVALID_INPUT", "Durable disk move identity required.")
	}
	raw, err := h.s.Engine.Store.MetadataBytes(diskMoveReceiptKind, id)
	if err != nil {
		return false, err
	}
	var r diskMoveReceipt
	digest, err := operations.Digest(in.Move)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 || wire.Decode(raw, &r) != nil || r.Version != 1 || r.OperationID != id || r.PlanID != p.ID ||
		r.PlanDigest != p.Digest || r.MoveDigest != digest ||
		(r.State != "intent" && r.State != "copied" && r.State != "retargeted" && r.State != "old-deleted") {
		return false, domain.Fail("RECOVERY_REQUIRED", "The move receipt is missing or differs; nothing will be copied, defined or deleted again.")
	}
	provider, err := h.provider()
	if err != nil {
		return false, err
	}
	state, err := provider.CheckDiskMove(ctx, in.Move)
	if err != nil {
		return false, err
	}
	// The move is complete once the VM uses the verified copy. Whether the
	// original was deleted is recorded in the receipt and never replayed.
	return state == "retargeted" || state == "old-deleted", ctx.Err()
}
