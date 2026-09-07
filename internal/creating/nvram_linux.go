//go:build linux && amd64

package creating

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const nvramDeclarationKind = "creation-nvram-declaration"

// A persisted cancellation request does not cancel the worker context. Stop at
// new-policy observation/publication boundaries while the worker is active.
// An explicit later reconciliation of an uncertain job can still observe the
// already-created definition without replay, even if its cancel flag remains.
func (s *Service) nvramBoundary(ctx context.Context, in input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if in.NVRAMDeclarationVersion == 0 || operations.OperationID(ctx) == "" {
		return nil
	}
	j, err := s.Store.Job(operations.OperationID(ctx))
	if err != nil {
		return err
	}
	if j.CancelRequested && j.State != "recovery-required" && j.State != "interrupted" {
		return domain.Fail("RECOVERY_REQUIRED", "creation canceled at NVRAM declaration boundary; retain the definition and volumes for explicit reconciliation")
	}
	return ctx.Err()
}

// A declaration is not a file identity, first-creation receipt or read grant.
type nvramDeclarationBinding struct {
	Version             int                 `json:"schemaVersion"`
	PlanID              string              `json:"planID"`
	InputDigest         string              `json:"inputDigest"`
	OperationID         string              `json:"operationID"`
	Resource            domain.ResourceKey  `json:"resource"`
	CreationBinding     string              `json:"creationBinding"`
	FirmwareDigest      string              `json:"firmwareDigest"`
	Firmware            domain.ColdFirmware `json:"firmware"`
	ObservedFingerprint string              `json:"observedFingerprint"`
}

func nvramDigest(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && value == strings.ToLower(value)
}

func nvramPath(value string) bool {
	if value == "" || value == "/" || len(value) > 4096 || !utf8.ValidString(value) || !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func nvramFirmwareMatches(f domain.ColdFirmware, wanted domain.CreationFirmware) bool {
	secure := "no"
	if wanted.SecureBoot {
		secure = "yes"
	}
	return wanted.Mode == "uefi" && nvramPath(wanted.Code) && nvramPath(wanted.Template) &&
		(wanted.Format == "raw" || wanted.Format == "qcow2") && f.Loader == wanted.Code &&
		f.LoaderType == "pflash" && f.LoaderReadOnly == "yes" && f.LoaderSecure == secure &&
		f.LoaderFormat == wanted.Format && f.LoaderStateless == "" && f.NVRAM != nil &&
		nvramPath(f.NVRAM.Path) && f.NVRAM.Format == wanted.Format &&
		f.NVRAM.Template == wanted.Template && f.NVRAM.TemplateFormat == wanted.Format
}

func validateNVRAMBinding(p domain.Plan, in input, r Receipt, b nvramDeclarationBinding) error {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: in.Target.Spec.UUID}
	creationBinding, err := operations.Digest([]string{p.ID, p.InputDigest})
	if err != nil {
		return err
	}
	if in.NVRAMDeclarationVersion != 1 || b.Version != 1 || b.PlanID != p.ID || b.InputDigest != p.InputDigest ||
		b.OperationID == "" || b.OperationID != r.OperationID || b.Resource != key ||
		r.PlanID != p.ID || r.VMID != key.UUID || r.Connection != p.ConnectionID ||
		r.Binding != creationBinding || b.CreationBinding != creationBinding ||
		!nvramDigest(in.Target.FirmwareDigest) || b.FirmwareDigest != in.Target.FirmwareDigest ||
		!nvramDigest(b.ObservedFingerprint) || !nvramFirmwareMatches(b.Firmware, in.Target.Spec.Firmware) {
		return domain.Fail("RECOVERY_REQUIRED", "NVRAM declaration record differs from its versioned creation identity or reviewed firmware")
	}
	return nil
}

