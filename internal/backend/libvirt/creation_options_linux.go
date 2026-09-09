//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

// CreationOptions observes the selected local backend. A writable libvirt API
// connection is required by GetDomainCapabilities; this method never invokes a
// domain, network, volume or pool mutation.
func (p *Provider) CreationOptions(ctx context.Context, uri, machine string) (domain.CreationOptions, error) {
	var empty domain.CreationOptions
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	c, err := connect(uri, true)
	if err != nil {
		return empty, err
	}
	defer c.Close()
	raw, err := c.GetCapabilities()
	if err != nil {
		return empty, err
	}
	machines, aliases, err := creationMachineChoices(raw)
	if err != nil {
		return empty, err
	}
	selected, err := chooseCreationMachine(machines, aliases, machine)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	rawDomain, err := c.GetDomainCapabilities("", "x86_64", selected, "kvm", 0)
	if err != nil {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "Cannot read creation options for this KVM machine: "+err.Error())
	}
	caps, err := decodeCreationChoices(rawDomain)
	if err != nil {
		return empty, err
	}
	if caps.Machine != selected {
		return empty, domain.Fail("SOURCE_CHANGED", "The advertised machine changed while reading creation options; refresh and choose again.")
	}
	info, err := c.GetNodeInfo()
	if err != nil {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "Cannot read host memory for VM creation: "+err.Error())
	}
	var descriptors []firmwareDescriptor
	if caps.OS.Loader.Supported == "yes" && enumHas(caps.OS.Loader.Enums, "type", "pflash") {
		descriptors, err = creationFirmwareDescriptors(ctx)
		if err != nil {
			return empty, err
		}
	}
	options, err := creationOptionsFromCaps(caps, machines, info.Memory/1024, descriptors)
	if err != nil {
		return empty, err
	}
	// Only expose firmware whose exact system files can pass the same ownership
	// and file checks as creation preflight. Cache repeated descriptor paths.
	checked := map[string]bool{}
	for _, option := range options.Firmware {
		if option.Firmware.Mode != "uefi" {
			continue
		}
		for _, path := range []string{option.Firmware.Code, option.Firmware.Template} {
			if checked[path] {
				continue
			}
			if err := ctx.Err(); err != nil {
				return empty, err
			}
			if _, err := firmwareFileDigest(path); err != nil {
				return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "An advertised firmware file is unavailable; repair the host firmware package or choose another machine: "+err.Error())
			}
			checked[path] = true
		}
	}
	return options, nil
}

type creationCapMachine struct {
	Name      string `xml:",chardata"`
	Canonical string `xml:"canonical,attr"`
}

func creationMachineChoices(raw string) ([]string, map[string]string, error) {
	var capabilities struct {
		XMLName xml.Name `xml:"capabilities"`
		Guests  []struct {
			OS   string `xml:"os_type"`
			Arch struct {
				Name     string               `xml:"name,attr"`
				Machines []creationCapMachine `xml:"machine"`
				Domains  []struct {
					Type     string               `xml:"type,attr"`
					Machines []creationCapMachine `xml:"machine"`
				} `xml:"domain"`
			} `xml:"arch"`
		} `xml:"guest"`
	}
	if len(raw) > 1<<20 || xml.Unmarshal([]byte(raw), &capabilities) != nil {
		return nil, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Host machine capabilities are invalid or exceed the supported size; check libvirt.")
	}
	machines, aliases := []string{}, map[string]string{}
	for _, guest := range capabilities.Guests {
		if guest.OS != "hvm" || guest.Arch.Name != "x86_64" {
			continue
		}
		for _, backend := range guest.Arch.Domains {
			if backend.Type != "kvm" {
				continue
			}
			entries := backend.Machines
			if len(entries) == 0 {
				entries = guest.Arch.Machines
			}
			if len(entries) > 4096 {
				return nil, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Host machine inventory exceeds the supported size.")
			}
			for _, entry := range entries {
				name, canonical := strings.TrimSpace(entry.Name), strings.TrimSpace(entry.Canonical)
				if canonical == "" {
					canonical = name
				}
				if !creationMachineName(canonical) || name == "" || len(name) > 128 {
					continue
				}
				if prior, ok := aliases[name]; ok && prior != canonical {
					return nil, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Host advertises conflicting machine aliases; check libvirt.")
				}
				aliases[name], aliases[canonical] = canonical, canonical
				if !slices.Contains(machines, canonical) {
					machines = append(machines, canonical)
				}
			}
		}
	}
	if len(machines) == 0 {
		return nil, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "No supported x86_64 KVM Q35 or i440fx machine is advertised; enable KVM support on this host.")
	}
	slices.Sort(machines)
	return machines, aliases, nil
}

