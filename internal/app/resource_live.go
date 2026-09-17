package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// liveResourceOperation changes what a VM is running with and nothing else: the
// saved definition, and therefore the next boot, are untouched (ADR 0068).
const liveResourceOperation = "vm.resources-live-v1"

// liveMemoryFloorMiB is the least memory a running guest may be squeezed to.
// Below it a general-purpose guest stops being able to do anything useful.
const liveMemoryFloorMiB = 256

// liveRequest is what the reviewed live change asks for. At least one value is
// present, and each differs from what the VM is running with.
type liveRequest struct {
	VCPUs     *uint64
	MemoryMiB *uint64
}

func liveResourceRequest(input map[string]any) (liveRequest, error) {
	var out liveRequest
	for _, name := range []string{"vcpus", "memoryMiB"} {
		value, present := input[name]
		if !present {
			continue
		}
		n, ok := value.(float64)
		if !ok || n < 1 || n > 1048576 || n != float64(uint64(n)) {
			return out, domain.Fail("INVALID_INPUT", "positive bounded integer resource value required")
		}
		integer := uint64(n)
		if name == "vcpus" {
			if integer > 512 {
				return out, domain.Fail("INVALID_INPUT", "vcpus exceeds the 512-CPU adapter bound")
			}
			out.VCPUs = &integer
		} else {
			out.MemoryMiB = &integer
		}
	}
	if out.VCPUs == nil && out.MemoryMiB == nil {
		return out, domain.Fail("INVALID_INPUT", "no CPU/RAM change requested")
	}
	return out, nil
}

// liveResources is what a running VM is running with, read from its live
// definition. The saved definition says nothing about it.
type liveResources struct {
	VCPUs, MaximumVCPUs             uint64
	MemoryBytes, MaximumMemoryBytes uint64
	Balloon                         string
}

// readLiveResources reports what a VM is running with, or why Virmill will not
// change it while it runs.
func readLiveResources(v domain.VM) (liveResources, error) {
	var out liveResources
	switch v.State {
	case "running":
	case "paused":
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "A paused guest cannot take CPU or memory changes. Resume it first, or change its next boot instead.")
	case "stopped":
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "This VM is not running. Change its CPU and memory for the next boot instead.")
	default:
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "CPU and memory can be changed while a VM is running; it is "+v.State)
	}
	if v.HasManagedSave {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "This VM has saved state bound to its old hardware. Restore it before changing what it runs with.")
	}
	if v.LiveXML == "" {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "Virmill cannot read what this VM is running with.")
	}
	values, err := xmlpatch.ReadResourceValues(v.LiveXML)
	if err != nil {
		return out, err
	}
	if values.VCPUs == nil || values.MaximumVCPUs == nil || values.MemoryBytes == nil || values.MaximumMemoryBytes == nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "This VM's running CPU and memory could not be read safely.")
	}
	balloon, err := xmlpatch.ReadMemoryBalloon(v.LiveXML)
	if err != nil {
		return out, err
	}
	return liveResources{VCPUs: *values.VCPUs, MaximumVCPUs: *values.MaximumVCPUs, MemoryBytes: *values.MemoryBytes,
		MaximumMemoryBytes: *values.MaximumMemoryBytes, Balloon: balloon}, nil
}

// liveChangeAllowed reports whether the running VM can take this change, with a
// refusal that names what to change instead.
func liveChangeAllowed(now liveResources, want liveRequest) error {
	if want.VCPUs != nil {
		switch {
		case *want.VCPUs == now.VCPUs:
			return domain.Fail("ALREADY_CONFIGURED", fmt.Sprintf("This VM is already running %d CPUs.", now.VCPUs))
		case *want.VCPUs > now.MaximumVCPUs && now.MaximumVCPUs == now.VCPUs:
			return domain.Fail("UNSUPPORTED_CAPABILITY", fmt.Sprintf("This VM is running all %d of its CPUs: it was not started with spare CPU slots. Change its CPUs for the next boot instead.", now.VCPUs))
		case *want.VCPUs > now.MaximumVCPUs:
			return domain.Fail("UNSUPPORTED_CAPABILITY", fmt.Sprintf("This VM can run at most %d CPUs until it is started again. Change its CPUs for the next boot instead.", now.MaximumVCPUs))
		}
	}
	if want.MemoryMiB != nil {
		wanted := *want.MemoryMiB << 20
		switch {
		case now.Balloon != "virtio":
			return domain.Fail("UNSUPPORTED_CAPABILITY", "This VM has no memory balloon, so its memory can only change at its next boot. Change its memory for the next boot, or create a VM with a memory balloon.")
		case wanted == now.MemoryBytes:
			return domain.Fail("ALREADY_CONFIGURED", fmt.Sprintf("This VM is already running with %d MiB of memory.", now.MemoryBytes>>20))
		case *want.MemoryMiB < liveMemoryFloorMiB:
			return domain.Fail("INVALID_INPUT", fmt.Sprintf("A running guest keeps at least %d MiB. Shut it down to give it less.", liveMemoryFloorMiB))
		case wanted > now.MaximumMemoryBytes:
			return domain.Fail("UNSUPPORTED_CAPABILITY", fmt.Sprintf("This VM can use at most %d MiB until it is started again. Change its memory for the next boot instead.", now.MaximumMemoryBytes>>20))
		case now.MemoryBytes%(1<<20) != 0:
			return domain.Fail("UNSUPPORTED_CAPABILITY", "This VM is running with a memory size Virmill cannot change safely while it runs.")
		}
	}
	return nil
}

