//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

const (
	pciMaxDevices       = 4096
	pciMaxXML           = 256 << 10
	pciMaxXMLTotal      = wire.MaxFrame - (128 << 10)
	pciDiscoveryWarning = "Read-only PCI observations do not establish safe passthrough, exclusive ownership, or IOMMU isolation. Assignment, driver rebinding, bootloader/initramfs changes and ACS overrides are not performed."
)

var _ domain.PCIInventoryProvider = (*Provider)(nil)

// InspectPCI only opens the explicitly selected local connection read-only.
// Native node-device discovery needs its own backend support and permissions;
// an unsupported or failed read is never replaced by an empty inventory.
func (p *Provider) InspectPCI(ctx context.Context, uri string) (domain.PCIInventory, error) {
	if err := ctx.Err(); err != nil {
		return domain.PCIInventory{}, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return domain.PCIInventory{}, err
	}
	defer c.Close()
	return inspectPCI(ctx, c)
}

func inspectPCI(ctx context.Context, c *native.Connect) (domain.PCIInventory, error) {
	if err := ctx.Err(); err != nil {
		return domain.PCIInventory{}, err
	}
	all, err := c.ListAllNodeDevices(native.CONNECT_LIST_NODE_DEVICES_CAP_PCI_DEV)
	if err != nil {
		return domain.PCIInventory{}, fmt.Errorf("read-only PCI node-device enumeration: %w", err)
	}
	defer func() {
		for i := range all {
			all[i].Free()
		}
	}()
	return collectPCI(ctx, len(all), func(i int) pciObservation { return &all[i] })
}

// This interface intentionally exposes no device mutation methods.
type pciObservation interface {
	GetName() (string, error)
	GetXMLDesc(native.NodeDeviceXMLFlags) (string, error)
}

func pciInvalid(reason string) error {
	return domain.Fail("INVALID_INPUT", "PCI inventory incomplete: "+reason)
}

func collectPCI(ctx context.Context, count int, get func(int) pciObservation) (domain.PCIInventory, error) {
	empty := domain.PCIInventory{}
	if count < 0 || count > pciMaxDevices {
		return empty, pciInvalid("node-device count exceeds 4096")
	}
	out := domain.PCIInventory{Devices: []domain.PCIDevice{}, Warnings: []string{pciDiscoveryWarning}}
	used, xmlBytes := 0, 0
	if err := inventoryBudget(out, &used); err != nil {
		return empty, err
	}
	names, addresses := map[string]bool{}, map[string]bool{}
	missingGroup, missingNUMA, missingDriver := 0, 0, 0
	for i := 0; i < count; i++ {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		n := get(i)
		name, err := n.GetName()
		if err != nil {
			return empty, fmt.Errorf("read PCI node-device name: %w", err)
		}
		data, err := n.GetXMLDesc(0)
		if err != nil {
			return empty, fmt.Errorf("read PCI node-device XML: %w", err)
		}
		xmlBytes += len(data)
		if xmlBytes > pciMaxXMLTotal {
			return empty, pciInvalid("aggregate node-device XML exceeds response bounds")
		}
		d, err := parsePCIDevice(name, data)
		if err != nil {
			return empty, err
		}
		if names[d.Name] || addresses[d.Address] {
			return empty, pciInvalid("duplicate node-device name or PCI address")
		}
		names[d.Name], addresses[d.Address] = true, true
		if err = inventoryBudget(d, &used); err != nil {
			return empty, err
		}
		if d.IOMMUGroup == nil {
			missingGroup++
		}
		if d.NUMANode == nil {
			missingNUMA++
		}
		if d.Driver == "" {
			missingDriver++
		}
		out.Devices = append(out.Devices, d)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := validatePCIGroups(out.Devices); err != nil {
		return empty, err
	}
	sort.Slice(out.Devices, func(i, j int) bool { return out.Devices[i].Address < out.Devices[j].Address })
	if missingGroup > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("IOMMU group unavailable for %d PCI device(s); missing data is not proof of isolation or of IOMMU being disabled.", missingGroup))
	}
	if missingNUMA > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("NUMA node unavailable for %d PCI device(s).", missingNUMA))
	}
	if missingDriver > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("No driver was reported for %d PCI device(s); availability for assignment was not checked.", missingDriver))
	}
	used = 0
	if err := inventoryBudget(out, &used); err != nil {
		return empty, err
	}
	return out, nil
}

