package xmlpatch

import (
	"strings"
	"testing"
)

func bootXML(legacy, extras string) string {
	return `<domain type='kvm' xmlns:x='urn:fixture'><name>boot-fixture</name><memory unit='KiB'>262144</memory><currentMemory unit='KiB'>262144</currentMemory><vcpu>2</vcpu><os><type arch='x86_64' machine='pc-q35-10.2'>hvm</type><loader readonly='yes' type='pflash'>/never-opened/firmware</loader><nvram>/never-opened/nvram</nvram>` + legacy + `<bootmenu enable='yes'/></os><metadata><x:opaque policy='keep'> exact text </x:opaque></metadata><devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='/never-opened/system.qcow2'/><target dev='vda' bus='virtio'/><boot order='2'/><alias name='ua-disk'/></disk><disk type='file' device='cdrom'><driver name='qemu' type='raw'/><source file='/never-opened/installer.iso'/><target dev='sda' bus='sata'/><readonly/><boot order='1'/><alias name='ua-media'/></disk><interface type='network'><mac address='52:54:00:11:22:33'/><source network='existing'/><model type='virtio'/><alias name='ua-nic'/></interface></devices>` + extras + `</domain>`
}
func TestBootAndEjectionAreOnePreservingEdit(t *testing.T) {
	original := bootXML("", "")
	view, err := InspectBoot(original)
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != "per-device" || view.BootVerified || len(view.Devices) != 3 || view.Devices[1].Order != 1 {
		t.Fatal(view)
	}
	out, err := EditHardware(original, HardwareEdit{EjectMedia: "sda", BootOrder: []BootChoice{{Kind: "disk", ID: "vda"}, {Kind: "interface", ID: "52:54:00:11:22:33"}}})
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.NewReplacer(`<source file='/never-opened/installer.iso'/>`, "", `<boot order='2'/>`, `<boot order="1"/>`, `<boot order='1'/>`, "", `<alias name='ua-nic'/></interface>`, `<alias name='ua-nic'/><boot order="2"/></interface>`).Replace(original)
	if out != expected {
		t.Fatal("unrelated bytes changed", out)
	}
	view, err = InspectBoot(out)
	if err != nil {
		t.Fatal(err)
	}
	if view.Devices[1].MediaPresent || view.Devices[1].Selectable || !view.Devices[1].ReadOnly || view.Devices[0].Order != 1 || view.Devices[2].Order != 2 {
		t.Fatal(view)
	}
	if !strings.Contains(out, "/never-opened/system.qcow2") || !strings.Contains(out, "/never-opened/nvram") || !strings.Contains(out, " exact text ") {
		t.Fatal("opaque/firmware/storage settings lost")
	}
}
func TestLegacyBootSelectionBecomesExplicitPerDevice(t *testing.T) {
	original := strings.ReplaceAll(bootXML(`<boot dev='cdrom'/><boot dev='hd'/>`, ""), `<boot order='2'/>`, "")
	original = strings.ReplaceAll(original, `<boot order='1'/>`, "")
	out, err := EditBootOrder(original, []BootChoice{{Kind: "disk", ID: "vda"}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := InspectBoot(out)
	if err != nil || len(view.LegacyOrder) != 0 || view.Mode != "per-device" || view.Devices[0].Order != 1 || view.Devices[1].Order != 0 {
		t.Fatal(view, err)
	}
	if strings.Contains(out, "<boot dev=") {
		t.Fatal("legacy policy retained with per-device boot")
	}
}
func TestBootAndMediaRefuseAmbiguousOrDependentPolicy(t *testing.T) {
	for _, choices := range [][]BootChoice{{}, {{Kind: "disk", ID: "absent"}}, {{Kind: "disk", ID: "vda"}, {Kind: "disk", ID: "vda"}}, {{Kind: "interface", ID: "52:54:00:11:22:34"}}} {
		if _, err := EditBootOrder(bootXML("", ""), choices); err == nil {
			t.Fatal("invalid boot selection accepted", choices)
		}
	}
	for _, change := range []func(string) string{
		func(x string) string {
			return strings.Replace(x, "<boot order='1'/>", "<boot order='1' loadparm='keep'/>", 1)
		},
		func(x string) string {
			return strings.Replace(x, "<os>", "<os><kernel>/never-opened/kernel</kernel>", 1)
		},
		func(x string) string { return strings.Replace(x, "<os>", "<os><boot dev='hd'/>", 1) },
		func(x string) string {
			return strings.Replace(x, "</interface>", "<link state='down'/></interface>", 1)
		},
	} {
		if _, err := EditBootOrder(change(bootXML("", "")), []BootChoice{{Kind: "interface", ID: "52:54:00:11:22:33"}}); err == nil {
			t.Fatal("unsafe boot policy accepted")
		}
	}
	if _, err := EjectMedia(bootXML("", ""), "sda", false); err == nil {
		t.Fatal("ejected boot candidate without replacement")
	}
	if _, err := EjectMedia(bootXML("", ""), "vda", true); err == nil {
		t.Fatal("ejected ordinary disk")
	}
	if _, err := EditHardware(bootXML("", ""), HardwareEdit{EjectMedia: "sda", BootOrder: []BootChoice{{Kind: "disk", ID: "sda"}}}); err == nil {
		t.Fatal("empty media retained as boot candidate")
	}
	original := strings.Replace(bootXML("", ""), "<boot order='1'/>", "", 1)
	if _, err := EjectMedia(original, "sda", false); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(string) string{func(x string) string { return strings.Replace(x, "<readonly/>", "", 1) }, func(x string) string {
		return strings.Replace(x, "<readonly/>", "<readonly/><auth username='keep'/>", 1)
	}, func(x string) string { return strings.Replace(x, "installer.iso'", "installer.iso' future='keep'", 1) }} {
		if _, err := EjectMedia(change(original), "sda", false); err == nil {
			t.Fatal("advanced media ejection accepted")
		}
	}
}
func TestHardwareFingerprintHasNarrowNormalization(t *testing.T) {
	original := bootXML("", "")
	base, err := HardwareDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	equivalent := strings.Replace(original, `<boot order='2'/><alias name='ua-disk'/>`, `<alias name="ua-disk"></alias>\n <boot order="2"/>`, 1)
	equivalent = strings.ReplaceAll(equivalent, `\n`, "\n")
	same, err := HardwareDigest(equivalent)
	if err != nil || same != base {
		t.Fatal("known boot positioning/formatting rejected", err)
	}
	for _, change := range []func(string) string{func(x string) string { return strings.Replace(x, " exact text ", "exact text", 1) }, func(x string) string { return strings.Replace(x, "urn:fixture", "urn:other", 1) }, func(x string) string { return strings.Replace(x, "ua-disk", "ua-other", 1) }, func(x string) string { return strings.Replace(x, "/never-opened/system.qcow2", "/different.qcow2", 1) }, func(x string) string {
		return strings.Replace(x, `<readonly/>`, `<readonly/><x:opaque> changed </x:opaque>`, 1)
	}} {
		other, err := HardwareDigest(change(original))
		if err != nil {
			t.Fatal(err)
		}
		if other == base {
			t.Fatal("unknown semantic change hidden")
		}
	}
}
func FuzzBootConfiguration(f *testing.F) {
	f.Add(bootXML("", ""))
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = InspectBoot(s)
		_, _ = HardwareDigest(s)
		_, _ = EditBootOrder(s, []BootChoice{{Kind: "disk", ID: "vda"}})
	})
}

