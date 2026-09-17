//go:build linux && cgo

package libvirt

import (
	"context"
	"time"

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

// liveSettle is how long a guest is given to answer a memory request. A balloon
// change is a request: QEMU holds the new target and the guest returns or takes
// the pages when its balloon driver gets to it, which takes a moment.
const liveSettle = 20 * time.Second

// liveMemoryBytes reads the memory the running domain reports, which is what the
// guest has acknowledged, not the target QEMU is holding.
func liveMemoryBytes(d *native.Domain, uri string) (uint64, error) {
	v, err := observe(d, uri)
	if err != nil {
		return 0, err
	}
	values, err := xmlpatch.ReadResourceValues(v.LiveXML)
	if err != nil {
		return 0, err
	}
	if values.MemoryBytes == nil {
		return 0, domain.Fail("UNSUPPORTED_CAPABILITY", "the running memory could not be read")
	}
	return *values.MemoryBytes, nil
}

// settledMemory waits for the guest to reach one size, and reports what it last
// observed. It never changes anything.
func settledMemory(ctx context.Context, d *native.Domain, uri string, wanted uint64) (uint64, error) {
	deadline := time.Now().Add(liveSettle)
	for {
		observed, err := liveMemoryBytes(d, uri)
		if err != nil || observed == wanted || !time.Now().Before(deadline) {
			return observed, err
		}
		select {
		case <-ctx.Done():
			return observed, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

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
		wanted := change.memoryKiB << 10
		observed, err := settledMemory(ctx, d, uri, wanted)
		if err != nil {
			return domain.Fail("RECOVERY_REQUIRED", "the memory request was made but could not be read back: "+validation.SafeText(err.Error()))
		}
		if observed != wanted {
			// The guest has not answered. Put the balloon back where it was, so
			// the VM is left running what it was reviewed as running and the job
			// can fail plainly instead of asking for a recovery decision.
			before, ok := input["liveBeforeMemoryBytes"].(float64)
			if !ok {
				return domain.Fail("RECOVERY_REQUIRED", "the guest did not answer the memory request and the earlier size is unknown")
			}
			if err = d.SetMemoryFlags(uint64(before)>>10, native.DOMAIN_MEM_LIVE); err != nil {
				return domain.Fail("RECOVERY_REQUIRED", "the guest did not answer the memory request and its earlier size could not be asked for again: "+validation.SafeText(err.Error()))
			}
			if back, err := settledMemory(ctx, d, uri, uint64(before)); err != nil || back != uint64(before) {
				return domain.Fail("RECOVERY_REQUIRED", "the guest answered neither the memory request nor the return to its earlier size")
			}
			return domain.Fail("WAIT_TIMEOUT", "this guest did not answer the memory request, so it is still running the size it was; its balloon driver may not be running inside it")
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
