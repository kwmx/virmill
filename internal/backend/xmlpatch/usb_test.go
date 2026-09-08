package xmlpatch

import (
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func usbTestPatch() USBDevicePatch {
	return USBDevicePatch{Alias: "ua-virmill-usb-12ab3456-7890-1234-abcd-123456789abc", VendorID: "04a9", ProductID: "00ef", Bus: 3, Device: 17}
}

func usbTestFragment(t *testing.T, p USBDevicePatch) string {
	t.Helper()
	x, err := USBDeviceXML(p)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func usbTestRefused(t *testing.T, raw string, p USBDevicePatch, code string) {
	t.Helper()
	x, err := USBDevice(raw, p)
	if err == nil || x != "" {
		t.Fatalf("USB patch must return no XML on refusal; output bytes=%d error=%v", len(x), err)
	}
	var typed *domain.Error
	if !errors.As(err, &typed) || code != "" && typed.Code != code {
		t.Fatalf("want typed %s, got %v", code, err)
	}
}

func TestUSBDeviceXMLExactIdentityAndBoundaries(t *testing.T) {
	p := usbTestPatch()
	want := `<hostdev mode="subsystem" type="usb" managed="yes"><source><vendor id="0x04a9"/><product id="0x00ef"/><address bus="3" device="17"/></source><alias name="ua-virmill-usb-12ab3456-7890-1234-abcd-123456789abc"/></hostdev>`
	if got := usbTestFragment(t, p); got != want {
		t.Fatalf("unexpected generated fragment: %s", got)
	}
	p.Remove = true
	if got := usbTestFragment(t, p); got != want {
		t.Fatal("detach fragment must retain complete identity")
	}
	for _, address := range [][2]uint{{1, 1}, {999, 127}} {
		p.Bus, p.Device = address[0], address[1]
		if len(usbTestFragment(t, p)) > 300 {
			t.Fatal("fixed fragment exceeds expected small bound")
		}
	}
	// Canonical native UUIDs are not restricted to version 4.
	p.Alias = "ua-virmill-usb-00000000-0000-0000-0000-000000000001"
	p.VendorID, p.ProductID = "ffff", "0000"
	usbTestFragment(t, p)
}

func TestUSBDeviceRejectsInvalidRequestedIdentity(t *testing.T) {
	cases := map[string]func(*USBDevicePatch){
		"missing alias":     func(p *USBDevicePatch) { p.Alias = "" },
		"automatic alias":   func(p *USBDevicePatch) { p.Alias = "hostdev0" },
		"wrong user prefix": func(p *USBDevicePatch) { p.Alias = "ua-other-12ab3456-7890-1234-abcd-123456789abc" },
		"zero UUID":         func(p *USBDevicePatch) { p.Alias = "ua-virmill-usb-00000000-0000-0000-0000-000000000000" },
		"uppercase UUID":    func(p *USBDevicePatch) { p.Alias = strings.ToUpper(p.Alias) },
		"short UUID":        func(p *USBDevicePatch) { p.Alias = p.Alias[:len(p.Alias)-1] },
		"alias injection":   func(p *USBDevicePatch) { p.Alias += `"/><disk/>` },
		"alias newline":     func(p *USBDevicePatch) { p.Alias += "\n" },
		"alias Unicode":     func(p *USBDevicePatch) { p.Alias += "\u200b" },
		"missing vendor":    func(p *USBDevicePatch) { p.VendorID = "" },
		"missing product":   func(p *USBDevicePatch) { p.ProductID = "" },
		"vendor prefix":     func(p *USBDevicePatch) { p.VendorID = "0x04a9" },
		"uppercase product": func(p *USBDevicePatch) { p.ProductID = "00EF" },
		"short product":     func(p *USBDevicePatch) { p.ProductID = "ef" },
		"long vendor":       func(p *USBDevicePatch) { p.VendorID = "104a9" },
		"nonhex vendor":     func(p *USBDevicePatch) { p.VendorID = "zzzz" },
		"product injection": func(p *USBDevicePatch) { p.ProductID = `00ef"/><address bus="1"` },
		"zero bus":          func(p *USBDevicePatch) { p.Bus = 0 },
		"schema bus bound":  func(p *USBDevicePatch) { p.Bus = 1000 },
		"bus overflow":      func(p *USBDevicePatch) { p.Bus = ^uint(0) },
		"zero device":       func(p *USBDevicePatch) { p.Device = 0 },
		"device bound":      func(p *USBDevicePatch) { p.Device = 128 },
		"device overflow":   func(p *USBDevicePatch) { p.Device = ^uint(0) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := usbTestPatch()
			change(&p)
			for _, remove := range []bool{false, true} {
				p.Remove = remove
				x, err := USBDeviceXML(p)
				if err == nil || x != "" {
					t.Fatal("fragment accepted invalid identity")
				}
				usbTestRefused(t, `<domain><devices/></domain>`, p, "INVALID_INPUT")
			}
		})
	}
}

func TestUSBDevicePreservesEveryUnrelatedByte(t *testing.T) {
	p := usbTestPatch()
	fragment := usbTestFragment(t, p)
	raw := `<?xml version="1.0"?>
<!-- exterior -->
<domain type='kvm' xmlns:qemu='http://libvirt.org/schemas/domain/qemu/1.0' xmlns:x='urn:opaque'>
  <name>original &amp; retained</name><metadata><x:blob xml:space='preserve'>  untouched
    <x:alias name='` + p.Alias + `'/></x:blob></metadata>
  <devices data-opaque='unchanged'>
    <hostdev mode='subsystem' type='pci' managed='no'><source><address bus='0x03' slot='0x01'/></source><x:policy a='b'/><alias name='hostdev0'/></hostdev>
    <disk unknown='preserved'><source><alias name='nested-ignored'/></source><alias name='ua-boot'/><!-- disk comment --></disk>
    <x:hostdev type='usb'><x:opaque/></x:hostdev>
    <!-- keep trailing whitespace -->
  </devices>
  <qemu:commandline><qemu:arg value='opaque expert option'/></qemu:commandline>
</domain><!-- epilog -->
`
	want := strings.Replace(raw, "</devices>", fragment+"</devices>", 1)
	got, err := USBDevice(raw, p)
	if err != nil || got != want {
		t.Fatalf("add changed unrelated bytes: %v", err)
	}
	again, err := USBDevice(raw, p)
	if err != nil || again != got {
		t.Fatal("patch is nondeterministic")
	}
	p.Remove = true
	removed, err := USBDevice(got, p)
	if err != nil || removed != raw {
		t.Fatalf("remove did not restore exact original document: %v", err)
	}
}

func TestUSBDeviceEmptyContainerOnlyChangesClosingSpan(t *testing.T) {
	p := usbTestPatch()
	fragment := usbTestFragment(t, p)
	for _, opening := range []string{`<devices/>`, `<devices />`, "<devices\n opaque = 'kept'  />"} {
		t.Run(opening, func(t *testing.T) {
			raw := "<domain><!-- before -->" + opening + "<!-- after --></domain>"
			want := strings.Replace(raw, "/>", ">"+fragment+"</devices>", 1)
			got, err := USBDevice(raw, p)
			if err != nil || got != want {
				t.Fatalf("empty container span not preserved: %v", err)
			}
			p.Remove = true
			removed, err := USBDevice(got, p)
			p.Remove = false
			if err != nil || removed != strings.Replace(want, fragment, "", 1) {
				t.Fatal("remove changed surrounding container bytes")
			}
		})
	}
}

func TestUSBDeviceAddRejectsAliasAndAddressConflicts(t *testing.T) {
	p := usbTestPatch()
	x := usbTestFragment(t, p)
	other := strings.Replace(x, p.Alias, "ua-another", 1)
	cases := map[string]string{
		"already present":                 x,
		"alias owned by disk":             `<disk><alias name="` + p.Alias + `"/></disk>`,
		"same address different alias":    other,
		"same address different product":  strings.Replace(other, "0x00ef", "0x1234", 1),
		"same explicit hex address":       strings.Replace(strings.Replace(other, `bus="3"`, `bus="0x003"`, 1), `device="17"`, `device="0x11"`, 1),
		"address only selector":           `<hostdev type="usb"><source><address bus="3" device="17"/></source></hostdev>`,
		"duplicate unrelated aliases":     `<disk><alias name="ua-other"/></disk><interface><alias name="ua-other"/></interface>`,
		"multiple alias children":         `<disk><alias name="a"/><alias name="b"/></disk>`,
		"alias missing name":              `<disk><alias/></disk>`,
		"duplicate unrelated USB address": strings.ReplaceAll(other+other, `bus="3"`, `bus="4"`),
		"vendor-only unresolved":          `<hostdev type="usb"><source><vendor id="0x04a9"/><product id="0x00ef"/></source></hostdev>`,
		"unresolved different vendor":     `<hostdev type="usb"><source><vendor id="0x0001"/><product id="0x0002"/></source></hostdev>`,
		"missing source":                  `<hostdev type="usb"/>`,
		"duplicate source":                `<hostdev type="usb"><source/><source/></hostdev>`,
		"duplicate address":               `<hostdev type="usb"><source><address bus="3" device="17"/><address bus="4" device="17"/></source></hostdev>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) { usbTestRefused(t, "<domain><devices>"+body+"</devices></domain>", p, "") })
	}
}

func TestUSBDeviceAddPreservesUnrelatedUSBPolicy(t *testing.T) {
	p := usbTestPatch()
	other := `<hostdev mode='subsystem' type='usb' managed='no' xmlns:z='urn:other' z:keep='yes'><source startupPolicy='optional' guestReset='off'><address bus='4' device='17'/><z:record/></source><alias name='ua-other'/><boot order='8'/></hostdev>`
	raw := "<domain><devices>" + other + "</devices></domain>"
	got, err := USBDevice(raw, p)
	if err != nil || got != strings.Replace(raw, "</devices>", usbTestFragment(t, p)+"</devices>", 1) {
		t.Fatalf("unrelated USB policy was reconstructed: %v", err)
	}
	p.Remove = true
	removed, err := USBDevice(got, p)
	if err != nil || removed != raw {
		t.Fatalf("unrelated USB policy was removed or reconstructed: %v", err)
	}
}

func TestUSBDeviceRemovalAllowsOnlyIdentityEquivalentRepresentations(t *testing.T) {
	p := usbTestPatch()
	x := usbTestFragment(t, p)
	cases := map[string]string{
		"rendered":                          x,
		"quote and order":                   strings.ReplaceAll(strings.Replace(x, `mode="subsystem" type="usb" managed="yes"`, `managed="yes" type="usb" mode="subsystem"`, 1), `"`, `'`),
		"hex ID case width":                 strings.Replace(strings.Replace(x, `id="0x04a9"`, `id="4A9"`, 1), `id="0x00ef"`, `id="0xEF"`, 1),
		"explicit hexadecimal host address": strings.Replace(strings.Replace(x, `bus="3"`, `bus="0x003"`, 1), `device="17"`, `device="0x011"`, 1),
		"expanded vendor":                   strings.Replace(x, `<vendor id="0x04a9"/>`, "<vendor id='0x04a9'> \n<!-- identity comment --></vendor>", 1),
		"guest placement":                   strings.Replace(x, "</hostdev>", `<address type="usb" bus="0" port="1"/></hostdev>`, 1),
		"nested guest placement":            strings.Replace(x, "</hostdev>", `<address type="usb" bus="0x0" port="1.2.0x03.4"/></hostdev>`, 1),
		"guest bus without port":            strings.Replace(x, "</hostdev>", `<address type="usb" bus="0"/></hostdev>`, 1),
	}
	p.Remove = true
	for name, hostdev := range cases {
		t.Run(name, func(t *testing.T) {
			prefix, suffix := "<domain><devices> \n<!-- selected -->", "\n<!-- retain --></devices></domain>"
			got, err := USBDevice(prefix+hostdev+suffix, p)
			if err != nil || got != prefix+suffix {
				t.Fatalf("identity-equivalent removal failed or changed unrelated bytes: %v", err)
			}
		})
	}
}

func TestUSBDeviceRemovalRefusesStaleAndUnsupportedSelectedState(t *testing.T) {
	p := usbTestPatch()
	x := usbTestFragment(t, p)
	cases := map[string]string{
		"absent":                        "",
		"different alias":               strings.Replace(x, p.Alias, "ua-other", 1),
		"alias on another class":        `<disk><alias name="` + p.Alias + `"/></disk>`,
		"duplicate alias":               x + x,
		"different vendor":              strings.Replace(x, "0x04a9", "0x1234", 1),
		"different product":             strings.Replace(x, "0x00ef", "0x1234", 1),
		"different bus":                 strings.Replace(x, `bus="3"`, `bus="4"`, 1),
		"different device":              strings.Replace(x, `device="17"`, `device="18"`, 1),
		"mode changed":                  strings.Replace(x, "subsystem", "capabilities", 1),
		"managed changed":               strings.Replace(x, `managed="yes"`, `managed="no"`, 1),
		"managed missing":               strings.Replace(x, ` managed="yes"`, "", 1),
		"unknown hostdev attribute":     strings.Replace(x, `<hostdev `, `<hostdev surprise="yes" `, 1),
		"opaque selected child":         strings.Replace(x, "</hostdev>", `<metadata><keep/></metadata></hostdev>`, 1),
		"foreign source child":          strings.Replace(x, "</source>", `<x:address xmlns:x="urn:foreign" bus="3" device="17"/></source>`, 1),
		"foreign alias":                 strings.Replace(x, "</hostdev>", `<x:alias xmlns:x="urn:foreign" name="elsewhere"/></hostdev>`, 1),
		"source startup policy":         strings.Replace(x, `<source>`, `<source startupPolicy="optional">`, 1),
		"source reset policy":           strings.Replace(x, `<source>`, `<source guestReset="off">`, 1),
		"source processing instruction": strings.Replace(x, "</source>", `<?opaque policy?></source>`, 1),
		"hostdev non-whitespace":        strings.Replace(x, "</hostdev>", `opaque</hostdev>`, 1),
		"missing vendor":                strings.Replace(x, `<vendor id="0x04a9"/>`, "", 1),
		"missing product":               strings.Replace(x, `<product id="0x00ef"/>`, "", 1),
		"duplicate vendor":              strings.Replace(x, `<vendor id="0x04a9"/>`, `<vendor id="0x04a9"/><vendor id="0x04a9"/>`, 1),
		"bad vendor":                    strings.Replace(x, "0x04a9", "0x10000", 1),
		"vendor content":                strings.Replace(x, `<vendor id="0x04a9"/>`, `<vendor id="0x04a9">opaque</vendor>`, 1),
		"alias policy":                  strings.Replace(x, `<alias `, `<alias other="policy" `, 1),
		"boot policy":                   strings.Replace(x, "</hostdev>", `<boot order="2"/></hostdev>`, 1),
		"guest address wrong family":    strings.Replace(x, "</hostdev>", `<address type="pci" bus="0"/></hostdev>`, 1),
		"guest address missing bus":     strings.Replace(x, "</hostdev>", `<address type="usb" port="1"/></hostdev>`, 1),
		"guest address repeated":        strings.Replace(x, "</hostdev>", `<address type="usb" bus="0"/><address type="usb" bus="1"/></hostdev>`, 1),
		"guest address policy":          strings.Replace(x, "</hostdev>", `<address type="usb" bus="0" multifunction="on"/></hostdev>`, 1),
		"guest port empty":              strings.Replace(x, "</hostdev>", `<address type="usb" bus="0" port=""/></hostdev>`, 1),
		"guest port zero":               strings.Replace(x, "</hostdev>", `<address type="usb" bus="0" port="0"/></hostdev>`, 1),
		"guest port overflow":           strings.Replace(x, "</hostdev>", `<address type="usb" bus="0" port="128"/></hostdev>`, 1),
		"guest port empty component":    strings.Replace(x, "</hostdev>", `<address type="usb" bus="0" port="1..2"/></hostdev>`, 1),
		"guest port too deep":           strings.Replace(x, "</hostdev>", `<address type="usb" bus="0" port="1.2.3.4.5"/></hostdev>`, 1),
	}
	p.Remove = true
	for name, body := range cases {
		t.Run(name, func(t *testing.T) { usbTestRefused(t, "<domain><devices>"+body+"</devices></domain>", p, "") })
	}
}

func TestUSBDeviceRefusesMalformedNativeAddressAndXML(t *testing.T) {
	p := usbTestPatch()
	x := strings.Replace(usbTestFragment(t, p), p.Alias, "ua-other", 1)
	for _, value := range []string{"", "0", "1000", "0xffff", "03", "0X03", "+3", " 3", "3 ", "3.1", "-1", "0x", "abc"} {
		t.Run("bus="+value, func(t *testing.T) {
			raw := `<domain><devices>` + strings.Replace(x, `bus="3"`, `bus="`+value+`"`, 1) + `</devices></domain>`
			usbTestRefused(t, raw, p, "")
		})
	}
	for _, raw := range []string{
		``, `<domain/>`, `<domain><devices/><devices/></domain>`, `<domain xmlns='urn:foreign'><devices/></domain>`,
		`<domain><devices></domain>`, `<domain><devices/></domain><domain/>`,
		`<!DOCTYPE domain [<!ENTITY payload SYSTEM "file:///etc/passwd">]><domain><devices>&payload;</devices></domain>`,
		`<domain><devices><hostdev type='usb' type='pci'/></devices></domain>`,
		`<domain><devices><hostdev type='usb'><source><address bus='3' device='17' device='18'/></source></hostdev></devices></domain>`,
		`<domain><devices><hostdev type='usb'><source><address bus='3' device='17'><opaque/></address></source></hostdev></devices></domain>`,
		`<domain><devices><hostdev type='usb'><source><address bus='3' device='17' port='1'/></source></hostdev></devices></domain>`,
		`<domain><devices/></domain>unrelated-text`,
		"<domain><devices/>" + strings.Repeat("<deep>", 64) + strings.Repeat("</deep>", 64) + "</domain>",
		"<domain><devices/>" + strings.Repeat("<x/>", 65536) + "</domain>",
		"<domain><devices/>" + strings.Repeat(" ", Limit) + "</domain>",
	} {
		t.Run("XML", func(t *testing.T) { usbTestRefused(t, raw, p, "") })
	}
	// The output bound also applies when an otherwise valid input is near Limit.
	raw := `<domain><devices></devices>` + strings.Repeat(" ", Limit-len(`<domain><devices></devices></domain>`)) + `</domain>`
	usbTestRefused(t, raw, p, "INVALID_INPUT")
}

func FuzzUSBDevicePatch(f *testing.F) {
	f.Add(`<domain><devices></devices></domain>`)
	f.Add(`<domain><devices/></domain>`)
	f.Add(`<domain><devices><hostdev type='usb'><source><address bus='1' device='2'/></source></hostdev></devices></domain>`)
	f.Fuzz(func(t *testing.T, raw string) {
		p := usbTestPatch()
		out, err := USBDevice(raw, p)
		if err != nil {
			if out != "" {
				t.Fatal("partial XML on failure")
			}
			return
		}
		p.Remove = true
		removed, err := USBDevice(out, p)
		if err != nil {
			t.Fatalf("cannot remove just-added exact fragment: %v", err)
		}
		p.Remove = false
		again, err := USBDevice(removed, p)
		if err != nil || again != out {
			t.Fatal("exact insertion/removal did not reach a stable byte representation")
		}
	})
}
