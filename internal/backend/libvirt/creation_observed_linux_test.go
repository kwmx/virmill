//go:build linux && cgo

package libvirt

import (
	"os"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func observedQ35Creation(t *testing.T) (domain.CreationTarget, []domain.CreatedVolume, string) {
	t.Helper()
	// This replays captured XML only. The separate native run, not this test,
	// exercised allocation, upload, readback and definition on a disposable VM.
	observed, err := os.ReadFile("../../../tests/fixtures/creation/qemu12-q35-unreviewed.xml")
	if err != nil {
		t.Fatal(err)
	}
	spec := domain.CreationSpec{
		UUID: "ae630461-91d3-4f07-ad88-e6842c3dc3ea", Name: "Virmill qualification Kali QCOW2",
		PoolID: "44bac6cf-3da8-4aed-a62c-cf74be34de6f", Architecture: "x86_64", Machine: "pc-q35-10.2",
		VCPUs: 2, MemoryMiB: 2048, CPU: domain.CreationCPU{Mode: "host-passthrough"},
		Firmware: domain.CreationFirmware{Mode: "bios"}, Clock: "utc", Graphics: "vnc-unix",
		Disks: []domain.CreationDisk{{SourceID: "boot", Bus: "virtio", BootOrder: 1}}, NICs: []domain.CreationNIC{},
	}
	volumes := []domain.CreatedVolume{{Intent: domain.VolumeIntent{SourceID: "boot", PoolID: spec.PoolID, Name: "virmill-ae630461-91d3-4f07-ad88-e6842c3dc3ea-disk-000.qcow2"}}}
	return domain.CreationTarget{Spec: spec, PoolName: "virmill-qualification-65930c6", Emulator: "/usr/bin/qemu-system-x86_64"}, volumes, string(observed)
}

func TestObservedQEMUCreationDefaultsRemainUnconfirmed(t *testing.T) {
	target, volumes, observed := observedQ35Creation(t)
	wanted, err := creationXML(target, volumes, "c1055f98ce07fbb79ce66febc46b9439fb0edd4d7004b00206e3ec6ee1509826")
	if err != nil {
		t.Fatal(err)
	}
	if err = matchesCreation(wanted, observed); err == nil {
		t.Fatal("unreviewed native defaults, including a reset watchdog, were silently accepted")
	}
}

func TestObservedQ35RequiresExactReviewedDevicePolicy(t *testing.T) {
	target, volumes, observed := observedQ35Creation(t)
	policy, err := domain.DefaultCreationDevices(target.Spec.Machine)
	if err != nil {
		t.Fatal(err)
	}
	target.Spec.DevicePolicy = policy
	conservative, err := creationXML(target, volumes, "c1055f98ce07fbb79ce66febc46b9439fb0edd4d7004b00206e3ec6ee1509826")
	if err != nil {
		t.Fatal(err)
	}
	if matchesCreationPolicy(conservative, observed, policy) == nil {
		t.Fatal("reset watchdog, USB and balloon substituted for disabled policy")
	}
	policy.USBController, policy.MemoryBalloon, policy.WatchdogAction = "qemu-xhci", "virtio", "reset"
	wanted, err := creationXML(target, volumes, "c1055f98ce07fbb79ce66febc46b9439fb0edd4d7004b00206e3ec6ee1509826")
	if err != nil {
		t.Fatal(err)
	}
	if err = matchesCreationPolicy(wanted, observed, policy); err != nil {
		t.Fatal(err)
	}
	if matchesCreation(wanted, observed) == nil {
		t.Fatal("automatic topology accepted without reviewed policy")
	}
	for name, edit := range map[string][2]string{
		"watchdog action":       {"action='reset'", "action='poweroff'"},
		"USB model":             {"model='qemu-xhci'", "model='nec-xhci'"},
		"balloon model":         {"model='virtio'", "model='virtio-transitional'"},
		"host audio":            {"type='none'", "type='pulseaudio'"},
		"host device":           {"</devices>", "<hostdev mode='subsystem' type='usb'/></devices>"},
		"controller ROM":        {"<target chassis='1'", "<rom file='/unreviewed'/><target chassis='1'"},
		"controller driver":     {"<target chassis='1'", "<driver iommu='on'/><target chassis='1'"},
		"target override":       {"chassis='1' port='0x10'", "chassis='1' port='0x10' hotplug='off'"},
		"duplicate index":       {"index='4'", "index='3'"},
		"duplicate chassis":     {"chassis='4'", "chassis='3'"},
		"duplicate port":        {"port='0x13'", "port='0x12'"},
		"missing bus":           {"bus='0x02'", "bus='0xfe'"},
		"duplicate address":     {"bus='0x03'", "bus='0x02'"},
		"invalid slot":          {"slot='0x1f'", "slot='0x20'"},
		"foreign domain":        {"domain='0x0000'", "domain='0x0001'"},
		"incomplete address":    {"domain='0x0000'", ""},
		"namespace escape":      {"<controller type='pci' index='4'", "<controller xmlns='urn:unknown' type='pci' index='4'"},
		"unknown topology":      {"index='4' model='pcie-root-port'", "index='4' model='pcie-expander-bus'"},
		"duplicate balloon":     {"</devices>", "<memballoon model='virtio'/></devices>"},
		"duplicate PCI address": {"<target chassis='1'", "<address type='pci' domain='0' bus='0' slot='3' function='0'/><target chassis='1'"},
	} {
		t.Run(name, func(t *testing.T) {
			changed := strings.Replace(observed, edit[0], edit[1], 1)
			if changed == observed {
				t.Fatal("mutation did not apply")
			}
			if matchesCreationPolicy(wanted, changed, policy) == nil {
				t.Fatal("unreviewed semantic change accepted")
			}
		})
	}
	t.Log("captured XML replay only; no hardware, VM, volume or firmware operation executed")
}

func TestObservedQ35DisabledDevicePolicyMatchesNativeDefinition(t *testing.T) {
	target, volumes, _ := observedQ35Creation(t)
	target.Spec.UUID = "a19bf9ee-cd7f-4921-baac-39ce1694eb35"
	target.Spec.Name = "Virmill policy Kali QCOW2"
	target.Spec.PoolID = "eede6ba7-13a9-48d6-8cba-b8611d36513b"
	target.PoolName = "virmill-policy-c7f8b76"
	var err error
	target.Spec.DevicePolicy, err = domain.DefaultCreationDevices(target.Spec.Machine)
	if err != nil {
		t.Fatal(err)
	}
	// This replays the definition libvirt stored for a run made with every
	// optional device off, which is the policy the captured XML carries. The
	// default balloon is virtio since ADR 0068; its own native run covers that.
	target.Spec.DevicePolicy.MemoryBalloon = "none"
	volumes[0].Intent.PoolID = target.Spec.PoolID
	volumes[0].Intent.Name = "virmill-a19bf9ee-cd7f-4921-baac-39ce1694eb35-disk-000.qcow2"
	wanted, err := creationXML(target, volumes, "d0259f2030c6a956841b3e494fc3cbbbcf8e3fdf7778b4344e174a67a06133c8")
	if err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile("../../../tests/fixtures/creation/qemu12-q35-reviewed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if err = matchesCreationPolicy(wanted, string(captured), target.Spec.DevicePolicy); err != nil {
		t.Fatal(err)
	}
	if matchesCreationPolicy(wanted, strings.Replace(string(captured), "action='none'", "action='reset'", 1), target.Spec.DevicePolicy) == nil {
		t.Fatal("native watchdog action changed silently")
	}
	t.Log("regression replay of exact native XML; separate evidence records the actual disposable run")
}
