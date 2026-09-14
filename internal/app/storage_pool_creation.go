package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"virmill.local/core/internal/backend/poolxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const storagePoolRecordKind = "storage-pool-creation-v1"

type storagePoolRecipe struct {
	Version    int                          `json:"version"`
	Definition domain.StoragePoolDefinition `json:"definition"`
}
type storagePoolRecord struct {
	Version    int                          `json:"version"`
	Connection string                       `json:"connection"`
	JobID      string                       `json:"jobID"`
	PlanID     string                       `json:"planID"`
	PlanDigest string                       `json:"planDigest"`
	Definition domain.StoragePoolDefinition `json:"definition"`
}

// DefaultStoragePoolPath is libvirt's standard images folder for a connection,
// the same folder other libvirt tools use for their default pool.
func DefaultStoragePoolPath(uri string) (string, error) {
	switch uri {
	case "qemu:///system":
		return "/var/lib/libvirt/images", nil
	case "qemu:///session":
		if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
			return filepath.Join(filepath.Clean(dir), "libvirt", "images"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) {
			return "", domain.Fail("INVALID_INPUT", "home folder unknown; choose a storage pool folder")
		}
		return filepath.Join(filepath.Clean(home), ".local", "share", "libvirt", "images"), nil
	}
	return "", domain.Fail("UNSUPPORTED_CAPABILITY", "storage pool creation requires qemu:///system or qemu:///session")
}

func storagePoolResources(uri string, d domain.StoragePoolDefinition) []string {
	out := []string{(domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool", UUID: d.UUID}).String(), (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool-allocation", UUID: "host"}).String()}
	slices.Sort(out)
	return out
}
func parseStoragePoolRecipe(p domain.Plan, raw []byte) (storagePoolRecipe, error) {
	var r storagePoolRecipe
	if wire.Decode(raw, &r) != nil || r.Version != 1 || p.Operation != "storage.pool.create" || (p.ConnectionID != "qemu:///system" && p.ConnectionID != "qemu:///session") || !slices.Equal(p.ResourceIDs, storagePoolResources(p.ConnectionID, r.Definition)) || len(p.Before) != 0 || poolxml.Validate(r.Definition) != nil {
		return r, domain.Fail("INVALID_INPUT", "invalid storage pool creation binding")
	}
	return r, nil
}
func storagePoolSteps(d domain.StoragePoolDefinition) []domain.Step {
	step := func(id, action, predicate string) domain.Step {
		return domain.Step{ID: id, Action: action, Preconditions: []string{"exact new pool identity", "no pool with the same name, UUID or an overlapping folder"}, Idempotency: "non-repeatable", Compensation: "Keep the pool definition and its folder for review; never remove folders, files or other pools", Reconciliation: "Observe the exact journal-bound pool by UUID without replaying definition or start", CompletionPredicate: predicate}
	}
	out := []domain.Step{step("define", "storage.pool.define", "Exact persistent directory pool exists"), step("start", "storage.pool.start", "Exact pool folder exists and the pool is active")}
	if d.Autostart {
		out = append(out, step("autostart", "storage.pool.autostart", "Exact active pool starts with the host"))
	}
	return out
}
func storagePoolRisks(d domain.StoragePoolDefinition) []string {
	start := "The pool starts now and again whenever the host starts"
	if !d.Autostart {
		start = "The pool starts now but not automatically after a host restart"
	}
	return []string{
		"Libvirt creates the folder if it is missing; an existing folder keeps its owner, permissions, security label and files",
		"Files already in the folder are listed as volumes of this pool; Virmill does not change or delete them",
		start,
		"Virmill never removes this pool or its folder automatically; other libvirt tools can still change them",
	}
}

// planStoragePoolCreation defaults to libvirt's standard "default" pool so a
// first VM needs no other tool; name, folder and autostart stay adjustable.
func (s *Service) planStoragePoolCreation(ctx context.Context, uid uint32, r Request) (domain.Plan, error) {
	var empty domain.Plan
	if r.ID != "" || r.Path != "" || r.After != 0 || r.Apply != nil || r.Action != "create" {
		return empty, domain.Fail("INVALID_INPUT", "storage pool creation accepts only name, path and autostart parameters")
	}
	if r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "storage pool creation requires qemu:///system or qemu:///session")
	}
	d := domain.StoragePoolDefinition{UUID: domain.ID(), Name: "default", Autostart: true}
	for key, value := range r.Input {
		switch key {
		case "name", "path":
			text, ok := value.(string)
			if !ok {
				return empty, domain.Fail("INVALID_INPUT", "storage pool "+key+" must be text")
			}
			if key == "name" && text != "" {
				d.Name = text
			}
			if key == "path" {
				d.Path = text
			}
		case "autostart":
			enabled, ok := value.(bool)
			if !ok {
				return empty, domain.Fail("INVALID_INPUT", "storage pool autostart must be true or false")
			}
			d.Autostart = enabled
		default:
			return empty, domain.Fail("INVALID_INPUT", "unknown storage pool parameter: "+key)
		}
	}
	if d.Path == "" {
		path, err := DefaultStoragePoolPath(r.Connection)
		if err != nil {
			return empty, err
		}
		d.Path = path
	}
	if err := poolxml.Validate(d); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	return s.Engine.Plan(ctx, uid, r.Connection, "storage.pool.create", storagePoolResources(r.Connection, d), nil, storagePoolRecipe{Version: 1, Definition: d}, storagePoolSteps(d), []string{"host-mutation"}, storagePoolRisks(d))
}

