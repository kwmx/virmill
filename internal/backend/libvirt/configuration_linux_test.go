//go:build linux && cgo

package libvirt

import (
	native "libvirt.org/go/libvirt"
	"strings"
	"testing"
	"virmill.local/core/internal/backend/xmlpatch"
)

func configNativeFixture(t *testing.T, graphics string) (*native.Connect, *native.Domain) {
	t.Helper()
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Skipf("native in-memory test driver unavailable: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	x := `<domain type='test'><name>virmill-resource-fixture</name><uuid>30f4fc6d-ed33-411f-bb40-ff8b2128d660</uuid><metadata><fixture:policy xmlns:fixture='urn:virmill:fixture' retain='yes'>opaque-policy</fixture:policy></metadata><memory unit='KiB' dumpCore='off'>262144</memory><currentMemory unit='KiB'>262144</currentMemory><vcpu placement='static'>2</vcpu><os><type arch='i686'>hvm</type></os><devices><disk type='file' device='disk'><source file='/never-opened/generated.qcow2'/><target dev='vda' bus='virtio'/></disk>` + graphics + `</devices></domain>`
	d, err := c.DomainDefineXMLFlags(x, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Free() })
	return c, d
}
func configNativeInput(t *testing.T, d *native.Domain) map[string]any {
	t.Helper()
	before, err := observe(d, "test:///default")
	if err != nil {
		t.Fatal(err)
	}
	cpu, ram := uint64(4), uint64(512)
	x, err := xmlpatch.EditResources(before.PersistentXML, xmlpatch.ResourceEdit{VCPUs: &cpu, MemoryMiB: &ram})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"vcpus": float64(cpu), "memoryMiB": float64(ram), "applyMode": "next-boot", "editVersion": float64(1), "editBeforeFingerprint": before.Fingerprint, "xmlSHA256": xmlpatch.Digest(x)}
}
func TestNativeSimulatedConfigurationPreservesOpaqueXMLAndReadback(t *testing.T) {
	c, d := configNativeFixture(t, "")
	input := configNativeInput(t, d)
	if err := checkConfiguration(c, d, "test:///default", input); err != nil {
		t.Fatal(err)
	}
	if err := executeConfiguration(c, d, "test:///default", input); err != nil {
		t.Fatal(err)
	}
	after, err := observe(d, "test:///default")
	if err != nil {
		t.Fatal(err)
	}
	if xmlpatch.Digest(after.PersistentXML) != input["xmlSHA256"] || !strings.Contains(after.PersistentXML, "opaque-policy") || !strings.Contains(after.PersistentXML, "/never-opened/generated.qcow2") || after.State != "stopped" {
		t.Fatal("unrelated XML changed or simulation booted", after)
	}
	ok, err := observeConfiguration(d, "test:///default", input)
	if err != nil || !ok {
		t.Fatal("readback not verified", ok, err)
	}
	if err := executeConfiguration(c, d, "test:///default", input); err == nil {
		t.Fatal("stale fingerprint accepted")
	}
	t.Log("official libvirt in-memory driver: fixed CPU/RAM redefine, opaque metadata preservation and secure readback only; no host domain or guest execution")
}
func TestNativeSimulatedSecureDifferenceRefusesBeforeRedefinition(t *testing.T) {
	c, d := configNativeFixture(t, `<graphics type='vnc' port='-1' autoport='yes' passwd='fixture-secret-not-real'/>`)
	input := configNativeInput(t, d)
	before, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		t.Fatal(err)
	}
	err = executeConfiguration(c, d, "test:///default", input)
	if err == nil || strings.Contains(err.Error(), "fixture-secret-not-real") {
		t.Fatal("sensitive configuration not refused safely", err)
	}
	after, e := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if e != nil || after != before {
		t.Fatal("secret-refusal mutated configuration", e)
	}
	public, e := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
	if e != nil {
		t.Fatal(e)
	}
	input["xmlSHA256"] = xmlpatch.Digest(public)
	if ok, e := observeConfiguration(d, "test:///default", input); e == nil || ok || strings.Contains(e.Error(), "fixture-secret-not-real") {
		t.Fatal("public XML alone certified sensitive configuration", ok, e)
	}
	t.Log("in-memory driver redacts a synthetic display password; preservation preflight refuses without echoing it or redefining the domain")
}
func TestSecureConfigurationErrorNeverContainsValues(t *testing.T) {
	err := checkSecureConfiguration("public", "private-fixture-sentinel")
	if err == nil || strings.Contains(err.Error(), "private-fixture-sentinel") {
		t.Fatal(err)
	}
}
