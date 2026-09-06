//go:build linux && cgo

package libvirt

import (
	native "libvirt.org/go/libvirt"
	"testing"
)

func TestOfficialBindingWithSimulatedLibvirtDriver(t *testing.T) {
	c, e := native.NewConnect("test:///default")
	if e != nil {
		t.Skipf("libvirt test driver absent: %v", e)
	}
	defer c.Close()
	domains, e := c.ListAllDomains(0)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		for i := range domains {
			domains[i].Free()
		}
	}()
	if len(domains) == 0 {
		t.Fatal("empty simulation fixture")
	}
	v, e := observe(&domains[0], "test:///default")
	if e != nil {
		t.Fatal(e)
	}
	if v.Key.UUID == "" || v.Ownership != "external" || v.Fingerprint == "" {
		t.Fatal(v)
	}
	if Connection("test:///default") == nil || Connection("qemu+ssh://host/system") == nil {
		t.Fatal("release provider admitted simulated/remote URI")
	}
	t.Log("official native binding exercised against libvirt's simulated test driver; this is not KVM/guest evidence")
}
