package tui

import "testing"

// ADR 0063: the removal form can select the pool-volume disks of the VMs
// Virmill creates, and never their installer media.
func TestRemovalFormListsPoolVolumeDisks(t *testing.T) {
	raw := `<domain><devices>` +
		`<disk type='volume' device='disk'><source pool='images' volume='app-disk-000.qcow2'/><target dev='sda'/></disk>` +
		`<disk type='volume' device='cdrom'><source pool='images' volume='seed.iso'/><target dev='sdb'/><readonly/></disk>` +
		`<disk type='file' device='disk'><source file='/var/lib/libvirt/images/data.img'/><target dev='vdb'/></disk>` +
		`</devices></domain>`
	got := removalDiskFields(raw)
	if len(got) != 2 || got[0].Name != "delete:sda" || got[0].Hint != "images/app-disk-000.qcow2" || got[1].Name != "delete:vdb" || got[1].Hint != "/var/lib/libvirt/images/data.img" {
		t.Fatalf("%#v", got)
	}
	// Two disks on one volume are ambiguous and stay unselectable.
	shared := `<domain><devices>` +
		`<disk type='volume' device='disk'><source pool='images' volume='app.qcow2'/><target dev='sda'/></disk>` +
		`<disk type='volume' device='disk'><source pool='images' volume='app.qcow2'/><target dev='sdc'/></disk>` +
		`</devices></domain>`
	if fields := removalDiskFields(shared); len(fields) != 0 {
		t.Fatalf("%#v", fields)
	}
}
