//go:build linux && cgo

package libvirt

import (
	"fmt"
	native "libvirt.org/go/libvirt"
	"strings"
	"testing"
	"virmill.local/core/internal/backend/xmlpatch"
)

func nativeBootFixture(t *testing.T, kind, source string) (*native.Connect, *native.Domain, string) {
	t.Helper()
	c, d := configNativeFixture(t, "")
	initial, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
	if err != nil {
		t.Fatal(err)
	}
	added := strings.Replace(initial, "</devices>", fmt.Sprintf(`<disk type='%s' device='cdrom'><driver name='qemu' type='raw'/><source %s/><target dev='sda' bus='sata'/><readonly/></disk></devices>`, kind, source), 1)
	added, err = xmlpatch.EditBootOrder(added, []xmlpatch.BootChoice{{Kind: "disk", ID: "sda"}, {Kind: "disk", ID: "vda"}})
	if err != nil {
		t.Fatal(err)
	}
	defined, err := c.DomainDefineXMLFlags(added, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		t.Fatal(err)
	}
	defined.Free()
	before, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
	if err != nil {
		t.Fatal(err)
	}
	return c, d, before
}
func TestNativeSimulatedBootAndMediaNormalization(t *testing.T) {
	for _, test := range []struct{ kind, source string }{{"file", `file='/never-opened/installer.iso'`}, {"block", `dev='/never-opened/optical-device'`}, {"volume", `pool='default-pool' volume='fixture.iso'`}} {
		t.Run(test.kind, func(t *testing.T) {
			c, d, before := nativeBootFixture(t, test.kind, test.source)
			expected, err := xmlpatch.EditHardware(before, xmlpatch.HardwareEdit{EjectMedia: "sda", BootOrder: []xmlpatch.BootChoice{{Kind: "disk", ID: "vda"}}})
			if err != nil {
				t.Fatal(err)
			}
			observed, err := observe(d, "test:///default")
			if err != nil {
				t.Fatal(err)
			}
			digest, err := xmlpatch.HardwareDigest(expected)
			if err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"editVersion": float64(2), "applyMode": "next-boot", "editBeforeFingerprint": observed.Fingerprint, "xmlSHA256": digest, "ejectMedia": "sda", "bootOrder": []xmlpatch.BootChoice{{Kind: "disk", ID: "vda"}}}
			if err = executeConfiguration(c, d, "test:///default", input); err != nil {
				t.Fatal(err)
			}
			if ok, err := observeConfiguration(d, "test:///default", input); err != nil || !ok {
				t.Fatal("hardware readback failed", ok, err)
			}
			after, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
			if err != nil {
				t.Fatal(err)
			}
			expectedHash, err := xmlpatch.HardwareDigest(expected)
			if err != nil {
				t.Fatal(err)
			}
			afterHash, err := xmlpatch.HardwareDigest(after)
			if err != nil {
				t.Fatal(err)
			}
			if expectedHash != afterHash {
				t.Fatalf("unrecognized normalization\nEXPECTED %s\nOBSERVED %s", expected, after)
			}
			view, err := xmlpatch.InspectBoot(after)
			if err != nil {
				t.Fatal(err)
			}
			if view.Devices[0].Order != 1 || view.Devices[1].MediaPresent || view.Devices[1].Order != 0 || !view.Devices[1].ReadOnly {
				t.Fatal(view)
			}
			t.Log("official native in-memory driver: boot reorder and empty read-only CD-ROM definition only; media paths never opened and no guest executed")
		})
	}
}