// A group is complete only if every listed member was enumerated and every
// member reports the same group and complete member set. This detects common
// permission-filtered or changing inventories without inferring missing facts.
func validatePCIGroups(devices []domain.PCIDevice) error {
	byAddress := make(map[string]domain.PCIDevice, len(devices))
	groups := map[uint32]string{}
	for _, d := range devices {
		byAddress[d.Address] = d
		if d.IOMMUGroup == nil {
			continue
		}
		members := strings.Join(d.GroupMembers, ",")
		if previous, found := groups[*d.IOMMUGroup]; found && previous != members {
			return pciInvalid("inconsistent membership reported for one IOMMU group; refresh after checking node-device access")
		}
		groups[*d.IOMMUGroup] = members
	}
	for _, d := range devices {
		if d.IOMMUGroup == nil {
			continue
		}
		for _, address := range d.GroupMembers {
			member, found := byAddress[address]
			if !found || member.IOMMUGroup == nil || *member.IOMMUGroup != *d.IOMMUGroup {
				return pciInvalid("IOMMU group members are missing or contradictory; refresh after checking node-device access")
			}
		}
	}
	return nil
}

// Decode the native no-namespace format exactly. A separate bounded parser
// avoids xml.Unmarshal's local-name matching and duplicate-field replacement.
func pciXMLTree(data string) (*xmlNode, error) {
	if len(data) == 0 || len(data) > pciMaxXML {
		return nil, pciInvalid("node-device XML exceeds 256 KiB or is empty")
	}
	d := xml.NewDecoder(strings.NewReader(data))
	var root *xmlNode
	type frame struct {
		node *xmlNode
		text strings.Builder
	}
	var stack []*frame
	nodes := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, pciInvalid("malformed node-device XML")
		}
		switch v := token.(type) {
		case xml.StartElement:
			nodes++
			if len(stack) >= 32 || nodes > 16384 {
				return nil, pciInvalid("node-device XML structure exceeds bounds")
			}
			if v.Name.Space != "" || strings.Contains(v.Name.Local, ":") {
				return nil, pciInvalid("foreign namespace in node-device XML")
			}
			seen := map[xml.Name]bool{}
			for _, a := range v.Attr {
				if a.Name.Space != "" || a.Name.Local == "xmlns" || strings.Contains(a.Name.Local, ":") || seen[a.Name] {
					return nil, pciInvalid("foreign namespace or duplicate node-device XML attribute")
				}
				seen[a.Name] = true
			}
			n := &xmlNode{name: v.Name, attrs: v.Attr}
			if len(stack) == 0 {
				if root != nil {
					return nil, pciInvalid("multiple node-device XML roots")
				}
				root = n
			} else {
				parent := stack[len(stack)-1].node
				parent.children = append(parent.children, n)
			}
			stack = append(stack, &frame{node: n})
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, pciInvalid("unbalanced node-device XML")
			}
			last := stack[len(stack)-1]
			last.node.text = last.text.String()
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return nil, pciInvalid("text outside node-device XML")
				}
			} else {
				stack[len(stack)-1].text.Write(v)
			}
		case xml.Directive:
			return nil, pciInvalid("XML directives are forbidden")
		case xml.ProcInst:
			if v.Target != "xml" || root != nil {
				return nil, pciInvalid("XML processing instructions are forbidden")
			}
		}
	}
	if root == nil || len(stack) != 0 || root.name.Local != "device" || !onlyAttrs(root, nil, nil) {
		return nil, pciInvalid("invalid node-device XML root")
	}
	return root, nil
}

func pciUniqueChild(parent *xmlNode, name string, required bool) (*xmlNode, error) {
	var found *xmlNode
	for _, n := range parent.children {
		if n.name.Local != name {
			continue
		}
		if found != nil {
			return nil, pciInvalid("duplicate " + name + " field")
		}
		found = n
	}
	if required && found == nil {
		return nil, pciInvalid("missing " + name + " field")
	}
	return found, nil
}

func pciText(n *xmlNode, limit int, required bool) (string, error) {
	if n == nil || len(n.children) != 0 || len(n.attrs) != 0 {
		return "", pciInvalid("invalid scalar field")
	}
	value := strings.TrimSpace(n.text)
	if len(value) > limit || !utf8.ValidString(value) || required && value == "" {
		return "", pciInvalid("invalid scalar text")
	}
	for _, r := range n.text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", pciInvalid("control character in node-device text")
		}
	}
	return value, nil
}