func TestHardwareDigestPreservesInheritedXMLSpaceAndExteriorTokens(t *testing.T) {
	base := bootXML("", "")
	digest := func(data string) string {
		t.Helper()
		d, err := HardwareDigest(data)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for _, owner := range []string{"domain", "devices", "disk"} {
		t.Run(owner, func(t *testing.T) {
			before := strings.Replace(base, "<"+owner, "<"+owner+" xml:space='preserve'", 1)
			after := strings.Replace(before, "<source file='/never-opened/system.qcow2'/>", "\n <source file='/never-opened/system.qcow2'/>\n", 1)
			if digest(before) == digest(after) {
				t.Fatal("xml:space inheritance was ignored")
			}
		})
	}
	preserved := strings.Replace(base, "<disk ", "<disk xml:space='preserve' ", 1)
	moved := strings.Replace(preserved, `<boot order='2'/><alias name='ua-disk'/>`, `<alias name='ua-disk'/><boot order='2'/>`, 1)
	if digest(preserved) == digest(moved) {
		t.Fatal("child position changed under xml:space preserve")
	}
	reset := strings.Replace(base, "<domain ", "<domain xml:space='preserve' ", 1)
	reset = strings.Replace(reset, "<disk ", "<disk xml:space='default' ", 1)
	indented := strings.Replace(reset, "<source file='/never-opened/system.qcow2'/>", "\n <source file='/never-opened/system.qcow2'/>\n", 1)
	if digest(reset) != digest(indented) {
		t.Fatal("xml:space default did not reset inheritance")
	}
	for _, pair := range [][2]string{
		{"<!--before-->" + base, base}, {base + "<!--after-->", base},
		{"<?fixture preserve?>" + base, base}, {base + "<?fixture preserve?>", base},
		{"<!--same-->" + base, base + "<!--same-->"},
	} {
		if digest(pair[0]) == digest(pair[1]) {
			t.Fatal("exterior token or its position was dropped")
		}
	}
}
