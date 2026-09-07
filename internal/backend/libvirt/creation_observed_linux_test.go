//go:build linux && cgo

package libvirt

import (
	"os"
	"testing"
	"virmill.local/core/internal/domain"
)

func TestObservedQEMUCreationDefaultsRemainUnconfirmed(t *testing.T) {
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
	wanted, err := creationXML(domain.CreationTarget{Spec: spec, PoolName: "virmill-qualification-65930c6", Emulator: "/usr/bin/qemu-system-x86_64"}, volumes, "c1055f98ce07fbb79ce66febc46b9439fb0edd4d7004b00206e3ec6ee1509826")
	if err != nil {
		t.Fatal(err)
	}
	if err = matchesCreation(wanted, string(observed)); err == nil {
		t.Fatal("unreviewed native defaults, including a reset watchdog, were silently accepted")
	}
}
