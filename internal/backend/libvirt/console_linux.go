//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strings"

	"virmill.local/core/internal/domain"
)

const consoleXMLLimit = 2 << 20

var consoleAlias = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

var _ domain.ConsoleInspector = (*Provider)(nil)

// InspectConsole observes the selected local domain through the official,
// read-only libvirt adapter. It never opens an endpoint or changes a device.
// A configured serial device is not evidence of a guest login prompt.
func (p *Provider) InspectConsole(ctx context.Context, uri, id string) (domain.ConsoleInfo, error) {
	return inspectConsole(ctx, uri, id, p.Get)
}

func inspectConsole(ctx context.Context, uri, id string, get func(context.Context, string, string) (domain.VM, error)) (domain.ConsoleInfo, error) {
	if err := ctx.Err(); err != nil {
		return domain.ConsoleInfo{}, err
	}
	if err := Connection(uri); err != nil {
		return domain.ConsoleInfo{}, err
	}
	if !uuidPattern.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" {
		return domain.ConsoleInfo{}, domain.Fail("INVALID_INPUT", "console access requires a canonical nonzero VM UUID")
	}
	v, err := get(ctx, uri, id)
	if ctx.Err() != nil {
		return domain.ConsoleInfo{}, ctx.Err()
	}
	if err != nil {
		return domain.ConsoleInfo{}, domain.Fail("OPERATION_FAILED", "could not read the selected VM console configuration")
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	if v.Key != key || v.Name == "" || !digestPattern.MatchString(v.Fingerprint) {
		return domain.ConsoleInfo{}, domain.Fail("SOURCE_CHANGED", "selected VM identity could not be verified")
	}
	return consoleFromVM(v)
}

func consoleFromVM(v domain.VM) (domain.ConsoleInfo, error) {
	out := domain.ConsoleInfo{Resource: v.Key, Name: v.Name, State: v.State, ConfigFingerprint: v.Fingerprint, Choices: []domain.ConsoleChoice{}, Warnings: []string{}}
	running := v.State == "running"
	raw := v.PersistentXML
	if running || v.LiveXML != "" {
		raw = v.LiveXML
	}
	root, err := consoleXML(raw)
	if err != nil {
		return domain.ConsoleInfo{}, err
	}
	id, err := consoleChild(root, "uuid", true)
	if err != nil || id == nil || len(id.children) != 0 || strings.TrimSpace(id.text) != v.Key.UUID {
		return domain.ConsoleInfo{}, consoleInvalid("VM XML identity differs from the selected VM")
	}
	name, err := consoleChild(root, "name", true)
	if err != nil || name == nil || len(name.children) != 0 || name.text != v.Name {
		return domain.ConsoleInfo{}, consoleInvalid("VM XML name differs from the selected VM")
	}
	devices, err := consoleChild(root, "devices", true)
	if err != nil {
		return domain.ConsoleInfo{}, err
	}
	// Display clients may interpret SPICE monitor ports as power controls.
	// Opaque QEMU command lines may also override the observed transport.
	unsafeDisplay := false
	for _, n := range root.children {
		if n.name.Space != "" && n.name.Local != "metadata" {
			unsafeDisplay = true
		}
	}
	for _, n := range devices.children {
		if n.name.Local == "channel" && attr(n, "type") == "spiceport" {
			unsafeDisplay = true
		}
	}
	serialCount, graphicsCount := 0, 0
	for _, n := range devices.children {
		if n.name.Local == "graphics" {
			graphicsCount++
		}
	}
	aliases := map[string]bool{}
	for _, n := range devices.children {
		if n.name.Local != "graphics" && n.name.Local != "console" {
			continue
		}
		if n.name.Space != "" {
			return domain.ConsoleInfo{}, consoleInvalid("foreign namespace in console devices")
		}
		if n.name.Local == "console" {
			serialCount++
			choice := domain.ConsoleChoice{ID: fmt.Sprintf("serial:%d", serialCount-1), Kind: "serial", Protocol: "serial", Label: fmt.Sprintf("Serial console %d", serialCount), Reason: "Start the VM before opening its console."}
			target, e := consoleChild(n, "target", true)
			if e != nil {
				return domain.ConsoleInfo{}, e
			}
			alias, e := consoleChild(n, "alias", false)
			if e != nil {
				return domain.ConsoleInfo{}, e
			}
			safe := attr(n, "type") == "pty" && (attr(target, "type") == "serial" || attr(target, "type") == "virtio")
			if alias != nil && aliases[attr(alias, "name")] {
				return domain.ConsoleInfo{}, consoleInvalid("duplicate serial console alias")
			}
			if alias != nil && consoleAlias.MatchString(attr(alias, "name")) {
				choice.Device = attr(alias, "name")
				aliases[choice.Device] = true
			} else if running {
				safe = false
			}
			if !safe {
				choice.Reason = "This console has no supported local PTY target; no connection will be opened."
			} else if running {
				choice.Available = true
				choice.Reason = "The guest must provide a serial console; a login prompt is not guaranteed."
			}
			out.Choices = append(out.Choices, choice)
		} else {
			protocol := attr(n, "type")
			choice := domain.ConsoleChoice{ID: fmt.Sprintf("graphics:%d", lenGraphics(out.Choices)), Kind: "graphical", Protocol: protocol, Label: "Graphical display", GraphicsIndex: lenGraphics(out.Choices), Reason: "Start the VM before opening its display."}
			safe := protocol == "spice" || protocol == "vnc"
			if safe {
				safe = consolePrivateGraphics(n)
			}
			if !safe {
				choice.Available = false
				choice.Reason = "Display access requires SPICE or VNC with an explicit local-only listener."
			} else if unsafeDisplay {
				choice.Reason = "Display access is disabled for monitor ports or opaque emulator extensions; a viewer must not control VM power."
			} else if graphicsCount != 1 {
				choice.Reason = "Multiple graphics devices need a viewer that selects an exact device; automatic selection is disabled."
			} else if running {
				choice.Available = true
				choice.Reason = "Opens a local viewer. Desktop integration depends on guest tools and viewer settings."
			}
			out.Choices = append(out.Choices, choice)
		}
	}
	if len(out.Choices) == 0 {
		out.Warnings = append(out.Warnings, "No supported console is configured. Add a serial or private graphical console using your VM configuration tools.")
	}
	if !running {
		out.Warnings = append(out.Warnings, "The VM must be running before a console can be opened.")
	}
	return out, nil
}

