//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

const optionsMachinesXML = `<capabilities><guest><os_type>hvm</os_type><arch name="x86_64"><domain type="qemu"><machine>pc-q35-1.0</machine></domain><domain type="kvm"><machine canonical="pc-q35-12.0">q35</machine><machine>pc-q35-12.0</machine><machine canonical="pc-i440fx-12.0">pc</machine><machine>microvm</machine></domain></arch></guest><guest><os_type>hvm</os_type><arch name="aarch64"><domain type="kvm"><machine>virt</machine></domain></arch></guest></capabilities>`
const optionsDomainXML = `<domainCapabilities><path>/usr/bin/qemu-system-x86_64</path><domain>kvm</domain><machine>pc-q35-12.0</machine><arch>x86_64</arch><vcpu max="1024"/><os supported="yes"><enum name="firmware"><value>bios</value><value>efi</value></enum><loader supported="yes"><value>/usr/share/firmware/CODE.fd</value><enum name="type"><value>pflash</value></enum><enum name="secure"><value>yes</value></enum></loader></os><cpu><mode name="host-model" supported="yes"/><mode name="host-passthrough" supported="no"/><mode name="custom" supported="yes"><model usable="yes">usable-cpu</model><model usable="no">unusable-cpu</model><model usable="unknown">uncertain-cpu</model></mode></cpu><devices><disk supported="yes"><enum name="bus"><value>virtio</value><value>sata</value><value>ide</value></enum><enum name="diskDevice"><value>disk</value><value>cdrom</value></enum></disk><graphics supported="yes"><enum name="type"><value>vnc</value></enum></graphics><tpm supported="yes"><enum name="model"><value>tpm-crb</value></enum><enum name="backendModel"><value>emulator</value></enum><enum name="backendVersion"><value>2.0</value></enum></tpm></devices></domainCapabilities>`
const optionsFirmwareJSON = `{"mapping":{"device":"flash","mode":"split","executable":{"filename":"/usr/share/firmware/CODE.fd","format":"raw"},"nvram-template":{"filename":"/usr/share/firmware/VARS.fd","format":"raw"}},"targets":[{"architecture":"x86_64","machines":["pc-q35-*"]}],"features":[]}`

func optionsFixture(t *testing.T) (domainCaps, firmwareDescriptor) {
	t.Helper()
	caps, err := decodeCreationChoices(optionsDomainXML)
	if err != nil {
		t.Fatal(err)
	}
	var descriptor firmwareDescriptor
	if err := json.Unmarshal([]byte(optionsFirmwareJSON), &descriptor); err != nil {
		t.Fatal(err)
	}
	return caps, descriptor
}

func TestCreationOptionsAdvertisedMachinesOnly(t *testing.T) {
	machines, aliases, err := creationMachineChoices(optionsMachinesXML)
	if err != nil || len(machines) != 2 || slices.Contains(machines, "pc-q35-1.0") {
		t.Fatalf("unexpected machines: %v %v", machines, err)
	}
	for _, requested := range []string{"", "q35", "pc-q35-12.0"} {
		got, err := chooseCreationMachine(machines, aliases, requested)
		if err != nil || got != "pc-q35-12.0" {
			t.Fatalf("selection %q: %q %v", requested, got, err)
		}
	}
	for _, requested := range []string{"pc-q35-99.0", "q35\n", "microvm", "virt"} {
		if _, err := chooseCreationMachine(machines, aliases, requested); err == nil {
			t.Fatalf("unadvertised machine accepted: %q", requested)
		}
	}
	inherited := `<capabilities><guest><os_type>hvm</os_type><arch name="x86_64"><machine canonical="pc-i440fx-11.1">pc</machine><domain type="kvm"/></arch></guest></capabilities>`
	machines, aliases, err = creationMachineChoices(inherited)
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := chooseCreationMachine(machines, aliases, ""); err != nil || selected != "pc-i440fx-11.1" {
		t.Fatalf("inherited fallback: %q %v", selected, err)
	}
}

