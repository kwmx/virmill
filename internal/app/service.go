// Package app is the shared service for CLI, TUI and the private coordinator API.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"virmill.local/core/contracts"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/app/lab"
	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
)

type Request struct {
	Connection string                   `json:"connection,omitempty"`
	ID         string                   `json:"id,omitempty"`
	Action     string                   `json:"action,omitempty"`
	Path       string                   `json:"path,omitempty"`
	After      int64                    `json:"after,omitempty"`
	Input      map[string]any           `json:"input,omitempty"`
	Apply      *operations.ApplyRequest `json:"apply,omitempty"`
}
type Response struct {
	APIVersion string        `json:"apiVersion"`
	Data       any           `json:"data"`
	Warnings   []string      `json:"warnings"`
	Error      *domain.Error `json:"error"`
}
type Service struct {
	Provider          domain.ComputeProvider
	Engine            *operations.Engine
	Inspector         func() []domain.Capability
	HostPrefixes      func(context.Context) (domain.HostNetworkPrefixes, error)
	NetworkAllocation func(context.Context) (network.AllocationConfig, error)
	Extensions        map[string]func(context.Context, uint32, Request) (any, error)
	InventoryVM       func(context.Context, domain.VM) (domain.VM, error)
	NetworkFirewall   NetworkFirewall
}

