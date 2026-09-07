//go:build linux && amd64

package creating

import "virmill.local/core/internal/domain"

func creationOperation(op string) bool { return op == "vm.create" || op == "vm.create.devices-v1" }

func creationRecipeVersion(p domain.Plan, in input) error {
	if in.NVRAMDeclarationVersion != 0 && (in.NVRAMDeclarationVersion != 1 || in.Target.Spec.Firmware.Mode != "uefi") {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "unsupported creation NVRAM declaration policy")
	}
	if !creationOperation(p.Operation) || (p.Operation == "vm.create") != (in.Target.Spec.DevicePolicy == nil) {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "creation operation and reviewed device policy version differ")
	}
	return in.Target.Spec.DevicePolicy.Validate(in.Target.Spec.Machine)
}
