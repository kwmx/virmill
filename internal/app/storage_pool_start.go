package app

import (
	"context"
	"regexp"
	"slices"

	"virmill.local/core/internal/backend/poolxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

var startPoolUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var startablePoolTypes = []string{"dir", "fs", "netfs"}

type storagePoolStartRecipe struct {
	Version   int    `json:"version"`
	PoolID    string `json:"poolID"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Path      string `json:"path"`
	Autostart bool   `json:"autostart"`
}

func storagePoolKey(uri, id string) string {
	return (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool", UUID: id}).String()
}
func parseStoragePoolStart(p domain.Plan, raw []byte) (storagePoolStartRecipe, string, error) {
	var r storagePoolStartRecipe
	if wire.Decode(raw, &r) != nil || r.Version != 1 || p.Operation != "storage.pool.start" || (p.ConnectionID != "qemu:///system" && p.ConnectionID != "qemu:///session") || !startPoolUUID.MatchString(r.PoolID) || r.Name == "" || !slices.Contains(startablePoolTypes, r.Type) {
		return r, "", domain.Fail("INVALID_INPUT", "invalid storage pool start binding")
	}
	key := storagePoolKey(p.ConnectionID, r.PoolID)
	if !slices.Equal(p.ResourceIDs, []string{key}) || len(p.Before) != 1 || p.Before[key] == "" {
		return r, "", domain.Fail("INVALID_INPUT", "invalid storage pool start binding")
	}
	return r, key, nil
}

// planStoragePoolStart reviews starting one existing stopped pool. By default
// it also starts with the host, so VM disks stay available after a reboot.
func (s *Service) planStoragePoolStart(ctx context.Context, uid uint32, r Request) (domain.Plan, error) {
	var empty domain.Plan
	if !startPoolUUID.MatchString(r.ID) || r.Path != "" || r.After != 0 || r.Apply != nil || r.Action != "start" {
		return empty, domain.Fail("INVALID_INPUT", "storage pool start needs one pool UUID and only the autostart parameter")
	}
	if r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "storage pool start requires qemu:///system or qemu:///session")
	}
	autostart := true
	for key, value := range r.Input {
		enabled, ok := value.(bool)
		if key != "autostart" || !ok {
			return empty, domain.Fail("INVALID_INPUT", "storage pool start accepts only autostart true or false")
		}
		autostart = enabled
	}
	inventory, ok := s.Provider.(domain.ResourceInventory)
	if !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "selected backend does not expose storage inventory")
	}
	pool, err := inventory.GetStoragePool(ctx, r.Connection, r.ID)
	if err != nil {
		return empty, err
	}
	switch {
	case pool.Key.UUID != r.ID || pool.Name == "" || pool.Fingerprint == "":
		return empty, domain.Fail("SOURCE_CHANGED", "storage pool identity could not be verified")
	case pool.Active:
		return empty, domain.Fail("INVALID_STATE", "storage pool "+pool.Name+" is already running")
	case !pool.Persistent:
		return empty, domain.Fail("INVALID_STATE", "storage pool "+pool.Name+" is not persistent")
	case !slices.Contains(startablePoolTypes, pool.Type):
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "only file-based pools (dir, fs, netfs) are started")
	}
	observed, _ := poolxml.Parse(pool.XML)
	recipe := storagePoolStartRecipe{Version: 1, PoolID: pool.Key.UUID, Name: pool.Name, Type: pool.Type, Path: observed.Path, Autostart: autostart && !pool.Autostart}
	key := storagePoolKey(r.Connection, pool.Key.UUID)
	step := func(id, action, predicate string) domain.Step {
		return domain.Step{ID: id, Action: action, Preconditions: []string{"unchanged pool fingerprint", "pool stopped and persistent"}, Idempotency: "non-repeatable", Compensation: "Leave the pool as observed; never stop, redefine or delete it", Reconciliation: "Observe the pool by UUID without replaying the start", CompletionPredicate: predicate}
	}
	steps := []domain.Step{step("start", "storage.pool.start", "Exact pool is active")}
	starts := "Automatic start with the host stays off"
	if pool.Autostart {
		starts = "Automatic start with the host is already on"
	}
	if recipe.Autostart {
		steps = append(steps, step("autostart", "storage.pool.autostart", "Exact active pool starts with the host"))
		starts = "The pool also starts whenever the host starts"
	}
	risks := []string{"Starts the existing pool exactly as it is defined; its folder and files are not created or changed", "Starting fails if the pool's folder or storage is missing", starts}
	return s.Engine.Plan(ctx, uid, r.Connection, "storage.pool.start", []string{key}, map[string]string{key: pool.Fingerprint}, recipe, steps, []string{"host-mutation"}, risks)
}

type storagePoolStartHandler struct {
	s *Service
}

func (*storagePoolStartHandler) RetainCompletedEffects() bool { return true }

func (h *storagePoolStartHandler) Review(ctx context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	r, _, err := parseStoragePoolStart(p, raw)
	if err != nil {
		return nil, err
	}
	pool := map[string]any{"uuid": r.PoolID, "name": r.Name, "type": r.Type, "path": r.Path}
	return map[string]any{"pool": pool, "enableAutostart": r.Autostart, "folderChanged": false}, ctx.Err()
}
func (h *storagePoolStartHandler) Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error) {
	return domain.Estimates{Notes: "Starts an existing pool. No disk space is reserved and nothing is created."}, nil
}
func (h *storagePoolStartHandler) pool(ctx context.Context, p domain.Plan, r storagePoolStartRecipe) (domain.StoragePool, error) {
	inventory, ok := h.s.Provider.(domain.ResourceInventory)
	if !ok {
		return domain.StoragePool{}, domain.Fail("UNSUPPORTED_CAPABILITY", "selected backend does not expose storage inventory")
	}
	pool, err := inventory.GetStoragePool(ctx, p.ConnectionID, r.PoolID)
	if err != nil {
		return pool, err
	}
	if pool.Key.UUID != r.PoolID || pool.Name != r.Name || pool.Type != r.Type || !pool.Persistent {
		return pool, domain.Fail("STALE_PLAN", "storage pool is not the reviewed persistent pool")
	}
	return pool, nil
}
func (h *storagePoolStartHandler) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	r, key, err := parseStoragePoolStart(p, raw)
	if err != nil {
		return err
	}
	if _, ok := h.s.Provider.(domain.StoragePoolStartProvider); !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "native storage pool start unavailable")
	}
	pool, err := h.pool(ctx, p, r)
	if err != nil {
		return err
	}
	if pool.Active {
		// Only this job's own start step may leave the pool running.
		if operations.OperationID(ctx) == "" {
			return domain.Fail("INVALID_STATE", "storage pool is already running")
		}
		return nil
	}
	if pool.Fingerprint != p.Before[key] {
		return domain.Fail("STALE_PLAN", "storage pool changed since review")
	}
	return nil
}
func (h *storagePoolStartHandler) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	r, key, err := parseStoragePoolStart(p, raw)
	if err != nil {
		return err
	}
	provider, ok := h.s.Provider.(domain.StoragePoolStartProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "native storage pool start unavailable")
	}
	switch step.ID {
	case "start":
		return provider.StartExistingStoragePool(ctx, p.ConnectionID, r.PoolID, p.Before[key])
	case "autostart":
		if !r.Autostart {
			return domain.Fail("INVALID_INPUT", "autostart was not reviewed")
		}
		return provider.EnableStoragePoolAutostart(ctx, p.ConnectionID, r.PoolID)
	default:
		return domain.Fail("INVALID_INPUT", "unknown storage pool start step")
	}
}
func (h *storagePoolStartHandler) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	r, _, err := parseStoragePoolStart(p, raw)
	if err != nil {
		return false, err
	}
	pool, err := h.pool(ctx, p, r)
	if err != nil {
		return false, err
	}
	switch step.ID {
	case "start":
		return pool.Active, ctx.Err()
	case "autostart":
		return pool.Active && pool.Autostart, ctx.Err()
	default:
		return false, domain.Fail("INVALID_INPUT", "unknown storage pool start step")
	}
}
