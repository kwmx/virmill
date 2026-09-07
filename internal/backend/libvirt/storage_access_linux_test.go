//go:build linux && amd64 && cgo

package libvirt

import (
	"strings"
	"testing"
)

func TestManagedDiskSelectionRequiresExactUnambiguousTarget(t *testing.T) {
	disk := `<disk type='file' device='disk'><source file='/fixture/root.qcow2'/><target dev='vda' bus='virtio'/></disk>`
	wrap := func(s string) string { return "<domain><devices>" + s + "</devices></domain>" }
	got, err := managedDiskSource(wrap(disk), "vda")
	if err != nil || attr(child(got, "source"), "file") != "/fixture/root.qcow2" {
		t.Fatal(got, err)
	}
	for _, xml := range []string{wrap(disk + disk), wrap(strings.Replace(disk, "</disk>", "<target dev='vdb'/></disk>", 1)), wrap(strings.Replace(disk, "</disk>", "<source file='/other'/></disk>", 1)), wrap(strings.Replace(disk, "device='disk'", "device='lun'", 1)), wrap(strings.Replace(disk, "device='disk'", "device='cdrom'", 1)), "<domain><devices/>" + "<devices>" + disk + "</devices></domain>", "<domain xmlns='urn:foreign'><devices>" + disk + "</devices></domain>", wrap(strings.ReplaceAll(disk, "target ", "foreign:target xmlns:foreign='urn:foreign' "))} {
		if _, err = managedDiskSource(xml, "vda"); err == nil {
			t.Fatal("ambiguous/unsupported disk accepted", xml)
		}
	}
	if _, err = managedDiskSource(wrap(disk), "vdb"); err == nil {
		t.Fatal("missing target accepted")
	}
	medium := strings.Replace(disk, "device='disk'", "device='cdrom'", 1)
	medium = strings.Replace(medium, "</disk>", "<readonly/></disk>", 1)
	if _, err = managedDiskSource(wrap(medium), "vda"); err != nil {
		t.Fatal("read-only medium refused", err)
	}
}
