package xmlpatch

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"virmill.local/core/internal/domain"
)

const GuestAgentTarget = "org.qemu.guest_agent.0"

// GuestAgentChannelView describes configuration only, never guest installation
// or responsiveness. The caller must separately require stopped persistent state,
// native support, and approval of this high-trust host/guest interface.
type GuestAgentChannelView struct {
	Present         bool   `json:"present"`
	CanEnable       bool   `json:"canEnable"`
	AddsController  bool   `json:"addsController"`
	ControllerIndex uint   `json:"controllerIndex"`
	Port            uint   `json:"port"`
	Reason          string `json:"reason"`
}

type guestAgentModel struct {
	devices *positionedNode
	view    GuestAgentChannelView
}

type guestAgentController struct{ ports uint }

func guestAgentRefuse(reason string) error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", "Guest-agent channel: "+reason)
}
func guestAgentValue(n *positionedNode, key string) string { v, _ := n.attr(key); return v }
func guestAgentEmpty(n *positionedNode) bool {
	for _, p := range n.parts {
		if p.child != nil || p.kind != "text" || strings.TrimSpace(p.text) != "" {
			return false
		}
	}
	return true
}
func guestAgentNumber(value string, maximum uint64) (uint, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || strconv.FormatUint(n, 10) != value || n > maximum {
		return 0, guestAgentRefuse("controller or port numbering is not a supported canonical integer")
	}
	return uint(n), nil
}
func guestAgentChildren(parent *positionedNode, name string) ([]*positionedNode, error) {
	var found []*positionedNode
	for _, p := range parent.parts {
		if p.child != nil && p.child.name.Local == name {
			if p.child.name.Space != "" {
				return nil, guestAgentRefuse("foreign namespace in selected device configuration")
			}
			found = append(found, p.child)
		}
	}
	return found, nil
}
func guestAgentSingle(parent *positionedNode, name string, required bool) (*positionedNode, error) {
	list, err := guestAgentChildren(parent, name)
	if err != nil {
		return nil, err
	}
	if len(list) > 1 || required && len(list) != 1 {
		return nil, guestAgentRefuse("missing or duplicate " + name + " configuration")
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

func modelGuestAgent(raw string) (*guestAgentModel, error) {
	root, err := positionedXML(raw)
	if err != nil {
		return nil, err
	}
	devices, err := guestAgentSingle(root, "devices", true)
	if err != nil {
		return nil, err
	}
	m := &guestAgentModel{devices: devices, view: GuestAgentChannelView{Reason: "Enable the channel, then install guest tools inside the VM."}}
	controllers := map[uint]guestAgentController{}
	list, err := guestAgentChildren(devices, "controller")
	if err != nil {
		return nil, err
	}
	for _, n := range list {
		if guestAgentValue(n, "type") != "virtio-serial" {
			continue
		}
		if !n.onlyAttrs("type", "index", "model", "ports", "vectors") {
			return nil, guestAgentRefuse("opaque virtio-serial controller policy needs a dedicated editor")
		}
		index, err := guestAgentNumber(guestAgentValue(n, "index"), 255)
		if err != nil {
			return nil, err
		}
		if _, ok := controllers[index]; ok {
			return nil, guestAgentRefuse("duplicate virtio-serial controller index")
		}
		model := guestAgentValue(n, "model")
		if model != "" && model != "virtio" && model != "virtio-transitional" && model != "virtio-non-transitional" {
			return nil, guestAgentRefuse("unsupported virtio-serial controller model")
		}
		ports := uint(31)
		if value, present := n.attr("ports"); present {
			ports, err = guestAgentNumber(value, 256)
			if err != nil || ports < 2 {
				return nil, guestAgentRefuse("controller has no supported port capacity")
			}
		}
		if value, present := n.attr("vectors"); present {
			if _, err = guestAgentNumber(value, 65535); err != nil {
				return nil, err
			}
		}
		seen := map[string]bool{}
		for _, p := range n.parts {
			if p.child == nil {
				if p.kind == "text" && strings.TrimSpace(p.text) == "" {
					continue
				}
				return nil, guestAgentRefuse("opaque virtio-serial controller contents")
			}
			c := p.child
			if c.name.Space != "" || seen[c.name.Local] {
				return nil, guestAgentRefuse("ambiguous controller child")
			}
			seen[c.name.Local] = true
			switch c.name.Local {
			case "alias":
				if !c.onlyAttrs("name") || !guestAgentEmpty(c) || !targetID.MatchString(guestAgentValue(c, "name")) {
					return nil, guestAgentRefuse("unsupported controller alias")
				}
			case "address":
				if guestAgentValue(c, "type") != "pci" || !c.onlyAttrs("type", "domain", "bus", "slot", "function", "multifunction") || !guestAgentEmpty(c) {
					return nil, guestAgentRefuse("opaque controller placement")
				}
			default:
				return nil, guestAgentRefuse("opaque virtio-serial controller dependency")
			}
		}
		controllers[index] = guestAgentController{ports: ports}
	}
	occupied := map[[2]uint]bool{}
	targetNames := map[string]bool{}
	for _, p := range devices.parts {
		n := p.child
		if n == nil {
			continue
		}
		if n.name.Local != "channel" && n.name.Local != "console" {
			continue
		}
		if n.name.Space != "" {
			return nil, guestAgentRefuse("foreign channel or console declaration")
		}
		target, err := guestAgentSingle(n, "target", true)
		if err != nil {
			return nil, err
		}
		name := guestAgentValue(target, "name")
		if name != "" {
			if targetNames[name] {
				return nil, guestAgentRefuse("duplicate channel target name")
			}
			targetNames[name] = true
		}
		agent := name == GuestAgentTarget
		if agent {
			if n.name.Local != "channel" || guestAgentValue(n, "type") != "unix" || !n.onlyAttrs("type") || guestAgentValue(target, "type") != "virtio" || !target.onlyAttrs("type", "name") || !guestAgentEmpty(target) {
				return nil, guestAgentRefuse("existing agent target has conflicting or opaque transport")
			}
			for _, part := range n.parts {
				c := part.child
				if c == nil {
					if part.kind == "text" && strings.TrimSpace(part.text) == "" {
						continue
					}
					return nil, guestAgentRefuse("opaque existing agent channel contents")
				}
				if c.name.Space != "" {
					return nil, guestAgentRefuse("foreign existing agent channel contents")
				}
				switch c.name.Local {
				case "target", "address":
				case "alias":
					if !c.onlyAttrs("name") || !guestAgentEmpty(c) || !targetID.MatchString(guestAgentValue(c, "name")) {
						return nil, guestAgentRefuse("unsupported agent alias")
					}
				default:
					return nil, guestAgentRefuse("existing agent channel has an explicit socket or other policy; it will not be replaced")
				}
			}
			if _, err := guestAgentSingle(n, "alias", false); err != nil {
				return nil, err
			}
			m.view.Present = true
		}
		address, err := guestAgentSingle(n, "address", false)
		if err != nil {
			return nil, err
		}
		if guestAgentValue(target, "type") != "virtio" {
			if agent {
				return nil, guestAgentRefuse("conflicting guest-agent target type")
			}
			if address != nil && guestAgentValue(address, "type") == "virtio-serial" {
				return nil, guestAgentRefuse("virtio address conflicts with target transport")
			}
			continue
		}
		if address == nil || guestAgentValue(address, "type") != "virtio-serial" || !address.onlyAttrs("type", "controller", "bus", "port") || !guestAgentEmpty(address) {
			return nil, guestAgentRefuse("existing virtio channel lacks an explicit supported port allocation")
		}
		index, err := guestAgentNumber(guestAgentValue(address, "controller"), 255)
		if err != nil {
			return nil, err
		}
		bus, err := guestAgentNumber(guestAgentValue(address, "bus"), 0)
		if err != nil || bus != 0 {
			return nil, guestAgentRefuse("unsupported virtio-serial bus")
		}
		port, err := guestAgentNumber(guestAgentValue(address, "port"), 255)
		if err != nil {
			return nil, err
		}
		controller, ok := controllers[index]
		if !ok || port >= controller.ports {
			return nil, guestAgentRefuse("channel refers to an absent controller or out-of-range port")
		}
		key := [2]uint{index, port}
		if occupied[key] {
			return nil, guestAgentRefuse("duplicate virtio-serial port allocation")
		}
		occupied[key] = true
		if agent {
			if port == 0 {
				return nil, guestAgentRefuse("agent channel cannot use the reserved console port")
			}
			m.view.ControllerIndex = index
			m.view.Port = port
		}
	}
	if m.view.Present {
		m.view.Reason = "The automatic guest-agent channel is configured. Guest software and responsiveness must be checked separately."
		return m, nil
	}
	if len(controllers) == 0 {
		m.view.CanEnable = true
		m.view.AddsController = true
		m.view.Port = 1
		return m, nil
	}
	indices := make([]int, 0, len(controllers))
	for index := range controllers {
		indices = append(indices, int(index))
	}
	sort.Ints(indices)
	for _, index := range indices {
		for port := uint(1); port < controllers[uint(index)].ports; port++ {
			if !occupied[[2]uint{uint(index), port}] {
				m.view.CanEnable = true
				m.view.ControllerIndex = uint(index)
				m.view.Port = port
				return m, nil
			}
		}
	}
	m.view.Reason = "All supported virtio-serial controller ports are occupied. No existing channel will be moved."
	return m, nil
}

// InspectGuestAgent does not alter XML or native state. Unsupported declarations
// fail explicitly rather than silently retargeting an adopted VM's existing agent.
func InspectGuestAgent(raw string) (GuestAgentChannelView, error) {
	m, err := modelGuestAgent(raw)
	if err != nil {
		return GuestAgentChannelView{}, err
	}
	return m.view, nil
}

// EnableGuestAgent inserts only an automatic Unix agent channel and, when absent,
// a virtio-serial controller. No socket source, reconnect policy, agent command,
// package installation or disabling/removal is implied. Already configured safe
// channels return the original bytes unchanged.
func EnableGuestAgent(raw string) (string, error) {
	m, err := modelGuestAgent(raw)
	if err != nil {
		return "", err
	}
	if m.view.Present {
		return raw, nil
	}
	if !m.view.CanEnable {
		return "", guestAgentRefuse(m.view.Reason)
	}
	fragment := ""
	if m.view.AddsController {
		fragment = `<controller type="virtio-serial" index="0" model="virtio"/>`
	}
	fragment += fmt.Sprintf(`<channel type="unix"><target type="virtio" name="%s"/><address type="virtio-serial" controller="%d" bus="0" port="%d"/></channel>`, GuestAgentTarget, m.view.ControllerIndex, m.view.Port)
	var edits []spanReplacement
	if m.devices.empty {
		edits = []spanReplacement{{m.devices.startEnd - 2, m.devices.startEnd, ">" + fragment + "</devices>"}}
	} else {
		edits = []spanReplacement{{m.devices.endStart, m.devices.endStart, fragment}}
	}
	out, err := replaceSpans(raw, edits)
	if err != nil {
		return "", err
	}
	after, err := modelGuestAgent(out)
	if err != nil || !after.view.Present || after.view.ControllerIndex != m.view.ControllerIndex || after.view.Port != m.view.Port {
		return "", guestAgentRefuse("inserted channel did not preserve its intended identity")
	}
	// Remove only the newly inserted bytes from the reparsed clone, then compare
	// semantic digests. This independently binds every pre-existing XML node,
	// namespace, opaque value, comment and sibling order to the original document.
	clone, err := removeGuestAgentAdditions(out, m.view)
	if err != nil {
		return "", err
	}
	beforeDigest, err := HardwareDigest(raw)
	if err != nil {
		return "", err
	}
	afterDigest, err := HardwareDigest(clone)
	if err != nil || beforeDigest != afterDigest {
		return "", guestAgentRefuse("unrelated XML changed during channel insertion")
	}
	return out, nil
}
func removeGuestAgentAdditions(raw string, view GuestAgentChannelView) (string, error) {
	root, err := positionedXML(raw)
	if err != nil {
		return "", err
	}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return "", err
	}
	var edits []spanReplacement
	for _, p := range devices.parts {
		n := p.child
		if n == nil || n.name.Space != "" {
			continue
		}
		if n.name.Local == "channel" {
			t, err := guestAgentSingle(n, "target", true)
			if err != nil {
				return "", err
			}
			if guestAgentValue(t, "name") == GuestAgentTarget {
				edits = append(edits, spanReplacement{n.start, n.end, ""})
			}
		}
		if view.AddsController && n.name.Local == "controller" && guestAgentValue(n, "type") == "virtio-serial" && guestAgentValue(n, "index") == "0" {
			edits = append(edits, spanReplacement{n.start, n.end, ""})
		}
	}
	expected := 1
	if view.AddsController {
		expected++
	}
	if len(edits) != expected {
		return "", guestAgentRefuse("added XML nodes are ambiguous")
	}
	return replaceSpans(raw, edits)
}

// GuestAgentDigest is the version-1 comparer for one enabled channel. It admits
// native formatting changes already supported by HardwareDigest, and only the
// expected aliases/PCI placement on the freshly added devices. It does not admit
// source paths, changes to existing controllers, or additional PCI controllers.
// The caller must use inactive persistent XML, never live socket allocation XML.
func GuestAgentDigest(raw string, controllerIndex, port uint, addsController bool) (string, error) {
	if controllerIndex > 255 || port == 0 || port > 255 || addsController && controllerIndex != 0 {
		return "", guestAgentRefuse("invalid reviewed channel allocation")
	}
	m, err := modelGuestAgent(raw)
	if err != nil {
		return "", err
	}
	if !m.view.Present || m.view.ControllerIndex != controllerIndex || m.view.Port != port {
		return "", guestAgentRefuse("agent allocation differs from the reviewed channel")
	}
	channels := 0
	foundController := false
	for _, part := range m.devices.parts {
		n := part.child
		if n == nil || n.name.Space != "" {
			continue
		}
		if n.name.Local == "channel" {
			target, err := guestAgentSingle(n, "target", true)
			if err != nil {
				return "", err
			}
			if guestAgentValue(target, "name") == GuestAgentTarget {
				alias, err := guestAgentSingle(n, "alias", false)
				if err != nil {
					return "", err
				}
				if alias != nil && guestAgentValue(alias, "name") != fmt.Sprintf("channel%d", channels) {
					return "", guestAgentRefuse("new channel alias differs from native device ordering")
				}
			}
			channels++
		}
		if addsController && n.name.Local == "controller" && guestAgentValue(n, "type") == "virtio-serial" && guestAgentValue(n, "index") == "0" {
			foundController = true
			if !n.onlyAttrs("type", "index", "model") || guestAgentValue(n, "model") != "virtio" {
				return "", guestAgentRefuse("new controller attributes differ from reviewed defaults")
			}
			for _, p := range n.parts {
				c := p.child
				if c == nil {
					if p.kind == "text" && strings.TrimSpace(p.text) == "" {
						continue
					}
					return "", guestAgentRefuse("opaque new controller contents")
				}
				switch c.name.Local {
				case "alias":
					if guestAgentValue(c, "name") != "virtio-serial0" {
						return "", guestAgentRefuse("new controller alias differs")
					}
				case "address":
					if !guestAgentAllocatedPCI(c) || !guestAgentPCIUnique(m.devices, n, c) {
						return "", guestAgentRefuse("new controller has unsupported or conflicting native PCI allocation")
					}
				default:
					return "", guestAgentRefuse("unreviewed new controller child")
				}
			}
		}
	}
	if addsController && !foundController {
		return "", guestAgentRefuse("reviewed new controller is absent")
	}
	view := GuestAgentChannelView{ControllerIndex: controllerIndex, Port: port, AddsController: addsController}
	clone, err := removeGuestAgentAdditions(raw, view)
	if err != nil {
		return "", err
	}
	remaining, err := HardwareDigest(clone)
	if err != nil {
		return "", err
	}
	bound, _ := json.Marshal(struct {
		Version           int
		Remaining, Target string
		Controller, Port  uint
		AddsController    bool
	}{1, remaining, GuestAgentTarget, controllerIndex, port, addsController})
	return Digest(string(bound)), nil
}
func guestAgentAllocatedPCI(n *positionedNode) bool {
	if guestAgentValue(n, "type") != "pci" || !n.onlyAttrs("type", "domain", "bus", "slot", "function") || !guestAgentEmpty(n) {
		return false
	}
	bounds := map[string]uint64{"domain": 0, "bus": 255, "slot": 31, "function": 0}
	for key, max := range bounds {
		text, present := n.attr(key)
		if !present {
			return false
		}
		base := 10
		digits := text
		if strings.HasPrefix(text, "0x") {
			base = 16
			digits = text[2:]
		}
		if digits == "" || len(digits) > 4 {
			return false
		}
		for _, c := range digits {
			if !(c >= '0' && c <= '9' || base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')) {
				return false
			}
		}
		value, err := strconv.ParseUint(digits, base, 16)
		if err != nil || value > max {
			return false
		}
	}
	return true
}

// Native allocation must not share an address with a pre-existing guest device.
// Host-side PCI addresses nested inside hostdev/source are not guest addresses.
func guestAgentPCIUnique(devices, inserted, address *positionedNode) bool {
	key := func(n *positionedNode) ([4]uint64, bool) {
		var values [4]uint64
		for i, name := range []string{"domain", "bus", "slot", "function"} {
			value, present := n.attr(name)
			if !present {
				return values, false
			}
			base := 10
			if strings.HasPrefix(value, "0x") {
				base = 16
				value = value[2:]
			}
			parsed, err := strconv.ParseUint(value, base, 16)
			if err != nil {
				return values, false
			}
			values[i] = parsed
		}
		return values, true
	}
	wanted, ok := key(address)
	if !ok {
		return false
	}
	for _, p := range devices.parts {
		n := p.child
		if n == nil || n == inserted {
			continue
		}
		for _, a := range n.children("address") {
			if guestAgentValue(a, "type") == "pci" {
				other, ok := key(a)
				if !ok || other == wanted {
					return false
				}
			}
		}
	}
	return true
}
