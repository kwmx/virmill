// Package networkxml renders and checks only newly owned managed networks.
// It is a declaration predicate, not evidence of packet isolation or ownership.
package networkxml

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"virmill.local/core/internal/domain"
)

const markerNamespace = "urn:virmill:v1"

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Validate refuses policy that this fixed native profile cannot express. The
// caller must independently reserve identities and check host/pool overlaps.
func Validate(d domain.NetworkDefinition) error {
	if !uuidPattern.MatchString(d.UUID) || d.UUID == "00000000-0000-0000-0000-000000000000" {
		return domain.Fail("INVALID_INPUT", "network UUID must be canonical lowercase and nonzero")
	}
	if d.Name != "virmill-"+d.UUID || d.Bridge != "vm"+strings.ReplaceAll(d.UUID, "-", "")[:12] {
		return domain.Fail("INVALID_INPUT", "network name and bridge must match their reserved UUID")
	}
	// A guest-only prefix is a logical reservation, never a bridge address.
	// Other profiles require an explicit subnet for their host-side service IP.
	if d.IPv4CIDR != "" || d.Type != "guest-only" {
		p, err := netip.ParsePrefix(d.IPv4CIDR)
		if err != nil || !p.Addr().Is4() || p.Bits() < 8 || p.Bits() > 30 || p != p.Masked() || p.String() != d.IPv4CIDR {
			return domain.Fail("INVALID_INPUT", "network IPv4 CIDR must be canonical and masked with prefix /8 through /30")
		}
		private := false
		for _, scope := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			r := netip.MustParsePrefix(scope)
			if p.Bits() >= r.Bits() && r.Contains(p.Addr()) {
				private = true
			}
		}
		if !private {
			return domain.Fail("INVALID_INPUT", "network IPv4 CIDR must fit entirely in RFC 1918 private space")
		}
	}
	if d.IPv6Mode != "disabled" || d.PolicyVersion() == 0 {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "managed network creation requires explicit IPv6 disabled and a supported host-access profile")
	}
	switch d.Type {
	case "nat":
		if d.Egress != "any" || d.AdvertiseDefaultRoute != d.DHCPEnabled {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "NAT requires egress any and advertises a default route exactly when DHCP is enabled")
		}
	case "lab":
		if d.Egress != "none" || d.AdvertiseDefaultRoute {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "lab requires egress none and no advertised DHCP default route")
		}
	case "guest-only":
		if d.Egress != "none" || d.DHCPEnabled || d.AdvertiseDefaultRoute {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "guest-only requires egress none without managed DHCP/DNS or a default route")
		}
	default:
		return domain.Fail("UNSUPPORTED_CAPABILITY", "managed network creation supports only NAT, lab and guest-only profiles")
	}
	return nil
}

// Render returns deterministic libvirt network XML with no caller-supplied XML
// or host interface. The adapter defines it before separately activating it.
func Render(d domain.NetworkDefinition) (string, error) {
	if err := Validate(d); err != nil {
		return "", err
	}
	digest, err := intentDigest(d)
	if err != nil {
		return "", err
	}
	// All substitutions are validated fixed-grammar identifiers, addresses or a
	// lowercase digest; no arbitrary string is interpolated into XML.
	var b strings.Builder
	fmt.Fprintf(&b, "<network ipv6=\"no\">\n  <name>%s</name>\n  <uuid>%s</uuid>\n", d.Name, d.UUID)
	fmt.Fprintf(&b, "  <metadata><virmill:networkCreation xmlns:virmill=\"%s\" apiVersion=\"virmill/v1\" version=\"%d\" intent=\"%s\"/></metadata>\n", markerNamespace, d.PolicyVersion(), digest)
	if d.Type == "nat" {
		b.WriteString("  <forward mode=\"nat\"><nat><port start=\"1024\" end=\"65535\"/></nat></forward>\n")
	}
	fmt.Fprintf(&b, "  <bridge name=\"%s\" zone=\"trusted\" stp=\"on\" delay=\"0\"/>\n", d.Bridge)
	if d.PolicyVersion() == 2 {
		enable := "no"
		if d.DHCPEnabled {
			enable = "yes"
		}
		fmt.Fprintf(&b, "  <dns enable=\"%s\"/>\n", enable)
	}
	if d.Type == "guest-only" {
		b.WriteString("</network>\n")
		return b.String(), nil
	}
	p := netip.MustParsePrefix(d.IPv4CIDR)
	bridge, first, last := addresses(p)
	fmt.Fprintf(&b, "  <ip family=\"ipv4\" address=\"%s\" prefix=\"%d\">", bridge, p.Bits())
	if d.DHCPEnabled {
		fmt.Fprintf(&b, "\n    <dhcp><range start=\"%s\" end=\"%s\"/></dhcp>\n  ", first, last)
	}
	b.WriteString("</ip>\n</network>\n")
	return b.String(), nil
}

func addresses(p netip.Prefix) (bridge, first, last string) {
	a := p.Addr().As4()
	base := binary.BigEndian.Uint32(a[:])
	addr := func(n uint32) string {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], n)
		return netip.AddrFrom4(b).String()
	}
	return addr(base + 1), addr(base + 2), addr(base + (uint32(1) << (32 - p.Bits())) - 2)
}

func intentDigest(d domain.NetworkDefinition) (string, error) {
	b, err := json.Marshal(struct {
		APIVersion string                   `json:"apiVersion"`
		Version    int                      `json:"version"`
		Definition domain.NetworkDefinition `json:"definition"`
	}{domain.APIVersion, d.PolicyVersion(), d})
	if err != nil {
		return "", domain.Fail("INVALID_INPUT", "network intent cannot be encoded")
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}