func parsePCIDevice(nativeName, data string) (domain.PCIDevice, error) {
	out := domain.PCIDevice{GroupMembers: []string{}}
	root, err := pciXMLTree(data)
	if err != nil {
		return out, err
	}
	n, err := pciUniqueChild(root, "name", true)
	if err != nil {
		return out, err
	}
	out.Name, err = pciText(n, 256, true)
	if err != nil {
		return out, err
	}
	if nativeName != out.Name {
		return out, pciInvalid("native and XML node-device names disagree")
	}
	driver, err := pciUniqueChild(root, "driver", false)
	if err != nil {
		return out, err
	}
	if driver != nil {
		if !onlyAttrs(driver, nil, nil) || len(driver.children) != 1 {
			return out, pciInvalid("invalid driver observation")
		}
		n, err = pciUniqueChild(driver, "name", true)
		if err != nil {
			return out, err
		}
		out.Driver, err = pciText(n, 256, true)
		if err != nil {
			return out, err
		}
	}
	capability, err := pciUniqueChild(root, "capability", true)
	if err != nil {
		return out, err
	}
	if !onlyAttrs(capability, []string{"type"}, nil) || attr(capability, "type") != "pci" {
		return out, pciInvalid("native PCI device lacks a unique PCI capability")
	}
	var address [4]uint64
	for i, name := range []string{"domain", "bus", "slot", "function"} {
		n, err = pciUniqueChild(capability, name, true)
		if err != nil {
			return out, err
		}
		value, err := pciText(n, 16, true)
		if err != nil {
			return out, err
		}
		number, ok := pciNumber(value, []uint64{65535, 255, 31, 7}[i])
		if !ok {
			return out, pciInvalid("invalid PCI " + name)
		}
		address[i] = number
	}
	out.Address = fmt.Sprintf("%04x:%02x:%02x.%x", address[0], address[1], address[2], address[3])
	for _, field := range []struct {
		name      string
		id, label *string
	}{{"vendor", &out.VendorID, &out.Vendor}, {"product", &out.ProductID, &out.Product}} {
		n, err = pciUniqueChild(capability, field.name, true)
		if err != nil {
			return out, err
		}
		id := attr(n, "id")
		if len(n.attrs) != 1 || n.attrs[0].Name.Local != "id" || len(id) != 6 || !strings.HasPrefix(id, "0x") {
			return out, pciInvalid("invalid PCI " + field.name + " ID")
		}
		number, ok := pciNumber(id, 65535)
		if !ok {
			return out, pciInvalid("invalid PCI " + field.name + " ID")
		}
		*field.id = fmt.Sprintf("0x%04x", number)
		copy := *n
		copy.attrs = nil
		*field.label, err = pciText(&copy, 1024, false)
		if err != nil {
			return out, err
		}
	}
	group, err := pciUniqueChild(capability, "iommuGroup", false)
	if err != nil {
		return out, err
	}
	if group != nil {
		if !onlyAttrs(group, []string{"number"}, nil) || len(group.children) == 0 || len(group.children) > pciMaxDevices {
			return out, pciInvalid("invalid IOMMU group observation")
		}
		number, err := strconv.ParseUint(attr(group, "number"), 10, 32)
		if err != nil {
			return out, pciInvalid("invalid IOMMU group number")
		}
		groupID := uint32(number)
		out.IOMMUGroup = &groupID
		seen := map[string]bool{}
		for _, member := range group.children {
			if member.name.Local != "address" || !onlyAttrs(member, []string{"domain", "bus", "slot", "function"}, nil) || len(member.children) != 0 {
				return out, pciInvalid("invalid IOMMU member address")
			}
			for i, name := range []string{"domain", "bus", "slot", "function"} {
				number, ok := pciNumber(attr(member, name), []uint64{65535, 255, 31, 7}[i])
				if !ok {
					return out, pciInvalid("invalid IOMMU member " + name)
				}
				address[i] = number
			}
			value := fmt.Sprintf("%04x:%02x:%02x.%x", address[0], address[1], address[2], address[3])
			if seen[value] {
				return out, pciInvalid("duplicate IOMMU member address")
			}
			seen[value] = true
			out.GroupMembers = append(out.GroupMembers, value)
		}
		if !seen[out.Address] {
			return out, pciInvalid("IOMMU group omits its own device")
		}
		sort.Strings(out.GroupMembers)
	}
	numa, err := pciUniqueChild(capability, "numa", false)
	if err != nil {
		return out, err
	}
	if numa != nil {
		if !onlyAttrs(numa, nil, []string{"node"}) || len(numa.children) != 0 {
			return out, pciInvalid("invalid NUMA observation")
		}
		if len(numa.attrs) != 0 {
			number, err := strconv.ParseInt(attr(numa, "node"), 10, 32)
			if err != nil || number < -1 || strings.HasPrefix(attr(numa, "node"), "+") {
				return out, pciInvalid("invalid NUMA node")
			}
			if number >= 0 {
				node := int32(number)
				out.NUMANode = &node
			}
		}
	}
	return out, nil
}