func lenGraphics(choices []domain.ConsoleChoice) int {
	n := 0
	for _, c := range choices {
		if c.Kind == "graphical" {
			n++
		}
	}
	return n
}

// Treat missing defaults, hostnames, network listeners and mixed exposure as
// unknown. No DNS resolution or advertised socket path is ever followed here.
// Socket/none are the local libvirt OpenGraphics transports; literal loopback
// addresses are local-only even when a password is not configured.
func consolePrivateGraphics(n *xmlNode) bool {
	// A separate WebSocket listener has its own exposure and is outside this
	// local console adapter. Inactive auto-allocation is not launchable anyway.
	if ws := attr(n, "websocket"); ws != "" && ws != "-1" {
		return false
	}
	explicit := false
	if value := attr(n, "listen"); value != "" {
		if !consoleLoopback(value) {
			return false
		}
		explicit = true
	}
	if value := attr(n, "socket"); value != "" {
		if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\x00\r\n") {
			return false
		}
		explicit = true
	}
	for _, l := range n.children {
		if l.name.Local != "listen" {
			continue
		}
		if l.name.Space != "" {
			return false
		}
		explicit = true
		switch attr(l, "type") {
		case "address":
			if !consoleLoopback(attr(l, "address")) || attr(l, "network") != "" || attr(l, "socket") != "" {
				return false
			}
		case "socket":
			if attr(l, "address") != "" || attr(l, "network") != "" {
				return false
			}
			if s := attr(l, "socket"); s != "" && (!strings.HasPrefix(s, "/") || strings.ContainsAny(s, "\x00\r\n")) {
				return false
			}
		case "none":
			if attr(l, "address") != "" || attr(l, "network") != "" || attr(l, "socket") != "" {
				return false
			}
		default:
			return false
		}
	}
	return explicit
}
func consoleLoopback(s string) bool {
	a, e := netip.ParseAddr(s)
	return e == nil && a.Zone() == "" && a.IsLoopback()
}

func consoleInvalid(reason string) error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", "Cannot inspect console safely: "+reason)
}
func consoleChild(parent *xmlNode, name string, required bool) (*xmlNode, error) {
	var found *xmlNode
	for _, n := range parent.children {
		if n.name.Local == name {
			if n.name.Space != "" || found != nil {
				return nil, consoleInvalid("ambiguous " + name + " element")
			}
			found = n
		}
	}
	if found == nil && required {
		return nil, consoleInvalid("missing " + name + " element")
	}
	return found, nil
}

// Native domain XML may retain foreign metadata. It remains opaque; selected
// identity/device fields must be unqualified and unique. Structure is bounded,
// duplicate attributes and executable XML directives are rejected.
func consoleXML(raw string) (*xmlNode, error) {
	if len(raw) == 0 || len(raw) > consoleXMLLimit {
		return nil, consoleInvalid("empty or oversized domain XML")
	}
	d := xml.NewDecoder(strings.NewReader(raw))
	var root *xmlNode
	var stack []*xmlNode
	nodes := 0
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, consoleInvalid("malformed domain XML")
		}
		switch v := t.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 32768 || len(stack) >= 64 {
				return nil, consoleInvalid("domain XML structure exceeds bounds")
			}
			seen := map[xml.Name]bool{}
			for _, a := range v.Attr {
				if seen[a.Name] {
					return nil, consoleInvalid("duplicate XML attribute")
				}
				seen[a.Name] = true
			}
			n := &xmlNode{name: v.Name, attrs: v.Attr}
			if len(stack) == 0 {
				if root != nil {
					return nil, consoleInvalid("multiple domain XML roots")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.children = append(p.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, consoleInvalid("unbalanced domain XML")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return nil, consoleInvalid("text outside domain XML")
				}
			} else {
				stack[len(stack)-1].text += string(v)
			}
		case xml.Directive:
			return nil, consoleInvalid("XML directives are forbidden")
		case xml.ProcInst:
			if v.Target != "xml" || root != nil {
				return nil, consoleInvalid("XML processing instructions are forbidden")
			}
		}
	}
	if root == nil || len(stack) != 0 || root.name != (xml.Name{Local: "domain"}) {
		return nil, consoleInvalid("invalid domain XML root")
	}
	return root, nil
}
