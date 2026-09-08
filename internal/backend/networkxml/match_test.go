package networkxml

import (
	"strings"
	"testing"
)

func TestMatchKnownLibvirtNormalization(t *testing.T) {
	d := definition()
	x := render(t, d)
	cases := map[string]string{
		"explicit IPv6 no omitted": strings.Replace(x, ` ipv6="no"`, "", 1),
		"bridge defaults omitted":  strings.Replace(x, ` stp="on" delay="0"`, "", 1),
		"kernel MAC table default": strings.Replace(x, `<bridge `, `<bridge macTableManager="kernel" `, 1),
		"IPv4 family omitted":      strings.Replace(x, ` family="ipv4"`, "", 1),
		"equivalent netmask":       strings.Replace(x, `prefix="24"`, `netmask="255.255.255.0"`, 1),
		"default forward mode":     strings.Replace(x, ` mode="nat"`, "", 1),
		"native MAC":               strings.Replace(x, `</network>`, `<mac address="52:54:00:12:ab:34"/></network>`, 1),
		"upper MAC":                strings.Replace(x, `</network>`, `<mac address="52:54:00:12:AB:34"/></network>`, 1),
		"platform MAC":             strings.Replace(x, `</network>`, `<mac address="00:16:3e:12:ab:34"/></network>`, 1),
		"live counter":             strings.Replace(x, `<network `, `<network connections="4294967295" `, 1),
		"zero live counter":        strings.Replace(x, `<network `, `<network connections="0" `, 1),
		"namespace prefix":         strings.ReplaceAll(x, "<virmill:", "<vm:") + "\n",
		"comments and quotes":      "<!-- native description -->\n" + strings.ReplaceAll(x, `"`, `'`),
	}
	// A namespace prefix may change, but its declaration must change with it.
	cases["namespace prefix"] = strings.ReplaceAll(cases["namespace prefix"], "xmlns:virmill=", "xmlns:vm=")
	for name, observed := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Match(observed, d); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Libvirt's schema interleaves these elements; ordering and namespace
	// declaration placement do not weaken the exact semantic predicate.
	got, err := parse(x)
	if err != nil {
		t.Fatal(err)
	}
	var name, uuid string
	for _, n := range got.children {
		if n.name == key("name") {
			name = "  <name>" + n.text + "</name>\n"
		}
		if n.name == key("uuid") {
			uuid = "  <uuid>" + n.text + "</uuid>\n"
		}
	}
	reordered := strings.Replace(strings.Replace(x, name, "", 1), uuid, "", 1)
	reordered = strings.Replace(reordered, "</network>", uuid+name+"</network>", 1)
	reordered = strings.Replace(reordered, `<network `, `<network xmlns:virmill="urn:virmill:v1" `, 1)
	reordered = strings.Replace(reordered, `<virmill:networkCreation xmlns:virmill="urn:virmill:v1"`, `<virmill:networkCreation`, 1)
	if err := Match(reordered, d); err != nil {
		t.Fatal(err)
	}
}

