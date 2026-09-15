//go:build linux && cgo

package libvirt

import (
	"testing"

	"virmill.local/core/internal/backend/xmlpatch"
)

// ADR 0061: a next-boot edit reviewed while the VM runs redefines only the
// saved definition and leaves the running guest's values alone.
func TestNativeSimulatedNextBootEditWhileRunning(t *testing.T) {
	c, d := configNativeFixture(t, "")
	if err := d.Create(); err != nil {
		t.Skipf("in-memory driver cannot start the fixture: %v", err)
	}
	t.Cleanup(func() { _ = d.Destroy() })
	input := configNativeInput(t, d)
	before, err := observe(d, "test:///default")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkConfiguration(c, d, "test:///default", input); err == nil {
		t.Fatal("a stopped-VM edit plan was accepted for a running VM")
	}
	input["editPrecondition"] = "persistent-xml-v1"
	input["editBeforePersistentSHA256"] = xmlpatch.Digest(before.PersistentXML)
	if err := executeConfiguration(c, d, "test:///default", input); err != nil {
		t.Fatal(err)
	}
	after, err := observe(d, "test:///default")
	if err != nil {
		t.Fatal(err)
	}
	live, err := xmlpatch.ReadResourceValues(after.LiveXML)
	if err != nil || after.State != "running" || xmlpatch.Digest(after.PersistentXML) != input["xmlSHA256"] || live.VCPUs == nil || *live.VCPUs != 2 {
		t.Fatal("saved definition not edited, or running values changed", after.State, err)
	}
	if ok, err := observeConfiguration(d, "test:///default", input); err != nil || !ok {
		t.Fatal("readback not verified", ok, err)
	}
	if err := executeConfiguration(c, d, "test:///default", input); err == nil {
		t.Fatal("stale saved definition accepted")
	}
}
