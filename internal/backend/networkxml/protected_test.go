package networkxml

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func protectedDefinition(kind string, dhcp bool) domain.NetworkDefinition {
	d := definition()
	d.Type, d.HostAccess, d.DHCPEnabled = kind, "services-only", dhcp
	d.AdvertiseDefaultRoute = kind == "nat" && dhcp
	if kind != "nat" {
		d.Egress = "none"
	}
	if kind == "guest-only" {
		d.HostAccess, d.IPv4CIDR = "deny", ""
	}
	return d
}

func TestVersionOneXMLAndIntentRemainByteExact(t *testing.T) {
	for _, tc := range []struct {
		kind           string
		dhcp           bool
		intent, xmlSHA string
	}{
		{"nat", false, "1010e81e253434eac9c76a26a10a6d8d4cd25bdec8f0f6b4535b7a2d37184fae", "f0a1ee1572d450329efde4920110e5d75d697c7d54fa30b37789c4b4258d3a18"},
		{"nat", true, "7f61eba658c1363a2dd0c92eb0077330600968877ac9eb82287cf42b1e4215d8", "822abcdfed0dfc3728849e9e45063741d7a29b277190e93956e89bf50de028f3"},
		{"lab", false, "e70bb40ed8a3f4f007604165fdff7cfde325d5a4a08912dd86264fa8de9da3b1", "15ce152da24987a0744f524fd3f29fdd8be3d4822db40d3e4bff6c2fdcdb62cc"},
		{"lab", true, "63fdff035f91c75c9d206ccb4de0c7f545193da8c05d8331cd2c0372db95cf02", "1028062fef34cbb6205b35c7d52f60840231a206b56febc4ad9d006760f04021"},
	} {
		t.Run(fmt.Sprintf("%s/dhcp=%t", tc.kind, tc.dhcp), func(t *testing.T) {
			d := protectedDefinition(tc.kind, tc.dhcp)
			d.HostAccess = "allow"
			raw := render(t, d)
			digest, err := intentDigest(d)
			if err != nil || digest != tc.intent || fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) != tc.xmlSHA {
				t.Fatal("legacy allowed-host XML or intent bytes changed", digest, err)
			}
			if strings.Contains(raw, "<dns") || !strings.Contains(raw, `version="1"`) || Match(raw, d) != nil {
				t.Fatal("legacy policy semantics changed")
			}
		})
	}
}

func TestProtectedProfilesServiceAndAddressDeclarations(t *testing.T) {
	for _, kind := range []string{"nat", "lab", "guest-only"} {
		for _, dhcp := range []bool{false, true} {
			if kind == "guest-only" && dhcp {
				continue
			}
			t.Run(fmt.Sprintf("%s/dhcp=%t", kind, dhcp), func(t *testing.T) {
				d := protectedDefinition(kind, dhcp)
				for _, cidr := range []string{"", "192.168.230.0/24"} {
					if kind != "guest-only" && cidr == "" {
						continue
					}
					d.IPv4CIDR = cidr
					raw := render(t, d)
					if d.PolicyVersion() != 2 || !strings.Contains(raw, `version="2"`) || Match(raw, d) != nil || raw != render(t, d) {
						t.Fatal("protected profile did not render deterministically with version2")
					}
					var doc struct {
						IP []struct {
							Address string     `xml:"address,attr"`
							DHCP    []struct{} `xml:"dhcp"`
						} `xml:"ip"`
						Forward []struct{} `xml:"forward"`
						DNS     []struct {
							Enable string `xml:"enable,attr"`
						} `xml:"dns"`
					}
					if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
						t.Fatal(err)
					}
					wantDNS := "no"
					if dhcp {
						wantDNS = "yes"
					}
					if len(doc.DNS) != 1 || doc.DNS[0].Enable != wantDNS || (len(doc.Forward) == 1) != (kind == "nat") {
						t.Fatal("wrong explicit DNS or forwarding", raw)
					}
					if kind == "guest-only" {
						if len(doc.IP) != 0 || strings.Contains(raw, "<dhcp") || strings.Contains(raw, "<forward") || strings.Contains(raw, "192.168.230.") {
							t.Fatal("logical guest reservation became host address or service", raw)
						}
					} else if len(doc.IP) != 1 || doc.IP[0].Address != "192.168.230.1" || (len(doc.IP[0].DHCP) == 1) != dhcp {
						t.Fatal("managed DHCP and DNS are not paired", raw)
					}
				}
			})
		}
	}
}

