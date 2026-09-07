// Package app is the shared service for CLI, TUI and the private coordinator API.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
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
	"virmill.local/core/internal/wire"
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
	Provider    domain.ComputeProvider
	Engine      *operations.Engine
	Inspector   func() []domain.Capability
	Extensions  map[string]func(context.Context, uint32, Request) (any, error)
	InventoryVM func(context.Context, domain.VM) (domain.VM, error)
}

func New(p domain.ComputeProvider, e *operations.Engine) *Service {
	s := &Service{Provider: p, Engine: e, Extensions: map[string]func(context.Context, uint32, Request) (any, error){}}
	for _, action := range []string{"start", "stop", "hard-stop", "pause", "resume", "save", "restore-saved", "autostart", "set"} {
		e.Handlers["vm."+action] = &vmHandler{s: s, action: action}
	}
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
	case "vm.plan":
		return s.planVM(ctx, uid, r)
	case "operation.apply":
		if r.Apply == nil {
			return nil, domain.Fail("INVALID_INPUT", "apply request required")
		}
		return s.Engine.Apply(ctx, uid, *r.Apply)
	case "plan.show":
		p, _, e := s.Engine.Store.Plan(r.ID)
		return p, e
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
	case "import.inspect":
		return importer.Inspect(ctx, r.Path, importer.DefaultLimits())
	case "lab.validate", "document.validate":
		b, e := os.ReadFile(r.Path)
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
		default:
			return map[string]any{"schemaValid": true, "semanticValidation": "not-implemented"}, domain.Fail("NOT_IMPLEMENTED", "backup policy semantic validation is not yet integrated")
		}
	case "backup.verify-manifest":
		b, e := os.ReadFile(r.Path)
		if e != nil {
			return nil, e
		}
		var m protection.Manifest
		if e = wire.Decode(b, &m); e != nil {
			return nil, domain.Fail("INVALID_INPUT", e.Error())
		}
		root, _ := r.Input["root"].(string)
		if root == "" {
			e = m.Validate()
		} else {
			e = m.Verify(root)
		}
		return map[string]any{"verification": "manifest-checked", "bootTested": false}, e
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
	var empty domain.Plan
	if r.ID == "" {
		return empty, domain.Fail("INVALID_INPUT", "stable VM UUID required")
	}
	v, e := s.GetVM(ctx, r.Connection, r.ID)
	if e != nil {
		return empty, e
	}
	input := r.Input
	if input == nil {
		input = map[string]any{}
	}
	for field := range input {
		allowed := (r.Action == "set" && (field == "vcpus" || field == "memoryMiB")) || (r.Action == "autostart" && field == "enabled")
		if !allowed {
			return empty, domain.Fail("INVALID_INPUT", "unknown parameter for this operation: "+field)
		}
	}
	input["vmID"] = r.ID
	acks := []string{"host-mutation"}
	risks := []string{"Modifies the selected local VM; support not yet hardware-qualified"}
	if r.Action == "hard-stop" {
		acks = append(acks, "data-loss-hard-stop")
		risks = append(risks, "Abrupt power loss may corrupt guest data")
	}
	if r.Action == "set" {
		if v.State != "stopped" {
			return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "this edit adapter requires a powered-off VM; shutdown is a separate approved operation")
		}
		changes := map[string]string{}
		for _, field := range []string{"vcpus", "memoryMiB"} {
			if value, ok := input[field]; ok {
				n, ok := value.(float64)
				if !ok || n != float64(int64(n)) || n < 1 || n > 1048576 {
					return empty, domain.Fail("INVALID_INPUT", "positive bounded integer required")
				}
				if field == "vcpus" {
					if n > 512 {
						return empty, domain.Fail("INVALID_INPUT", "vcpus exceeds supported bound")
					}
					changes["domain/vcpu"] = strconv.FormatInt(int64(n), 10)
				} else {
					return empty, domain.Fail("NOT_IMPLEMENTED", "memory units and max/current-memory edit validation require a dedicated adapter")
				}
			}
		}
		if len(changes) == 0 {
			return empty, domain.Fail("INVALID_INPUT", "no recognized edit fields")
		}
		x, e := xmlpatch.Patch(v.PersistentXML, changes)
		if e != nil {
			return empty, e
		}
		input["xml"] = x
	}
	key := v.Key.String()
	step := domain.Step{ID: "effect", Action: "vm." + r.Action, Preconditions: []string{"unchanged domain fingerprint", "current capability and actor checks"}, Idempotency: "reconcile-before-retry", Compensation: "Preserve domain and disks; create a reviewed recovery plan", Reconciliation: "Read stable UUID and expected backend state without replay", CompletionPredicate: "Native backend reports requested state"}
	return s.Engine.Plan(ctx, uid, r.Connection, "vm."+r.Action, []string{key}, map[string]string{key: v.Fingerprint}, input, []domain.Step{step}, acks, risks)
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
	for _, key := range []string{"vcpus", "memoryMiB", "enabled"} {
		if value, ok := input[key]; ok {
			requested[key] = value
		}
	}
	return map[string]any{"action": h.action, "vmID": input["vmID"], "requested": requested, "connection": p.ConnectionID, "persistentEdit": h.action == "set", "diskDeletion": false}, nil
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
		expected, _ := input["xml"].(string)
		return v.PersistentXML == expected, nil
	}
	target := map[string]string{"start": "running", "restore-saved": "running", "stop": "stopped", "hard-stop": "stopped", "pause": "paused", "resume": "running", "save": "stopped"}[h.action]
	return v.State == target, nil
}