func TestMatchRejectsPolicyAndExtensionDrift(t *testing.T) {
	d := definition()
	x := render(t, d)
	replace := func(old, new string) string {
		t.Helper()
		if !strings.Contains(x, old) {
			t.Fatalf("missing fixture replacement %q", old)
		}
		return strings.Replace(x, old, new, 1)
	}
	add := func(child string) string { return replace("</network>", child+"</network>") }
	cases := map[string]string{
		"name":                     replace(d.Name, "someone-else"),
		"UUID":                     replace("<uuid>"+d.UUID, "<uuid>aaaaaaaa-1234-4234-8234-123456789abc"),
		"name whitespace":          replace("<name>", "<name> "),
		"foreign bridge":           replace(d.Bridge, "eth0"),
		"missing bridge zone":      replace(` zone="trusted"`, ""),
		"zone drift":               replace(`zone="trusted"`, `zone="libvirt"`),
		"STP off":                  replace(`stp="on"`, `stp="off"`),
		"bridge delay":             replace(`delay="0"`, `delay="2"`),
		"managed MAC table":        replace(`<bridge `, `<bridge macTableManager="libvirt" `),
		"IPv6 guest communication": replace(`ipv6="no"`, `ipv6="yes"`),
		"empty IPv6":               replace(`ipv6="no"`, `ipv6=""`),
		"IPv6 address":             add(`<ip family="ipv6" address="fd00::1" prefix="64"/>`),
		"hostdev":                  replace(`mode="nat"`, `mode="hostdev"`),
		"route":                    replace(`mode="nat"`, `mode="route"`),
		"open":                     replace(`mode="nat"`, `mode="open"`),
		"forward dev":              replace(`<forward `, `<forward dev="eth0" `),
		"forward interface":        replace(`</forward>`, `<interface dev="eth0"/></forward>`),
		"forward PF":               replace(`</forward>`, `<pf dev="eth0"/></forward>`),
		"forward PCI":              replace(`</forward>`, `<address type="pci" domain="0" bus="3" slot="0" function="1"/></forward>`),
		"NAT address override":     replace(`</nat>`, `<address start="1.2.3.4" end="1.2.3.4"/></nat>`),
		"NAT IPv6":                 replace(`<nat>`, `<nat ipv6="yes">`),
		"NAT port drift":           replace(`start="1024"`, `start="1025"`),
		"missing NAT port":         replace(`<port start="1024" end="65535"/>`, ""),
		"address":                  replace(`address="192.168.230.1"`, `address="192.168.230.2"`),
		"prefix":                   replace(`prefix="24"`, `prefix="25"`),
		"family":                   replace(`family="ipv4"`, `family="ipv6"`),
		"two masks":                replace(`prefix="24"`, `prefix="24" netmask="255.255.255.0"`),
		"noncontiguous mask":       replace(`prefix="24"`, `netmask="255.0.255.0"`),
		"malformed mask":           replace(`prefix="24"`, `netmask="255.255.255.000"`),
		"wrong mask":               replace(`prefix="24"`, `netmask="255.255.0.0"`),
		"extra static route":       add(`<route address="0.0.0.0" prefix="0" gateway="192.168.230.2"/>`),
		"DHCP start":               replace(`start="192.168.230.2"`, `start="192.168.230.1"`),
		"DHCP end":                 replace(`end="192.168.230.254"`, `end="192.168.230.255"`),
		"DHCP extra range":         replace(`</dhcp>`, `<range start="192.168.230.100" end="192.168.230.101"/></dhcp>`),
		"DHCP bootp":               replace(`</dhcp>`, `<bootp file="pxe"/></dhcp>`),
		"DHCP static host":         replace(`</dhcp>`, `<host mac="52:54:00:11:22:33" ip="192.168.230.20"/></dhcp>`),
		"lease":                    replace(`<range start="192.168.230.2" end="192.168.230.254"/>`, `<range start="192.168.230.2" end="192.168.230.254"><lease expiry="0"/></range>`),
		"TFTP":                     replace(`</ip>`, `<tftp root="/private"/></ip>`),
		"DNS disabled":             add(`<dns enable="no"/>`),
		"DNS forwarder":            add(`<dns><forwarder addr="8.8.8.8"/></dns>`),
		"DNS domain":               add(`<domain name="example.com"/>`),
		"dnsmasq gateway override": add(`<dnsmasq:options xmlns:dnsmasq="http://libvirt.org/schemas/network/dnsmasq/1.0"><dnsmasq:option value="dhcp-option=3,192.168.230.1"/></dnsmasq:options>`),
		"unbound dnsmasq":          add(`<dnsmasq:options><dnsmasq:option value="dhcp-option=3,192.168.230.1"/></dnsmasq:options>`),
		"VLAN":                     add(`<vlan><tag id="20"/></vlan>`),
		"MTU":                      add(`<mtu size="9000"/>`),
		"portgroup":                add(`<portgroup name="override" default="yes"/>`),
		"virtualport":              add(`<virtualport type="openvswitch"/>`),
		"guest filter trust":       replace(`<network `, `<network trustGuestRxFilters="yes" `),
		"port isolation":           add(`<port isolated="yes"/>`),
		"unknown future attribute": replace(`<bridge `, `<bridge safetyMode="changed" `),
		"unknown element":          add(`<future/>`),
		"extra description":        add(`<description>not a supported ownership field</description>`),
		"intent":                   replace(`intent="`, `intent="a`),
		"marker version":           replace(`version="1"`, `version="2"`),
		"marker API":               replace(`apiVersion="virmill/v1"`, `apiVersion="virmill/v2"`),
		"marker namespace":         replace(`urn:virmill:v1`, `urn:other:v1`),
		"marker text":              replace(`</metadata>`, `not-blank</metadata>`),
		"marker child":             replace(`</metadata>`, `<unknown/></metadata>`),
		"marker attr":              replace(`intent="`, `extra="yes" intent="`),
		"root text":                replace(`</network>`, `not-blank</network>`),
	}
	for name, observed := range cases {
		t.Run(name, func(t *testing.T) { code(t, Match(observed, d), "SOURCE_CHANGED") })
	}
}

