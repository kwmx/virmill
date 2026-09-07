//go:build linux && cgo

package libvirt

import (
	native "libvirt.org/go/libvirt"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func TestExplicitDevicePolicyNativeSimulatedRoundTrip(t *testing.T) {
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Skipf("native simulated driver unavailable: %v", err)
	}
	defer c.Close()
	for _, machine := range []string{"pc-q35-10.2", "pc-i440fx-10.2"} {
		for _, usb := range []string{"none", "qemu-xhci"} {
			for _, balloon := range []string{"none", "virtio"} {
				t.Run(machine+"/"+usb+"/"+balloon, func(t *testing.T) {
					target, volumes := creationFixture()
					target.Spec.Machine = machine
					p, err := domain.DefaultCreationDevices(machine)
					if err != nil {
						t.Fatal(err)
					}
					p.USBController, p.MemoryBalloon = usb, balloon
					target.Spec.DevicePolicy = p
					wanted, err := creationXML(target, volumes, strings.Repeat("a", 64))
					if err != nil {
						t.Fatal(err)
					}
					d, err := c.DomainDefineXMLFlags(wanted, native.DOMAIN_DEFINE_VALIDATE)
					if err != nil {
						t.Fatal("native simulated parser rejected explicit policy", err)
					}
					defer d.Free()
					got, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
					if err != nil {
						t.Fatal(err)
					}
					if err = matchesCreationPolicy(wanted, got, p); err != nil {
						t.Fatal(err, got)
					}
					if err = d.Undefine(); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
	t.Log("native in-memory parser/validator only; no QEMU machine, guest, physical controller or firmware execution")
}
