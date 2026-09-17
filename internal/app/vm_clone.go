package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

// ADR 0066: copy every writable disk of a stopped VM and define an independent
// VM with a new identity. The original is never changed.
const cloneOperation = "vm.clone-v1"
const cloneReceiptKind = "vm-clone-receipt"

type cloneRecipe struct {
	Version int            `json:"version"`
	Clone   domain.VMClone `json:"clone"`
}

// The receipt records which copies were verified and whether the clone was
// defined, so recovery observes instead of copying again.
type cloneReceipt struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationID"`
	PlanID      string `json:"planID"`
	PlanDigest  string `json:"planDigest"`
	CloneDigest string `json:"cloneDigest"`
	Verified    []bool `json:"verified"`
	Defined     bool   `json:"defined"`
}

func validCloneRecipe(c domain.VMClone) bool {
	if !removalIdentity(c.Source) || !removalDigest(c.SourceFingerprint) || !removalDigest(c.DefinitionSHA256) ||
		!removalUUID.MatchString(c.UUID) || c.UUID == c.Source.UUID || c.Name == c.SourceName || len(c.Disks) == 0 || len(c.Disks) > 64 {
		return false
	}
	if _, err := validation.DisplayName(c.Name); err != nil {
		return false
	}
	newKey := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: c.Source.ConnectionID, Kind: "vm", UUID: c.UUID}
	need := map[string]uint64{}
	for _, d := range c.Disks {
		s := d.Disk
		if !removalTarget.MatchString(s.Target) || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || !removalUUID.MatchString(s.PoolID) ||
			(s.Format != "qcow2" && s.Format != "raw") || s.CapacityBytes == 0 || !removalUUID.MatchString(d.PoolID) || d.PoolName == "" ||
			d.VolumeName == "" || filepath.Base(d.VolumePath) != d.VolumeName || !filepath.IsAbs(d.VolumePath) ||
			validation.SafeText(d.VolumePath) != d.VolumePath || d.CopyBytes < s.CapacityBytes ||
			!slices.Contains(c.ResourceIDs, "local-file|"+d.VolumePath) {
			return false
		}
		need[d.PoolName] += d.CopyBytes
	}
	if len(c.Pools) != len(need) {
		return false
	}
	for _, p := range c.Pools {
		if need[p.PoolName] != p.NeedBytes {
			return false
		}
	}
	return slices.IsSorted(c.ResourceIDs) && slices.Contains(c.ResourceIDs, c.Source.String()) && slices.Contains(c.ResourceIDs, newKey.String())
}