func TestMatchRejectsAmbiguousAndMalformedXML(t *testing.T) {
	d := definition()
	x := render(t, d)
	add := func(child string) string { return strings.Replace(x, "</network>", child+"</network>", 1) }
	cases := map[string]string{
		"empty": "", "truncated": x[:len(x)-12], "two roots": x + x,
		"text outside": x + "junk", "DTD": `<!DOCTYPE network [<!ENTITY x "abc">]>` + x,
		"entity":                            strings.Replace(x, "<name>", "<name>&missing;", 1),
		"processing instruction":            `<?change policy?>` + x,
		"XML declaration unsupported":       `<?xml version="1.0"?>` + x,
		"attribute duplicate same":          strings.Replace(x, `ipv6="no"`, `ipv6="no" ipv6="no"`, 1),
		"attribute duplicate conflict":      strings.Replace(x, `ipv6="no"`, `ipv6="no" ipv6="yes"`, 1),
		"duplicate bridge":                  add(`<bridge name="eth0" zone="trusted"/>`),
		"duplicate metadata":                add(`<metadata/>`),
		"duplicate marker different prefix": strings.Replace(x, "</metadata>", `<other:networkCreation xmlns:other="urn:virmill:v1"/></metadata>`, 1),
		"expanded attribute duplicate":      strings.Replace(x, `<network `, `<network xmlns:a="urn:virmill:v1" xmlns:b="urn:virmill:v1" a:x="1" b:x="2" `, 1),
		"duplicate namespace":               strings.Replace(x, `xmlns:virmill="urn:virmill:v1"`, `xmlns:virmill="urn:virmill:v1" xmlns:virmill="urn:wrong"`, 1),
		"reserved namespace rebind":         strings.Replace(x, `<network `, `<network xmlns:xml="urn:virmill:v1" `, 1),
		"reserved xmlns rebind":             strings.Replace(x, `<network `, `<network xmlns:xmlns="urn:virmill:v1" `, 1),
		"empty prefixed namespace":          strings.Replace(x, `<network `, `<network xmlns:bad="" `, 1),
		"namespace local spoof":             strings.Replace(x, `<bridge `, `<bridge xmlns="urn:virmill:v1" `, 1),
		"deep":                              strings.Repeat("<x>", 9) + strings.Repeat("</x>", 9),
		"too many nodes":                    "<network>" + strings.Repeat("<x><y/></x>", 32) + "</network>",
		"too large":                         strings.Repeat(" ", maxXMLBytes) + x,
		"bad UTF8":                          strings.Replace(x, "<name>", "<name>\xff", 1),
		"counter negative":                  strings.Replace(x, `<network `, `<network connections="-1" `, 1),
		"counter leading zero":              strings.Replace(x, `<network `, `<network connections="01" `, 1),
		"counter overflow":                  strings.Replace(x, `<network `, `<network connections="4294967296" `, 1),
		"counter empty":                     strings.Replace(x, `<network `, `<network connections="" `, 1),
	}
	for _, mac := range []string{"00:00:00:00:00:00", "ff:ff:ff:ff:ff:ff", "01:00:00:11:22:33", "52-54-00-11-22-33", "5254.0011.2233", "52:54:00:11:22:33:44:55", "garbage"} {
		cases["bad MAC "+mac] = add(`<mac address="` + mac + `"/>`)
	}
	cases["two MACs"] = add(`<mac address="52:54:00:11:22:33"/><mac address="52:54:00:11:22:34"/>`)
	cases["MAC extra attr"] = add(`<mac address="52:54:00:11:22:33" extra="bad"/>`)
	cases["MAC nested"] = add(`<mac address="52:54:00:11:22:33"><unknown/></mac>`)
	for name, observed := range cases {
		t.Run(name, func(t *testing.T) { code(t, Match(observed, d), "SOURCE_CHANGED") })
	}
}

func TestMatchLabDoesNotPermitNATOrDHCPInjection(t *testing.T) {
	d := definition()
	d.Type, d.Egress, d.AdvertiseDefaultRoute = "lab", "none", false
	x := render(t, d)
	code(t, Match(strings.Replace(x, "</network>", `<forward mode="nat"/></network>`, 1), d), "SOURCE_CHANGED")
	code(t, Match(strings.Replace(x, "</network>", `<forward mode="none"/></network>`, 1), d), "SOURCE_CHANGED")
	d.DHCPEnabled = false
	x = render(t, d)
	code(t, Match(strings.Replace(x, "</ip>", `<dhcp><range start="192.168.230.2" end="192.168.230.254"/></dhcp></ip>`, 1), d), "SOURCE_CHANGED")
}

func FuzzMatch(f *testing.F) {
	d := definition()
	f.Add(render(f, d))
	d.Type, d.Egress, d.AdvertiseDefaultRoute = "lab", "none", false
	f.Add(render(f, d))
	f.Add(`<network xmlns:x="urn:virmill:v1"><metadata><x:networkCreation/><x:networkCreation/></metadata></network>`)
	f.Add(`<!DOCTYPE network><network/>`)
	f.Add("\xff")
	f.Fuzz(func(t *testing.T, observed string) {
		for _, kind := range []string{"nat", "lab"} {
			def := definition()
			if kind == "lab" {
				def.Type, def.Egress, def.AdvertiseDefaultRoute = "lab", "none", false
			}
			first, second := Match(observed, def), Match(observed, def)
			if (first == nil) != (second == nil) || first != nil && first.Error() != second.Error() {
				t.Fatal("nondeterministic XML predicate")
			}
		}
	})
}
