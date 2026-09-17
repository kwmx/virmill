//go:build linux && cgo

package libvirt

import (
	"context"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// liveChange is a reviewed change to what a running VM is running with
// (ADR 0068). It is recomputed here from the running domain, never trusted from
// the plan alone.
type liveChange struct {
	setVCPUs, setMemory bool
	vcpus               uint
	memoryKiB           uint64
}

func liveStale(message string) error { return domain.Fail("STALE_PLAN", message) }

// liveTarget reads what the VM is running with and checks the reviewed change
// against it. It refuses before any effect: nothing here changes the VM.
func liveTarget(v domain.VM, input map[string]any) (liveChange, error) {
	var out liveChange
	if input["liveVersion"] != float64(1) || input["applyMode"] != "now" {
		return out, liveStale("fresh live CPU/memory preview required")
	}
	if v.State != "running" || v.HasManagedSave || v.LiveXML == "" {
		return out, liveStale("changing CPU or memory while a VM runs requires a running VM Virmill can read, without saved state")
	}
	values, err := xmlpatch.ReadResourceValues(v.LiveXML)
	if err != nil {
		return out, err
	}
	balloon, err := xmlpatch.ReadMemoryBalloon(v.LiveXML)
	if err != nil {
		return out, err
	}
	if values.VCPUs == nil || values.MaximumVCPUs == nil || values.MemoryBytes == nil || values.MaximumMemoryBytes == nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "the running CPU and memory could not be read safely")
	}
	if input["liveBeforeVCPUs"] != float64(*values.VCPUs) || input["liveMaximumVCPUs"] != float64(*values.MaximumVCPUs) ||
		input["liveBeforeMemoryBytes"] != float64(*values.MemoryBytes) || input["liveMaximumMemoryBytes"] != float64(*values.MaximumMemoryBytes) ||
		input["liveBalloon"] != balloon {
		return out, liveStale("the VM is no longer running what the change was reviewed against")
	}
	if requested, ok := input["vcpus"].(float64); ok {
		count := uint64(requested)
		if requested != float64(count) || count < 1 || count > uint64(*values.MaximumVCPUs) || count == *values.VCPUs {
			return out, liveStale("the reviewed CPU count is not one this running VM can take")
		}
		out.setVCPUs, out.vcpus = true, uint(count)
	}
	if requested, ok := input["memoryMiB"].(float64); ok {
		mib := uint64(requested)
		bytes := mib << 20
		if requested != float64(mib) || mib < 256 || balloon != "virtio" || bytes > *values.MaximumMemoryBytes || bytes == *values.MemoryBytes {
			return out, liveStale("the reviewed memory size is not one this running VM can take")
		}
		out.setMemory, out.memoryKiB = true, bytes>>10
	}
	if !out.setVCPUs && !out.setMemory {
		return out, liveStale("a live change must set a CPU count or a memory size")
	}
	return out, nil
}

func (p *Provider) SetLiveResources(ctx context.Context, uri, id string, input map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(uri, true)
	if err != nil {
		return err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		return err
	}
	defer d.Free()
	v, err := observe(d, uri)
	if err != nil {
		return err
	}
	change, err := liveTarget(v, input)
	if err != nil {
		return err
	}
	// The CPU count is set first: libvirt refuses an unsupported CPU change
	// before doing anything, so a refusal there leaves the VM untouched.
	if change.setVCPUs {
		if err = d.SetVcpusFlags(change.vcpus, native.DOMAIN_VCPU_LIVE); err != nil {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "this VM's CPUs could not be changed while it runs: "+validation.SafeText(err.Error()))
		}
	}
	if change.setMemory {
		if err = d.SetMemoryFlags(change.memoryKiB, native.DOMAIN_MEM_LIVE); err != nil {
			code := "UNSUPPORTED_CAPABILITY"
			if change.setVCPUs {
				// The CPU change already happened; this job keeps its uncertainty.
				code = "RECOVERY_REQUIRED"
			}
			return domain.Fail(code, "this VM's memory could not be changed while it runs: "+validation.SafeText(err.Error()))
		}
	}
	return nil
}

func (p *Provider) ObserveLiveResources(ctx context.Context, uri, id string, input map[string]any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return false, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		return false, err
	}
	defer d.Free()
	v, err := observe(d, uri)
	if err != nil {
		return false, err
	}
	if v.State != "running" || v.LiveXML == "" {
		return false, nil
	}
	values, err := xmlpatch.ReadResourceValues(v.LiveXML)
	if err != nil {
		return false, err
	}
	if values.VCPUs == nil || values.MemoryBytes == nil {
		return false, domain.Fail("RECOVERY_REQUIRED", "the running CPU and memory could not be read; no live recovery claim")
	}
	// The reviewed effect is the live definition's own values. What the guest has
	// done with a balloon request is the guest's business, not this claim.
	if requested, ok := input["vcpus"].(float64); ok && requested != float64(*values.VCPUs) {
		return false, nil
	}
	if requested, ok := input["memoryMiB"].(float64); ok && requested != float64(*values.MemoryBytes>>20) {
		return false, nil
	}
	return true, nil
}
