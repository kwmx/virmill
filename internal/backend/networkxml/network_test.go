package networkxml

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func definition() domain.NetworkDefinition {
	return domain.NetworkDefinition{
		UUID: "12345678-1234-4234-8234-123456789abc", Name: "virmill-12345678-1234-4234-8234-123456789abc", Bridge: "vm123456781234",
		Type: "nat", IPv4CIDR: "192.168.230.0/24", DHCPEnabled: true, AdvertiseDefaultRoute: true,
		IPv6Mode: "disabled", HostAccess: "allow", Egress: "any",
	}
}

func render(t testing.TB, d domain.NetworkDefinition) string {
	t.Helper()
	x, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func code(t testing.TB, err error, want string) {
	t.Helper()
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != want {
		t.Fatalf("got %v; want %s", err, want)
	}
}

func TestValidateRefusesInvalidIdentitiesAndCIDRs(t *testing.T) {
	cases := []struct {
		name string
		edit func(*domain.NetworkDefinition)
	}{
		{"empty UUID", func(d *domain.NetworkDefinition) { d.UUID = "" }},
		{"zero UUID", func(d *domain.NetworkDefinition) { d.UUID = "00000000-0000-0000-0000-000000000000" }},
		{"uppercase UUID", func(d *domain.NetworkDefinition) { d.UUID = strings.ToUpper(d.UUID) }},
		{"UUID injection", func(d *domain.NetworkDefinition) { d.UUID += `"/><forward mode="open"/>` }},
		{"friendly native name", func(d *domain.NetworkDefinition) { d.Name = "internet" }},
		{"name injection", func(d *domain.NetworkDefinition) { d.Name += `</name><forward mode="open"/>` }},
		{"unreserved bridge", func(d *domain.NetworkDefinition) { d.Bridge = "eth0" }},
		{"bridge injection", func(d *domain.NetworkDefinition) { d.Bridge += `" zone="public` }},
		{"empty CIDR", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "" }},
		{"host bits", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "192.168.230.1/24" }},
		{"public", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "8.0.0.0/8" }},
		{"loopback", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "127.0.0.0/8" }},
		{"CGNAT", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "100.64.0.0/10" }},
		{"link local", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "169.254.0.0/16" }},
		{"crosses private boundary", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "172.0.0.0/8" }},
		{"crosses second private boundary", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "192.168.0.0/15" }},
		{"too broad", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "10.0.0.0/7" }},
		{"no DHCP space", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "10.0.0.0/31" }},
		{"IPv6", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "fd00::/8" }},
		{"mapped IPv4", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "::ffff:10.0.0.0/104" }},
		{"noncanonical prefix", func(d *domain.NetworkDefinition) { d.IPv4CIDR = "10.0.0.0/08" }},
		{"whitespace", func(d *domain.NetworkDefinition) { d.IPv4CIDR = " 10.0.0.0/8" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := definition()
			tc.edit(&d)
			code(t, Validate(d), "INVALID_INPUT")
			x, err := Render(d)
			code(t, err, "INVALID_INPUT")
			if x != "" {
				t.Fatal("invalid definition produced XML")
			}
			code(t, Match("", d), "INVALID_INPUT")
		})
	}
}

func TestValidateRefusesUnsupportedPolicy(t *testing.T) {
	cases := []struct {
		name string
		edit func(*domain.NetworkDefinition)
	}{
		{"unknown type", func(d *domain.NetworkDefinition) { d.Type = "bridge" }},
		{"internet-only", func(d *domain.NetworkDefinition) { d.Egress = "internet-only" }},
		{"NAT no egress", func(d *domain.NetworkDefinition) { d.Egress = "none" }},
		{"NAT DHCP without route", func(d *domain.NetworkDefinition) { d.AdvertiseDefaultRoute = false }},
		{"route without DHCP", func(d *domain.NetworkDefinition) { d.DHCPEnabled = false }},
		{"host denied NAT", func(d *domain.NetworkDefinition) { d.HostAccess = "deny" }},
		{"guest only", func(d *domain.NetworkDefinition) { d.HostAccess = "guest-only" }},
		{"implicit host policy", func(d *domain.NetworkDefinition) { d.HostAccess = "" }},
		{"IPv6 NAT", func(d *domain.NetworkDefinition) { d.IPv6Mode = "nat" }},
		{"implicit IPv6", func(d *domain.NetworkDefinition) { d.IPv6Mode = "" }},
		{"lab forwarding", func(d *domain.NetworkDefinition) { d.Type = "lab"; d.AdvertiseDefaultRoute = false }},
		{"lab default route", func(d *domain.NetworkDefinition) { d.Type = "lab"; d.Egress = "none" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := definition()
			tc.edit(&d)
			code(t, Validate(d), "UNSUPPORTED_CAPABILITY")
			x, err := Render(d)
			code(t, err, "UNSUPPORTED_CAPABILITY")
			if x != "" {
				t.Fatal("unsupported policy produced XML")
			}
		})
	}
}