func (s *Service) planClone(ctx context.Context, uid uint32, req Request) (domain.Plan, error) {
	var empty domain.Plan
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: req.Connection, Kind: "vm", UUID: req.ID}
	if !removalIdentity(key) || req.Path != "" || req.After != 0 || req.Apply != nil {
		return empty, domain.Fail("INVALID_INPUT", "Choose one local VM to clone.")
	}
	b, err := json.Marshal(req.Input)
	var input struct {
		Name string `json:"name"`
		Pool string `json:"pool"`
	}
	if err != nil || validation.Schema("vm-clone-input", b) != nil || wire.Decode(b, &input) != nil {
		return empty, domain.Fail("INVALID_INPUT", `Give the clone a name, for example {"name":"web-2"}, and optionally one storage pool for every copy.`)
	}
	provider, ok := s.Provider.(domain.CloneProvider)
	if !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "This provider cannot clone VMs.")
	}
	c, err := provider.InspectClone(ctx, req.Connection, req.ID, input.Name, input.Pool, domain.ID())
	if err != nil {
		return empty, err
	}
	if c.Source != key || c.Name != input.Name || !validCloneRecipe(c) {
		return empty, domain.Fail("INVALID_INPUT", "Clone inspection returned an incomplete or different clone.")
	}
	for _, d := range c.Disks {
		if input.Pool != "" && d.PoolName != input.Pool {
			return empty, domain.Fail("INVALID_INPUT", "Clone inspection placed a copy outside the chosen pool.")
		}
	}
	acks := []string{"host-mutation", "exclusive-storage-writer", "exclusive-configuration-writer", "copy-managed-volumes", "new-vm-identity"}
	total := uint64(0)
	for _, d := range c.Disks {
		total += d.CopyBytes
	}
	risks := []string{
		fmt.Sprintf("Copies %d disk(s) of %s into new volumes and defines %s, a separate stopped VM; %s is not changed.", len(c.Disks), c.SourceName, c.Name, c.SourceName),
		"Everything inside the disks is copied as it is: hostname, machine ID, SSH host keys, accounts and licences are the original's. Change them in the clone before running both on one network.",
		"The clone gets a new UUID and new MAC addresses on the same networks.",
		"The copies are checked against the disks they were made from, by reading them back, before the clone is defined.",
	}
	if len(c.SharedMedia) > 0 {
		risks = append(risks, "Read-only media ("+strings.Join(c.SharedMedia, ", ")+") are shared with the original, not copied.")
	}
	if c.FreshNVRAM {
		acks = append(acks, "new-firmware-state")
		risks = append(risks, "The clone's UEFI variables start fresh from their template, so its boot entries are reset; guests that boot through the standard fallback path still boot.")
	}
	overcommit := false
	for _, p := range c.Pools {
		risks = append(risks, fmt.Sprintf("Needs up to %s in pool %s.", sizeText(p.NeedBytes), p.PoolName))
		if p.NeedBytes > p.AvailableBytes {
			overcommit = true
		}
	}
	if overcommit {
		acks = append(acks, "pool-overcommit")
		risks = append(risks, "A destination pool reports less free space than its copies need; copying can fail part way through.")
	}
	step := domain.Step{ID: "clone-vm", Action: cloneOperation,
		Preconditions:       []string{"unchanged stopped original and saved definition", "every copy name and the clone's name and UUID are unused", "no other VM uses the original's writable disks"},
		Idempotency:         "reconcile-before-retry",
		Compensation:        "Keep the original unchanged and every verified copy after any uncertainty; copies are never deleted during recovery",
		Reconciliation:      "Observe the copies and whether the clone is defined; never copy or define twice",
		CompletionPredicate: "The clone is defined, stopped, with a new identity, and every writable disk names its verified copy"}
	return s.Engine.Plan(ctx, uid, req.Connection, cloneOperation, slices.Clone(c.ResourceIDs),
		map[string]string{key.String(): c.SourceFingerprint}, cloneRecipe{Version: 1, Clone: c}, []domain.Step{step}, acks, risks)
}

func parseClone(p domain.Plan, b []byte) (cloneRecipe, error) {
	var r cloneRecipe
	if wire.Decode(b, &r) != nil || r.Version != 1 || p.Operation != cloneOperation || !validCloneRecipe(r.Clone) ||
		r.Clone.Source.ConnectionID != p.ConnectionID || !reflect.DeepEqual(p.ResourceIDs, r.Clone.ResourceIDs) ||
		len(p.Before) != 1 || p.Before[r.Clone.Source.String()] != r.Clone.SourceFingerprint {
		return r, domain.Fail("INVALID_INPUT", "Invalid clone recipe binding.")
	}
	return r, nil
}

type cloneHandler struct{ s *Service }

func (h *cloneHandler) provider() (domain.CloneProvider, error) {
	provider, ok := h.s.Provider.(domain.CloneProvider)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Clone provider unavailable.")
	}
	return provider, nil
}

func (h *cloneHandler) Estimate(_ context.Context, p domain.Plan, b []byte) (domain.Estimates, error) {
	in, err := parseClone(p, b)
	total := uint64(0)
	for _, d := range in.Clone.Disks {
		total += d.CopyBytes
	}
	return domain.Estimates{RequiresDowntime: true, AdditionalBytes: total,
		Notes: "The original must already be stopped and stays stopped while its disks are copied. It is not changed."}, err
}

func (h *cloneHandler) Review(_ context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := parseClone(p, b)
	if err != nil {
		return nil, err
	}
	c := in.Clone
	disks := []map[string]any{}
	for _, d := range c.Disks {
		disks = append(disks, map[string]any{"target": d.Disk.Target, "fromPool": d.SourcePoolName, "fromVolume": d.Disk.VolumeName,
			"toPool": d.PoolName, "volume": d.VolumeName, "sizeBytes": d.CopyBytes})
	}
	return map[string]any{"action": "clone", "resource": c.Source, "sourceName": c.SourceName, "name": c.Name, "uuid": c.UUID,
		"disks": disks, "sharedMedia": c.SharedMedia, "freshFirmwareVariables": c.FreshNVRAM, "pools": c.Pools,
		"changesOriginal": false, "requiresStopped": true, "automaticStop": false, "diskDeletion": false}, nil
}

