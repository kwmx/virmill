//go:build linux && amd64 && cgo

package creating

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

// ADR 0062: add one new empty disk to a stopped VM. The blank image is built in
// a private folder, uploaded through the same verified path as creation, and
// only then named in the VM's saved definition.
const diskAddOperation = "vm.disk.add-v1"
const diskAddReceiptKind = "vm-disk-add-receipt"
const diskAddReserveBytes = 64 << 20

// EmptyDiskTool builds a verified blank qcow2 inside its own workspace.
type EmptyDiskTool interface {
	CreateEmpty(context.Context, string, int64, int64) error
}

type diskAddInput struct {
	Version   int                     `json:"version"`
	Plan      domain.DiskAdditionPlan `json:"plan"`
	Cache     string                  `json:"cacheDirectory"`
	Workspace string                  `json:"workspace"`
}

type diskAddReceipt struct {
	Version     int                   `json:"version"`
	OperationID string                `json:"operationID"`
	PlanID      string                `json:"planID"`
	PlanDigest  string                `json:"planDigest"`
	Digest      string                `json:"digest"`
	Allocated   *domain.CreatedVolume `json:"allocated"`
	Verified    bool                  `json:"verified"`
	Defined     bool                  `json:"defined"`
}

type diskAddHandler struct {
	s       *Service
	backend domain.DiskAdditionBackend
	empty   EmptyDiskTool
}

// registerDiskAdd wires disk addition when the provider supports it.
func registerDiskAdd(service *app.Service, s *Service) {
	backend, ok := service.Provider.(domain.DiskAdditionBackend)
	if !ok {
		return
	}
	h := &diskAddHandler{s: s, backend: backend, empty: image.Tool{}}
	service.Engine.Handlers[diskAddOperation] = h
	service.Extensions["vm.disk.add"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		return h.Plan(ctx, uid, r)
	}
	// Every addition needs a reviewed way out: an unresolved job keeps this
	// VM and its pool locked until it is closed (ADR 0062).
	if disposal, ok := service.Provider.(domain.DiskAdditionDisposalBackend); ok {
		d := &diskAddDisposeHandler{s: s, backend: disposal}
		service.Engine.Handlers[diskAddDisposeOperation] = d
		service.Extensions["vm.disk.add.dispose"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
			return d.Plan(ctx, uid, r)
		}
	}
}

func diskFileBound(virtual int64) int64 { return virtual + virtual/4 + (16 << 20) }

func sealDiskFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// build creates the blank disk in a private folder under the coordinator's
// cache and records its exact bytes. The folder is removed on failure here and
// after the disk has been uploaded.
func (h *diskAddHandler) build(ctx context.Context, t domain.DiskAdditionTarget, virtual int64) (string, domain.VolumeIntent, error) {
	var intent domain.VolumeIntent
	// The private-cache checks are shared with reviewed seed generation.
	parent, root, _, err := seedCache(h.s.SeedCache)
	if err != nil {
		return "", intent, err
	}
	defer parent.Close()
	defer root.Close()
	name := "disk-add-" + domain.ID()
	if err = root.Mkdir(name, 0700); err != nil {
		return "", intent, err
	}
	if err = h.empty.CreateEmpty(ctx, filepath.Join(h.s.SeedCache, name), virtual, diskFileBound(virtual)); err != nil {
		_ = root.RemoveAll(name)
		return "", intent, err
	}
	digest, size, err := sealDiskFile(filepath.Join(h.s.SeedCache, name, "disk.qcow2"))
	if err != nil {
		_ = root.RemoveAll(name)
		return "", intent, err
	}
	intent = domain.VolumeIntent{PoolID: t.PoolID, Name: t.VolumeName, SourceID: name, VirtualBytes: uint64(virtual), FileBytes: uint64(size), SHA256: digest}
	return name, intent, nil
}

func (h *diskAddHandler) discard(in diskAddInput) {
	if in.Cache == "" || in.Workspace == "" {
		return
	}
	if root, err := os.OpenRoot(in.Cache); err == nil {
		_ = root.RemoveAll(in.Workspace)
		root.Close()
	}
}

