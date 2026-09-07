//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/xml"
	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
)

func (p *Provider) CheckConfiguration(ctx context.Context, uri, id string, input map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Secure XML is a read, but libvirt refuses that read on a read-only connection.
	// No secret XML is returned to clients, journaled, or passed into an error.
	c, err := connect(uri, true)
	if err != nil {
		return err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		return err
	}
	defer d.Free()
	return checkConfiguration(c, d, uri, input)
}
func checkConfiguration(c *native.Connect, d *native.Domain, uri string, input map[string]any) error {
	if (input["editVersion"] != float64(1) && input["editVersion"] != float64(2)) || input["applyMode"] != "next-boot" {
		return domain.Fail("STALE_PLAN", "fresh preservation-aware next-boot edit preview required")
	}
	v, err := observe(d, uri)
	if err != nil {
		return err
	}
	if v.State != "stopped" || v.PersistentXML == "" || v.HasManagedSave {
		return domain.Fail("STALE_PLAN", "configuration editing requires a stopped persistent VM without managed-save state")
	}
	if v.Fingerprint != input["editBeforeFingerprint"] {
		return domain.Fail("STALE_PLAN", "domain changed immediately before configuration effect")
	}
	edit, _, err := configurationXML(v.PersistentXML, input)
	if err != nil {
		return err
	}
	if edit.VCPUs != nil {
		var root struct {
			Type string `xml:"type,attr"`
		}
		if err = xml.Unmarshal([]byte(v.PersistentXML), &root); err != nil {
			return domain.Fail("INVALID_INPUT", "invalid domain type")
		}
		max, err := c.GetMaxVcpus(root.Type)
		if err != nil {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "backend maximum-vCPU probe failed")
		}
		if max <= 0 || *edit.VCPUs > uint64(max) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "requested vCPU count exceeds the observed backend limit")
		}
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("PERMISSION_DENIED", "secure configuration read is required to check preservation; no edit attempted")
	}
	if err = checkSecureConfiguration(v.PersistentXML, secure); err != nil {
		return err
	}
	// Repeat the public observation after the secure read. Libvirt has no atomic
	// compare-and-swap for DefineXML; external-writer coordination remains required.
	latest, err := observe(d, uri)
	if err != nil {
		return err
	}
	if latest.Fingerprint != v.Fingerprint {
		return domain.Fail("STALE_PLAN", "domain changed during configuration preservation checks")
	}
	return nil
}
func checkSecureConfiguration(public, secure string) error {
	if public != secure {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "persistent XML omits sensitive settings; a dedicated secret-preserving adapter is required before editing")
	}
	return nil
}
func executeConfiguration(c *native.Connect, d *native.Domain, uri string, input map[string]any) error {
	if err := checkConfiguration(c, d, uri, input); err != nil {
		return err
	}
	before, err := observe(d, uri)
	if err != nil {
		return err
	}
	if before.Fingerprint != input["editBeforeFingerprint"] {
		return domain.Fail("STALE_PLAN", "domain changed before definition")
	}
	_, expected, err := configurationXML(before.PersistentXML, input)
	if err != nil {
		return err
	}
	defined, err := c.DomainDefineXMLFlags(expected, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		// A native validation error can quote opaque XML values. The operation
		// retains uncertainty without putting those values in its event journal.
		return domain.Fail("RECOVERY_REQUIRED", "native configuration definition failed; XML values withheld; inspect and reconcile without replay")
	}
	defer defined.Free()
	// A successful define acknowledgement is insufficient: retain uncertainty if
	// backend normalization or another writer changes anything beyond this edit.
	v, err := observe(defined, uri)
	if err != nil {
		return err
	}
	match, err := configurationMatches(v.PersistentXML, input)
	if err != nil {
		return err
	}
	if v.State != "stopped" || v.HasManagedSave || !match {
		return domain.Fail("RECOVERY_REQUIRED", "persistent configuration readback differs; inspect the operation without replaying it")
	}
	secure, err := defined.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "secure configuration readback unavailable; do not replay the edit")
	}
	if secure != v.PersistentXML {
		return domain.Fail("RECOVERY_REQUIRED", "sensitive configuration readback differs; do not replay the edit")
	}
	return nil
}

func (p *Provider) ObserveConfiguration(ctx context.Context, uri, id string, input map[string]any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c, err := connect(uri, true)
	if err != nil {
		return false, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		return false, err
	}
	defer d.Free()
	return observeConfiguration(d, uri, input)
}
func observeConfiguration(d *native.Domain, uri string, input map[string]any) (bool, error) {
	expected, ok := input["xmlSHA256"].(string)
	if !ok || len(expected) != 64 || (input["editVersion"] != float64(1) && input["editVersion"] != float64(2)) {
		return false, domain.Fail("RECOVERY_REQUIRED", "legacy configuration uncertainty requires explicit disposition; do not replay")
	}
	v, err := observe(d, uri)
	if err != nil {
		return false, err
	}
	match, err := configurationMatches(v.PersistentXML, input)
	if err != nil {
		return false, err
	}
	if v.State != "stopped" || v.HasManagedSave || !match {
		return false, nil
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return false, domain.Fail("RECOVERY_REQUIRED", "secure readback unavailable; no configuration recovery claim")
	}
	if secure != v.PersistentXML {
		return false, domain.Fail("RECOVERY_REQUIRED", "sensitive configuration differs; no configuration recovery claim")
	}
	return true, nil
}

func configurationXML(data string, input map[string]any) (xmlpatch.ResourceEdit, string, error) {
	if input["editVersion"] == float64(2) {
		edit, err := xmlpatch.ParseHardwareInput(input)
		if err != nil {
			return edit.Resources, "", err
		}
		expected, err := xmlpatch.EditHardware(data, edit)
		if err != nil {
			return edit.Resources, "", err
		}
		match, err := configurationMatches(expected, input)
		if err != nil {
			return edit.Resources, "", err
		}
		if !match {
			return edit.Resources, "", domain.Fail("STALE_PLAN", "hardware edit differs from the reviewed semantic fingerprint")
		}
		return edit.Resources, expected, nil
	}
	var edit xmlpatch.ResourceEdit
	for _, name := range []string{"vcpus", "memoryMiB"} {
		if value, exists := input[name]; exists {
			n, ok := value.(float64)
			if !ok || n < 1 || n > 1048576 || n != float64(uint64(n)) {
				return edit, "", domain.Fail("INVALID_INPUT", "bounded integer resource value required")
			}
			integer := uint64(n)
			if name == "vcpus" {
				edit.VCPUs = &integer
			} else {
				edit.MemoryMiB = &integer
			}
		}
	}
	expected, err := xmlpatch.EditResources(data, edit)
	if err != nil {
		return edit, "", err
	}
	if xmlpatch.Digest(expected) != input["xmlSHA256"] {
		return edit, "", domain.Fail("STALE_PLAN", "resource edit does not preserve the observed domain XML")
	}
	return edit, expected, nil
}

func configurationMatches(data string, input map[string]any) (bool, error) {
	if input["editVersion"] == float64(2) {
		digest, err := xmlpatch.HardwareDigest(data)
		if err != nil {
			return false, err
		}
		return digest == input["xmlSHA256"], nil
	}
	return xmlpatch.Digest(data) == input["xmlSHA256"], nil
}