func creationMachineName(name string) bool {
	if len(name) > 128 || (!strings.HasPrefix(name, "pc-q35-") && !strings.HasPrefix(name, "pc-i440fx-")) {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return false
		}
	}
	return true
}

func chooseCreationMachine(machines []string, aliases map[string]string, requested string) (string, error) {
	if requested != "" {
		if selected, ok := aliases[requested]; ok && slices.Contains(machines, selected) {
			return selected, nil
		}
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "The selected machine is not advertised by this host; refresh and choose an available machine.")
	}
	if selected := aliases["q35"]; slices.Contains(machines, selected) {
		return selected, nil
	}
	for _, machine := range machines {
		if strings.HasPrefix(machine, "pc-q35-") {
			return machine, nil
		}
	}
	if len(machines) > 0 {
		return machines[0], nil
	}
	return "", domain.Fail("UNSUPPORTED_CAPABILITY", "No supported KVM machine is available.")
}

func decodeCreationChoices(raw string) (domainCaps, error) {
	var caps domainCaps
	var root struct{ XMLName xml.Name }
	if len(raw) > 1<<20 || xml.Unmarshal([]byte(raw), &root) != nil || root.XMLName.Local != "domainCapabilities" || xml.Unmarshal([]byte(raw), &caps) != nil || caps.Domain != "kvm" || caps.Arch != "x86_64" || !creationMachineName(caps.Machine) || caps.Path == "" || caps.VCPU.Max == 0 {
		return caps, domain.Fail("UNSUPPORTED_CAPABILITY", "The selected machine does not advertise usable x86_64 KVM creation capabilities.")
	}
	return caps, nil
}

func creationOptionsFromCaps(caps domainCaps, machines []string, hostMemoryMiB uint64, descriptors []firmwareDescriptor) (domain.CreationOptions, error) {
	out := domain.CreationOptions{Architecture: "x86_64", Machines: machines, Machine: caps.Machine, MaxVCPUs: min(512, caps.VCPU.Max), HostMemoryMiB: hostMemoryMiB, CPUModes: []string{}, CPUModels: []string{}, Firmware: []domain.CreationFirmwareOption{}, DiskBuses: []string{}, Graphics: []string{"none"}}
	if hostMemoryMiB == 0 {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "Host memory capacity is unavailable; check libvirt before choosing VM memory.")
	}
	for _, mode := range caps.CPU.Modes {
		if mode.Supported != "yes" {
			continue
		}
		switch mode.Name {
		case "host-passthrough", "host-model":
			if !slices.Contains(out.CPUModes, mode.Name) {
				out.CPUModes = append(out.CPUModes, mode.Name)
			}
		case "custom":
			for _, model := range mode.Models {
				if model.Usable == "yes" && model.Name != "" && len(model.Name) <= 128 && !slices.Contains(out.CPUModels, model.Name) {
					out.CPUModels = append(out.CPUModels, model.Name)
				}
			}
			if len(out.CPUModels) > 0 && !slices.Contains(out.CPUModes, "custom") {
				out.CPUModes = append(out.CPUModes, "custom")
			}
		}
	}
	if len(out.CPUModes) == 0 {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "No supported CPU mode is available for this machine; choose another machine.")
	}
	slices.Sort(out.CPUModels)
	if caps.Devices.Disk.Supported == "yes" {
		for _, bus := range []string{"virtio", "sata", "scsi"} {
			if enumHas(caps.Devices.Disk.Enums, "bus", bus) {
				out.DiskBuses = append(out.DiskBuses, bus)
			}
		}
	}
	if len(out.DiskBuses) == 0 {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "No supported disk bus is available for this machine; choose another machine.")
	}
	if caps.Devices.Graphics.Supported == "yes" && enumHas(caps.Devices.Graphics.Enums, "type", "vnc") {
		out.Graphics = append(out.Graphics, "vnc-unix")
	}
	if creationDefaultBIOSSupported(caps) {
		out.Firmware = append(out.Firmware, domain.CreationFirmwareOption{Label: "BIOS", Firmware: domain.CreationFirmware{Mode: "bios"}})
	}
	seen := map[domain.CreationFirmware]bool{}
	if caps.OS.Supported == "yes" && caps.OS.Loader.Supported == "yes" && enumHas(caps.OS.Loader.Enums, "type", "pflash") {
		for _, descriptor := range descriptors {
			mapping := descriptor.Mapping
			if !slices.Contains(caps.OS.Loader.Values, mapping.Executable.Filename) || (mapping.Executable.Format != "raw" && mapping.Executable.Format != "qcow2") || !filepath.IsAbs(mapping.Executable.Filename) || !filepath.IsAbs(mapping.Template.Filename) {
				continue
			}
			for _, secure := range []bool{false, true} {
				if secure && !enumHas(caps.OS.Loader.Enums, "secure", "yes") {
					continue
				}
				f := domain.CreationFirmware{Mode: "uefi", Code: mapping.Executable.Filename, Template: mapping.Template.Filename, Format: mapping.Executable.Format, SecureBoot: secure}
				if !descriptorMatches(descriptor, domain.CreationSpec{Architecture: out.Architecture, Machine: out.Machine, Firmware: f}) {
					continue
				}
				for _, tpm := range []bool{false, true} {
					if tpm && (caps.Devices.TPM.Supported != "yes" || !enumHas(caps.Devices.TPM.Enums, "model", "tpm-crb") || !enumHas(caps.Devices.TPM.Enums, "backendModel", "emulator") || !enumHas(caps.Devices.TPM.Enums, "backendVersion", "2.0")) {
						continue
					}
					f.TPM = tpm
					if seen[f] {
						continue
					}
					seen[f] = true
					label := "UEFI"
					if secure {
						label += " + Secure Boot"
					}
					if tpm {
						label += " + TPM 2.0"
					}
					label += " (" + filepath.Base(f.Code) + ")"
					out.Firmware = append(out.Firmware, domain.CreationFirmwareOption{Label: label, Firmware: f})
				}
			}
		}
	}
	if len(out.Firmware) == 0 {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "No compatible BIOS or UEFI firmware is available; install a matching host firmware package or choose another machine.")
	}
	return out, nil
}

