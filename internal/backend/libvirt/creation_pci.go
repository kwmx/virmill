//go:build linux && cgo

package libvirt

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"virmill.local/core/internal/domain"
)

func pciNumber(value string, max uint64) (uint64, bool) {
	base := 10
	if strings.HasPrefix(value, "0x") {
		value, base = value[2:], 16
	}
	if value == "" {
		return 0, false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || base == 16 && (r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F')) {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(value, base, 64)
	return n, err == nil && n <= max
}

func onlyAttrs(n *xmlNode, required, optional []string) bool {
	if n == nil || n.name.Space != "" || strings.TrimSpace(n.text) != "" {
		return false
	}
	allowed, found := map[string]bool{}, map[string]bool{}
	for _, a := range append(append([]string{}, required...), optional...) {
		allowed[a] = true
	}
	for _, a := range n.attrs {
		if a.Name.Space != "" || !allowed[a.Name.Local] || found[a.Name.Local] {
			return false
		}
		found[a.Name.Local] = true
	}
	for _, a := range required {
		if !found[a] {
			return false
		}
	}
	return true
}

type creationPCIAddress struct{ bus, slot, function uint64 }

func parseCreationPCI(n *xmlNode) (creationPCIAddress, bool) {
	var out creationPCIAddress
	if n == nil || n.name.Local != "address" || len(n.children) != 0 || !onlyAttrs(n, []string{"type", "domain", "bus", "slot", "function"}, []string{"multifunction"}) || attr(n, "type") != "pci" {
		return out, false
	}
	if d, ok := pciNumber(attr(n, "domain"), 0); !ok || d != 0 {
		return out, false
	}
	multi := attr(n, "multifunction")
	if multi != "" && multi != "on" && multi != "off" {
		return out, false
	}
	var ok bool
	if out.bus, ok = pciNumber(attr(n, "bus"), 255); !ok {
		return out, false
	}
	if out.slot, ok = pciNumber(attr(n, "slot"), 31); !ok {
		return out, false
	}
	if out.function, ok = pciNumber(attr(n, "function"), 7); !ok {
		return out, false
	}
	return out, multi != "on" || out.function == 0
}

// Automatic placement is an explicit policy, not a wildcard for new devices.
// Validate the complete PCI graph before removing only known topology nodes for
// semantic comparison. No arbitrary driver, source, ROM, NUMA or host device is
// accepted through this normalization path.
func normalizeCreationPCI(w, g *xmlNode, policy *domain.CreationDevicePolicy) error {
	fail := func() error {
		return domain.Fail("RECOVERY_REQUIRED", "defined PCI topology differs from the reviewed automatic placement policy")
	}
	os := child(w, "os")
	if os == nil || child(os, "type") == nil {
		return fail()
	}
	if err := policy.Validate(attr(child(os, "type"), "machine")); err != nil {
		return err
	}
	devices := child(g, "devices")
	if devices == nil {
		return fail()
	}
	buses, controllerIDs := map[uint64]*xmlNode{}, map[string]bool{}
	addresses := map[creationPCIAddress]bool{}
	parents := map[uint64]creationPCIAddress{}
	chassis, ports := map[uint64]bool{}, map[uint64]bool{}
	for _, n := range devices.children {
		if n.name == (xml.Name{Local: "controller"}) {
			index, ok := pciNumber(attr(n, "index"), 255)
			key := fmt.Sprintf("%s/%d", attr(n, "type"), index)
			if !ok || controllerIDs[key] {
				return fail()
			}
			controllerIDs[key] = true
			if attr(n, "type") == "pci" {
				buses[index] = n
			}
		}
		count, aliases := 0, 0
		for _, a := range n.children {
			if a.name == (xml.Name{Local: "alias"}) {
				aliases++
				if aliases > 1 {
					return fail()
				}
			}
			if a.name != (xml.Name{Local: "address"}) {
				continue
			}
			count++
			if count > 1 {
				return fail()
			}
			if attr(a, "type") != "pci" {
				continue
			}
			address, ok := parseCreationPCI(a)
			if !ok || count > 1 || addresses[address] {
				return fail()
			}
			addresses[address] = true
		}
	}
	if len(buses) == 0 || len(buses) > 256 || buses[0] == nil {
		return fail()
	}
	for address := range addresses {
		if buses[address.bus] == nil {
			return fail()
		}
	}
	for index := uint64(0); index < uint64(len(buses)); index++ {
		n := buses[index]
		if n == nil || !onlyAttrs(n, []string{"type", "index", "model"}, nil) {
			return fail()
		}
		model := attr(n, "model")
		if index == 0 {
			expected := "pci-root"
			if policy.Chipset == "q35" {
				expected = "pcie-root"
			}
			if model != expected || len(n.children) != 0 {
				return fail()
			}
			continue
		}
		if !(policy.Chipset == "q35" && (model == "pcie-root-port" || model == "pcie-to-pci-bridge") || model == "pci-bridge") {
			return fail()
		}
		seen := map[string]bool{}
		for _, c := range n.children {
			if c.name.Space != "" || seen[c.name.Local] {
				return fail()
			}
			seen[c.name.Local] = true
			switch c.name.Local {
			case "address":
				address, ok := parseCreationPCI(c)
				if !ok || address.bus >= index {
					return fail()
				}
				parents[index] = address
			case "model":
				if !onlyAttrs(c, []string{"name"}, nil) || len(c.children) != 0 || attr(c, "name") != model {
					return fail()
				}
			case "target":
				if model == "pcie-root-port" {
					if !onlyAttrs(c, []string{"chassis", "port"}, nil) || len(c.children) != 0 {
						return fail()
					}
					ch, ok := pciNumber(attr(c, "chassis"), 255)
					if !ok || ch == 0 || chassis[ch] {
						return fail()
					}
					port, ok := pciNumber(attr(c, "port"), 255)
					if !ok || ports[port] {
						return fail()
					}
					chassis[ch], ports[port] = true, true
				} else if model == "pci-bridge" {
					if !onlyAttrs(c, []string{"chassisNr"}, nil) || len(c.children) != 0 {
						return fail()
					}
					if ch, ok := pciNumber(attr(c, "chassisNr"), 255); !ok || ch == 0 {
						return fail()
					}
				} else {
					return fail()
				}
			case "alias":
				if !creationDefaultChild("/domain/devices/controller", c) {
					return fail()
				}
			default:
				return fail()
			}
		}
		if !seen["address"] {
			return fail()
		}
		parent := parents[index].bus
		if model == "pcie-root-port" && (parent != 0 || parents[index].slot == 0 || !seen["model"] || !seen["target"]) {
			return fail()
		}
		if model == "pcie-to-pci-bridge" && attr(buses[parent], "model") != "pcie-root-port" {
			return fail()
		}
		if model == "pci-bridge" && attr(buses[parent], "model") != "pci-root" && attr(buses[parent], "model") != "pci-bridge" && attr(buses[parent], "model") != "pcie-to-pci-bridge" {
			return fail()
		}
	}
	kept := make([]*xmlNode, 0, len(devices.children))
	for _, n := range devices.children {
		if n.name == (xml.Name{Local: "controller"}) && attr(n, "type") == "pci" {
			index, _ := pciNumber(attr(n, "index"), 255)
			if index != 0 {
				continue
			}
		}
		if n.name == (xml.Name{Local: "memballoon"}) && policy.MemoryBalloon == "virtio" && attr(n, "model") == "virtio" {
			children := make([]*xmlNode, 0, len(n.children))
			for _, c := range n.children {
				if creationDefaultChild("/domain/devices/controller", c) {
					continue
				}
				children = append(children, c)
			}
			n.children = children
		}
		kept = append(kept, n)
	}
	devices.children = kept
	return nil
}