func TestProtectedIntentEnvelopeBindsVersionAndLogicalReservation(t *testing.T) {
	d := protectedDefinition("guest-only", false)
	raw := render(t, d)
	for _, changed := range []string{"192.168.230.0/24", "10.0.0.0/8"} {
		other := d
		other.IPv4CIDR = changed
		code(t, Match(raw, other), "SOURCE_CHANGED")
		hash, _ := intentDigest(other)
		// Construct the published field ordering independently of intentDigest.
		definitionJSON, err := json.Marshal(other)
		if err != nil {
			t.Fatal(err)
		}
		envelope := `{"apiVersion":"virmill/v1","version":2,"definition":` + string(definitionJSON) + `}`
		if hash != fmt.Sprintf("%x", sha256.Sum256([]byte(envelope))) {
			t.Fatal("wrong version2 intent envelope")
		}
	}
	code(t, Match(strings.Replace(raw, `version="2"`, `version="1"`, 1), d), "SOURCE_CHANGED")
	allowed := definition()
	services := allowed
	services.HostAccess = "services-only"
	code(t, Match(render(t, allowed), services), "SOURCE_CHANGED")
	code(t, Match(render(t, services), allowed), "SOURCE_CHANGED")
}

func TestProtectedProfilesRejectContradictionsAndLogicalCIDRInjection(t *testing.T) {
	for _, kind := range []string{"nat", "lab", "guest-only"} {
		for _, cidr := range []string{"8.8.8.0/24", "172.0.0.0/8", "10.0.0.1/24", "10.0.0.0/31", "fd00::/64", "::ffff:10.0.0.0/104", "10.0.0.0/08", " 10.0.0.0/8", `10.0.0.0/8"/><ip address="8.8.8.8`} {
			t.Run(kind+"/"+cidr, func(t *testing.T) {
				d := protectedDefinition(kind, false)
				d.IPv4CIDR = cidr
				x, err := Render(d)
				code(t, err, "INVALID_INPUT")
				if x != "" {
					t.Fatal("invalid reservation emitted XML")
				}
			})
		}
	}
	for name, edit := range map[string]func(*domain.NetworkDefinition){
		"DHCP":          func(d *domain.NetworkDefinition) { d.DHCPEnabled = true },
		"default route": func(d *domain.NetworkDefinition) { d.AdvertiseDefaultRoute = true },
		"forwarding":    func(d *domain.NetworkDefinition) { d.Egress = "any" },
		"host allow":    func(d *domain.NetworkDefinition) { d.HostAccess = "allow" },
		"host services": func(d *domain.NetworkDefinition) { d.HostAccess = "services-only" },
		"implicit host": func(d *domain.NetworkDefinition) { d.HostAccess = "" },
		"IPv6":          func(d *domain.NetworkDefinition) { d.IPv6Mode = "routed" },
	} {
		t.Run("guest-only/"+name, func(t *testing.T) {
			d := protectedDefinition("guest-only", false)
			edit(&d)
			x, err := Render(d)
			code(t, err, "UNSUPPORTED_CAPABILITY")
			if x != "" {
				t.Fatal("contradictory guest policy emitted XML")
			}
		})
	}
	for _, kind := range []string{"nat", "lab"} {
		d := protectedDefinition(kind, false)
		d.IPv4CIDR = ""
		code(t, Validate(d), "INVALID_INPUT")
		d = protectedDefinition(kind, true)
		d.AdvertiseDefaultRoute = kind != "nat"
		code(t, Validate(d), "UNSUPPORTED_CAPABILITY")
	}
}