func (h *diskAddHandler) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	if r.ID == "" || r.Path != "" || r.After != 0 || r.Apply != nil {
		return empty, domain.Fail("INVALID_INPUT", "Choose one local VM and the new disk's size.")
	}
	b, err := json.Marshal(r.Input)
	if err != nil {
		return empty, err
	}
	if validation.Schema("vm-disk-add-input", b) != nil {
		return empty, domain.Fail("INVALID_INPUT", `Give the new disk's size in GiB, for example {"sizeGiB":20}.`)
	}
	var request struct {
		SizeGiB uint64 `json:"sizeGiB"`
		Bus     string `json:"bus"`
	}
	if err = wire.Decode(b, &request); err != nil {
		return empty, err
	}
	target, err := h.backend.InspectDiskAddition(ctx, r.Connection, r.ID, request.Bus)
	if err != nil {
		return empty, err
	}
	name, intent, err := h.build(ctx, target, int64(request.SizeGiB)<<30)
	if err != nil {
		return empty, err
	}
	in := diskAddInput{Version: 1, Plan: domain.DiskAdditionPlan{Target: target, Volume: intent}, Cache: h.s.SeedCache, Workspace: name}
	if target.PoolAvailableBytes < intent.FileBytes+diskAddReserveBytes {
		h.discard(in)
		return empty, domain.Fail("INSUFFICIENT_SPACE", "this VM's storage pool does not have room for the new disk")
	}
	step := domain.Step{ID: "add-disk", Action: diskAddOperation,
		Preconditions:       []string{"unchanged stopped VM and saved definition", "the new volume name is absent from the pool"},
		Idempotency:         "reconcile-before-retry",
		Compensation:        "Keep the new volume and the unchanged VM after any uncertainty",
		Reconciliation:      "Observe the new volume and the saved definition; never upload or define twice",
		CompletionPredicate: "The reviewed volume is verified and the saved definition names the new disk"}
	acks := []string{"host-mutation", "exclusive-storage-writer", "exclusive-configuration-writer"}
	risks := []string{
		"Creates one new empty disk file in this VM's storage pool and names it in the saved definition.",
		"The new disk is empty: partition and format it inside the guest. Existing disks are not written.",
		"The VM must already be stopped; the disk appears the next time it starts.",
	}
	// The file starts nearly empty and grows as the guest writes, so a disk
	// larger than the pool's free space is an explicit overcommitment.
	if intent.VirtualBytes > target.PoolAvailableBytes {
		acks = append(acks, "pool-overcommit")
		risks = append(risks, "The disk's full size is larger than the pool's free space; the guest can run out of space as it writes.")
	}
	plan, err := h.s.Engine.Plan(ctx, uid, r.Connection, diskAddOperation, slices.Clone(target.ResourceIDs), map[string]string{target.VM.String(): target.VMFingerprint}, in, []domain.Step{step}, acks, risks)
	if err != nil {
		h.discard(in)
	}
	return plan, err
}

func parseDiskAdd(p domain.Plan, b []byte) (diskAddInput, error) {
	var in diskAddInput
	if wire.Decode(b, &in) != nil || in.Version != 1 || p.Operation != diskAddOperation ||
		in.Plan.Target.VM.ConnectionID != p.ConnectionID || !slices.Equal(p.ResourceIDs, in.Plan.Target.ResourceIDs) ||
		len(p.Before) != 1 || p.Before[in.Plan.Target.VM.String()] != in.Plan.Target.VMFingerprint ||
		!filepath.IsAbs(in.Cache) || filepath.Clean(in.Cache) != in.Cache ||
		in.Workspace == "" || filepath.Base(in.Workspace) != in.Workspace {
		return in, domain.Fail("INVALID_INPUT", "Invalid disk addition recipe binding.")
	}
	return in, nil
}

func (h *diskAddHandler) Estimate(_ context.Context, p domain.Plan, b []byte) (domain.Estimates, error) {
	in, err := parseDiskAdd(p, b)
	return domain.Estimates{RequiresDowntime: true, AdditionalBytes: in.Plan.Volume.FileBytes,
		Notes: "The VM must already be stopped. A new empty disk file is created in its pool; the guest must partition and format it. Existing disks are not written."}, err
}

func (h *diskAddHandler) Review(_ context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := parseDiskAdd(p, b)
	if err != nil {
		return nil, err
	}
	t := in.Plan.Target
	return map[string]any{"action": "add-disk", "resource": t.VM, "pool": t.PoolName, "volume": t.VolumeName,
		"target": t.Target, "bus": t.Bus, "sizeBytes": in.Plan.Volume.VirtualBytes, "fileBytes": in.Plan.Volume.FileBytes,
		"poolAvailableBytes": t.PoolAvailableBytes, "emptyDisk": true, "requiresStopped": true, "diskDeletion": false}, nil
}

