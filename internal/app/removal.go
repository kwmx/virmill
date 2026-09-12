package app

import (
	"context"
	"encoding/hex"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

var removalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type removalRecipe struct {
	Version    int                      `json:"version"`
	Definition domain.DefinitionRemoval `json:"definition"`
}
type removalReceipt struct {
	Version     int                      `json:"version"`
	OperationID string                   `json:"operationID"`
	PlanID      string                   `json:"planID"`
	PlanDigest  string                   `json:"planDigest"`
	Definition  domain.DefinitionRemoval `json:"definition"`
}

func removalIdentity(key domain.ResourceKey) bool {
	return key.ProviderID == "libvirt" && key.Kind == "vm" && (key.ConnectionID == "qemu:///system" || key.ConnectionID == "qemu:///session") && removalUUID.MatchString(key.UUID) && key.UUID != "00000000-0000-0000-0000-000000000000"
}
func removalDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == strings.ToLower(s)
}
func validRemoval(d domain.DefinitionRemoval) bool {
	if !removalIdentity(d.Resource) || !removalDigest(d.Fingerprint) || !removalDigest(d.DefinitionSHA256) || d.Name == "" || len(d.Name) > 4096 || !utf8.ValidString(d.Name) || validation.SafeText(d.Name) != d.Name || len(d.RetainedSources) > 4096 {
		return false
	}
	for i, s := range d.RetainedSources {
		if s == "" || len(s) > 4096 || !utf8.ValidString(s) || strings.ContainsRune(s, 0) || (i > 0 && d.RetainedSources[i-1] >= s) {
			return false
		}
	}
	return true
}
func (s *Service) planRemoval(ctx context.Context, uid uint32, r Request) (domain.Plan, error) {
	var empty domain.Plan
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "vm", UUID: r.ID}
	if !removalIdentity(key) || (r.Action != "" && r.Action != "remove") || r.Path != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 {
		return empty, domain.Fail("INVALID_INPUT", "Choose one local VM UUID. Remove keeps its disks; extra settings and disk deletion are not accepted.")
	}
	provider, ok := s.Provider.(domain.DefinitionRemovalProvider)
	if !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "This provider cannot safely remove a VM definition while retaining storage.")
	}
	d, err := provider.InspectDefinitionRemoval(ctx, r.Connection, r.ID)
	if err != nil {
		return empty, err
	}
	if !validRemoval(d) || d.Resource != key {
		return empty, domain.Fail("INVALID_INPUT", "Removal inspection did not match the selected VM and its storage declarations.")
	}
	if err := s.bindRemovalInventory(ctx, d); err != nil {
		return empty, err
	}
	step := domain.Step{ID: "remove-definition", Action: "vm.remove-definition-v1", Preconditions: []string{"unchanged stopped persistent definition", "no autostart, saved state, snapshots, checkpoints or auxiliary firmware state"}, Idempotency: "non-repeatable", Compensation: "Retain disks and backups; inspect uncertain removal without replay", Reconciliation: "Require a bound durable removal receipt and exact native UUID absence", CompletionPredicate: "Definition removed; no storage deletion requested"}
	return s.Engine.Plan(ctx, uid, r.Connection, "vm.remove-definition-v1", []string{key.String()}, map[string]string{key.String(): d.Fingerprint}, removalRecipe{Version: 1, Definition: d}, []domain.Step{step}, []string{"host-mutation", "remove-vm-definition", "exclusive-lifecycle-writer"}, []string{"The VM definition is removed. Disks and existing backups are kept; this operation creates no configuration backup.", "No disk space is freed. To use retained disks again, create or restore a VM definition.", "Coordinate other VM editors. A lost acknowledgement or a replacement VM requires inspection; removal is never automatically repeated."})
}

func (s *Service) bindRemovalInventory(ctx context.Context, d domain.DefinitionRemoval) error {
	v, err := s.GetVM(ctx, d.Resource.ConnectionID, d.Resource.UUID)
	if err != nil {
		return err
	}
	if v.Key != d.Resource || v.Name != d.Name || v.Fingerprint != d.Fingerprint || v.State != "stopped" || v.PersistentXML == "" || v.Autostart || v.HasManagedSave {
		return domain.Fail("STALE_PLAN", "The VM or its ownership record changed. Refresh and review removal again.")
	}
	return ctx.Err()
}

