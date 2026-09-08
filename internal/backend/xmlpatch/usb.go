package xmlpatch

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"virmill.local/core/internal/domain"
)

// USBDevicePatch selects a current host device, not a persistent serial/port
// binding. Remove changes the domain patch only; USBDeviceXML always emits the
// same complete identity fragment for a separately authorized native operation.
type USBDevicePatch struct {
	Alias, VendorID, ProductID string
	Bus, Device                uint
	Remove                     bool
}

var usbPatchAlias = regexp.MustCompile(`^ua-virmill-usb-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var usbPatchID = regexp.MustCompile(`^[0-9a-f]{4}$`)
var usbObservedID = regexp.MustCompile(`^(0x)?[0-9a-fA-F]{1,4}$`)

func validUSBPatch(p USBDevicePatch) error {
	if !usbPatchAlias.MatchString(p.Alias) || p.Alias == "ua-virmill-usb-00000000-0000-0000-0000-000000000000" {
		return domain.Fail("INVALID_INPUT", "USB patch requires a stable ua-virmill-usb- alias with a canonical nonzero UUID")
	}
	if !usbPatchID.MatchString(p.VendorID) || !usbPatchID.MatchString(p.ProductID) {
		return domain.Fail("INVALID_INPUT", "USB vendor and product IDs must each be four lowercase hexadecimal digits")
	}
	// libvirt's usbAddr schema permits at most three unprefixed digits. Keep
	// generated host addresses decimal rather than emitting an invalid large bus.
	if p.Bus == 0 || p.Bus > 999 || p.Device == 0 || p.Device > 127 {
		return domain.Fail("INVALID_INPUT", "USB patch requires current bus 1..999 and device 1..127")
	}
	return nil
}

// USBDeviceXML emits only the fixed host-local USB device profile. It neither
// discovers a device nor establishes host-use, hotplug or reconnect permission.
func USBDeviceXML(change USBDevicePatch) (string, error) {
	if err := validUSBPatch(change); err != nil {
		return "", err
	}
	return fmt.Sprintf(`<hostdev mode="subsystem" type="usb" managed="yes"><source><vendor id="0x%s"/><product id="0x%s"/><address bus="%d" device="%d"/></source><alias name="%s"/></hostdev>`,
		change.VendorID, change.ProductID, change.Bus, change.Device, change.Alias), nil
}

// USBDevice inserts or removes exactly one selected hostdev span. All bytes
// outside that span (or the devices container's empty closing tag) are retained.
func USBDevice(raw string, change USBDevicePatch) (string, error) {
	fragment, err := USBDeviceXML(change)
	if err != nil {
		return "", err
	}
	root, err := positionedXML(raw)
	if err != nil {
		return "", err
	}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return "", err
	}
	aliases := map[string]bool{}
	addresses := map[[2]uint]bool{}
	var selected *positionedNode
	for _, part := range devices.parts {
		n := part.child
		if n == nil || n.name.Space != "" {
			continue
		}
		list := n.children("alias")
		if len(list) > 1 {
			return "", domain.Fail("SOURCE_CHANGED", "device aliases are ambiguous")
		}
		if len(list) == 1 {
			alias, present := list[0].attr("name")
			if !present || alias == "" || aliases[alias] {
				return "", domain.Fail("SOURCE_CHANGED", "device aliases are missing or duplicated")
			}
			aliases[alias] = true
			if alias == change.Alias {
				selected = n
			}
		}
		kind, _ := n.attr("type")
		if n.name.Local != "hostdev" || kind != "usb" {
			continue
		}
		address, e := usbHostAddress(n)
		if e != nil {
			return "", e
		}
		if addresses[address] {
			return "", domain.Fail("SOURCE_CHANGED", "duplicate current host USB address")
		}
		addresses[address] = true
	}
	if change.Remove {
		if selected == nil {
			return "", domain.Fail("SOURCE_CHANGED", "selected USB alias is absent")
		}
		if err := usbRemovalIdentity(selected, change); err != nil {
			return "", err
		}
		return replaceSpans(raw, []spanReplacement{{selected.start, selected.end, ""}})
	}
	if selected != nil || addresses[[2]uint{change.Bus, change.Device}] {
		return "", domain.Fail("RESOURCE_BUSY", "USB alias or current host address is already assigned")
	}
	if devices.empty {
		return replaceSpans(raw, []spanReplacement{{devices.startEnd - 2, devices.startEnd, ">" + fragment + "</devices>"}})
	}
	return replaceSpans(raw, []spanReplacement{{devices.endStart, devices.endStart, fragment}})
}

// Address-only existing USB selectors are sufficient for collision checks.
// A selector without an address cannot prove that the new address is unused.
func usbHostAddress(n *positionedNode) ([2]uint, error) {
	var result [2]uint
	source, err := onlyChild(n, "source")
	if err != nil {
		return result, err
	}
	address, err := onlyChild(source, "address")
	if err != nil {
		return result, err
	}
	if !address.onlyAttrs("bus", "device") || !usbEmpty(address) {
		return result, domain.Fail("UNSUPPORTED_CAPABILITY", "unsupported current host USB address structure")
	}
	bus, hasBus := address.attr("bus")
	device, hasDevice := address.attr("device")
	var ok bool
	result[0], ok = usbAddressNumber(bus, 999, false)
	if !hasBus || !ok {
		return [2]uint{}, domain.Fail("UNSUPPORTED_CAPABILITY", "unsupported current host USB bus")
	}
	result[1], ok = usbAddressNumber(device, 127, false)
	if !hasDevice || !ok {
		return [2]uint{}, domain.Fail("UNSUPPORTED_CAPABILITY", "unsupported current host USB device")
	}
	return result, nil
}

func usbAddressNumber(text string, maximum uint64, zero bool) (uint, bool) {
	base, digits := 10, text
	if strings.HasPrefix(text, "0x") {
		base, digits = 16, text[2:]
	}
	if len(digits) < 1 || len(digits) > 3 || base == 10 && len(digits) > 1 && digits[0] == '0' {
		return 0, false
	}
	for _, c := range digits {
		if c >= '0' && c <= '9' || base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			continue
		}
		return 0, false
	}
	n, err := strconv.ParseUint(digits, base, 16)
	return uint(n), err == nil && n <= maximum && (n > 0 || zero)
}

func usbEmpty(n *positionedNode) bool {
	for _, p := range n.parts {
		if p.child != nil || p.kind != "comment" && (p.kind != "text" || strings.TrimSpace(p.text) != "") {
			return false
		}
	}
	return true
}

func usbKnownChildren(n *positionedNode, allowed ...string) bool {
	for _, p := range n.parts {
		if p.child == nil {
			if p.kind != "comment" && (p.kind != "text" || strings.TrimSpace(p.text) != "") {
				return false
			}
			continue
		}
		known := false
		for _, name := range allowed {
			known = known || p.child.name == (xml.Name{Local: name})
		}
		if !known {
			return false
		}
	}
	return true
}

func usbRemovalIdentity(n *positionedNode, change USBDevicePatch) error {
	kind, _ := n.attr("type")
	mode, _ := n.attr("mode")
	managed, _ := n.attr("managed")
	if n.name != (xml.Name{Local: "hostdev"}) || kind != "usb" || mode != "subsystem" || managed != "yes" {
		return domain.Fail("SOURCE_CHANGED", "selected alias is not the reviewed USB hostdev profile")
	}
	if !n.onlyAttrs("mode", "type", "managed") || !usbKnownChildren(n, "source", "alias", "address") {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB hostdev has unsupported policy or children")
	}
	source, err := onlyChild(n, "source")
	if err != nil {
		return err
	}
	if !source.onlyAttrs() || !usbKnownChildren(source, "vendor", "product", "address") {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB source has unsupported policy or children")
	}
	for _, field := range []struct{ name, expected string }{{"vendor", change.VendorID}, {"product", change.ProductID}} {
		node, err := onlyChild(source, field.name)
		if err != nil {
			return err
		}
		value, _ := node.attr("id")
		if !node.onlyAttrs("id") || !usbEmpty(node) || !usbObservedID.MatchString(value) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB source has an unsupported vendor/product identity")
		}
		id, err := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 16)
		if err != nil || fmt.Sprintf("%04x", id) != field.expected {
			return domain.Fail("SOURCE_CHANGED", "selected USB vendor/product identity changed")
		}
	}
	address, err := usbHostAddress(n)
	if err != nil {
		return err
	}
	if address != [2]uint{change.Bus, change.Device} {
		return domain.Fail("SOURCE_CHANGED", "selected USB host address changed")
	}
	alias, err := onlyChild(n, "alias")
	if err != nil {
		return err
	}
	if !alias.onlyAttrs("name") || !usbEmpty(alias) {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB alias has unsupported structure")
	}
	guest := n.children("address")
	if len(guest) > 1 {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB guest placement is ambiguous")
	}
	if len(guest) == 1 {
		g := guest[0]
		kind, _ := g.attr("type")
		bus, _ := g.attr("bus")
		port, present := g.attr("port")
		_, ok := usbAddressNumber(bus, 999, true)
		if kind != "usb" || !ok || !g.onlyAttrs("type", "bus", "port") || !usbEmpty(g) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB guest placement is unsupported")
		}
		if present {
			parts := strings.Split(port, ".")
			if len(parts) > 4 {
				return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB guest port exceeds supported depth")
			}
			for _, part := range parts {
				if _, ok := usbAddressNumber(part, 127, false); !ok {
					return domain.Fail("UNSUPPORTED_CAPABILITY", "selected USB guest port is unsupported")
				}
			}
		}
	}
	return nil
}
