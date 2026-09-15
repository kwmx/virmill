package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

// ADR 0062: grow one disk of a stopped VM.
const diskGrowOperation = "vm.disk.grow-v1"
const diskGrowReceiptKind = "vm-disk-grow-receipt"

type diskGrowRecipe struct {
	Version int             `json:"version"`
	Grow    domain.DiskGrow `json:"grow"`
}

// The receipt records the intent before the resize and its confirmation after.
type diskGrowReceipt struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationID"`
	PlanID      string `json:"planID"`
	PlanDigest  string `json:"planDigest"`
	GrowDigest  string `json:"growDigest"`
	State       string `json:"state"`
}

func validDiskGrow(g domain.DiskGrow) bool {
	d := g.Disk
	if !removalIdentity(g.VM) || !removalDigest(g.VMFingerprint) || !removalTarget.MatchString(d.Target) || !filepath.IsAbs(d.Path) || filepath.Clean(d.Path) != d.Path || validation.SafeText(d.Path) != d.Path || !removalUUID.MatchString(d.PoolID) || filepath.Base(d.Path) != d.VolumeName || d.VolumeKey == "" || d.Generation == "" || !removalDigest(d.Fingerprint) || (d.Format != "raw" && d.Format != "qcow2") || d.CapacityBytes == 0 || g.CapacityBytes <= d.CapacityBytes {
		return false
	}
	return slices.IsSorted(g.ResourceIDs) && slices.Contains(g.ResourceIDs, g.VM.String()) && len(g.ResourceIDs) <= 16
}

func sizeText(bytes uint64) string {
	if bytes%(1<<30) == 0 {
		return fmt.Sprintf("%d GiB", bytes>>30)
	}
	return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
}

func (s *Service) planDiskGrow(ctx context.Context, uid uint32, req Request) (domain.Plan, error) {
	var empty domain.Plan
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: req.Connection, Kind: "vm", UUID: req.ID}
	if !removalIdentity(key) || req.Path != "" || req.After != 0 || req.Apply != nil {
		return empty, domain.Fail("INVALID_INPUT", "Choose one local VM and one of its disks to grow.")
	}
	b, err := json.Marshal(req.Input)
	var input struct {
		Target  string `json:"target"`
		SizeGiB uint64 `json:"sizeGiB"`
	}
	if err != nil || validation.Schema("vm-disk-grow-input", b) != nil || wire.Decode(b, &input) != nil {
		return empty, domain.Fail("INVALID_INPUT", `Give the disk and its new size in GiB, for example {"target":"vda","sizeGiB":40}.`)
	}
	provider, ok := s.Provider.(domain.DiskGrowProvider)
	if !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "This provider cannot grow disks.")
	}
	capacity := input.SizeGiB << 30
	g, err := provider.InspectDiskGrow(ctx, req.Connection, req.ID, input.Target, capacity)
	if err != nil {
		return empty, err
	}
	if g.VM != key || g.Disk.Target != input.Target || g.CapacityBytes != capacity {
		return empty, domain.Fail("INVALID_INPUT", "Disk inspection did not match the selected VM and disk.")
	}
	if capacity <= g.Disk.CapacityBytes {
		return empty, domain.Fail("INVALID_INPUT", fmt.Sprintf("%s is already %s; choose a larger size. Shrinking a disk is not supported.", input.Target, sizeText(g.Disk.CapacityBytes)))
	}
	if !validDiskGrow(g) {
		return empty, domain.Fail("INVALID_INPUT", "Disk inspection returned an incomplete disk identity.")
	}
	acks := []string{"host-mutation", "exclusive-storage-writer"}
	risks := []string{
		"Grows the virtual disk only: partitions and filesystems inside the guest keep their size until you extend them there.",
		"On a system connection libvirt resizes this VM's own disk image with qemu-img as root; imported or untrusted images are never resized.",
		"A grown disk is not shrunk back.",
	}
	if capacity-g.Disk.CapacityBytes > g.PoolAvailableBytes {
		acks = append(acks, "pool-overcommit")
		risks = append(risks, "The pool has less free space than the added size; the guest can run out of space as it writes.")
	}
	step := domain.Step{ID: "grow-disk", Action: diskGrowOperation, Preconditions: []string{"unchanged stopped VM and exact volume identity", "no other VM or image uses the volume"}, Idempotency: "reconcile-before-retry", Compensation: "Keep the disk; a grown disk is not shrunk back", Reconciliation: "Observe the volume's identity and capacity; never resize again", CompletionPredicate: "The same volume has the requested capacity"}
	return s.Engine.Plan(ctx, uid, req.Connection, diskGrowOperation, slices.Clone(g.ResourceIDs), map[string]string{key.String(): g.VMFingerprint}, diskGrowRecipe{Version: 1, Grow: g}, []domain.Step{step}, acks, risks)
}