// liveBaseUnchanged reports whether the VM is still running what the change was
// reviewed against. The live fingerprint is not used: it moves as the guest
// runs, for reasons that have nothing to do with this change (ADR 0061).
func liveBaseUnchanged(now liveResources, input map[string]any) bool {
	return input["liveVersion"] == float64(1) &&
		input["liveBeforeVCPUs"] == float64(now.VCPUs) && input["liveMaximumVCPUs"] == float64(now.MaximumVCPUs) &&
		input["liveBeforeMemoryBytes"] == float64(now.MemoryBytes) && input["liveMaximumMemoryBytes"] == float64(now.MaximumMemoryBytes) &&
		input["liveBalloon"] == now.Balloon
}

func liveReview(now liveResources, want liveRequest) map[string]any {
	after := now
	if want.VCPUs != nil {
		after.VCPUs = *want.VCPUs
	}
	if want.MemoryMiB != nil {
		after.MemoryBytes = *want.MemoryMiB << 20
	}
	// Numbers are reviewed as JSON numbers, the same shape a stored review is
	// read back in, so one renderer serves both.
	values := func(r liveResources) map[string]any {
		return map[string]any{"vcpus": float64(r.VCPUs), "maximumVcpus": float64(r.MaximumVCPUs),
			"memoryBytes": float64(r.MemoryBytes), "maximumMemoryBytes": float64(r.MaximumMemoryBytes)}
	}
	return map[string]any{"running": values(now), "afterChange": values(after), "memoryBalloon": now.Balloon,
		"savedDefinitionUnchanged": true, "nextBootUnchanged": true}
}

// planLiveResources reviews a change to what a running VM is running with.
func (s *Service) planLiveResources(ctx context.Context, uid uint32, r Request, v domain.VM) (domain.Plan, error) {
	var empty domain.Plan
	if _, ok := s.Provider.(domain.LiveResourceWriter); !ok {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "This backend cannot change CPU or memory while a VM runs.")
	}
	want, err := liveResourceRequest(r.Input)
	if err != nil {
		return empty, err
	}
	now, err := readLiveResources(v)
	if err != nil {
		return empty, err
	}
	if err = liveChangeAllowed(now, want); err != nil {
		return empty, err
	}
	input := map[string]any{"vmID": r.ID, "applyMode": "now", "liveVersion": float64(1),
		"liveBeforeVCPUs": float64(now.VCPUs), "liveMaximumVCPUs": float64(now.MaximumVCPUs),
		"liveBeforeMemoryBytes": float64(now.MemoryBytes), "liveMaximumMemoryBytes": float64(now.MaximumMemoryBytes),
		"liveBalloon": now.Balloon}
	if want.VCPUs != nil {
		input["vcpus"] = float64(*want.VCPUs)
	}
	if want.MemoryMiB != nil {
		input["memoryMiB"] = float64(*want.MemoryMiB)
	}
	acks := []string{"host-mutation", "exclusive-configuration-writer"}
	risks := []string{"Changes only the running VM: its saved definition and its next boot stay as they are"}
	takesAway := want.VCPUs != nil && *want.VCPUs < now.VCPUs || want.MemoryMiB != nil && *want.MemoryMiB<<20 < now.MemoryBytes
	if takesAway {
		acks = append(acks, "guest-resource-pressure")
	}
	if want.VCPUs != nil {
		if *want.VCPUs > now.VCPUs {
			risks = append(risks, "A guest uses a new CPU only if it supports adding one while it runs; some systems need it enabled inside the guest")
		} else {
			risks = append(risks, "Only a CPU that was added while this VM ran can be removed again; work running on it moves to the remaining CPUs")
		}
	}
	if want.MemoryMiB != nil {
		risks = append(risks, "Memory moves through the guest's balloon driver: without that driver nothing changes inside the guest, and with it the change can take time")
		if *want.MemoryMiB<<20 < now.MemoryBytes {
			risks = append(risks, "Taking memory from a running guest can make it slow or stop programs inside it")
		}
	}
	risks = append(risks, "Coordinate external administrators: another writer can change what this VM runs with at any time")
	key := v.Key.String()
	step := domain.Step{ID: "effect", Action: liveResourceOperation,
		Preconditions:       []string{"running VM with the reviewed live CPU and memory", "current capability and actor checks"},
		Idempotency:         "reconcile-before-retry",
		Compensation:        "Set the reviewed earlier value back through a new reviewed plan",
		Reconciliation:      "Read the live definition's CPU count and memory without replay",
		CompletionPredicate: "The live definition reports the reviewed values"}
	return s.Engine.Plan(ctx, uid, r.Connection, liveResourceOperation, []string{key}, map[string]string{key: v.Fingerprint}, input, []domain.Step{step}, acks, risks)
}