func (h *cloneHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := parseClone(p, b)
	if err != nil {
		return err
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	state, _, err := provider.CheckClone(ctx, in.Clone)
	if err != nil {
		return err
	}
	if state != "before" {
		return domain.Fail("STALE_PLAN", "Copies or a VM with this clone's identity already exist; review again.")
	}
	return nil
}

func (h *cloneHandler) saveReceipt(id string, previous *[]byte, r cloneReceipt) error {
	if err := h.s.Engine.Store.ComparePut(cloneReceiptKind, id, *previous, r); err != nil {
		return err
	}
	b, err := h.s.Engine.Store.MetadataBytes(cloneReceiptKind, id)
	if err == nil {
		*previous = b
	}
	return err
}

// uncreatedClone turns a failure before the clone was defined into a plain
// failure that releases the locks, naming every copy that may exist so its
// space can be reclaimed. At most new, unreferenced volumes were written.
func uncreatedClone(c domain.VMClone, upTo int, err error) error {
	var refusal *domain.Error
	if !errors.As(err, &refusal) {
		refusal = domain.Fail("OPERATION_FAILED", "the clone could not be made: "+err.Error())
	}
	failure := domain.Fail(refusal.Code, refusal.Message)
	failure.Details = refusal.Details
	names := []string{}
	for i := 0; i <= upTo && i < len(c.Disks); i++ {
		names = append(names, c.Disks[i].VolumeName+" in "+c.Disks[i].PoolName)
	}
	failure.SafeNextActions = []string{"Delete the unused copies if they were created: " + strings.Join(names, ", "), "Review the clone again"}
	return operations.NotDone(failure)
}

func (h *cloneHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := parseClone(p, b)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "clone-vm" {
		return domain.Fail("INVALID_INPUT", "Durable clone identity required.")
	}
	provider, err := h.provider()
	if err != nil {
		return err
	}
	digest, err := operations.Digest(in.Clone)
	if err != nil {
		return err
	}
	c := in.Clone
	r := cloneReceipt{Version: 1, OperationID: id, PlanID: p.ID, PlanDigest: p.Digest, CloneDigest: digest, Verified: make([]bool, len(c.Disks))}
	var previous []byte
	if err = h.saveReceipt(id, &previous, r); err != nil {
		return err
	}
	state, _, err := provider.CheckClone(ctx, c)
	if err != nil {
		return err
	}
	if state != "before" {
		return domain.Fail("STALE_PLAN", "Copies or a VM with this clone's identity already exist; nothing was copied.")
	}
	for i := range c.Disks {
		if err = provider.CopyCloneDisk(ctx, c, i); err != nil {
			return uncreatedClone(c, i, err)
		}
		r.Verified[i] = true
		if err = h.saveReceipt(id, &previous, r); err != nil {
			return err
		}
	}
	if err = provider.DefineClone(ctx, c); err != nil {
		return err
	}
	r.Defined = true
	return h.saveReceipt(id, &previous, r)
}

func (h *cloneHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := parseClone(p, b)
	if err != nil {
		return false, err
	}
	id := operations.OperationID(ctx)
	if id == "" || step.ID != "clone-vm" {
		return false, domain.Fail("INVALID_INPUT", "Durable clone identity required.")
	}
	raw, err := h.s.Engine.Store.MetadataBytes(cloneReceiptKind, id)
	if err != nil {
		return false, err
	}
	digest, err := operations.Digest(in.Clone)
	if err != nil {
		return false, err
	}
	var r cloneReceipt
	if len(raw) == 0 || wire.Decode(raw, &r) != nil || r.Version != 1 || r.OperationID != id || r.PlanID != p.ID ||
		r.PlanDigest != p.Digest || r.CloneDigest != digest || len(r.Verified) != len(in.Clone.Disks) {
		return false, domain.Fail("RECOVERY_REQUIRED", "The clone receipt is missing or differs; nothing will be copied or defined again.")
	}
	// Every copy must have been verified before the define ran.
	for _, verified := range r.Verified {
		if !verified {
			return false, ctx.Err()
		}
	}
	provider, err := h.provider()
	if err != nil {
		return false, err
	}
	state, _, err := provider.CheckClone(ctx, in.Clone)
	if err != nil {
		return false, err
	}
	return state == "defined", ctx.Err()
}
