package network

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func TestDefinedPrefixesIPv4IPv6AndExplicitZero(t *testing.T) {
	cases := []struct {
		name, raw string
		want      []string
	}{
		{"IPv4 netmask", `<network><ip address='192.0.2.129' netmask='255.255.255.128'/></network>`, []string{"192.0.2.128/25"}},
		{"IPv4 prefix", `<network><ip family='ipv4' address='10.9.8.7' prefix='16'/></network>`, []string{"10.9.0.0/16"}},
		{"IPv6 prefix", `<network><ip family='ipv6' address='fd10:abcd:ef12:3::19' prefix='64'/></network>`, []string{"fd10:abcd:ef12:3::/64"}},
		{"IPv4 explicit zero", `<network><ip address='10.9.8.7' prefix='0'/></network>`, []string{"0.0.0.0/0"}},
		{"IPv4 zero netmask", `<network><ip address='10.9.8.7' netmask='0.0.0.0'/></network>`, []string{"0.0.0.0/0"}},
		{"IPv6 explicit zero", `<network><ip family='ipv6' address='fd10::19' prefix='0'/></network>`, []string{"::/0"}},
		{"host prefixes", `<network><ip address='192.0.2.1' prefix='32'/><ip family='ipv6' address='2001:db8::1' prefix='128'/></network>`, []string{"192.0.2.1/32", "2001:db8::1/128"}},
		{"full IPv4 netmask", `<network><ip address='192.0.2.1' netmask='255.255.255.255'/></network>`, []string{"192.0.2.1/32"}},
		{"no host layer three", `<network><name>guest-only</name><bridge name='fixture-bridge'/></network>`, []string{}},
		{"only direct IP and route declarations", `<network><ip address='192.0.2.1' prefix='24'><dhcp><host ip='192.0.2.19'/></dhcp></ip><route address='10.0.0.0' prefix='8' gateway='192.0.2.254'/><metadata><ip address='198.51.100.1' prefix='24'/><route address='172.16.0.0' prefix='12' gateway='192.0.2.254'/></metadata></network>`, []string{"192.0.2.0/24", "10.0.0.0/8"}},
		{"XML declaration and comments", `<?xml version='1.0'?><!--before--><network><!--inside--><ip address='192.0.2.1' prefix='24'/></network><!--after-->`, []string{"192.0.2.0/24"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DefinedPrefixes(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got) != len(tc.want) {
				t.Fatalf("prefixes = %v, want %v", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i].String() != want {
					t.Fatalf("prefix %d = %s, want %s", i, got[i], want)
				}
			}
		})
	}
	// These are declared network allocations, not routing-table defaults.
	// A /0 must therefore continue to block an occupied candidate allocation.
	occupied, err := DefinedPrefixes(`<network><ip address='192.0.2.1' prefix='0'/></network>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Allocate([]netip.Prefix{netip.MustParsePrefix("10.1.0.0/24")}, nil, occupied); err == nil {
		t.Fatal("explicit network /0 was discarded as a default route")
	}
}

func TestDefinedPrefixesKeepsLivePersistentAndOpaqueObservationsSeparate(t *testing.T) {
	// Generated definitions, not a host capture. Arbitrary extension structure
	// must remain opaque and must not contribute invented address declarations.
	const opaque = `<metadata><vendor:state xmlns:vendor='urn:virmill:test:network' exact='yes'> opaque &amp; retained <vendor:ip address='203.0.113.1' prefix='24'/><vendor:route address='203.0.113.0' prefix='24'/></vendor:state></metadata><extra:configuration xmlns:extra='urn:virmill:test:other' policy='keep'><ip address='198.51.100.1' prefix='24'/><route address='198.51.100.0' prefix='24'/></extra:configuration>`
	observed := domain.VirtualNetwork{
		Active: true, Persistent: true, Ownership: "external", IsolationVerification: "not-run",
		LiveXML:       `<network connections='2'><name>fixture</name><ip address='10.12.1.1' netmask='255.255.255.0'/>` + opaque + `</network>`,
		PersistentXML: `<network><name>fixture</name><ip address='10.13.2.1' prefix='24'/><ip family='ipv6' address='fd12:3456:789a:2::1' prefix='64'/>` + opaque + `</network>`,
	}
	before := observed
	live, err := DefinedPrefixes(observed.LiveXML)
	if err != nil {
		t.Fatal(err)
	}
	persistent, err := DefinedPrefixes(observed.PersistentXML)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(live, []netip.Prefix{netip.MustParsePrefix("10.12.1.0/24")}) || !reflect.DeepEqual(persistent, []netip.Prefix{netip.MustParsePrefix("10.13.2.0/24"), netip.MustParsePrefix("fd12:3456:789a:2::/64")}) {
		t.Fatal("live/persistent prefixes or opaque namespace boundaries were conflated", live, persistent)
	}
	if observed != before || !strings.Contains(observed.LiveXML, opaque) || !strings.Contains(observed.PersistentXML, opaque) {
		t.Fatal("prefix inspection changed observed source definitions or ownership")
	}
}

func TestDefinedPrefixesRejectsMalformedAndAmbiguousDeclarations(t *testing.T) {
	wrap := func(ip string) string { return "<network>" + ip + "</network>" }
	cases := map[string]string{
		"empty":                                    "",
		"whitespace only":                          " \n\t",
		"wrong root":                               `<domain/>`,
		"root namespace":                           `<network xmlns='urn:foreign'><ip address='192.0.2.1' prefix='24'/></network>`,
		"prefixed root":                            `<x:network xmlns:x='urn:foreign'/>`,
		"multiple roots":                           `<network/><network/>`,
		"unclosed":                                 `<network>`,
		"mismatched close":                         `<network><ip></network>`,
		"text outside":                             `<network/>outside`,
		"malformed attribute":                      `<network><ip address='192.0.2.1 prefix='24'/></network>`,
		"DTD":                                      `<!DOCTYPE network><network/>`,
		"external entity":                          `<!DOCTYPE network [<!ENTITY host SYSTEM 'file:///never-opened'>]><network><description>&host;</description></network>`,
		"undefined entity":                         `<network><name>&unknown;</name></network>`,
		"invalid Unicode":                          "<network><name>\xff</name></network>",
		"foreign IP element":                       wrap(`<x:ip xmlns:x='urn:foreign' address='192.0.2.1' prefix='24'/>`),
		"foreign IP default namespace":             wrap(`<ip xmlns='urn:foreign' address='192.0.2.1' prefix='24'/>`),
		"foreign address":                          wrap(`<ip xmlns:x='urn:foreign' x:address='192.0.2.1' prefix='24'/>`),
		"foreign prefix":                           wrap(`<ip xmlns:x='urn:foreign' address='192.0.2.1' x:prefix='24'/>`),
		"foreign netmask":                          wrap(`<ip xmlns:x='urn:foreign' address='192.0.2.1' x:netmask='255.255.255.0'/>`),
		"foreign family beside native":             wrap(`<ip xmlns:x='urn:foreign' address='192.0.2.1' family='ipv4' x:family='ipv6' prefix='24'/>`),
		"foreign prefix beside native":             wrap(`<ip xmlns:x='urn:foreign' address='192.0.2.1' prefix='24' x:prefix='16'/>`),
		"duplicate root attribute":                 `<network connections='1' connections='2'/>`,
		"duplicate address":                        wrap(`<ip address='192.0.2.1' address='198.51.100.1' prefix='24'/>`),
		"duplicate prefix":                         wrap(`<ip address='192.0.2.1' prefix='24' prefix='16'/>`),
		"duplicate netmask":                        wrap(`<ip address='192.0.2.1' netmask='255.255.255.0' netmask='255.255.0.0'/>`),
		"duplicate family":                         wrap(`<ip address='192.0.2.1' family='ipv4' family='ipv4' prefix='24'/>`),
		"duplicate expanded namespace attribute":   `<network><metadata><x:state xmlns:x='urn:foreign' xmlns:y='urn:foreign' x:policy='one' y:policy='two'/></metadata></network>`,
		"missing address":                          wrap(`<ip prefix='24'/>`),
		"empty address":                            wrap(`<ip address='' prefix='24'/>`),
		"invalid address":                          wrap(`<ip address='999.0.2.1' prefix='24'/>`),
		"address contains prefix":                  wrap(`<ip address='192.0.2.1/24' prefix='24'/>`),
		"zoned IPv6":                               wrap(`<ip family='ipv6' address='fe80::1%eth0' prefix='64'/>`),
		"mapped IPv6":                              wrap(`<ip family='ipv6' address='::ffff:192.0.2.1' prefix='120'/>`),
		"IPv6 family missing":                      wrap(`<ip address='2001:db8::1' prefix='64'/>`),
		"IPv4 family mismatch":                     wrap(`<ip family='ipv6' address='192.0.2.1' prefix='24'/>`),
		"IPv6 family mismatch":                     wrap(`<ip family='ipv4' address='2001:db8::1' prefix='64'/>`),
		"unknown family":                           wrap(`<ip family='inet' address='192.0.2.1' prefix='24'/>`),
		"explicit empty family":                    wrap(`<ip family='' address='192.0.2.1' prefix='24'/>`),
		"no explicit IPv4 mask":                    wrap(`<ip address='192.0.2.1'/>`),
		"no explicit IPv6 prefix":                  wrap(`<ip family='ipv6' address='2001:db8::1'/>`),
		"empty prefix":                             wrap(`<ip address='192.0.2.1' prefix=''/>`),
		"empty netmask":                            wrap(`<ip address='192.0.2.1' netmask=''/>`),
		"negative prefix":                          wrap(`<ip address='192.0.2.1' prefix='-1'/>`),
		"signed prefix":                            wrap(`<ip address='192.0.2.1' prefix='+24'/>`),
		"leading zero prefix":                      wrap(`<ip address='192.0.2.1' prefix='024'/>`),
		"whitespace prefix":                        wrap(`<ip address='192.0.2.1' prefix=' 24'/>`),
		"IPv4 prefix overflow":                     wrap(`<ip address='192.0.2.1' prefix='33'/>`),
		"IPv6 prefix overflow":                     wrap(`<ip family='ipv6' address='2001:db8::1' prefix='129'/>`),
		"integer prefix overflow":                  wrap(`<ip address='192.0.2.1' prefix='999999999999999999999999999'/>`),
		"noncontiguous mask":                       wrap(`<ip address='192.0.2.1' netmask='255.0.255.0'/>`),
		"short mask":                               wrap(`<ip address='192.0.2.1' netmask='255.255.0'/>`),
		"IPv6 netmask":                             wrap(`<ip family='ipv6' address='2001:db8::1' netmask='ffff:ffff:ffff:ffff::'/>`),
		"IPv4 with IPv6 mask":                      wrap(`<ip address='192.0.2.1' netmask='ffff:ffff:ffff:ffff::'/>`),
		"both masks":                               wrap(`<ip address='192.0.2.1' prefix='24' netmask='255.255.255.0'/>`),
		"empty prefix with netmask":                wrap(`<ip address='192.0.2.1' prefix='' netmask='255.255.255.0'/>`),
		"prefix with empty netmask":                wrap(`<ip address='192.0.2.1' prefix='24' netmask=''/>`),
		"partial result followed by invalid IP":    wrap(`<ip address='192.0.2.1' prefix='24'/><ip address='198.51.100.1'/>`),
		"partial result followed by malformed XML": `<network><ip address='192.0.2.1' prefix='24'/><broken></network>`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := DefinedPrefixes(raw)
			if err == nil || got != nil {
				t.Fatalf("malformed observation returned prefixes %v, error %v", got, err)
			}
		})
	}
}

