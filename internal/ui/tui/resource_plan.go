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

// liveResourcePlanChanges renders what a running VM will be running with, and
// what stays as it is (ADR 0068).
func liveResourcePlanChanges(f *details, p domain.Plan) map[string]bool {
	shown := map[string]bool{}
	if p.Operation != "vm.resources-live-v1" {
		return shown
	}
	read := func(name, field string) (uint64, bool) {
		values, ok := p.Review[name].(map[string]any)
		if !ok {
			return 0, false
		}
		number, ok := values[field].(float64)
		return uint64(number), ok && number >= 0 && number == float64(uint64(number))
	}
	pair := func(field string) (uint64, uint64, bool) {
		before, bok := read("running", field)
		after, aok := read("afterChange", field)
		return before, after, bok && aok && before != after
	}
	if before, after, changed := pair("vcpus"); changed {
		f.scalar("CPU cores now running", resourceValue(&before)+" -> "+resourceValue(&after), 0)
		shown["vcpus"] = true
	}
	if before, after, changed := pair("memoryBytes"); changed {
		f.scalar("RAM now running", resourceMemory(&before)+" -> "+resourceMemory(&after), 0)
		shown["memoryMiB"] = true
	}
	if len(shown) > 0 {
		f.line("The saved settings and the next boot stay as they are.", 0)
		shown["applyMode"] = true
	}
	return shown
}