func New(p domain.ComputeProvider, e *operations.Engine) *Service {
	s := &Service{Provider: p, Engine: e, Extensions: map[string]func(context.Context, uint32, Request) (any, error){}}
	for _, action := range []string{"start", "stop", "hard-stop", "pause", "resume", "save", "restore-saved", "autostart", "set"} {
		e.Handlers["vm."+action] = &vmHandler{s: s, action: action}
	}
	// A distinct durable operation prevents older binaries from executing the new
	// preservation contract through their legacy vm.set handler.
	e.Handlers["vm.configure-resources"] = &vmHandler{s: s, action: "set"}
	e.Handlers["vm.configure-hardware"] = &vmHandler{s: s, action: "set"}
	e.Handlers["vm.reboot"] = &rebootHandler{s: s}
	e.Handlers["network.create"] = &networkCreationHandler{s: s}
	e.Handlers["network.creation.resume"] = &networkResumeHandler{networkCreationHandler{s: s}}
	return s
}
func (s *Service) Call(ctx context.Context, uid uint32, method string, r Request) Response {
	result, e := s.dispatch(ctx, uid, method, r)
	out := Response{APIVersion: domain.APIVersion, Data: result, Warnings: []string{}}
	if e != nil {
		if d, ok := e.(*domain.Error); ok {
			out.Error = d
		} else {
			out.Error = domain.Fail("OPERATION_FAILED", validation.SafeText(e.Error()))
		}
	}
	return out
}
func (s *Service) dispatch(ctx context.Context, uid uint32, method string, r Request) (any, error) {
	if r.Connection == "" {
		r.Connection = "qemu:///system"
	}
	switch method {
	case "version":
		return buildinfo.Info(), nil
	case "host.doctor", "host.inspect":
		if s.Inspector == nil {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "host inspection adapter unavailable")
		}
		return s.Inspector(), nil
	case "host.pci.list":
		inventory, ok := s.Provider.(domain.PCIInventoryProvider)
		if !ok {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "selected backend does not expose PCI discovery")
		}
		return inventory.InspectPCI(ctx, r.Connection)
	case "device.usb.list":
		if r.ID != "" || r.Path != "" || r.Action != "" || len(r.Input) != 0 || r.After != 0 || r.Apply != nil {
			return nil, domain.Fail("INVALID_INPUT", "USB discovery accepts only a local connection")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		inventory, ok := s.Provider.(domain.USBInventoryProvider)
		if !ok {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "selected backend does not expose USB discovery")
		}
		devices, err := inventory.InspectUSB(ctx, r.Connection)
		if err != nil {
			return nil, err
		}
		return devices, ctx.Err()
	case "network.cidr.check":
		return s.checkCIDRs(ctx, r)
	case "network.create":
		return s.planNetworkCreation(ctx, uid, r)
	case "network.creation.resume":
		return s.planNetworkResume(ctx, uid, r)
	case "network.creation.result":
		return s.networkCreationResult(ctx, uid, r)
	case "host.capabilities":
		suite, err := contracts.Capabilities()
		if err != nil {
			return nil, err
		}
		backend, err := s.Provider.Capabilities(ctx, r.Connection)
		result := map[string]any{"suite": suite, "backend": backend}
		if err != nil {
			result["backendError"] = validation.SafeText(err.Error())
		}
		return result, nil
	case "inventory.list":
		vms, err := s.Provider.List(ctx, r.Connection)
		if err != nil {
			return nil, err
		}
		if s.InventoryVM != nil {
			for i := range vms {
				vms[i], err = s.InventoryVM(ctx, vms[i])
				if err != nil {
					return nil, err
				}
			}
		}
		return vms, nil
	case "inventory.get":
		return s.GetVM(ctx, r.Connection, r.ID)
	case "vm.recovery.inspect":
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.ID == "" || r.Path != "" || r.Action != "" || len(r.Input) != 0 || r.After != 0 || r.Apply != nil {
			return nil, domain.Fail("INVALID_INPUT", "recovery inspection requires only a stable VM UUID and local connection")
		}
		if r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "recovery inspection requires an explicit local libvirt connection")
		}
		inspector, ok := s.Provider.(domain.ColdStateInspector)
		if !ok {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native auxiliary-state layout inspection unavailable")
		}
		inspection, err := inspector.InspectColdState(ctx, r.Connection, r.ID)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return inspection, nil
	case "vm.readiness.show":
		if r.ID == "" || r.Path != "" || r.Action != "" || len(r.Input) != 0 || r.After != 0 || r.Apply != nil {
			return nil, domain.Fail("INVALID_INPUT", "guest readiness accepts one stable VM UUID and local connection")
		}
		observer, ok := s.Provider.(domain.GuestReadinessProvider)
		if !ok {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native guest-agent readiness observation unavailable")
		}
		return observer.InspectGuestReadiness(ctx, r.Connection, r.ID)
	case "vm.boot.get":
		if r.ID == "" {
			return nil, domain.Fail("INVALID_INPUT", "stable VM UUID required for boot inspection")
		}
		v, err := s.GetVM(ctx, r.Connection, r.ID)
		if err != nil {
			return nil, err
		}
		layers := map[string]any{"resource": v.Key, "state": v.State, "hasManagedSave": v.HasManagedSave, "persistent": nil, "live": nil, "guestBootVerified": false}
		for name, xml := range map[string]string{"persistent": v.PersistentXML, "live": v.LiveXML} {
			if xml != "" {
				view, err := xmlpatch.InspectBoot(xml)
				if err != nil {
					return nil, err
				}
				layers[name] = view
			}
		}
		return layers, nil
	case "storage.pool.list", "storage.pool.get", "network.list", "network.get":
		inventory, ok := s.Provider.(domain.ResourceInventory)
		if !ok {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "selected backend does not expose storage/network inventory")
		}
		switch method {
		case "storage.pool.list":
			return inventory.ListStoragePools(ctx, r.Connection)
		case "network.list":
			return inventory.ListNetworks(ctx, r.Connection)
		case "storage.pool.get":
			if r.ID == "" {
				return nil, domain.Fail("INVALID_INPUT", "stable storage pool UUID required")
			}
			return inventory.GetStoragePool(ctx, r.Connection, r.ID)
		default:
			if r.ID == "" {
				return nil, domain.Fail("INVALID_INPUT", "stable network UUID required")
			}
			return inventory.GetNetwork(ctx, r.Connection, r.ID)
		}
	case "vm.creation.options":
		return s.creationOptions(ctx, r)
	case "vm.plan":
		return s.planVM(ctx, uid, r)
	case "operation.apply":
		if r.Apply == nil {
			return nil, domain.Fail("INVALID_INPUT", "apply request required")
		}
		return s.Engine.Apply(ctx, uid, *r.Apply)
	case "plan.show":
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, _, e := s.Engine.Store.Plan(r.ID)
		if e != nil {
			return nil, e
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return p, nil
	case "operation.get":
		return s.Engine.Store.Job(r.ID)
	case "operation.list":
		return s.Engine.Store.Jobs()
	case "operation.watch":
		return s.Engine.Store.Events(r.ID, r.After)
	case "operation.cancel":
		return s.Engine.Cancel(r.ID)
	case "operation.reconcile":
		return s.Engine.Reconcile(ctx, r.ID)
	case "import.describe":
		return importer.Describe(ctx, r.Path, importer.DefaultLimits())
	case "import.inspect":
		return importer.Inspect(ctx, r.Path, importer.DefaultLimits())
	case "backup.policy.validate", "backup.policy.preview":
		return s.backupPolicy(ctx, method, r)
	case "lab.validate", "document.validate":
		b, e := readDeclaration(ctx, r.Path)
		if e != nil {
			return nil, e
		}
		value, raw, e := validation.Document(b)
		if e != nil {
			return nil, domain.Fail("INVALID_INPUT", e.Error())
		}
		kind := value["kind"].(string)
		switch kind {
		case "Lab":
			return lab.Validate(raw)
		case "VM":
			b, _ := json.Marshal(value["spec"])
			var v lab.VM
			json.Unmarshal(b, &v)
			warnings, e := lab.ValidateVM(v, nil)
			return map[string]any{"valid": e == nil, "warnings": warnings, "hostPreflight": "not-run"}, e
		case "Network":
			b, _ := json.Marshal(value["spec"])
			var n network.Spec
			json.Unmarshal(b, &n)
			e = network.Validate(n)
			return map[string]any{"valid": e == nil, "policyEnforced": false}, e
		case "BackupPolicy":
			report, err := protection.ValidatePolicy(raw)
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return report, nil
		case "GuestRecipe":
			return map[string]any{"valid": true, "hostPreflight": "not-run", "guestExecution": "not-run"}, ctx.Err()
		default:
			return nil, domain.Fail("INVALID_INPUT", "unsupported declaration kind")
		}
	case "backup.verify-manifest":
		if r.Path == "" || r.ID != "" || r.Action != "" || r.Apply != nil || r.After != 0 {
			return nil, domain.Fail("INVALID_INPUT", "manifest path and optional member root required")
		}
		root := ""
		for key, value := range r.Input {
			if key != "root" {
				return nil, domain.Fail("INVALID_INPUT", "unknown manifest verification parameter")
			}
			var ok bool
			root, ok = value.(string)
			if !ok || root == "" {
				return nil, domain.Fail("INVALID_INPUT", "member root must be a nonempty path")
			}
		}
		report, err := protection.CheckManifestContext(ctx, r.Path, root)
		if err != nil {
			return nil, err
		}
		return report, nil
	default:
		if extension, ok := s.Extensions[method]; ok {
			return extension(ctx, uid, r)
		}
		return nil, domain.Fail("NOT_IMPLEMENTED", "unknown or unimplemented service method "+method)
	}
}