type liveResourceHandler struct{ s *Service }

func (h *liveResourceHandler) Estimate(ctx context.Context, _ domain.Plan, _ []byte) (domain.Estimates, error) {
	if err := ctx.Err(); err != nil {
		return domain.Estimates{}, err
	}
	return domain.Estimates{Notes: "No new managed disks are allocated. The guest keeps running: nothing is shut down, and nothing is written to the saved definition. What the guest does with the change is not estimated."}, nil
}

// liveState reads the VM and what it is running with.
func (h *liveResourceHandler) liveState(ctx context.Context, p domain.Plan, input map[string]any) (liveResources, liveRequest, error) {
	var now liveResources
	var want liveRequest
	id, _ := input["vmID"].(string)
	v, err := h.s.GetVM(ctx, p.ConnectionID, id)
	if err != nil {
		return now, want, err
	}
	if want, err = liveResourceRequest(input); err != nil {
		return now, want, err
	}
	if now, err = readLiveResources(v); err != nil {
		return now, want, err
	}
	if !liveBaseUnchanged(now, input) {
		return now, want, domain.Fail("STALE_PLAN", "This VM is no longer running what the change was reviewed against; review it again.")
	}
	return now, want, nil
}

func (h *liveResourceHandler) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var input map[string]any
	if err := json.Unmarshal(b, &input); err != nil {
		return nil, err
	}
	now, want, err := h.liveState(ctx, p, input)
	if err != nil {
		return nil, err
	}
	if err = liveChangeAllowed(now, want); err != nil {
		return nil, err
	}
	review := liveReview(now, want)
	review["action"], review["vmID"], review["connection"] = "set-live", input["vmID"], p.ConnectionID
	review["persistentEdit"], review["requiresShutdown"], review["diskDeletion"] = false, false, false
	requested := map[string]any{"applyMode": "now"}
	for _, key := range []string{"vcpus", "memoryMiB"} {
		if value, ok := input[key]; ok {
			requested[key] = value
		}
	}
	review["requested"] = requested
	return review, nil
}

func (h *liveResourceHandler) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	var input map[string]any
	if err := json.Unmarshal(b, &input); err != nil {
		return err
	}
	if p.Operation != liveResourceOperation || input["applyMode"] != "now" {
		return domain.Fail("STALE_PLAN", "a live CPU/memory change requires a fresh live preview")
	}
	if _, ok := h.s.Provider.(domain.LiveResourceWriter); !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "This backend cannot change CPU or memory while a VM runs.")
	}
	now, want, err := h.liveState(ctx, p, input)
	if err != nil {
		return err
	}
	return liveChangeAllowed(now, want)
}

func (h *liveResourceHandler) Execute(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) error {
	var input map[string]any
	if err := json.Unmarshal(b, &input); err != nil {
		return err
	}
	id, _ := input["vmID"].(string)
	writer, ok := h.s.Provider.(domain.LiveResourceWriter)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "This backend cannot change CPU or memory while a VM runs.")
	}
	err := writer.SetLiveResources(ctx, p.ConnectionID, id, input)
	if err == nil {
		return nil
	}
	// A refusal that never reached the guest leaves the VM running exactly as it
	// was. The job then fails plainly and frees the VM instead of asking for a
	// recovery decision about nothing.
	var refusal *domain.Error
	if errors.As(err, &refusal) && refusal.Code != "RECOVERY_REQUIRED" {
		if v, e := h.s.GetVM(ctx, p.ConnectionID, id); e == nil {
			if now, e := readLiveResources(v); e == nil && liveBaseUnchanged(now, input) {
				return operations.NotDone(err)
			}
		}
	}
	return err
}

func (h *liveResourceHandler) Reconcile(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) (bool, error) {
	var input map[string]any
	if err := json.Unmarshal(b, &input); err != nil {
		return false, err
	}
	id, _ := input["vmID"].(string)
	writer, ok := h.s.Provider.(domain.LiveResourceWriter)
	if !ok {
		return false, domain.Fail("UNSUPPORTED_CAPABILITY", "live CPU/memory readback adapter unavailable")
	}
	return writer.ObserveLiveResources(ctx, p.ConnectionID, id, input)
}
