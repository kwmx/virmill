//go:build linux && amd64 && cgo

package libvirt

import "testing"

func TestDefinitionUsesVolumeFindsSourcesAndBackingChains(t *testing.T) {
	const path = "/var/lib/libvirt/images/app.qcow2"
	for _, test := range []struct {
		name, xml string
		used      bool
	}{
		{"file source", `<domain><devices><disk type='file' device='disk'><source file='/var/lib/libvirt/images/app.qcow2'/><target dev='vda'/></disk></devices></domain>`, true},
		{"pool volume", `<domain><devices><disk type='volume' device='disk'><source pool='default' volume='app.qcow2'/><target dev='vda'/></disk></devices></domain>`, true},
		{"backing chain", `<domain><devices><disk type='file' device='disk'><source file='/var/lib/libvirt/images/overlay.qcow2'/><backingStore type='file'><source file='/var/lib/libvirt/images/app.qcow2'/></backingStore><target dev='vda'/></disk></devices></domain>`, true},
		{"other disk", `<domain><devices><disk type='file' device='disk'><source file='/var/lib/libvirt/images/other.qcow2'/><target dev='vda'/></disk></devices></domain>`, false},
		{"same name, other pool", `<domain><devices><disk type='volume' device='disk'><source pool='fast' volume='app.qcow2'/><target dev='vda'/></disk></devices></domain>`, false},
		{"source outside a disk", `<domain><devices><interface type='network'><source network='app.qcow2'/></interface><hostdev><source file='/var/lib/libvirt/images/app.qcow2'/></hostdev></devices></domain>`, false},
	} {
		used, err := definitionUsesVolume(test.xml, path, "default", "app.qcow2")
		if err != nil || used != test.used {
			t.Fatal(test.name, used, err)
		}
	}
	if _, err := definitionUsesVolume("<domain><devices><disk>", path, "default", "app.qcow2"); err == nil {
		t.Fatal("truncated definition accepted")
	}
}

func TestVolumeBackingPath(t *testing.T) {
	for raw, want := range map[string]string{
		`<volume><name>a.qcow2</name></volume>`: "",
		`<volume><name>a.qcow2</name><backingStore><path>/images/base.qcow2</path><format type='qcow2'/></backingStore></volume>`: "/images/base.qcow2",
		`<volume><name>a.qcow2</name><backingStore/></volume>`:                                                                    "",
	} {
		if got, err := volumeBackingPath(raw); err != nil || got != want {
			t.Fatal(raw, got, err)
		}
	}
}