type removalHandler struct{ s *Service }

func parseRemoval(p domain.Plan, raw []byte) (removalRecipe, error) {
	var r removalRecipe
	if wire.Decode(raw, &r) != nil || r.Version != 1 || p.Operation != "vm.remove-definition-v1" || !validRemoval(r.Definition) || r.Definition.Resource.ConnectionID != p.ConnectionID || len(p.ResourceIDs) != 1 || p.ResourceIDs[0] != r.Definition.Resource.String() || len(p.Before) != 1 || p.Before[r.Definition.Resource.String()] != r.Definition.Fingerprint {
		return r, domain.Fail("INVALID_INPUT", "Invalid VM removal recipe binding.")
	}
	return r, nil
}
func (h *removalHandler) Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error) {
	return domain.Estimates{RequiresDowntime: true, Notes: "VM must already be stopped. Keeps storage and frees no disk space; no new allocation or guest restart is requested."}, nil
}
func (h *removalHandler) Review(_ context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	r, err := parseRemoval(p, raw)
	if err != nil {
		return nil, err
	}
	d := r.Definition
	return map[string]any{"action": "remove", "resource": d.Resource, "vmName": d.Name, "definitionSHA256": d.DefinitionSHA256, "retainedSources": d.RetainedSources, "diskDeletion": false, "backupsDeleted": false, "configurationRemoved": true, "backupCreated": false, "requiresStopped": true, "automaticStop": false}, nil
}
func (h *removalHandler) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	r, err := parseRemoval(p, raw)
	if err != nil {
		return err
	}
	provider, ok := h.s.Provider.(domain.DefinitionRemovalProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "VM removal provider unavailable.")
	}
	current, err := provider.InspectDefinitionRemoval(ctx, p.ConnectionID, r.Definition.Resource.UUID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, r.Definition) {
		return domain.Fail("STALE_PLAN", "This VM changed since preview. Go back and review its current settings.")
	}
	return h.s.bindRemovalInventory(ctx, r.Definition)
}
func (h *removalHandler) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	r, err := parseRemoval(p, raw)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "remove-definition" {
		return domain.Fail("INVALID_INPUT", "Durable removal operation identity required.")
	}
	provider, ok := h.s.Provider.(domain.DefinitionRemovalProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "VM removal provider unavailable.")
	}
	if err := h.s.bindRemovalInventory(ctx, r.Definition); err != nil {
		return err
	}
	if err := provider.RemoveDefinition(ctx, r.Definition); err != nil {
		return err
	}
	return h.s.Engine.Store.ComparePut("vm-removal-receipt", id, nil, removalReceipt{Version: 1, OperationID: id, PlanID: p.ID, PlanDigest: p.Digest, Definition: r.Definition})
}
func (h *removalHandler) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	r, err := parseRemoval(p, raw)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "remove-definition" {
		return false, domain.Fail("INVALID_INPUT", "Durable removal operation identity required.")
	}
	rawReceipt, err := h.s.Engine.Store.MetadataBytes("vm-removal-receipt", id)
	if err != nil {
		return false, err
	}
	var receipt removalReceipt
	if len(rawReceipt) == 0 || wire.Decode(rawReceipt, &receipt) != nil || receipt.Version != 1 || receipt.OperationID != id || receipt.PlanID != p.ID || receipt.PlanDigest != p.Digest || !reflect.DeepEqual(receipt.Definition, r.Definition) {
		return false, domain.Fail("RECOVERY_REQUIRED", "No valid removal receipt. A missing VM alone does not prove this operation succeeded. Inspect the VM and retained disks; do not repeat removal.")
	}
	provider, ok := h.s.Provider.(domain.DefinitionRemovalProvider)
	if !ok {
		return false, domain.Fail("UNSUPPORTED_CAPABILITY", "VM removal provider unavailable.")
	}
	absent, err := provider.DefinitionAbsent(ctx, r.Definition.Resource)
	if err != nil {
		return false, err
	}
	if !absent {
		return false, domain.Fail("RECOVERY_REQUIRED", "A VM with this UUID is still present or was recreated. Preserve it and inspect the operation; removal will not be repeated.")
	}
	return true, ctx.Err()
}