func TestProtectedMatchRejectsServiceAndNativePolicyDrift(t *testing.T) {
	for _, d := range []domain.NetworkDefinition{protectedDefinition("nat", true), protectedDefinition("nat", false), protectedDefinition("lab", true), protectedDefinition("lab", false), protectedDefinition("guest-only", false)} {
		t.Run(fmt.Sprintf("%s/dhcp=%t", d.Type, d.DHCPEnabled), func(t *testing.T) {
			x := render(t, d)
			dns := `<dns enable="no"/>`
			opposite := `<dns enable="yes"/>`
			if d.DHCPEnabled {
				dns, opposite = opposite, dns
			}
			for name, observed := range map[string]string{
				"missing explicit DNS":    strings.Replace(x, dns, "", 1),
				"implicit DNS":            strings.Replace(x, dns, `<dns/>`, 1),
				"service toggle":          strings.Replace(x, dns, opposite, 1),
				"duplicate DNS":           strings.Replace(x, dns, dns+opposite, 1),
				"duplicate enable":        strings.Replace(x, dns, `<dns enable="no" enable="yes"/>`, 1),
				"namespaced DNS":          strings.Replace(x, dns, `<dns xmlns="urn:virmill:v1" enable="no"/>`, 1),
				"DNS child even disabled": strings.Replace(x, dns, `<dns enable="no"><forwarder addr="8.8.8.8"/></dns>`, 1),
				"DNS attr":                strings.Replace(x, dns, `<dns enable="yes" forwardPlainNames="no"/>`, 1),
				"dnsmasq option":          strings.Replace(x, "</network>", `<dnsmasq:options xmlns:dnsmasq="http://libvirt.org/schemas/network/dnsmasq/1.0"><dnsmasq:option value="dhcp-option=3,192.168.230.1"/></dnsmasq:options></network>`, 1),
				"host IP injection":       strings.Replace(x, "</network>", `<ip address="192.168.230.1" prefix="24"/></network>`, 1),
				"host IPv6 injection":     strings.Replace(x, "</network>", `<ip family="ipv6" address="fd00::1" prefix="64"/></network>`, 1),
				"forward injection":       strings.Replace(x, "</network>", `<forward mode="open"/></network>`, 1),
				"hostdev injection":       strings.Replace(x, "</network>", `<forward mode="hostdev"><pf dev="eth0"/></forward></network>`, 1),
				"route injection":         strings.Replace(x, "</network>", `<route address="0.0.0.0" prefix="0" gateway="192.168.230.1"/></network>`, 1),
				"v1 marker":               strings.Replace(x, `version="2"`, `version="1"`, 1),
				"duplicate marker":        strings.Replace(x, "</metadata>", `<x:networkCreation xmlns:x="urn:virmill:v1" version="2"/></metadata>`, 1),
			} {
				t.Run(name, func(t *testing.T) { code(t, Match(observed, d), "SOURCE_CHANGED") })
			}
			// Existing harmless normalization remains valid under the v2 marker.
			for _, observed := range []string{strings.Replace(x, ` ipv6="no"`, "", 1), strings.Replace(x, ` stp="on" delay="0"`, "", 1), strings.Replace(x, "</network>", `<mac address="52:54:00:12:34:56"/></network>`, 1), strings.Replace(x, `<network `, `<network connections="0" `, 1)} {
				if err := Match(observed, d); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func FuzzProtectedMatch(f *testing.F) {
	profiles := []domain.NetworkDefinition{protectedDefinition("nat", true), protectedDefinition("nat", false), protectedDefinition("lab", true), protectedDefinition("lab", false), protectedDefinition("guest-only", false)}
	for _, d := range profiles {
		f.Add(render(f, d))
	}
	f.Add(`<network><dns enable="no" enable="yes"/></network>`)
	f.Fuzz(func(t *testing.T, raw string) {
		for _, d := range profiles {
			a, b := Match(raw, d), Match(raw, d)
			if (a == nil) != (b == nil) || a != nil && a.Error() != b.Error() {
				t.Fatal("nondeterministic protected XML match")
			}
		}
	})
}