func (s *Service) loadNVRAMBinding(ctx context.Context, p domain.Plan, in input, r Receipt) (*nvramDeclarationBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var binding nvramDeclarationBinding
	if err = wire.Decode(raw, &binding); err != nil {
		return nil, err
	}
	// encoding/json matches struct keys without regard to case. A stored proof
	// requires exact field names and a complete versioned shape, including all
	// nested fields. Canonical equality permits whitespace/key order changes,
	// but refuses omitted fields and case aliases that Decode alone accepts.
	recorded, err := operations.Canonical(json.RawMessage(raw))
	if err != nil {
		return nil, err
	}
	decoded, err := operations.Canonical(binding)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(recorded, decoded) {
		return nil, domain.Fail("RECOVERY_REQUIRED", "NVRAM declaration record has missing or noncanonical field names")
	}
	if err = validateNVRAMBinding(p, in, r, binding); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s *Service) bindNVRAMDeclaration(ctx context.Context, p domain.Plan, in input, r Receipt, vm domain.VM) error {
	if err := s.nvramBoundary(ctx, in); err != nil {
		return err
	}
	if in.NVRAMDeclarationVersion == 0 {
		return nil // Historical recipe semantics; no stronger proof is inferred.
	}
	inspector, ok := s.Backend.(domain.ColdStateInspector)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "creation NVRAM declaration requires native cold-state inspection")
	}
	observed, err := inspector.InspectColdState(ctx, p.ConnectionID, in.Target.Spec.UUID)
	if err != nil {
		return err
	}
	if err = s.nvramBoundary(ctx, in); err != nil {
		return err
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: in.Target.Spec.UUID}
	if observed.Resource != key || vm.Key != key || observed.Layout.VMID != key.UUID ||
		observed.Fingerprint != vm.Fingerprint || !nvramDigest(vm.Fingerprint) ||
		vm.State != "stopped" || vm.HasManagedSave || vm.Autostart ||
		observed.State != "stopped" || observed.HasManagedSave || observed.Autostart {
		return domain.Fail("SOURCE_CHANGED", "NVRAM declaration observation no longer matches the same stopped native definition")
	}
	current := nvramDeclarationBinding{Version: 1, PlanID: p.ID, InputDigest: p.InputDigest, OperationID: r.OperationID, Resource: key,
		CreationBinding: r.Binding, FirmwareDigest: in.Target.FirmwareDigest, Firmware: observed.Layout.Firmware, ObservedFingerprint: observed.Fingerprint}
	if err = validateNVRAMBinding(p, in, r, current); err != nil {
		return err
	}
	previous, err := s.loadNVRAMBinding(ctx, p, in, r)
	if err != nil {
		return err
	}
	if previous != nil {
		// Preserve the historical observation; compare the declaration, not a
		// fabricated claim that no unrelated native state ever changed.
		current.ObservedFingerprint = previous.ObservedFingerprint
		if !same(*previous, current) {
			return domain.Fail("SOURCE_CHANGED", "assigned NVRAM declaration changed after its durable binding; retain state without rebinding")
		}
		return s.nvramBoundary(ctx, in)
	}
	if err = s.nvramBoundary(ctx, in); err != nil {
		return err
	}
	if err = s.Store.ComparePut(nvramDeclarationKind, p.ID, nil, current); err != nil {
		return err
	}
	return s.nvramBoundary(ctx, in)
}

func (s *Service) nvramResult(ctx context.Context, p domain.Plan, in input, r Receipt, present bool, result map[string]any) error {
	if in.Target.Spec.Firmware.Mode != "uefi" {
		return nil
	}
	result["nvramDeclarationBound"] = false
	result["nvramInitializationVerified"] = false
	result["nvramDeclarationStatus"] = "legacy-unbound"
	if in.NVRAMDeclarationVersion == 0 {
		return nil
	}
	result["nvramDeclarationStatus"] = "pending"
	if !present {
		return nil
	}
	binding, err := s.loadNVRAMBinding(ctx, p, in, r)
	if err != nil {
		return err
	}
	if binding == nil {
		if r.Defined {
			return domain.Fail("RECOVERY_REQUIRED", "versioned UEFI definition lacks its durable NVRAM declaration; inspect without replay")
		}
		return nil
	}
	result["nvramDeclarationBound"] = true
	result["nvramDeclarationStatus"] = "declaration-bound"
	result["nvramDeclaration"] = binding
	return nil
}