func parseDiskGrow(p domain.Plan, b []byte) (diskGrowRecipe, error) {
	var r diskGrowRecipe
	if wire.Decode(b, &r) != nil || r.Version != 1 || p.Operation != diskGrowOperation || !validDiskGrow(r.Grow) || r.Grow.VM.ConnectionID != p.ConnectionID || !reflect.DeepEqual(p.ResourceIDs, r.Grow.ResourceIDs) || len(p.Before) != 1 || p.Before[r.Grow.VM.String()] != r.Grow.VMFingerprint {
		return r, domain.Fail("INVALID_INPUT", "Invalid disk grow recipe binding.")
	}
	return r, nil
}

type diskGrowHandler struct{ s *Service }

func (h *diskGrowHandler) provider() (domain.DiskGrowProvider, error) {
	provider, ok := h.s.Provider.(domain.DiskGrowProvider)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Disk grow provider unavailable.")
	}
	return provider, nil
}
func (h *diskGrowHandler) Estimate(_ context.Context, p domain.Plan, b []byte) (domain.Estimates, error) {
	_, err := parseDiskGrow(p, b)
	return domain.Estimates{RequiresDowntime: true, Notes: "The VM must already be stopped. The disk's virtual size grows; host space is used only as the guest writes. Partitions and filesystems inside the guest are not changed."}, err
}
func (h *diskGrowHandler) Review(_ context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := parseDiskGrow(p, b)
	if err != nil {
		return nil, err
	}
	g := in.Grow
	return map[string]any{"action": "grow-disk", "resource": g.VM, "target": g.Disk.Target, "path": g.Disk.Path, "beforeCapacityBytes": g.Disk.CapacityBytes, "afterCapacityBytes": g.CapacityBytes, "poolAvailableBytes": g.PoolAvailableBytes, "guestFilesystemsGrown": false, "requiresStopped": true, "automaticStop": false, "diskDeletion": false}, nil
}
func (h *diskGrowHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := parseDiskGrow(p, b)
	if err != nil {
		return err
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	state, err := provider.CheckDiskGrow(ctx, in.Grow)
	if err != nil {
		return err
	}
	if state != "before" {
		return domain.Fail("STALE_PLAN", "The disk already has the requested size or changed; review again.")
	}
	return nil
}
func (h *diskGrowHandler) saveReceipt(id string, previous *[]byte, r diskGrowReceipt) error {
	if err := h.s.Engine.Store.ComparePut(diskGrowReceiptKind, id, *previous, r); err != nil {
		return err
	}
	b, err := h.s.Engine.Store.MetadataBytes(diskGrowReceiptKind, id)
	if err == nil {
		*previous = b
	}
	return err
}
func (h *diskGrowHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := parseDiskGrow(p, b)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "grow-disk" {
		return domain.Fail("INVALID_INPUT", "Durable disk grow identity required.")
	}
	if err = h.Validate(ctx, p, b); err != nil {
		return err
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	digest, err := operations.Digest(in.Grow)
	if err != nil {
		return err
	}
	r := diskGrowReceipt{Version: 1, OperationID: id, PlanID: p.ID, PlanDigest: p.Digest, GrowDigest: digest, State: "intent"}
	var previous []byte
	if err = h.saveReceipt(id, &previous, r); err != nil {
		return err
	}
	if err = provider.GrowDisk(ctx, in.Grow); err != nil {
		// A resize that visibly did not happen leaves nothing to recover.
		if state, e := provider.CheckDiskGrow(ctx, in.Grow); e == nil && state == "before" {
			return operations.NotDone(err)
		}
		return err
	}
	state, err := provider.CheckDiskGrow(ctx, in.Grow)
	if err != nil {
		return err
	}
	if state != "grown" {
		return domain.Fail("RECOVERY_REQUIRED", "The resize returned but the new size could not be confirmed.")
	}
	r.State = "grown"
	return h.saveReceipt(id, &previous, r)
}
func (h *diskGrowHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := parseDiskGrow(p, b)
	if err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "grow-disk" {
		return false, domain.Fail("INVALID_INPUT", "Durable disk grow identity required.")
	}
	raw, err := h.s.Engine.Store.MetadataBytes(diskGrowReceiptKind, id)
	if err != nil {
		return false, err
	}
	var r diskGrowReceipt
	digest, err := operations.Digest(in.Grow)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 || wire.Decode(raw, &r) != nil || r.Version != 1 || r.OperationID != id || r.PlanID != p.ID || r.PlanDigest != p.Digest || r.GrowDigest != digest || (r.State != "intent" && r.State != "grown") {
		return false, domain.Fail("RECOVERY_REQUIRED", "The grow receipt is missing or differs; the disk will not be resized again.")
	}
	provider, err := h.provider()
	if err != nil {
		return false, err
	}
	state, err := provider.CheckDiskGrow(ctx, in.Grow)
	if err != nil {
		return false, err
	}
	return state == "grown", ctx.Err()
}