func TestDefinedPrefixesStaticRoutesAndDocumentedDefaults(t *testing.T) {
	// The route defaults are documented by libvirt's network XML contract:
	// https://libvirt.org/formatnetwork.html#static-routes
	cases := []struct{ name, route, want string }{
		{"IPv4 destination prefix", `<route address='10.9.8.7' prefix='16' gateway='192.0.2.254'/>`, "10.9.0.0/16"},
		{"IPv4 destination netmask", `<route family='ipv4' address='10.9.8.7' netmask='255.255.255.0' gateway='192.0.2.254'/>`, "10.9.8.0/24"},
		{"IPv6 destination", `<route family='ipv6' address='fd10:abcd:3::19' prefix='64' gateway='2001:db8::2'/>`, "fd10:abcd:3::/64"},
		{"IPv4 explicit default", `<route address='0.0.0.0' prefix='0' gateway='192.0.2.254'/>`, "0.0.0.0/0"},
		{"IPv6 explicit default", `<route family='ipv6' address='::' prefix='0' gateway='2001:db8::2'/>`, "::/0"},
		{"IPv4 zero netmask", `<route address='0.0.0.0' netmask='0.0.0.0' gateway='192.0.2.254'/>`, "0.0.0.0/0"},
		{"IPv4 gateway-only default", `<route gateway='192.0.2.254'/>`, "0.0.0.0/0"},
		{"IPv6 gateway-only default", `<route family='ipv6' gateway='2001:db8::2'/>`, "::/0"},
		{"omitted destination", `<route prefix='8' gateway='192.0.2.254'/>`, "0.0.0.0/8"},
		{"omitted mask", `<route address='10.9.8.7' gateway='192.0.2.254'/>`, "0.0.0.0/0"},
		{"IPv4 host destination", `<route address='10.9.8.7' prefix='32' gateway='192.0.2.254' metric='2'/>`, "10.9.8.7/32"},
		{"IPv6 host destination", `<route family='ipv6' address='fd10::19' prefix='128' gateway='2001:db8::2'/>`, "fd10::19/128"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DefinedPrefixes("<network>" + tc.route + "</network>")
			if err != nil || len(got) != 1 || got[0].String() != tc.want {
				t.Fatalf("route prefixes = %v, error %v; want %s", got, err, tc.want)
			}
			if tc.want == "0.0.0.0/0" {
				if _, err = Allocate([]netip.Prefix{netip.MustParsePrefix("10.1.0.0/24")}, nil, got); err == nil {
					t.Fatal("configured route /0 was dropped from occupied declarations")
				}
			}
		})
	}
	observed := domain.VirtualNetwork{Active: false, Persistent: true, Ownership: "external", PersistentXML: `<network><name>inactive-route-fixture</name><ip address='192.0.2.1' prefix='24'/><route address='10.42.0.0' prefix='16' gateway='192.0.2.254'/></network>`}
	occupied, err := DefinedPrefixes(observed.PersistentXML)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Allocate([]netip.Prefix{netip.MustParsePrefix("10.42.1.0/24"), netip.MustParsePrefix("10.43.1.0/24")}, nil, occupied)
	if err != nil || got != netip.MustParsePrefix("10.43.1.0/24") {
		t.Fatal("inactive configured static route did not prevent overlapping allocation", got, err)
	}
}

