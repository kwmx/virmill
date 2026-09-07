package xmlpatch

import (
	"bytes"
	"encoding/json"
	"net"
	"regexp"
	"strconv"
	"strings"
	"virmill.local/core/internal/domain"
)

type BootChoice struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type BootDevice struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Device       string `json:"device"`
	SourceType   string `json:"sourceType,omitempty"`
	Bus          string `json:"bus,omitempty"`
	Order        int    `json:"order"`
	MediaPresent bool   `json:"mediaPresent"`
	ReadOnly     bool   `json:"readOnly"`
	Link         string `json:"link,omitempty"`
	Selectable   bool   `json:"selectable"`
	Reason       string `json:"reason,omitempty"`
}
type BootView struct {
	Mode         string       `json:"mode"`
	LegacyOrder  []string     `json:"legacyOrder"`
	Devices      []BootDevice `json:"devices"`
	BootVerified bool         `json:"bootVerified"`
}
type bootModel struct {
	root, os, devices *positionedNode
	nodes             map[string]*positionedNode
	view              BootView
}

var targetID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

func bootKey(kind, id string) string { return kind + ":" + id }
func mediaPresent(n *positionedNode) bool {
	sources := n.children("source")
	if len(sources) != 1 {
		return false
	}
	source := sources[0]
	kind, _ := n.attr("type")
	switch kind {
	case "file":
		v, _ := source.attr("file")
		return v != ""
	case "block":
		v, _ := source.attr("dev")
		return v != ""
	case "volume":
		p, _ := source.attr("pool")
		v, _ := source.attr("volume")
		return p != "" && v != ""
	default:
		return len(source.attrs) > 0 || len(source.parts) > 0
	}
}
func modelBoot(data string) (*bootModel, error) {
	root, err := positionedXML(data)
	if err != nil {
		return nil, err
	}
	os, err := onlyChild(root, "os")
	if err != nil {
		return nil, err
	}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return nil, err
	}
	m := &bootModel{root: root, os: os, devices: devices, nodes: map[string]*positionedNode{}, view: BootView{Mode: "firmware-default", LegacyOrder: []string{}, Devices: []BootDevice{}}}
	if len(root.children("bootloader")) > 0 || len(os.children("kernel")) > 0 || len(os.children("init")) > 0 {
		m.view.Mode = "direct-boot"
	}
	for _, n := range os.children("boot") {
		dev, _ := n.attr("dev")
		m.view.LegacyOrder = append(m.view.LegacyOrder, dev)
	}
	orders := map[int]bool{}
	for _, part := range devices.parts {
		n := part.child
		if n == nil || n.name.Space != "" {
			continue
		}
		if n.name.Local != "disk" && n.name.Local != "interface" {
			if len(n.children("boot")) > 0 {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "boot order on another device class requires a dedicated adapter")
			}
			continue
		}
		if len(m.view.Devices) >= 256 {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "boot inventory exceeds the 256-device bound")
		}
		v := BootDevice{Kind: n.name.Local, Selectable: true, Link: ""}
		if n.name.Local == "disk" {
			target, e := onlyChild(n, "target")
			if e != nil {
				return nil, e
			}
			v.ID, _ = target.attr("dev")
			v.Bus, _ = target.attr("bus")
			v.Device, _ = n.attr("device")
			v.SourceType, _ = n.attr("type")
			v.MediaPresent = mediaPresent(n)
			v.ReadOnly = len(n.children("readonly")) == 1
			if !targetID.MatchString(v.ID) {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "disk lacks a stable supported target name")
			}
			if v.Device != "disk" && v.Device != "cdrom" && v.Device != "floppy" && v.Device != "lun" {
				v.Selectable = false
				v.Reason = "unsupported disk device class"
			}
			if !v.MediaPresent {
				v.Selectable = false
				v.Reason = "no media source is present"
			}
		} else {
			mac, e := onlyChild(n, "mac")
			if e != nil {
				return nil, e
			}
			value, _ := mac.attr("address")
			parsed, e := net.ParseMAC(value)
			if e != nil || len(parsed) != 6 || parsed[0]&1 != 0 {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "interface needs an unambiguous unicast MAC")
			}
			v.ID = parsed.String()
			v.Device = "network"
			v.MediaPresent = true
			links := n.children("link")
			if len(links) > 1 {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "ambiguous interface link configuration")
			}
			v.Link = "unspecified"
			if len(links) == 1 {
				v.Link, _ = links[0].attr("state")
			}
			if v.Link == "down" {
				v.Selectable = false
				v.Reason = "interface link is explicitly down"
			}
		}
		key := bootKey(v.Kind, v.ID)
		if m.nodes[key] != nil {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "duplicate boot device identity")
		}
		m.nodes[key] = n
		boots := n.children("boot")
		if len(boots) > 1 {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "ambiguous per-device boot order")
		}
		if len(boots) == 1 {
			value, _ := boots[0].attr("order")
			order, e := strconv.Atoi(value)
			if e != nil || order < 1 || order > 65535 || orders[order] {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "invalid or duplicate per-device boot order")
			}
			orders[order] = true
			v.Order = order
		}
		m.view.Devices = append(m.view.Devices, v)
	}
	if m.view.Mode != "direct-boot" {
		if len(orders) > 0 {
			m.view.Mode = "per-device"
		}
		if len(m.view.LegacyOrder) > 0 {
			if len(orders) > 0 {
				m.view.Mode = "conflicting"
			} else {
				m.view.Mode = "legacy-firmware"
			}
		}
	}
	return m, nil
}
func InspectBoot(data string) (BootView, error) {
	m, err := modelBoot(data)
	if err != nil {
		return BootView{}, err
	}
	return m.view, nil
}
func simpleBoot(n *positionedNode, attr string) bool {
	if !n.onlyAttrs(attr) {
		return false
	}
	for _, p := range n.parts {
		if p.child != nil || p.kind != "text" || strings.TrimSpace(p.text) != "" {
			return false
		}
	}
	return true
}
func EditBootOrder(data string, choices []BootChoice) (string, error) {
	if len(choices) < 1 || len(choices) > 128 {
		return "", domain.Fail("INVALID_INPUT", "explicit boot order needs 1 through 128 selected devices")
	}
	m, err := modelBoot(data)
	if err != nil {
		return "", err
	}
	if m.view.Mode == "direct-boot" || m.view.Mode == "conflicting" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "direct or conflicting boot methods require a dedicated adapter")
	}
	osType, err := onlyChild(m.os, "type")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(osType.text()) != "hvm" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "per-device firmware boot requires an hvm domain")
	}
	chosen := map[string]int{}
	observed := map[string]BootDevice{}
	for _, v := range m.view.Devices {
		observed[bootKey(v.Kind, v.ID)] = v
	}
	for i, c := range choices {
		key := bootKey(c.Kind, c.ID)
		v, ok := observed[key]
		if !ok || chosen[key] != 0 {
			return "", domain.Fail("INVALID_INPUT", "boot order must select unique observed disk targets or interface MACs")
		}
		if !v.Selectable {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "selected boot device is unavailable: "+v.Reason)
		}
		chosen[key] = i + 1
	}
	spans := []spanReplacement{}
	for _, old := range m.os.children("boot") {
		value, _ := old.attr("dev")
		if !simpleBoot(old, "dev") || (value != "hd" && value != "cdrom" && value != "network" && value != "fd") {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "legacy boot element has unsupported policy")
		}
		spans = append(spans, spanReplacement{old.start, old.end, ""})
	}
	for key, node := range m.nodes {
		old := node.children("boot")
		if len(old) == 1 && !simpleBoot(old[0], "order") {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "per-device boot parameters require a dedicated adapter")
		}
		order := chosen[key]
		value := ""
		if order > 0 {
			value = `<boot order="` + strconv.Itoa(order) + `"/>`
		}
		if len(old) == 1 {
			spans = append(spans, spanReplacement{old[0].start, old[0].end, value})
		} else if order > 0 {
			if node.empty {
				return "", domain.Fail("UNSUPPORTED_CAPABILITY", "empty device cannot receive a boot order")
			}
			spans = append(spans, spanReplacement{node.endStart, node.endStart, value})
		}
	}
	return replaceSpans(data, spans)
}
func EjectMedia(data, target string, willReplaceBootOrder bool) (string, error) {
	if !targetID.MatchString(target) {
		return "", domain.Fail("INVALID_INPUT", "stable CD-ROM target name required")
	}
	m, err := modelBoot(data)
	if err != nil {
		return "", err
	}
	node := m.nodes[bootKey("disk", target)]
	if node == nil {
		return "", domain.Fail("INVALID_INPUT", "selected CD-ROM target is absent")
	}
	device, _ := node.attr("device")
	if device != "cdrom" || len(node.children("readonly")) != 1 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "media ejection requires an explicitly read-only CD-ROM")
	}
	if !mediaPresent(node) {
		return "", domain.Fail("INVALID_INPUT", "selected CD-ROM is already empty")
	}
	if !willReplaceBootOrder {
		if len(node.children("boot")) > 0 {
			return "", domain.Fail("INVALID_INPUT", "ejecting a boot candidate requires an explicit replacement bootOrder")
		}
		for _, dev := range m.view.LegacyOrder {
			if dev == "cdrom" {
				return "", domain.Fail("INVALID_INPUT", "legacy CD-ROM boot policy requires an explicit replacement bootOrder")
			}
		}
	}
	if len(node.children("auth")) > 0 || len(node.children("encryption")) > 0 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "media has authentication/encryption policy requiring a dedicated adapter")
	}
	for _, backing := range node.children("backingStore") {
		if len(backing.attrs) > 0 || len(backing.parts) > 0 {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "CD-ROM has a nonempty backing graph")
		}
	}
	source, err := onlyChild(node, "source")
	if err != nil {
		return "", err
	}
	kind, _ := node.attr("type")
	allowed := false
	switch kind {
	case "file":
		allowed = source.onlyAttrs("file", "startupPolicy", "index")
	case "block":
		allowed = source.onlyAttrs("dev", "startupPolicy", "index")
	case "volume":
		allowed = source.onlyAttrs("pool", "volume", "mode", "startupPolicy", "index")
	}
	if !allowed {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "CD-ROM source type or attributes require a dedicated ejection adapter")
	}
	for _, part := range source.parts {
		if part.child != nil || part.kind != "text" || strings.TrimSpace(part.text) != "" {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "structured CD-ROM source policy requires a dedicated adapter")
		}
	}
	spans := []spanReplacement{{source.start, source.end, ""}}
	// Libvirt normalizes empty block/volume CD-ROMs to type=file. Make that
	// explicit in the reviewed edit; never hide it in the fingerprint comparer.
	if kind != "file" {
		changed, err := attributeSpan(data, node, "type", "file")
		if err != nil {
			return "", err
		}
		spans = append(spans, changed)
	}
	return replaceSpans(data, spans)
}

