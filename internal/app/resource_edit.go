package app

import (
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
)

func editableVM(v domain.VM) error {
	if v.State != "stopped" || v.PersistentXML == "" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "configuration edits require a powered-off persistent VM; shutdown is a separate approved operation")
	}
	if v.HasManagedSave {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "managed-save state is bound to the old hardware; restore and shut down the guest before editing")
	}
	return nil
}

// ADR 0061: next-boot CPU and memory edits are also allowed while a VM runs or
// is paused. They are checked against the saved definition alone, because the
// live one changes as the guest runs.
const persistentPrecondition = "persistent-xml-v1"

func resourcesEditable(v domain.VM) error {
	if v.PersistentXML == "" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "this VM has no saved definition to edit")
	}
	if v.HasManagedSave {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "managed-save state is bound to the old hardware; restore and shut down the guest before editing")
	}
	if v.State != "stopped" && v.State != "running" && v.State != "paused" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "CPU and memory can be changed while the VM is stopped, running or paused; it is "+v.State)
	}
	return nil
}

// editBaseUnchanged reports whether the VM still matches what the edit was
// reviewed against.
func editBaseUnchanged(v domain.VM, input map[string]any) bool {
	if input["editPrecondition"] == persistentPrecondition {
		return input["editVersion"] == float64(1) && !v.HasManagedSave && xmlpatch.Digest(v.PersistentXML) == input["editBeforePersistentSHA256"]
	}
	return input["editPrecondition"] == nil && v.Fingerprint == input["editBeforeFingerprint"]
}

func resourceEdit(input map[string]any) (xmlpatch.ResourceEdit, error) {
	var edit xmlpatch.ResourceEdit
	if input["applyMode"] != "next-boot" {
		return edit, domain.Fail("UNSUPPORTED_CAPABILITY", "this adapter supports next-boot CPU/RAM edits on powered-off VMs; now and both require dedicated live adapters")
	}
	for _, name := range []string{"vcpus", "memoryMiB"} {
		if value, present := input[name]; present {
			n, ok := value.(float64)
			if !ok || n < 1 || n > 1048576 || n != float64(uint64(n)) {
				return edit, domain.Fail("INVALID_INPUT", "positive bounded integer resource value required")
			}
			integer := uint64(n)
			if name == "vcpus" {
				if integer > 512 {
					return edit, domain.Fail("INVALID_INPUT", "vcpus exceeds the 512-CPU adapter bound")
				}
				edit.VCPUs = &integer
			} else {
				edit.MemoryMiB = &integer
			}
		}
	}
	if edit.VCPUs == nil && edit.MemoryMiB == nil {
		return edit, domain.Fail("INVALID_INPUT", "no CPU/RAM edit requested")
	}
	return edit, nil
}
