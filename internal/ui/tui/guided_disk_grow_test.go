package tui

import (
	"testing"

	"virmill.local/core/internal/domain"
)

func TestDiskGrowFormPlansTheChosenDiskAndSize(t *testing.T) {
	vm := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: workspaceVMID}, Name: "app", State: "stopped",
		PersistentXML: `<domain><devices><disk type='file' device='disk'><source file='/var/lib/libvirt/images/app.qcow2'/><target dev='vda'/></disk><disk type='file' device='disk'><source file='/var/lib/libvirt/images/data.qcow2'/><target dev='vdb'/></disk><disk type='file' device='cdrom'><source file='/iso/install.iso'/><target dev='sda'/><readonly/></disk></devices></domain>`}
	f, err := NewGuidedForm("disk-grow", vm)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Fields[0].Choices; len(got) != 2 || got[0] != "vda" || got[1] != "vdb" {
		t.Fatal("expected the two writable disks, not the installer", got)
	}
	f.Fields[0].Value, f.Fields[1].Value = "vdb", "40"
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "vm.plan" || r.Action != "grow-disk" || r.ID != workspaceVMID || r.Input["target"] != "vdb" || r.Input["sizeGiB"] != float64(40) {
		t.Fatal(method, r, err)
	}
	f.Fields[1].Value = "0"
	if _, _, err := f.Request("qemu:///system"); err == nil {
		t.Fatal("zero size accepted")
	}
	vm.State = "running"
	if _, err := NewGuidedForm("disk-grow", vm); err == nil {
		t.Fatal("running VM offered a disk grow")
	}
}