func TestCreationOptionsMachineInputBounds(t *testing.T) {
	for _, raw := range []string{"<broken", "<capabilities/>", strings.Repeat("x", 1<<20+1), strings.ReplaceAll(optionsMachinesXML, `type="kvm"`, `type="qemu"`), strings.Replace(optionsMachinesXML, "<machine>microvm</machine>", `<machine canonical="pc-q35-11.0">q35</machine>`, 1)} {
		if _, _, err := creationMachineChoices(raw); err == nil {
			t.Fatal("invalid or unavailable machine capabilities accepted")
		}
	}
	for _, name := range []string{"pc-q35-12.0\x1b", "q35", "pc", "pc-q35-../../host", "pc-q35-" + strings.Repeat("0", 128)} {
		if creationMachineName(name) {
			t.Fatalf("invalid machine name accepted: %q", name)
		}
	}
}

func TestCreationOptionsPositiveCapabilities(t *testing.T) {
	caps, descriptor := optionsFixture(t)
	options, err := creationOptionsFromCaps(caps, []string{caps.Machine}, 8192, []firmwareDescriptor{descriptor, descriptor})
	if err != nil {
		t.Fatal(err)
	}
	if options.MaxVCPUs != 512 || options.HostMemoryMiB != 8192 || options.Machine != caps.Machine || options.Architecture != "x86_64" {
		t.Fatalf("wrong observed limits: %+v", options)
	}
	if strings.Join(options.CPUModes, ",") != "host-model,custom" || strings.Join(options.CPUModels, ",") != "usable-cpu" || strings.Join(options.DiskBuses, ",") != "virtio,sata" || strings.Join(options.Graphics, ",") != "none,vnc-unix" {
		t.Fatalf("unsupported option leaked: %+v", options)
	}
	if len(options.Firmware) != 3 || options.Firmware[0].Firmware.Mode != "bios" {
		t.Fatalf("wrong firmware choices: %+v", options.Firmware)
	}
	for _, option := range options.Firmware {
		spec := domain.CreationSpec{Architecture: options.Architecture, Machine: options.Machine, VCPUs: 1, CPU: domain.CreationCPU{Mode: "host-model"}, Firmware: option.Firmware, Graphics: "none"}
		if err := checkCaps(caps, spec); err != nil {
			t.Fatalf("offered firmware cannot pass shared capabilities validation: %+v %v", option, err)
		}
		if option.Firmware.Mode == "uefi" && !descriptorMatches(descriptor, spec) {
			t.Fatal("offered firmware does not match descriptor")
		}
	}
}

func TestCreationOptionsFirmwarePolicies(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*domainCaps, *firmwareDescriptor)
		want int
	}{
		{"no TPM advertisement", func(c *domainCaps, _ *firmwareDescriptor) { c.Devices.TPM.Supported = "no" }, 2},
		{"partial TPM advertisement", func(c *domainCaps, _ *firmwareDescriptor) { c.Devices.TPM.Enums = c.Devices.TPM.Enums[:2] }, 2},
		{"BIOS not advertised", func(c *domainCaps, _ *firmwareDescriptor) { c.OS.Enums = nil }, 2},
		{"loader not advertised", func(c *domainCaps, _ *firmwareDescriptor) { c.OS.Loader.Values = nil }, 1},
		{"wrong firmware machine", func(_ *domainCaps, d *firmwareDescriptor) { d.Targets[0].Machines = []string{"pc-i440fx-*"} }, 1},
		{"wrong format pair", func(_ *domainCaps, d *firmwareDescriptor) { d.Mapping.Template.Format = "qcow2" }, 1},
		{"unknown format", func(_ *domainCaps, d *firmwareDescriptor) {
			d.Mapping.Executable.Format = "vmdk"
			d.Mapping.Template.Format = "vmdk"
		}, 1},
		{"secure template", func(_ *domainCaps, d *firmwareDescriptor) {
			d.Features = []string{"secure-boot", "enrolled-keys", "requires-smm"}
		}, 3},
		{"secure not advertised", func(c *domainCaps, d *firmwareDescriptor) {
			c.OS.Loader.Enums = c.OS.Loader.Enums[:1]
			d.Features = []string{"secure-boot", "enrolled-keys", "requires-smm"}
		}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			caps, descriptor := optionsFixture(t)
			test.edit(&caps, &descriptor)
			options, err := creationOptionsFromCaps(caps, []string{caps.Machine}, 8192, []firmwareDescriptor{descriptor})
			if err != nil || len(options.Firmware) != test.want {
				t.Fatalf("firmware count: %d want %d (%v)", len(options.Firmware), test.want, err)
			}
			if test.name == "secure template" {
				for _, option := range options.Firmware[1:] {
					if !option.Firmware.SecureBoot {
						t.Fatal("enrolled-keys template offered without Secure Boot")
					}
				}
			}
		})
	}
}

