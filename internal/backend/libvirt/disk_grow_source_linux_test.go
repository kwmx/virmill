//go:build linux && amd64 && cgo

package libvirt

import "testing"

// Virmill creates pool-volume disks; imported definitions may use files.
func TestGrowDiskSourceAcceptsFileAndPoolVolumeDisks(t *testing.T) {
	raw := `<domain type='kvm'><name>grow-fixture</name><uuid>7e472a89-a207-4615-af5f-2a10a734943b</uuid><memory unit='KiB'>2097152</memory><vcpu>2</vcpu><os><type arch='x86_64' machine='q35'>hvm</type></os><devices>` +
		`<disk type='volume' device='disk'><driver name='qemu' type='qcow2'/><source pool='images' volume='app-disk-000.qcow2'/><target dev='sda' bus='sata'/></disk>` +
		`<disk type='file' device='disk'><driver name='qemu' type='raw'/><source file='/var/lib/libvirt/images/data.img'/><target dev='vdb' bus='virtio'/></disk>` +
		`<disk type='volume' device='cdrom'><driver name='qemu' type='raw'/><source pool='images' volume='install.iso'/><target dev='sdb' bus='sata'/><readonly/></disk>` +
		`</devices></domain>`
	got, err := growDiskSource(raw, "sda")
	if err != nil || got != (growSource{target: "sda", pool: "images", volume: "app-disk-000.qcow2", format: "qcow2"}) {
		t.Fatal(got, err)
	}
	got, err = growDiskSource(raw, "vdb")
	if err != nil || got != (growSource{target: "vdb", file: "/var/lib/libvirt/images/data.img", format: "raw"}) {
		t.Fatal(got, err)
	}
	for _, target := range []string{"sdb", "vdz", "/dev/sda"} {
		if _, err := growDiskSource(raw, target); err == nil {
			t.Fatal(target, "accepted")
		}
	}
}