// The firmware enum describes firmware AUTOSELECTION, not every boot path:
// https://libvirt.org/formatdomaincaps.html#guest-firmware
// QEMU PC machines also have a default BIOS ROM without an explicit loader:
// https://libvirt.org/formatdomain.html#guest-firmware
// https://www.qemu.org/docs/master/system/i386/pc.html
// Q35 shares this BIOS default (pc_q35_machine_options, QEMU v10.2.0):
// https://github.com/qemu/qemu/blob/v10.2.0/hw/i386/pc_q35.c
// Require the observed PC/KVM machine and positive OS/ROM support before
// offering that existing creation mode. This does not verify a guest boot or
// invent a firmware path; QEMU resolves its own default ROM at startup.
func creationDefaultBIOSSupported(caps domainCaps) bool {
	if caps.Domain != "kvm" || caps.Arch != "x86_64" || !creationMachineName(caps.Machine) || caps.OS.Supported != "yes" {
		return false
	}
	return enumHas(caps.OS.Enums, "firmware", "bios") ||
		(caps.OS.Loader.Supported == "yes" && enumHas(caps.OS.Loader.Enums, "type", "rom"))
}

func creationFirmwareDescriptors(ctx context.Context) ([]firmwareDescriptor, error) {
	paths, err := filepath.Glob("/usr/share/qemu/firmware/*.json")
	if err != nil || len(paths) > 256 {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Firmware descriptor inventory is unavailable or exceeds the supported size.")
	}
	out := []firmwareDescriptor{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := openSystemFile(path)
		if err != nil {
			return nil, err
		}
		st, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.Mode().IsRegular() || owner.Uid != 0 || st.Mode().Perm()&0022 != 0 || st.Size() > 64<<10 {
			file.Close()
			return nil, domain.Fail("PERMISSION_DENIED", "Untrusted system firmware descriptor; repair the host firmware package.")
		}
		data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := wire.Validate(data); err != nil {
			return nil, fmt.Errorf("invalid system firmware descriptor: %w", err)
		}
		var descriptor firmwareDescriptor
		if err := json.Unmarshal(data, &descriptor); err != nil {
			return nil, err
		}
		out = append(out, descriptor)
	}
	return out, nil
}
