//go:build linux && amd64 && cgo

package libvirt

import (
	"strings"
	"testing"
)

// ADR 0063: disks of the VMs Virmill creates are pool volumes, and deletion
// needs their exact registered path.
func TestRemovalDiskSourcesResolvePoolVolumes(t *testing.T) {
	got, err := removalDiskSources(uefiRemovalXML, []string{"sda"}, newRemovalFixture())
	want := removalDiskSource{"sda", "/var/lib/libvirt/images/created-disk-000.qcow2", "qcow2"}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatal(got, err)
	}
	second := `<disk type="volume" device="disk"><driver name="qemu" type="qcow2"/><source pool="images" volume="created-disk-000.qcow2"/><target dev="sdc" bus="sata"/></disk>`
	shared := strings.Replace(uefiRemovalXML, "<tpm", second+"<tpm", 1)
	if shared == uefiRemovalXML {
		t.Fatal("fixture unchanged")
	}
	if _, err := removalDiskSources(shared, []string{"sda"}, newRemovalFixture()); err == nil {
		t.Fatal("two disks sharing one pool volume accepted")
	}
	if _, err := removalDiskSources(uefiRemovalXML, []string{"sdb"}, newRemovalFixture()); err == nil {
		t.Fatal("a VM without that disk target was accepted")
	}
}
