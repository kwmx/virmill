package app

import (
	"context"
	"regexp"
	"strconv"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

var resourceViewUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *Service) resourceView(ctx context.Context, r Request) (domain.VMResourceView, error) {
	out := domain.VMResourceView{ApplyModes: []string{}}
	if !resourceViewUUID.MatchString(r.ID) || r.ID == "00000000-0000-0000-0000-000000000000" || r.Path != "" || r.Action != "" || r.After != 0 || len(r.Input) != 0 || r.Apply != nil {
		return out, domain.Fail("INVALID_INPUT", "Resource settings require one stable VM UUID and local connection")
	}
	if r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "Resource settings require a local libvirt connection")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if s.Provider == nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "VM observation is unavailable")
	}
	v, err := s.GetVM(ctx, r.Connection, r.ID)
	if err != nil {
		return out, err
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "vm", UUID: r.ID}
	if v.Key != key {
		return out, domain.Fail("SOURCE_CHANGED", "Observed VM identity differs from the selected VM")
	}
	out.Resource, out.Name, out.State, out.Fingerprint, out.HasManagedSave = v.Key, v.Name, v.State, v.Fingerprint, v.HasManagedSave
	read := func(raw string) domain.ResourceValues {
		if raw == "" {
			return domain.ResourceValues{CPUError: "No persistent configuration is available", MemoryError: "No persistent configuration is available"}
		}
		values, err := xmlpatch.ReadResourceValues(raw)
		if err != nil {
			return domain.ResourceValues{CPUError: "Resource configuration could not be read safely", MemoryError: "Resource configuration could not be read safely"}
		}
		return values
	}
	out.Persistent = read(v.PersistentXML)
	if v.LiveXML != "" {
		values := read(v.LiveXML)
		out.Live = &values
	}
	out.CPUReason = out.Persistent.CPUError
	out.MemoryReason = out.Persistent.MemoryError
	if out.Persistent.VCPUs != nil && out.CPUReason == "" {
		// The existing value may exceed the allowed NEW value range. A supported
		// reduction must still be offered without modifying the observed value.
		probe := uint64(1)
		_, err := xmlpatch.EditResources(v.PersistentXML, xmlpatch.ResourceEdit{VCPUs: &probe})
		out.CanEditCPU = err == nil
		if err != nil {
			out.CPUReason = "CPU layout needs advanced configuration; the basic editor cannot safely change it. " + validation.SafeText(err.Error())
		}
	}
	if out.Persistent.MemoryBytes != nil && out.MemoryReason == "" {
		// Cover exact binary units through TiB and the supported decimal KB/MB
		// units. Larger decimal units have no whole-MiB target within the editor
		// range. These are in-memory preservation probes, never requested changes.
		var probeErr error
		for _, mib := range []uint64{1, 125, 15625, 1048576} {
			_, probeErr = xmlpatch.EditResources(v.PersistentXML, xmlpatch.ResourceEdit{MemoryMiB: &mib})
			if probeErr == nil {
				out.CanEditMemory = true
				break
			}
		}
		if probeErr != nil {
			out.MemoryReason = "Memory layout needs advanced configuration; the basic editor cannot safely change it. " + validation.SafeText(probeErr.Error())
		}
	}
	if _, ok := s.Provider.(domain.ConfigurationValidator); !ok {
		out.CanEditCPU, out.CanEditMemory = false, false
		out.CPUReason, out.MemoryReason = "Backend cannot verify configuration preservation", "Backend cannot verify configuration preservation"
	}
	if out.CanEditCPU || out.CanEditMemory {
		out.ApplyModes = []string{"next-boot"}
	}
	if err := nextBootEditable(v); err != nil {
		reason := "Wait until the VM is stopped, running or paused before changing next-boot settings"
		if v.HasManagedSave {
			reason = "Restore the saved VM, then shut it down before editing hardware"
		}
		if v.PersistentXML == "" {
			reason = "This VM has no persistent configuration to edit"
		}
		if out.CanEditCPU {
			out.CPUReason = reason
		}
		if out.CanEditMemory {
			out.MemoryReason = reason
		}
		out.CanEditCPU, out.CanEditMemory = false, false
	}
	// Edits to a VM that is not stopped apply only after it shuts down (ADR 0061).
	out.RequiresShutdown = v.State != "stopped" && (out.CanEditCPU || out.CanEditMemory)
	s.liveResourceOffer(&out, v)
	return out, ctx.Err()
}

// liveResourceOffer reports what this VM can change while it runs, and says why
// it cannot when it cannot (ADR 0068).
func (s *Service) liveResourceOffer(out *domain.VMResourceView, v domain.VM) {
	reason := func(text string) {
		out.CanChangeLiveCPU, out.CanChangeLiveMemory = false, false
		out.LiveCPUReason, out.LiveMemoryReason = text, text
	}
	if _, ok := s.Provider.(domain.LiveResourceWriter); !ok {
		reason("This backend cannot change CPU or memory while a VM runs")
		return
	}
	now, err := readLiveResources(v)
	if err != nil {
		reason(validation.SafeText(err.Error()))
		return
	}
	out.MemoryBalloon = now.Balloon
	if out.CanChangeLiveCPU = now.MaximumVCPUs > now.VCPUs; !out.CanChangeLiveCPU {
		out.LiveCPUReason = "This VM is running all " + strconv.FormatUint(now.VCPUs, 10) +
			" of its CPUs: it was not started with spare CPU slots. Change its CPUs for the next boot instead"
	}
	switch {
	case now.Balloon != "virtio":
		out.LiveMemoryReason = "This VM has no memory balloon, so its memory can only change at its next boot"
	case now.MemoryBytes%(1<<20) != 0:
		out.LiveMemoryReason = "This VM is running with a memory size Virmill cannot change safely while it runs"
	case now.MaximumMemoryBytes>>20 <= liveMemoryFloorMiB:
		out.LiveMemoryReason = "This VM is too small to give memory back while it runs"
	default:
		out.CanChangeLiveMemory = true
	}
	if out.CanChangeLiveCPU || out.CanChangeLiveMemory {
		out.ApplyModes = append(out.ApplyModes, "now")
	}
}