func TestRenderFixedProfileAndAddressBoundaries(t *testing.T) {
	for _, cidr := range []struct{ prefix, address, first, last string }{
		{"10.0.0.0/8", "10.0.0.1", "10.0.0.2", "10.255.255.254"},
		{"172.16.0.0/12", "172.16.0.1", "172.16.0.2", "172.31.255.254"},
		{"192.168.0.0/16", "192.168.0.1", "192.168.0.2", "192.168.255.254"},
		{"192.168.230.252/30", "192.168.230.253", "192.168.230.254", "192.168.230.254"},
	} {
		for _, kind := range []string{"nat", "lab"} {
			for _, dhcp := range []bool{true, false} {
				t.Run(cidr.prefix+"/"+kind+map[bool]string{true: "/DHCP", false: "/static"}[dhcp], func(t *testing.T) {
					d := definition()
					d.IPv4CIDR, d.Type, d.DHCPEnabled, d.AdvertiseDefaultRoute = cidr.prefix, kind, dhcp, dhcp && kind == "nat"
					if kind == "lab" {
						d.Egress = "none"
					}
					x := render(t, d)
					if x != render(t, d) || Match(x, d) != nil {
						t.Fatal("render is not deterministic or self-matching")
					}
					if !strings.Contains(x, `ipv6="no"`) || !strings.Contains(x, `zone="trusted"`) || !strings.Contains(x, `address="`+cidr.address+`"`) {
						t.Fatal("missing explicit fixed safety/host address setting")
					}
					if strings.Contains(x, "<forward") != (kind == "nat") || strings.Contains(x, "<dhcp>") != dhcp {
						t.Fatal("profile forwarding or DHCP mismatch")
					}
					if dhcp && !strings.Contains(x, `<range start="`+cidr.first+`" end="`+cidr.last+`"/>`) {
						t.Fatal("wrong DHCP host/broadcast exclusion")
					}
				})
			}
		}
	}
}

func TestCanonicalUUIDIsNotLimitedToV4(t *testing.T) {
	d := definition()
	d.UUID = "12345678-1234-1234-1234-123456789abc"
	d.Name = "virmill-" + d.UUID
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
}

func TestMarkerBindsCompleteTypedDefinition(t *testing.T) {
	d := definition()
	x := render(t, d)
	var doc struct {
		Metadata struct {
			Marker struct {
				XMLName xml.Name
				API     string `xml:"apiVersion,attr"`
				Version string `xml:"version,attr"`
				Intent  string `xml:"intent,attr"`
			} `xml:"networkCreation"`
		} `xml:"metadata"`
	}
	if err := xml.Unmarshal([]byte(x), &doc); err != nil {
		t.Fatal(err)
	}
	m := doc.Metadata.Marker
	if m.XMLName.Space != markerNamespace || m.API != domain.APIVersion || m.Version != "1" || len(m.Intent) != 64 {
		t.Fatal("incorrect metadata contract")
	}
	changes := []func(*domain.NetworkDefinition){
		func(v *domain.NetworkDefinition) { v.UUID = "abcd" }, func(v *domain.NetworkDefinition) { v.Name += "x" },
		func(v *domain.NetworkDefinition) { v.Bridge += "x" }, func(v *domain.NetworkDefinition) { v.Type = "lab" },
		func(v *domain.NetworkDefinition) { v.IPv4CIDR = "10.1.0.0/16" }, func(v *domain.NetworkDefinition) { v.DHCPEnabled = false },
		func(v *domain.NetworkDefinition) { v.AdvertiseDefaultRoute = false }, func(v *domain.NetworkDefinition) { v.IPv6Mode = "routed" },
		func(v *domain.NetworkDefinition) { v.HostAccess = "guest-only" }, func(v *domain.NetworkDefinition) { v.Egress = "none" },
	}
	for i, change := range changes {
		other := d
		change(&other)
		hash, err := intentDigest(other)
		if err != nil || hash == m.Intent {
			t.Fatalf("definition field %d missing from digest", i)
		}
	}
	// Updating XML without updating its marker, and updating only its marker,
	// both fail. The reproducible hash itself is not ownership authentication.
	other := d
	other.IPv4CIDR = "10.1.0.0/16"
	changed := render(t, other)
	newHash, _ := intentDigest(other)
	code(t, Match(strings.ReplaceAll(changed, newHash, m.Intent), other), "SOURCE_CHANGED")
	code(t, Match(strings.ReplaceAll(x, m.Intent, newHash), other), "SOURCE_CHANGED")
}
