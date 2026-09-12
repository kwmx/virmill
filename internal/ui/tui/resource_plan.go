package tui

import (
	"encoding/json"
	"virmill.local/core/internal/domain"
)

// Render changed resources as exact before/after values. Technical details
// retain the full stored review; unavailable older reviews use normal fields.
func resourcePlanChanges(f *details, p domain.Plan) map[string]bool {
	shown := map[string]bool{}
	if p.Operation != "vm.configure-resources" {
		return shown
	}
	read := func(name string) (domain.ResourceValues, bool) {
		var v domain.ResourceValues
		b, err := json.Marshal(p.Review[name])
		if err != nil {
			return v, false
		}
		err = json.Unmarshal(b, &v)
		return v, err == nil
	}
	before, bok := read("beforeResources")
	after, aok := read("afterResources")
	if !bok || !aok {
		return shown
	}
	if before.VCPUs != nil && after.VCPUs != nil && *before.VCPUs != *after.VCPUs {
		f.scalar("CPU cores (next boot)", resourceValue(before.VCPUs)+" -> "+resourceValue(after.VCPUs), 0)
		shown["vcpus"] = true
	}
	if before.MemoryBytes != nil && after.MemoryBytes != nil && *before.MemoryBytes != *after.MemoryBytes {
		f.scalar("RAM (next boot)", resourceMemory(before.MemoryBytes)+" -> "+resourceMemory(after.MemoryBytes), 0)
		shown["memoryMiB"] = true
	}
	return shown
}
