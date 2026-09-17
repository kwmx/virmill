package xmlpatch

import (
	"encoding/xml"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// CloneDisk names the copy one writable disk of the original points at in the
// clone.
type CloneDisk struct{ Target, Pool, Volume string }

// ClonePatch is the reviewed identity and storage of a full clone (ADR 0066).
// FreshNVRAM records that the original has firmware variables, which the clone
// recreates from their template instead of copying.
type ClonePatch struct {
	UUID, Name string
	Disks      []CloneDisk
	FreshNVRAM bool
}

const virmillNamespace = "urn:virmill:v1"

func cloneRefuse(message string) error { return domain.Fail("UNSUPPORTED_CAPABILITY", message) }

// writableDisk reports whether a disk element is one the clone must copy.
func writableDisk(n *positionedNode) bool {
	device, _ := n.attr("device")
	return (device == "" || device == "disk") && len(n.children("readonly")) == 0
}

// CloneDefinition turns the original's saved definition into the clone's. It
// changes only the UUID and name, the source of each writable disk, the MAC
// address of each network adapter, Virmill's creation metadata and, for UEFI,
// the firmware variables path; every other byte is kept. It opens no files.
func CloneDefinition(raw string, change ClonePatch) (string, error) {
	newName, nameErr := validation.DisplayName(change.Name)
	if !restoreValidUUID(change.UUID) || nameErr != nil || len(change.Disks) == 0 || len(change.Disks) > 64 {
		return "", domain.Fail("INVALID_INPUT", "a new UUID, a valid name and at least one disk copy are required")
	}
	root, err := positionedXML(raw)
	if err != nil {
		return "", err
	}
	if err = restoreSafeTree(root); err != nil {
		return "", err
	}
	if err = restoreNamespaces(raw); err != nil {
		return "", err
	}
	uid, err := restoreChild(root, "uuid", true)
	if err != nil {
		return "", err
	}
	name, err := restoreChild(root, "name", true)
	if err != nil {
		return "", err
	}
	oldName, oldErr := validation.DisplayName(name.text())
	if !uid.onlyAttrs() || !name.onlyAttrs() || !restoreScalar(uid) || !restoreScalar(name) || oldErr != nil {
		return "", domain.Fail("INVALID_INPUT", "the original's UUID and name must be plain values")
	}
	if uid.text() == change.UUID || oldName == newName {
		return "", domain.Fail("INVALID_INPUT", "the clone needs a UUID and a name different from the original's")
	}
	spans := []spanReplacement{{uid.startEnd, uid.endStart, change.UUID}, {name.startEnd, name.endStart, restoreEscape(change.Name)}}

	for _, metadata := range root.children("metadata") {
		for _, part := range metadata.parts {
			if c := part.child; c != nil && c.name == (xml.Name{Space: virmillNamespace, Local: "creation"}) {
				spans = append(spans, spanReplacement{c.start, c.end, ""})
			}
		}
	}

	mapping := map[string]CloneDisk{}
	for _, d := range change.Disks {
		if !targetID.MatchString(d.Target) || mapping[d.Target].Target != "" || !diskVolumeName.MatchString(d.Pool) || !diskVolumeName.MatchString(d.Volume) {
			return "", domain.Fail("INVALID_INPUT", "each disk copy needs a unique target and an exact pool and volume")
		}
		mapping[d.Target] = d
	}
	devices, disks, err := diskElements(raw)
	if err != nil {
		return "", err
	}
	copied := 0
	for _, disk := range disks {
		copy, mapped := mapping[disk.target]
		if !writableDisk(disk.node) {
			if mapped {
				return "", cloneRefuse("disk " + disk.target + " is read-only media; the clone shares it instead of copying it")
			}
			continue
		}
		if !mapped {
			return "", cloneRefuse("every writable disk needs a copy; " + disk.target + " has none")
		}
		// The same checks Move applies to the one disk it retargets.
		retargeted, err := RetargetDisk(raw, disk.target, copy.Pool, copy.Volume)
		if err != nil {
			return "", err
		}
		source, _ := onlyChild(disk.node, "source")
		// RetargetDisk replaced exactly this one source element, so its new text
		// sits at the same start, shifted only by the change in length.
		delta := len(retargeted) - len(raw)
		replacement := retargeted[source.start : source.end+delta]
		spans = append(spans, spanReplacement{source.start, source.end, replacement})
		copied++
	}
	if copied != len(mapping) {
		return "", domain.Fail("INVALID_INPUT", "a disk copy names a target this VM does not have")
	}

	for _, part := range devices.parts {
		n := part.child
		if n == nil || n.name.Space != "" {
			continue
		}
		switch n.name.Local {
		case "tpm":
			return "", cloneRefuse("this VM has an emulated TPM, whose state cannot be copied")
		case "interface":
			if kind, _ := n.attr("type"); kind != "network" && kind != "bridge" {
				return "", cloneRefuse("only network and bridge adapters can be cloned")
			}
			for _, mac := range n.children("mac") {
				if !mac.onlyAttrs("address") {
					return "", cloneRefuse("an adapter's MAC address carries settings this editor does not model")
				}
				spans = append(spans, spanReplacement{mac.start, mac.end, ""})
			}
		}
	}

	osNode, err := restoreChild(root, "os", true)
	if err != nil {
		return "", err
	}
	nvram, err := restoreChild(osNode, "nvram", false)
	if err != nil {
		return "", err
	}
	if (nvram != nil) != change.FreshNVRAM {
		return "", domain.Fail("STALE_PLAN", "the firmware variables differ from the review; review again")
	}
	if nvram != nil {
		template, hasTemplate := nvram.attr("template")
		if !hasTemplate || !restorePath(template) {
			return "", cloneRefuse("this VM's firmware variables have no template, so the clone cannot get fresh ones")
		}
		if !nvram.onlyAttrs("type", "format", "template", "templateFormat") {
			return "", cloneRefuse("this VM's firmware variables carry settings this editor does not model")
		}
		for _, part := range nvram.parts {
			if c := part.child; c != nil && (c.name.Space != "" || c.name.Local != "source") {
				return "", cloneRefuse("this VM's firmware variables carry settings this editor does not model")
			}
		}
		replacement := `<nvram template="` + restoreEscape(template) + `"`
		for _, attr := range []string{"templateFormat", "format"} {
			if value, ok := nvram.attr(attr); ok {
				replacement += ` ` + attr + `="` + restoreEscape(value) + `"`
			}
		}
		spans = append(spans, spanReplacement{nvram.start, nvram.end, replacement + `/>`})
	}
	return replaceSpans(raw, spans)
}

// CloneComparable removes what libvirt assigns when it stores a clone: MAC
// addresses and the firmware variables path. The rest of a stored clone must
// match the reviewed definition under HardwareDigest.
func CloneComparable(raw string) (string, error) {
	root, err := positionedXML(raw)
	if err != nil {
		return "", err
	}
	spans := []spanReplacement{}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return "", err
	}
	for _, n := range devices.children("interface") {
		for _, mac := range n.children("mac") {
			spans = append(spans, spanReplacement{mac.start, mac.end, ""})
		}
	}
	if osNode, e := onlyChild(root, "os"); e == nil {
		for _, nvram := range osNode.children("nvram") {
			replacement := `<nvram`
			for _, a := range nvram.attrs {
				if a.Name.Space == "" && (a.Name.Local == "template" || a.Name.Local == "templateFormat" || a.Name.Local == "format") {
					replacement += ` ` + a.Name.Local + `="` + restoreEscape(a.Value) + `"`
				}
			}
			spans = append(spans, spanReplacement{nvram.start, nvram.end, replacement + `/>`})
		}
	}
	return replaceSpans(raw, spans)
}

// MACAddresses lists the MAC addresses a definition's adapters carry.
func MACAddresses(raw string) ([]string, error) {
	root, err := positionedXML(raw)
	if err != nil {
		return nil, err
	}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, n := range devices.children("interface") {
		for _, mac := range n.children("mac") {
			if address, ok := mac.attr("address"); ok {
				out = append(out, address)
			}
		}
	}
	return out, nil
}
