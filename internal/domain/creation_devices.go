package domain

import "strings"

func CreationChipset(machine string) string {
	if machine == "q35" || strings.HasPrefix(machine, "pc-q35-") {
		return "q35"
	}
	if machine == "pc" || strings.HasPrefix(machine, "pc-i440fx-") {
		return "i440fx"
	}
	return ""
}

func DefaultCreationDevices(machine string) (*CreationDevicePolicy, error) {
	p := &CreationDevicePolicy{Version: 1, Chipset: CreationChipset(machine), PCIPlacement: "libvirt-auto", USBController: "none", MemoryBalloon: "none", WatchdogAction: "none", Input: "ps2", Audio: "none", Serial: "isa-serial"}
	return p, p.Validate(machine)
}

func (p *CreationDevicePolicy) Validate(machine string) error {
	if p == nil {
		return nil
	} // Previously persisted recipes stay strict and readable.
	if p.Version != 1 {
		return Fail("UNSUPPORTED_CAPABILITY", "unsupported creation device policy version")
	}
	if p.Chipset == "" || p.Chipset != CreationChipset(machine) || p.PCIPlacement != "libvirt-auto" || p.Input != "ps2" || p.Audio != "none" || p.Serial != "isa-serial" {
		return Fail("INVALID_INPUT", "device policy requires the selected PC chipset, automatic PCI placement, PS/2 input, no host audio and an ISA serial console")
	}
	if (p.USBController != "none" && p.USBController != "qemu-xhci") || (p.MemoryBalloon != "none" && p.MemoryBalloon != "virtio") || (p.WatchdogAction != "none" && p.WatchdogAction != "reset") || (p.Chipset == "i440fx" && p.WatchdogAction != "none") {
		return Fail("INVALID_INPUT", "unsupported USB, balloon or chipset watchdog policy")
	}
	return nil
}