// HardwareEdit can combine CPU/RAM, complete boot selection and one media ejection
// into one reviewed definition. Ejection never removes a backing file or volume.
type HardwareEdit struct {
	Resources  ResourceEdit
	BootOrder  []BootChoice
	EjectMedia string
}

func EditHardware(data string, edit HardwareEdit) (string, error) {
	var err error
	if edit.Resources.VCPUs != nil || edit.Resources.MemoryMiB != nil {
		data, err = EditResources(data, edit.Resources)
		if err != nil {
			return "", err
		}
	}
	if edit.EjectMedia != "" {
		data, err = EjectMedia(data, edit.EjectMedia, edit.BootOrder != nil)
		if err != nil {
			return "", err
		}
	}
	if edit.BootOrder != nil {
		data, err = EditBootOrder(data, edit.BootOrder)
		if err != nil {
			return "", err
		}
	}
	if edit.Resources.VCPUs == nil && edit.Resources.MemoryMiB == nil && edit.EjectMedia == "" && edit.BootOrder == nil {
		return "", domain.Fail("INVALID_INPUT", "no hardware edit requested")
	}
	return data, nil
}

func ParseHardwareInput(input map[string]any) (HardwareEdit, error) {
	var edit HardwareEdit
	if input["applyMode"] != "next-boot" {
		return edit, domain.Fail("UNSUPPORTED_CAPABILITY", "hardware edit requires next-boot intent; live modes need dedicated adapters")
	}
	for _, name := range []string{"vcpus", "memoryMiB"} {
		if value, ok := input[name]; ok {
			n, valid := value.(float64)
			if !valid || n < 1 || n > 1048576 || n != float64(uint64(n)) {
				return edit, domain.Fail("INVALID_INPUT", "bounded positive integer resource value required")
			}
			integer := uint64(n)
			if name == "vcpus" {
				if integer > 512 {
					return edit, domain.Fail("INVALID_INPUT", "vCPU adapter limit exceeded")
				}
				edit.Resources.VCPUs = &integer
			} else {
				edit.Resources.MemoryMiB = &integer
			}
		}
	}
	if value, ok := input["bootOrder"]; ok {
		data, err := json.Marshal(value)
		if err != nil || len(data) > 65536 {
			return edit, domain.Fail("INVALID_INPUT", "bounded boot order required")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&edit.BootOrder); err != nil || len(edit.BootOrder) < 1 || len(edit.BootOrder) > 128 {
			return edit, domain.Fail("INVALID_INPUT", "bootOrder requires 1 through 128 typed device selectors")
		}
		for _, choice := range edit.BootOrder {
			if (choice.Kind != "disk" && choice.Kind != "interface") || len(choice.ID) < 1 || len(choice.ID) > 64 {
				return edit, domain.Fail("INVALID_INPUT", "boot selector needs a disk target or interface MAC")
			}
		}
	}
	if value, ok := input["ejectMedia"]; ok {
		var valid bool
		edit.EjectMedia, valid = value.(string)
		if !valid || !targetID.MatchString(edit.EjectMedia) {
			return edit, domain.Fail("INVALID_INPUT", "ejectMedia requires a stable CD-ROM target")
		}
	}
	if edit.Resources.VCPUs == nil && edit.Resources.MemoryMiB == nil && edit.BootOrder == nil && edit.EjectMedia == "" {
		return edit, domain.Fail("INVALID_INPUT", "no hardware edit requested")
	}
	return edit, nil
}