func TestDefinedPrefixesRejectsMalformedAndAmbiguousStaticRoutes(t *testing.T) {
	cases := map[string]string{
		"missing gateway":               `<route address='10.0.0.0' prefix='8'/>`,
		"empty gateway":                 `<route gateway=''/>`,
		"invalid gateway":               `<route gateway='invalid'/>`,
		"zoned gateway":                 `<route family='ipv6' gateway='fe80::1%eth0'/>`,
		"mapped gateway":                `<route family='ipv6' gateway='::ffff:192.0.2.254'/>`,
		"IPv6 gateway family missing":   `<route gateway='2001:db8::2'/>`,
		"gateway family mismatch":       `<route family='ipv6' address='2001:db8:1::' prefix='64' gateway='192.0.2.254'/>`,
		"empty destination":             `<route address='' gateway='192.0.2.254'/>`,
		"invalid destination":           `<route address='999.0.0.0' prefix='8' gateway='192.0.2.254'/>`,
		"mapped destination":            `<route family='ipv6' address='::ffff:10.0.0.0' prefix='104' gateway='2001:db8::2'/>`,
		"destination family mismatch":   `<route family='ipv6' address='10.0.0.0' prefix='8' gateway='2001:db8::2'/>`,
		"empty family":                  `<route family='' gateway='192.0.2.254'/>`,
		"unknown family":                `<route family='inet' gateway='192.0.2.254'/>`,
		"empty prefix":                  `<route prefix='' gateway='192.0.2.254'/>`,
		"empty netmask":                 `<route netmask='' gateway='192.0.2.254'/>`,
		"both masks":                    `<route address='10.0.0.0' prefix='8' netmask='255.0.0.0' gateway='192.0.2.254'/>`,
		"empty prefix with netmask":     `<route prefix='' netmask='255.0.0.0' gateway='192.0.2.254'/>`,
		"prefix with empty netmask":     `<route prefix='8' netmask='' gateway='192.0.2.254'/>`,
		"noncontiguous netmask":         `<route netmask='255.0.255.0' gateway='192.0.2.254'/>`,
		"IPv6 netmask":                  `<route family='ipv6' netmask='ffff:ffff:ffff:ffff::' gateway='2001:db8::2'/>`,
		"negative prefix":               `<route prefix='-1' gateway='192.0.2.254'/>`,
		"signed prefix":                 `<route prefix='+8' gateway='192.0.2.254'/>`,
		"noncanonical prefix":           `<route prefix='08' gateway='192.0.2.254'/>`,
		"overflow prefix":               `<route prefix='33' gateway='192.0.2.254'/>`,
		"IPv6 overflow prefix":          `<route family='ipv6' prefix='129' gateway='2001:db8::2'/>`,
		"duplicate gateway":             `<route gateway='192.0.2.254' gateway='192.0.2.253'/>`,
		"duplicate prefix":              `<route prefix='8' prefix='16' gateway='192.0.2.254'/>`,
		"duplicate destination":         `<route address='10.0.0.0' address='172.16.0.0' prefix='8' gateway='192.0.2.254'/>`,
		"foreign element":               `<x:route xmlns:x='urn:foreign' gateway='192.0.2.254'/>`,
		"foreign default namespace":     `<route xmlns='urn:foreign' gateway='192.0.2.254'/>`,
		"foreign gateway":               `<route xmlns:x='urn:foreign' x:gateway='192.0.2.254'/>`,
		"foreign destination":           `<route xmlns:x='urn:foreign' x:address='10.0.0.0' prefix='8' gateway='192.0.2.254'/>`,
		"foreign prefix beside native":  `<route xmlns:x='urn:foreign' prefix='8' x:prefix='16' gateway='192.0.2.254'/>`,
		"foreign gateway beside native": `<route xmlns:x='urn:foreign' gateway='192.0.2.254' x:gateway='192.0.2.253'/>`,
		"undefined entity":              `<route gateway='&untrusted;'/>`,
		"malformed":                     `<route gateway='192.0.2.254'>`,
	}
	for name, route := range cases {
		t.Run(name, func(t *testing.T) {
			// No valid earlier observation may escape after an invalid route.
			got, err := DefinedPrefixes(`<network><ip address='192.0.2.1' prefix='24'/>` + route + `</network>`)
			if err == nil || got != nil {
				t.Fatalf("malformed route returned partial prefixes %v, error %v", got, err)
			}
		})
	}
}

