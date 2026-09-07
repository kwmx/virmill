package network

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// DefinedPrefixes reads only direct libvirt network IP and static-route declarations. Unknown
// configuration is preserved by observation, never rewritten from this view.
func DefinedPrefixes(raw string) ([]netip.Prefix, error) {
	out := []netip.Prefix{}
	if raw == "" || len(raw) > 1<<20 {
		return nil, errors.New("missing or oversized network XML")
	}
	d := xml.NewDecoder(strings.NewReader(raw))
	depth, roots, tokens := 0, 0, 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			if depth != 0 || roots != 1 {
				return nil, errors.New("incomplete network XML")
			}
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		tokens++
		if tokens > 65536 {
			return nil, errors.New("network XML token limit")
		}
		switch v := token.(type) {
		case xml.Directive:
			return nil, errors.New("network XML directives prohibited")
		case xml.StartElement:
			depth++
			if depth > 64 {
				return nil, errors.New("network XML depth limit")
			}
			if depth == 1 {
				roots++
				if roots != 1 || v.Name != (xml.Name{Local: "network"}) {
					return nil, errors.New("expected one unnamespaced network root")
				}
			}
			attrs := map[xml.Name]string{}
			for _, a := range v.Attr {
				if _, exists := attrs[a.Name]; exists {
					return nil, errors.New("duplicate network XML attribute")
				}
				attrs[a.Name] = a.Value
			}
			if depth != 2 || (v.Name.Local != "ip" && v.Name.Local != "route") {
				continue
			}
			if v.Name.Space != "" {
				return nil, errors.New("namespaced network IP declaration")
			}
			for _, a := range v.Attr {
				if a.Name.Space != "" && (a.Name.Local == "address" || a.Name.Local == "netmask" || a.Name.Local == "prefix" || a.Name.Local == "family" || a.Name.Local == "gateway") {
					return nil, errors.New("ambiguous network IP attribute namespace")
				}
			}
			get := func(k string) string { return attrs[xml.Name{Local: k}] }
			for _, k := range []string{"address", "family", "prefix", "netmask", "gateway"} {
				if value, present := attrs[xml.Name{Local: k}]; present && value == "" {
					return nil, errors.New("empty network IP attribute")
				}
			}
			family := get("family")
			if family == "" {
				family = "ipv4"
			}
			address := get("address")
			isRoute := v.Name.Local == "route"
			if isRoute && address == "" {
				address = "0.0.0.0"
				if family == "ipv6" {
					address = "::"
				}
			}
			a, err := netip.ParseAddr(address)
			if err != nil || a.Zone() != "" || a.Is4In6() {
				return nil, errors.New("invalid network IP address")
			}
			if isRoute {
				gateway, err := netip.ParseAddr(get("gateway"))
				if err != nil || gateway.Zone() != "" || gateway.Is4In6() || gateway.Is4() != a.Is4() {
					return nil, errors.New("invalid or missing network route gateway")
				}
			}
			if family != "ipv4" && family != "ipv6" || (family == "ipv4") != a.Is4() {
				return nil, errors.New("network IP family mismatch")
			}
			bits := -1
			if p := get("prefix"); p != "" {
				bits, err = strconv.Atoi(p)
				if err != nil || strconv.Itoa(bits) != p || bits < 0 || bits > a.BitLen() {
					return nil, errors.New("invalid network IP prefix")
				}
			}
			if mask := get("netmask"); mask != "" {
				if bits != -1 || !a.Is4() {
					return nil, errors.New("ambiguous network IP mask")
				}
				m, err := netip.ParseAddr(mask)
				if err != nil || !m.Is4() {
					return nil, errors.New("invalid network IPv4 netmask")
				}
				b := m.As4()
				var width int
				bits, width = net.IPMask(b[:]).Size()
				if width != 32 {
					return nil, errors.New("noncontiguous network IPv4 netmask")
				}
			}
			if bits < 0 && isRoute {
				bits = 0
			} // libvirt documents omitted route mask as default /0.
			if bits < 0 {
				return nil, errors.New("network IP has no explicit prefix or mask; cannot safely infer overlap")
			}
			out = append(out, netip.PrefixFrom(a, bits).Masked())
			if len(out) > 256 {
				return nil, errors.New("network IP declaration limit")
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return nil, fmt.Errorf("text outside network XML")
			}
		}
	}
}