func (s *Service) GetVM(ctx context.Context, connection, id string) (domain.VM, error) {
	vm, err := s.Provider.Get(ctx, connection, id)
	if err != nil || s.InventoryVM == nil {
		return vm, err
	}
	return s.InventoryVM(ctx, vm)
}
func (s *Service) planVM(ctx context.Context, uid uint32, r Request) (domain.Plan, error) {
	if r.Action == "reboot" {
		return s.planReboot(ctx, uid, r)
	}
	var empty domain.Plan
	if r.ID == "" {
		return empty, domain.Fail("INVALID_INPUT", "stable VM UUID required")
	}
	if r.Action == "set" {
		b, err := json.Marshal(r.Input)
		schema := "vm-resource-edit-input"
		if hardwareRequest(r.Input) {
			schema = "vm-hardware-edit-input"
		}
		if err != nil || validation.Schema(schema, b) != nil {
			return empty, domain.Fail("INVALID_INPUT", "configuration input must match its bundled schema; rejected values are withheld")
		}
	}
	v, e := s.GetVM(ctx, r.Connection, r.ID)
	if e != nil {
		return empty, e
	}
	input := map[string]any{}
	for field, value := range r.Input {
		allowed := (r.Action == "set" && (field == "vcpus" || field == "memoryMiB" || field == "applyMode" || field == "bootOrder" || field == "ejectMedia")) || (r.Action == "autostart" && field == "enabled")
		if !allowed {
			return empty, domain.Fail("INVALID_INPUT", "unknown parameter for this operation: "+field)
		}
		input[field] = value
	}
	input["vmID"] = r.ID
	acks := []string{"host-mutation"}
	risks := []string{"Modifies the selected local VM; support not yet hardware-qualified"}
	if r.Action == "hard-stop" {
		acks = append(acks, "data-loss-hard-stop")
		risks = append(risks, "Abrupt power loss may corrupt guest data")
	}
	if r.Action == "set" {
		if _, ok := input["applyMode"]; !ok {
			input["applyMode"] = "next-boot"
		}
		if e = editableVM(v); e != nil {
			return empty, e
		}
		if hardwareRequest(input) {
			edit, err := xmlpatch.ParseHardwareInput(input)
			if err != nil {
				return empty, err
			}
			x, err := xmlpatch.EditHardware(v.PersistentXML, edit)
			if err != nil {
				return empty, err
			}
			digest, err := xmlpatch.HardwareDigest(x)
			if err != nil {
				return empty, err
			}
			input["xmlSHA256"] = digest
			input["editVersion"] = float64(2)
			if edit.BootOrder != nil {
				acks = append(acks, "replace-boot-order")
			}
			if edit.EjectMedia != "" {
				acks = append(acks, "eject-retain-media")
				risks = append(risks, "Ejects the selected medium while retaining its volume/file and read-only drive; an empty block/volume drive is explicitly represented as type=file")
			}
			risks = append(risks, "Boot selection is persistent firmware intent, not proof of bootability, installer completion or provisioning readiness")
		} else {
			edit, err := resourceEdit(input)
			if err != nil {
				return empty, err
			}
			x, err := xmlpatch.EditResources(v.PersistentXML, edit)
			if err != nil {
				return empty, err
			}
			input["xmlSHA256"] = xmlpatch.Digest(x)
			input["editVersion"] = float64(1)
		}
		input["editBeforeFingerprint"] = v.Fingerprint
		acks = append(acks, "exclusive-configuration-writer")
		risks = append(risks, "Changes only the reviewed persistent configuration; guest readiness, available host memory and next boot are not verified", "Coordinate external administrators: libvirt definition has no atomic compare-and-swap; immediate state checks cannot exclude every concurrent writer")
	}
	key := v.Key.String()
	operation := "vm." + r.Action
	if r.Action == "set" {
		operation = "vm.configure-resources"
		if hardwareRequest(input) {
			operation = "vm.configure-hardware"
		}
	}
	step := domain.Step{ID: "effect", Action: operation, Preconditions: []string{"unchanged domain fingerprint", "current capability and actor checks"}, Idempotency: "reconcile-before-retry", Compensation: "Preserve domain and disks; create a reviewed recovery plan", Reconciliation: "Read stable UUID and expected backend state without replay", CompletionPredicate: "Native backend reports requested state"}
	return s.Engine.Plan(ctx, uid, r.Connection, operation, []string{key}, map[string]string{key: v.Fingerprint}, input, []domain.Step{step}, acks, risks)
}