func TestDefinedPrefixesBounds(t *testing.T) {
	const ip = `<ip address='192.0.2.1' prefix='24'/>`
	const route = `<route address='10.0.0.0' prefix='8' gateway='192.0.2.254'/>`
	wrap := func(body string) string { return "<network>" + body + "</network>" }
	valid := []string{
		wrap(strings.Repeat(ip, 256)),
		wrap(strings.Repeat("<x>", 63) + strings.Repeat("</x>", 63)),
		wrap("<!--" + strings.Repeat("x", (1<<20)-len("<network><!--"+"--></network>")) + "-->"),
		wrap(strings.Repeat("<x/>", 32767)), // 65,536 tokens including the root.
		wrap(strings.Repeat(route, 256)),
		wrap(strings.Repeat(ip, 128) + strings.Repeat(route, 128)),
	}
	for i, raw := range valid {
		if _, err := DefinedPrefixes(raw); err != nil {
			t.Fatalf("boundary %d rejected: %v", i, err)
		}
	}
	invalid := []struct{ name, raw, reason string }{
		{"declarations", wrap(strings.Repeat(ip, 257)), "declaration limit"},
		{"route declarations", wrap(strings.Repeat(route, 257)), "declaration limit"},
		{"combined declarations", wrap(strings.Repeat(ip, 129) + strings.Repeat(route, 128)), "declaration limit"},
		{"depth", wrap(strings.Repeat("<x>", 64) + strings.Repeat("</x>", 64)), "depth limit"},
		{"bytes", valid[2] + " ", "oversized"},
		{"tokens", wrap(strings.Repeat("<x/>", 32768)), "token limit"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DefinedPrefixes(tc.raw)
			if err == nil || got != nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("bound failure: prefixes %v, error %v", got, err)
			}
		})
	}
}

func FuzzDefinedPrefixes(f *testing.F) {
	for _, raw := range []string{
		`<network><ip address='192.0.2.1' netmask='255.255.255.0'/></network>`,
		`<network><ip family='ipv6' address='2001:db8::1' prefix='0'/></network>`,
		`<network><route address='10.0.0.0' prefix='8' gateway='192.0.2.254'/></network>`,
		`<network><route gateway='192.0.2.254'/><route family='ipv6' gateway='2001:db8::2'/></network>`,
		`<network><metadata><x:state xmlns:x='urn:foreign' preserve='yes'/></metadata></network>`,
		`<!DOCTYPE network><network/>`,
	} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := DefinedPrefixes(raw)
		if err != nil {
			if got != nil {
				t.Fatal("error leaked partial prefixes", got)
			}
			return
		}
		if got == nil || len(got) > 256 {
			t.Fatal("successful inventory exceeds its contract", got)
		}
		for _, prefix := range got {
			if !prefix.IsValid() || prefix != prefix.Masked() || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" {
				t.Fatal("invalid successful prefix", prefix)
			}
		}
	})
}
