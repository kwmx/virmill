package tui

import (
	"testing"

	"virmill.local/core/internal/domain"
)

// ADR 0062: the form asks only for a size and how the disk attaches.
func TestDiskAddFormPlansASizeAndOptionalBus(t *testing.T) {
	vm := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: workspaceVMID},
		Name: "app", State: "stopped", PersistentXML: `<domain><devices><disk type='volume' device='disk'><source pool='images' volume='a.qcow2'/><target dev='sda' bus='sata'/></disk></devices></domain>`}
	f, err := NewGuidedForm("disk-add", vm)
	if err != nil {
		t.Fatal(err)
	}
	f.Fields[0].Value = "20"
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "vm.disk.add" || r.ID != workspaceVMID || r.Input["sizeGiB"] != float64(20) {
		t.Fatal(method, r, err)
	}
	// Automatic follows the VM's own disks, so no bus is requested.
	if _, present := r.Input["bus"]; present {
		t.Fatal(r.Input)
	}
	f.Fields[1].Value = "virtio"
	if _, r, err = f.Request("qemu:///system"); err != nil || r.Input["bus"] != "virtio" {
		t.Fatal(r.Input, err)
	}
	for _, bad := range [][2]string{{"0", "automatic"}, {"", "automatic"}, {"20", "ide"}} {
		f.Fields[0].Value, f.Fields[1].Value = bad[0], bad[1]
		if _, _, err = f.Request("qemu:///system"); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	vm.State = "running"
	if _, err = NewGuidedForm("disk-add", vm); err == nil {
		t.Fatal("running VM offered a new disk")
	}
}