type vmHandler struct {
	s      *Service
	action string
}

func (h *vmHandler) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var input map[string]any
	if err := json.Unmarshal(b, &input); err != nil {
		return nil, err
	}
	requested := map[string]any{}
	for _, key := range []string{"vcpus", "memoryMiB", "enabled", "applyMode", "bootOrder", "ejectMedia"} {
		if value, ok := input[key]; ok {
			requested[key] = value
		}
	}
	review := map[string]any{"action": h.action, "vmID": input["vmID"], "requested": requested, "connection": p.ConnectionID, "persistentEdit": h.action == "set", "requiresShutdown": h.action == "set", "diskDeletion": false}
	if h.action == "set" && input["editVersion"] == float64(2) {
		id, _ := input["vmID"].(string)
		v, err := h.s.GetVM(ctx, p.ConnectionID, id)
		if err != nil {
			return nil, err
		}
		if v.Fingerprint != input["editBeforeFingerprint"] {
			return nil, domain.Fail("STALE_PLAN", "domain changed during hardware review")
		}
		edit, err := xmlpatch.ParseHardwareInput(input)
		if err != nil {
			return nil, err
		}
		after, err := xmlpatch.EditHardware(v.PersistentXML, edit)
		if err != nil {
			return nil, err
		}
		beforeView, err := xmlpatch.InspectBoot(v.PersistentXML)
		if err != nil {
			return nil, err
		}
		afterView, err := xmlpatch.InspectBoot(after)
		if err != nil {
			return nil, err
		}
		review["beforeBoot"] = beforeView
		review["afterBoot"] = afterView
	}
	return review, nil
}