func TestCreationOptionsUnavailableIsActionable(t *testing.T) {
	for _, edit := range []func(*domainCaps){func(c *domainCaps) { c.CPU.Modes = nil }, func(c *domainCaps) { c.Devices.Disk.Supported = "no" }, func(c *domainCaps) { c.OS.Supported = "no" }} {
		caps, descriptor := optionsFixture(t)
		edit(&caps)
		if _, err := creationOptionsFromCaps(caps, []string{caps.Machine}, 8192, []firmwareDescriptor{descriptor}); err == nil {
			t.Fatal("missing capability silently accepted")
		}
	}
	caps, descriptor := optionsFixture(t)
	if _, err := creationOptionsFromCaps(caps, []string{caps.Machine}, 0, []firmwareDescriptor{descriptor}); err == nil {
		t.Fatal("unknown host memory silently accepted")
	}
	for _, raw := range []string{"<broken", "<wrong/>", strings.Replace(optionsDomainXML, "<domain>kvm", "<domain>qemu", 1), strings.Replace(optionsDomainXML, `max="1024"`, `max="0"`, 1), strings.Repeat("x", 1<<20+1)} {
		if _, err := decodeCreationChoices(raw); err == nil {
			t.Fatal("invalid domain capabilities accepted")
		}
	}
}

func TestCreationOptionsCanceledBeforeConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Provider{}).CreationOptions(ctx, "qemu:///system", ""); err != context.Canceled {
		t.Fatalf("canceled request reached native connection: %v", err)
	}
}

func TestCreationOptionsDefaultBIOSIsIndependentOfAutoselection(t *testing.T) {
	caps, descriptor := optionsFixture(t)
	caps.OS.Enums = []capEnum{{Name: "firmware", Values: []string{"efi"}}}
	caps.OS.Loader.Enums = append(caps.OS.Loader.Enums, capEnum{Name: "type", Values: []string{"rom"}})
	options, err := creationOptionsFromCaps(caps, []string{caps.Machine}, 8192, []firmwareDescriptor{descriptor})
	if err != nil || len(options.Firmware) != 3 {
		t.Fatalf("EFI-only autoselection hid supported default BIOS: %+v %v", options.Firmware, err)
	}
	if options.Firmware[0].Firmware != (domain.CreationFirmware{Mode: "bios"}) {
		t.Fatalf("default ROM choice invented a BIOS path or auxiliary state: %+v", options.Firmware[0])
	}
	for name, change := range map[string]func(*domainCaps){
		"OS unavailable":       func(c *domainCaps) { c.OS.Supported = "no" },
		"loader unavailable":   func(c *domainCaps) { c.OS.Loader.Supported = "no" },
		"ROM not advertised":   func(c *domainCaps) { c.OS.Loader.Enums = nil },
		"another architecture": func(c *domainCaps) { c.Arch = "aarch64" },
		"another hypervisor":   func(c *domainCaps) { c.Domain = "qemu" },
		"another machine":      func(c *domainCaps) { c.Machine = "microvm" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := caps
			change(&changed)
			if creationDefaultBIOSSupported(changed) {
				t.Fatal("unsupported or unobserved default BIOS offered")
			}
		})
	}
}