func (h *diskAddHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := parseDiskAdd(p, b)
	if err != nil {
		return err
	}
	state, err := h.backend.CheckDiskAddition(ctx, in.Plan)
	if err != nil {
		return err
	}
	if state != "before" {
		return domain.Fail("STALE_PLAN", "This VM or the new disk changed since the review; review again.")
	}
	pool, err := h.s.Inventory.GetStoragePool(ctx, p.ConnectionID, in.Plan.Target.PoolID)
	if err != nil {
		return err
	}
	if !pool.Active || pool.State != "running" || pool.AvailableBytes == nil || *pool.AvailableBytes < in.Plan.Volume.FileBytes+diskAddReserveBytes {
		return domain.Fail("INSUFFICIENT_SPACE", "this VM's storage pool does not have room for the new disk")
	}
	return nil
}

func (h *diskAddHandler) save(id string, previous *[]byte, r diskAddReceipt) error {
	if err := h.s.Store.ComparePut(diskAddReceiptKind, id, *previous, r); err != nil {
		return err
	}
	b, err := h.s.Store.MetadataBytes(diskAddReceiptKind, id)
	if err == nil {
		*previous = b
	}
	return err
}

func (h *diskAddHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := parseDiskAdd(p, b)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "add-disk" {
		return domain.Fail("INVALID_INPUT", "Durable disk addition identity required.")
	}
	if err = h.Validate(ctx, p, b); err != nil {
		return err
	}
	digest, err := operations.Digest(in.Plan)
	if err != nil {
		return err
	}
	receipt := diskAddReceipt{Version: 1, OperationID: id, PlanID: p.ID, PlanDigest: p.Digest, Digest: digest}
	var previous []byte
	if err = h.save(id, &previous, receipt); err != nil {
		return err
	}
	// Until the definition changes, a failure has written at most a new,
	// unreferenced volume: nothing unsafe is in flight and nothing needs a
	// replay, so the job fails plainly and releases this VM's locks. The new
	// file is named so it can be removed.
	unreferenced := func(cause error, volume *domain.CreatedVolume) error {
		var refusal *domain.Error
		if !errors.As(cause, &refusal) {
			refusal = domain.Fail("OPERATION_FAILED", cause.Error())
		}
		copied := *refusal
		if volume != nil {
			copied.Resource = "local-file|" + volume.Path
			copied.SafeNextActions = []string{"delete the unused new disk file named in this error", "review the disk addition again"}
		}
		return operations.NotDone(&copied)
	}
	// The prepared blank disk must still be the reviewed bytes.
	path := filepath.Join(in.Cache, in.Workspace, "disk.qcow2")
	sealed, size, err := sealDiskFile(path)
	if err != nil {
		return unreferenced(err, nil)
	}
	if sealed != in.Plan.Volume.SHA256 || uint64(size) != in.Plan.Volume.FileBytes {
		return unreferenced(domain.Fail("SOURCE_CHANGED", "the prepared empty disk changed since the review"), nil)
	}
	created, err := h.s.Backend.AllocateVolume(ctx, p.ConnectionID, in.Plan.Volume)
	if err != nil {
		return unreferenced(err, nil)
	}
	receipt.Allocated = &created
	if err = h.save(id, &previous, receipt); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return unreferenced(err, &created)
	}
	err = h.s.Backend.PopulateVolume(ctx, p.ConnectionID, created, f)
	f.Close()
	if err != nil {
		return unreferenced(err, &created)
	}
	if err = h.s.Backend.VerifyCreatedVolume(ctx, p.ConnectionID, created); err != nil {
		return unreferenced(err, &created)
	}
	receipt.Verified = true
	if err = h.save(id, &previous, receipt); err != nil {
		return err
	}
	if err = h.backend.DefineAddedDisk(ctx, in.Plan); err != nil {
		return err
	}
	receipt.Defined = true
	if err = h.save(id, &previous, receipt); err != nil {
		return err
	}
	h.discard(in)
	return nil
}

func (h *diskAddHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := parseDiskAdd(p, b)
	if err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "add-disk" {
		return false, domain.Fail("INVALID_INPUT", "Durable disk addition identity required.")
	}
	raw, err := h.s.Store.MetadataBytes(diskAddReceiptKind, id)
	if err != nil {
		return false, err
	}
	digest, err := operations.Digest(in.Plan)
	if err != nil {
		return false, err
	}
	var receipt diskAddReceipt
	if len(raw) == 0 || wire.Decode(raw, &receipt) != nil || receipt.Version != 1 || receipt.OperationID != id ||
		receipt.PlanID != p.ID || receipt.PlanDigest != p.Digest || receipt.Digest != digest {
		return false, domain.Fail("RECOVERY_REQUIRED", "The disk addition receipt is missing or differs; the new disk will not be written again.")
	}
	state, err := h.backend.CheckDiskAddition(ctx, in.Plan)
	if err != nil {
		return false, err
	}
	if state == "added" && receipt.Verified {
		h.discard(in)
		return true, ctx.Err()
	}
	return false, ctx.Err()
}