func (h *vmHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	var input map[string]any
	if e := json.Unmarshal(b, &input); e != nil {
		return e
	}
	id, _ := input["vmID"].(string)
	v, e := h.s.GetVM(ctx, p.ConnectionID, id)
	if e != nil {
		return e
	}
	if p.Before[v.Key.String()] != v.Fingerprint {
		return domain.Fail("STALE_PLAN", "domain changed since the preview")
	}
	if h.action == "set" {
		if !configurationVersion(p.Operation, input) || input["editBeforeFingerprint"] != v.Fingerprint {
			return domain.Fail("STALE_PLAN", "configuration plan requires a fresh preservation-aware preview")
		}
		if e = editableVM(v); e != nil {
			return e
		}
		digest, err := configurationPreviewDigest(v.PersistentXML, input)
		if err != nil {
			return err
		}
		if digest != input["xmlSHA256"] {
			return domain.Fail("STALE_PLAN", "configuration edit differs from the reviewed fields")
		}
		checker, ok := h.s.Provider.(domain.ConfigurationValidator)
		if !ok {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "provider cannot check preservation of a persistent configuration edit")
		}
		if err = checker.CheckConfiguration(ctx, p.ConnectionID, id, input); err != nil {
			return err
		}
	}
	if h.action == "restore-saved" && !v.HasManagedSave {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "VM has no managed save image to restore")
	}
	required := map[string]string{"start": "stopped", "restore-saved": "stopped", "stop": "running", "hard-stop": "running", "pause": "running", "resume": "paused", "save": "running", "set": "stopped"}
	if expected, ok := required[h.action]; ok && v.State != expected {
		return domain.Fail("UNSUPPORTED_CAPABILITY", fmt.Sprintf("%s requires %s state, observed %s", h.action, expected, v.State))
	}
	return nil
}
func (h *vmHandler) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	var input map[string]any
	if e := json.Unmarshal(b, &input); e != nil {
		return e
	}
	id, _ := input["vmID"].(string)
	return h.s.Provider.Execute(ctx, p.ConnectionID, id, h.action, input)
}
func (h *vmHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	var input map[string]any
	if e := json.Unmarshal(b, &input); e != nil {
		return false, e
	}
	id, _ := input["vmID"].(string)
	v, e := h.s.GetVM(ctx, p.ConnectionID, id)
	if e != nil {
		return false, e
	}
	if h.action == "autostart" {
		enabled, ok := input["enabled"].(bool)
		return ok && v.Autostart == enabled, nil
	}
	if h.action == "save" {
		return v.State == "stopped" && v.HasManagedSave, nil
	}
	if h.action == "set" {
		if !configurationVersion(p.Operation, input) {
			return false, domain.Fail("RECOVERY_REQUIRED", "legacy configuration uncertainty requires explicit disposition; do not replay")
		}
		checker, ok := h.s.Provider.(domain.ConfigurationValidator)
		if !ok {
			return false, domain.Fail("UNSUPPORTED_CAPABILITY", "configuration readback adapter unavailable")
		}
		return checker.ObserveConfiguration(ctx, p.ConnectionID, id, input)
	}
	target := map[string]string{"start": "running", "restore-saved": "running", "stop": "stopped", "hard-stop": "stopped", "pause": "paused", "resume": "running", "save": "stopped"}[h.action]
	return v.State == target, nil
}