type storagePoolCreationHandler struct {
	s *Service
}

func (*storagePoolCreationHandler) RetainCompletedEffects() bool { return true }

func (h *storagePoolCreationHandler) Review(ctx context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	r, err := parseStoragePoolRecipe(p, raw)
	if err != nil {
		return nil, err
	}
	xml, err := poolxml.Render(r.Definition)
	if err != nil {
		return nil, err
	}
	return map[string]any{"definition": r.Definition, "poolXML": xml, "autostart": r.Definition.Autostart, "existingFilesChanged": false, "otherPoolsChanged": false}, ctx.Err()
}
func (h *storagePoolCreationHandler) Estimate(_ context.Context, p domain.Plan, raw []byte) (domain.Estimates, error) {
	r, err := parseStoragePoolRecipe(p, raw)
	if err != nil {
		return domain.Estimates{}, err
	}
	return domain.Estimates{Notes: "No disk space is reserved. VM disks created later in this pool use space in " + r.Definition.Path + "."}, nil
}
func (h *storagePoolCreationHandler) provider() (domain.StoragePoolCreationProvider, error) {
	provider, ok := h.s.Provider.(domain.StoragePoolCreationProvider)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native storage pool creation unavailable")
	}
	return provider, nil
}
func (h *storagePoolCreationHandler) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	r, err := parseStoragePoolRecipe(p, raw)
	if err != nil {
		return err
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	record, err := h.s.storagePoolRecord(p, r)
	if err != nil {
		return err
	}
	if record.JobID == "" {
		return provider.CheckStoragePoolCreation(ctx, p.ConnectionID, r.Definition)
	}
	if record.JobID != operations.OperationID(ctx) || record.PlanID != p.ID || record.PlanDigest != p.Digest {
		return domain.Fail("RESOURCE_BUSY", "storage pool identity is already reserved")
	}
	_, err = provider.InspectCreatedStoragePool(ctx, p.ConnectionID, r.Definition)
	return err
}
func (h *storagePoolCreationHandler) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	r, err := parseStoragePoolRecipe(p, raw)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" {
		return domain.Fail("INVALID_INPUT", "storage pool change requires durable job identity")
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	switch step.ID {
	case "define":
		if err = provider.CheckStoragePoolCreation(ctx, p.ConnectionID, r.Definition); err != nil {
			return err
		}
		record := storagePoolRecord{Version: r.Version, Connection: p.ConnectionID, JobID: id, PlanID: p.ID, PlanDigest: p.Digest, Definition: r.Definition}
		if err = h.s.Engine.Store.ComparePut(storagePoolRecordKind, r.Definition.UUID, nil, record); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		return provider.DefineStoragePool(ctx, p.ConnectionID, r.Definition)
	case "start":
		return provider.StartStoragePool(ctx, p.ConnectionID, r.Definition)
	case "autostart":
		if !r.Definition.Autostart {
			return domain.Fail("INVALID_INPUT", "autostart was not reviewed")
		}
		return provider.SetStoragePoolAutostart(ctx, p.ConnectionID, r.Definition)
	default:
		return domain.Fail("INVALID_INPUT", "unknown storage pool creation step")
	}
}
func (h *storagePoolCreationHandler) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	r, err := parseStoragePoolRecipe(p, raw)
	if err != nil {
		return false, err
	}
	record, err := h.s.storagePoolRecord(p, r)
	if err != nil {
		return false, err
	}
	if record.JobID == "" || record.JobID != operations.OperationID(ctx) || record.PlanID != p.ID {
		return false, domain.Fail("RECOVERY_REQUIRED", "exact storage pool reservation is absent; nothing is replayed")
	}
	provider, err := h.provider()
	if err != nil {
		return false, err
	}
	n, err := provider.InspectCreatedStoragePool(ctx, p.ConnectionID, r.Definition)
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	switch step.ID {
	case "define":
		return n.Persistent, nil
	case "start":
		return n.Persistent && n.Active, nil
	case "autostart":
		return n.Active && n.Autostart, nil
	default:
		return false, domain.Fail("INVALID_INPUT", "unknown storage pool reconciliation step")
	}
}
func (s *Service) storagePoolRecord(p domain.Plan, r storagePoolRecipe) (storagePoolRecord, error) {
	var out storagePoolRecord
	raw, err := s.Engine.Store.MetadataBytes(storagePoolRecordKind, r.Definition.UUID)
	if err != nil {
		return out, err
	}
	if len(raw) == 0 {
		return out, nil
	}
	if wire.Decode(raw, &out) != nil || out.Version != r.Version || out.Connection != p.ConnectionID || out.Definition != r.Definition {
		return out, domain.Fail("SOURCE_CHANGED", "storage pool reservation differs from its reviewed plan")
	}
	return out, nil
}
