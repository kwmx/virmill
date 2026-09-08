package networkxml

import (
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"virmill.local/core/internal/domain"
)

const maxXMLBytes = 64 << 10

type element struct {
	name     xml.Name
	attrs    map[xml.Name]string
	text     string
	children []*element
}

// Match checks every observed declaration against the fixed profile. Only
// documented defaults, equivalent IPv4 masks, namespace prefixes, formatting,
// one native-assigned MAC and the live connections counter are normalized.
// It does not attest to runtime firewall/DNS state or authenticate the marker.
func Match(observedXML string, d domain.NetworkDefinition) error {
	wantXML, err := Render(d)
	if err != nil {
		return err
	}
	want, err := parse(wantXML)
	if err != nil {
		return domain.Fail("INTERNAL", "rendered network XML failed its structural predicate")
	}
	got, err := parse(observedXML)
	if err == nil {
		err = normalize(got)
	}
	if err == nil {
		err = normalize(want)
	}
	if err == nil {
		err = equal(got, want)
	}
	if err != nil {
		return domain.Fail("SOURCE_CHANGED", "network XML does not match reserved intent: "+err.Error())
	}
	return nil
}

func parse(raw string) (*element, error) {
	if len(raw) == 0 || len(raw) > maxXMLBytes {
		return nil, fmt.Errorf("XML size is outside the bounded declaration limit")
	}
	d := xml.NewDecoder(strings.NewReader(raw))
	var root *element
	var stack []*element
	nodes := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			if root == nil || len(stack) != 0 {
				return nil, fmt.Errorf("incomplete XML")
			}
			return root, nil
		}
		if err != nil {
			return nil, fmt.Errorf("malformed XML")
		}
		switch t := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 32 || len(stack) >= 8 || len(t.Attr) > 16 {
				return nil, fmt.Errorf("XML structure exceeds the bounded declaration limit")
			}
			n := &element{name: t.Name, attrs: map[xml.Name]string{}}
			seen := map[xml.Name]bool{}
			for _, a := range t.Attr {
				if seen[a.Name] {
					return nil, fmt.Errorf("duplicate XML attribute")
				}
				seen[a.Name] = true
				if a.Name.Space == "xmlns" || a.Name == (xml.Name{Local: "xmlns"}) {
					if (a.Name.Space == "xmlns" && (a.Name.Local == "xml" || a.Name.Local == "xmlns" || a.Value == "" || strings.Contains(a.Name.Local, ":"))) || (a.Value != markerNamespace && a.Value != "") {
						return nil, fmt.Errorf("unsupported XML namespace declaration")
					}
					continue
				}
				n.attrs[a.Name] = a.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("multiple XML roots")
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				for _, sibling := range parent.children {
					if sibling.name == n.name {
						return nil, fmt.Errorf("duplicate XML element")
					}
				}
				parent.children = append(parent.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != t.Name {
				return nil, fmt.Errorf("unbalanced XML")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if !white(string(t)) {
					return nil, fmt.Errorf("text outside network XML")
				}
			} else {
				stack[len(stack)-1].text += string(t)
			}
		case xml.Comment:
			// Comments have no libvirt network semantics.
		default:
			// No DTD/entity declaration or processing instructions are needed by
			// native network XML. Refuse rather than interpreting extensions.
			return nil, fmt.Errorf("unsupported XML directive")
		}
	}
}

func white(s string) bool   { return strings.Trim(s, " \t\r\n") == "" }
func key(s string) xml.Name { return xml.Name{Local: s} }

func normalize(n *element) error {
	if n.name != key("network") {
		return fmt.Errorf("expected an unnamespaced network root")
	}
	if value, ok := n.attrs[key("connections")]; ok {
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil || strconv.FormatUint(v, 10) != value {
			return fmt.Errorf("invalid live connections counter")
		}
		delete(n.attrs, key("connections"))
	}
	if n.attrs[key("ipv6")] == "no" {
		delete(n.attrs, key("ipv6"))
	}
	kept := make([]*element, 0, len(n.children))
	for _, c := range n.children {
		if c.name.Space != "" {
			return fmt.Errorf("unsupported namespaced network element")
		}
		switch c.name.Local {
		case "bridge":
			if _, ok := c.attrs[key("stp")]; !ok {
				c.attrs[key("stp")] = "on"
			}
			if _, ok := c.attrs[key("delay")]; !ok {
				c.attrs[key("delay")] = "0"
			}
			if c.attrs[key("macTableManager")] == "kernel" {
				delete(c.attrs, key("macTableManager"))
			}
		case "mac":
			if len(c.attrs) != 1 || len(c.children) != 0 || !white(c.text) {
				return fmt.Errorf("unsupported native MAC declaration")
			}
			v := c.attrs[key("address")]
			mac, err := net.ParseMAC(v)
			if err != nil || len(mac) != 6 || strings.ToLower(v) != mac.String() || mac[0]&1 != 0 || mac.String() == "00:00:00:00:00:00" {
				return fmt.Errorf("invalid native-assigned bridge MAC")
			}
			continue
		case "forward":
			if _, ok := c.attrs[key("mode")]; !ok {
				c.attrs[key("mode")] = "nat"
			}
		case "ip":
			if _, ok := c.attrs[key("family")]; !ok {
				c.attrs[key("family")] = "ipv4"
			}
			if mask, ok := c.attrs[key("netmask")]; ok {
				if _, exists := c.attrs[key("prefix")]; exists {
					return fmt.Errorf("contradictory IP prefix and netmask")
				}
				a, err := netip.ParseAddr(mask)
				if err != nil || !a.Is4() || a.String() != mask {
					return fmt.Errorf("invalid IPv4 netmask")
				}
				bytes := a.As4()
				v := binary.BigEndian.Uint32(bytes[:])
				count := bits.OnesCount32(v)
				if v != ^uint32(0)<<(32-count) {
					return fmt.Errorf("noncontiguous IPv4 netmask")
				}
				delete(c.attrs, key("netmask"))
				c.attrs[key("prefix")] = strconv.Itoa(count)
			}
		}
		kept = append(kept, c)
	}
	n.children = kept
	return nil
}

func equal(got, want *element) error {
	if got.name != want.name || len(got.attrs) != len(want.attrs) || len(got.children) != len(want.children) {
		return fmt.Errorf("unexpected element or attribute at %s", want.name.Local)
	}
	for k, v := range want.attrs {
		if actual, exists := got.attrs[k]; !exists || actual != v {
			return fmt.Errorf("different %s attribute on %s", k.Local, want.name.Local)
		}
	}
	if want.name == key("name") || want.name == key("uuid") {
		if got.text != want.text {
			return fmt.Errorf("different network identity")
		}
	} else if !white(got.text) {
		return fmt.Errorf("unexpected text in %s", want.name.Local)
	}
	for _, expected := range want.children {
		var actual *element
		for _, c := range got.children {
			if c.name == expected.name {
				actual = c
			}
		}
		if actual == nil {
			return fmt.Errorf("missing %s element", expected.name.Local)
		}
		if err := equal(actual, expected); err != nil {
			return err
		}
	}
	return nil
}
