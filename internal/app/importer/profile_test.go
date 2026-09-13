package importer

import (
	"encoding/json"
	"strings"
	"testing"
)

func profileDescriptor(body string) []byte {
	return []byte(`<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData" xmlns:vssd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_VirtualSystemSettingData" xmlns:vbox="http://www.virtualbox.org/ovf/machine" xmlns:vmw="http://www.vmware.com/schema/ovf" xmlns:fake="urn:untrusted-lookalike"><VirtualSystem ovf:id="guest">` + body + `</VirtualSystem></Envelope>`)
}

func inspectProfile(t *testing.T, body string) System {
	t.Helper()
	var report Report
	if err := parseOVF(profileDescriptor(body), "unrelated-filename.ovf", &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Systems) != 1 {
		t.Fatal("missing system")
	}
	return report.Systems[0]
}

func TestOVFProfileOwnerLikeVirtualBoxMetadata(t *testing.T) {
	s := inspectProfile(t, `<OperatingSystemSection ovf:id="103"><Description>Windows 11 (64-bit)</Description></OperatingSystemSection><VirtualHardwareSection><System><vssd:VirtualSystemIdentifier>DFIR workstation</vssd:VirtualSystemIdentifier><vssd:VirtualSystemType>virtualbox-2.2</vssd:VirtualSystemType></System><Item><rasd:ResourceType>10</rasd:ResourceType><rasd:ResourceSubType>E1000</rasd:ResourceSubType><rasd:Description>Network adapter</rasd:Description><rasd:Connection>Private lab</rasd:Connection></Item><Item><rasd:ResourceType>32768</rasd:ResourceType><rasd:ResourceSubType>nvram</rasd:ResourceSubType><rasd:Description>Firmware variables</rasd:Description><rasd:HostResource>ovf:/file/nvram1</rasd:HostResource></Item></VirtualHardwareSection><vbox:Machine xmlns="http://www.virtualbox.org/" name="VBox fallback"><Hardware><Firmware type="EFI"/></Hardware></vbox:Machine>`)
	if s.Name != "DFIR workstation" || s.OS != "Windows 11 (64-bit)" || s.Firmware != "uefi" {
		t.Fatalf("lost detected profile: %+v", s)
	}
	if len(s.Items) != 2 || s.Items[0].ResourceSubType != "E1000" || s.Items[0].Description != "Network adapter" || s.Items[0].Connections[0] != "Private lab" {
		t.Fatalf("lost device description: %+v", s.Items)
	}
	if s.Items[1].ResourceType != "32768" || s.Items[1].ResourceSubType != "nvram" || len(s.Items[1].HostResources) != 1 || s.Items[1].HostResources[0] != "ovf:/file/nvram1" {
		t.Fatal("unmapped NVRAM device/reference was lost")
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var decoded System
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OS != s.OS || decoded.Firmware != s.Firmware || decoded.Items[0].ResourceSubType != "E1000" {
		t.Fatal("profile metadata lost during JSON roundtrip")
	}
}

func TestOVFProfileNamePrecedenceAndAmbiguity(t *testing.T) {
	identifier := `<VirtualHardwareSection><System><vssd:VirtualSystemIdentifier>identifier</vssd:VirtualSystemIdentifier></System></VirtualHardwareSection>`
	machine := `<vbox:Machine name="machine"/>`
	for _, tc := range []struct{ name, body, want string }{
		{"explicit", `<Name>explicit</Name>` + identifier + machine, "explicit"},
		{"identifier", identifier + machine, "identifier"},
		{"machine", machine, "machine"},
		{"none", "", ""},
		{"duplicate-name", `<Name>one</Name><Name>two</Name>` + identifier + machine, ""},
		{"duplicate-identifier", identifier + identifier + machine, ""},
		{"duplicate-machine", machine + machine, ""},
		{"empty-name", `<Name/>` + identifier + machine, ""},
		{"wrong-namespace", `<fake:Name>fake</fake:Name><fake:Machine name="fake"/>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inspectProfile(t, tc.body).Name; got != tc.want {
				t.Fatalf("name=%q; want %q", got, tc.want)
			}
		})
	}
}

func TestOVFProfileFirmwareRequiresExactUnambiguousDeclaration(t *testing.T) {
	vbox := func(value string) string {
		return `<vbox:Machine xmlns="http://www.virtualbox.org/" name="guest"><Hardware><Firmware type="` + value + `"/></Hardware></vbox:Machine>`
	}
	vmware := func(value string) string {
		return `<VirtualHardwareSection><vmw:Config vmw:key="firmware" vmw:value="` + value + `"/></VirtualHardwareSection>`
	}
	for _, tc := range []struct{ name, body, want string }{
		{"vbox-efi", vbox("EFI"), "uefi"}, {"vbox-bios", vbox("BIOS"), "bios"},
		{"vmware-efi", vmware("efi"), "uefi"}, {"vmware-bios", vmware("bios"), "bios"},
		{"missing", "", ""}, {"unknown", vbox("Automatic"), ""},
		{"duplicate", vbox("EFI") + vbox("EFI"), ""}, {"conflict", vbox("EFI") + vmware("bios"), ""},
		{"known-plus-unknown", vbox("EFI") + vmware("other"), ""},
		{"wrong-machine-namespace", `<fake:Machine xmlns="http://www.virtualbox.org/"><Hardware><Firmware type="EFI"/></Hardware></fake:Machine>`, ""},
		{"ovf-default-hardware-namespace", `<vbox:Machine><Hardware><Firmware type="EFI"/></Hardware></vbox:Machine>`, "uefi"},
		{"wrong-hardware-namespace", `<vbox:Machine><fake:Hardware><fake:Firmware type="EFI"/></fake:Hardware></vbox:Machine>`, ""},
		{"wrong-vmware-attrs", `<VirtualHardwareSection><vmw:Config key="firmware" value="efi"/></VirtualHardwareSection>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inspectProfile(t, tc.body).Firmware; got != tc.want {
				t.Fatalf("firmware=%q; want %q", got, tc.want)
			}
		})
	}
}

func TestOVFProfileVirtualBoxOSConflictDevicesAndFileReferences(t *testing.T) {
	data := []byte(`<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:vbox="http://www.virtualbox.org/ovf/machine" xmlns:vssd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_VirtualSystemSettingData" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData"><References><File ovf:id="file2" ovf:href="Sample-Win11.nvram"/></References><VirtualSystem ovf:id="guest"><OperatingSystemSection ovf:id="120"><Description>Windows10_64</Description><vbox:OSType>Windows11_64</vbox:OSType></OperatingSystemSection><VirtualHardwareSection><System><vssd:VirtualSystemIdentifier>Sample-Win11</vssd:VirtualSystemIdentifier></System><Item><rasd:ResourceType>32768</rasd:ResourceType><rasd:HostResource>ovf:/file/file2</rasd:HostResource></Item></VirtualHardwareSection><vbox:Machine name="Sample-Win11" OSType="Windows11_64"><Hardware><Firmware type="EFI"/><USB><Controllers><Controller name="OHCI" type="OHCI"/><Controller name="EHCI" type="EHCI" enabled="false"/></Controllers></USB><AudioAdapter controller="HDA" enabled="true" enabledOut="true"/></Hardware></vbox:Machine></VirtualSystem></Envelope>`)
	var report Report
	if err := parseOVF(data, "appliance.ovf", &report); err != nil {
		t.Fatal(err)
	}
	s := report.Systems[0]
	if s.Name != "Sample-Win11" || s.OS != "Windows11_64" || s.OSSource != "virtualbox" || s.OVFOS != "Windows10_64" || s.Firmware != "uefi" {
		t.Fatalf("owner metadata mislabeled: %+v", s)
	}
	if len(report.Warnings) != 1 || report.Warnings[0] != "GUEST_OS_METADATA_CONFLICT" {
		t.Fatal("conflicting standard OS silently discarded", report.Warnings)
	}
	if report.FileReferences["file2"] != "Sample-Win11.nvram" || s.Items[0].HostResources[0] != "ovf:/file/file2" {
		t.Fatal("exact NVRAM reference lost")
	}
	if len(s.Devices) != 3 {
		t.Fatalf("USB/audio hints missing: %+v", s.Devices)
	}
	if s.Devices[0].Kind != "audio" || s.Devices[0].Model != "HDA" || s.Devices[0].Enabled == nil || !*s.Devices[0].Enabled {
		t.Fatal("enabled audio lost")
	}
	if s.Devices[1].Kind != "usb" || s.Devices[1].Model != "OHCI" || s.Devices[1].Enabled != nil {
		t.Fatal("unspecified USB enabled state guessed")
	}
	if s.Devices[2].Enabled == nil || *s.Devices[2].Enabled {
		t.Fatal("explicit disabled USB state lost")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Systems[0].OVFOS != s.OVFOS || decoded.FileReferences["file2"] != "Sample-Win11.nvram" || decoded.Systems[0].Devices[2].Enabled == nil || *decoded.Systems[0].Devices[2].Enabled {
		t.Fatal("profile report JSON lost hints or false boolean")
	}
}

func TestOVFProfileVendorOSDisagreementDoesNotFallBackToWrongStandard(t *testing.T) {
	s := inspectProfile(t, `<OperatingSystemSection><Description>Windows10_64</Description><vbox:OSType>Windows11_64</vbox:OSType></OperatingSystemSection><vbox:Machine OSType="Linux_64"/>`)
	if s.OS != "" || s.OSSource != "" || s.OVFOS != "Windows10_64" {
		t.Fatalf("conflicting vendor metadata silently selected: %+v", s)
	}
	s = inspectProfile(t, `<OperatingSystemSection><Description>Windows11_64</Description><fake:OSType>Windows10_64</fake:OSType></OperatingSystemSection><fake:Machine OSType="Windows10_64"/>`)
	if s.OS != "Windows11_64" || s.OSSource != "ovf" {
		t.Fatal("lookalike namespace overrode standard OS")
	}
}

func TestOVFProfileDeviceHintsAreBoundedAndNamespaceAnchored(t *testing.T) {
	body := `<vbox:Machine><Hardware><USB><Controllers>` + strings.Repeat(`<Controller type="XHCI"/>`, 70) + `</Controllers></USB></Hardware></vbox:Machine>`
	var report Report
	if err := parseOVF(profileDescriptor(body), "guest.ovf", &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Systems[0].Devices) != 64 || len(report.Warnings) != 1 || report.Warnings[0] != "DEVICE_METADATA_LIMIT" {
		t.Fatal("device hint bound or truncation warning missing")
	}
	s := inspectProfile(t, `<fake:Machine><Hardware><AudioAdapter controller="HDA" enabled="true"/></Hardware></fake:Machine><vbox:Machine><fake:Hardware><fake:AudioAdapter controller="HDA" enabled="true"/></fake:Hardware></vbox:Machine>`)
	if len(s.Devices) != 0 {
		t.Fatal("unrelated namespace produced configured devices")
	}
}

func TestOVFProfileUnknownOSAndDuplicateDeviceDescriptionsNotGuessed(t *testing.T) {
	for _, body := range []string{"", `<OperatingSystemSection><fake:Description>fake</fake:Description></OperatingSystemSection>`, `<OperatingSystemSection><Description>A</Description><Description>B</Description></OperatingSystemSection>`, `<OperatingSystemSection><Description>A</Description></OperatingSystemSection><OperatingSystemSection><Description>B</Description></OperatingSystemSection>`} {
		if s := inspectProfile(t, body); s.OS != "" {
			t.Fatalf("ambiguous/missing OS guessed: %+v", s)
		}
	}
	s := inspectProfile(t, `<VirtualHardwareSection><Item><rasd:ResourceType>10</rasd:ResourceType><rasd:ResourceSubType>one</rasd:ResourceSubType><rasd:ResourceSubType>two</rasd:ResourceSubType><fake:Description>wrong namespace</fake:Description></Item></VirtualHardwareSection>`)
	if s.Items[0].ResourceSubType != "" || s.Items[0].Description != "" {
		t.Fatal("ambiguous or nonstandard device metadata guessed")
	}
	data, err := json.Marshal(System{ID: "old", Items: []Item{{ResourceType: "10"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"os"`, `"firmware"`, `"resourceSubType"`, `"description"`} {
		if strings.Contains(string(data), key) {
			t.Fatalf("old report acquired fabricated metadata %s", key)
		}
	}
}

func TestOVFProfileRejectsDuplicateAttributes(t *testing.T) {
	for _, body := range []string{
		`<vbox:Machine><Hardware><Firmware type="EFI" type="BIOS"/></Hardware></vbox:Machine>`,
		`<vbox:Machine name="one" name="two"/>`,
		`<vbox:Machine OSType="Windows11_64" OSType="Linux_64"/>`,
		`<VirtualHardwareSection><vmw:Config xmlns:alias="http://www.vmware.com/schema/ovf" vmw:key="firmware" alias:key="other" vmw:value="efi"/></VirtualHardwareSection>`,
	} {
		var report Report
		if err := parseOVF(profileDescriptor(body), "guest.ovf", &report); err == nil || !strings.Contains(err.Error(), "duplicate XML attribute") {
			t.Fatalf("ambiguous attribute accepted: %v", err)
		}
	}
}

func TestOVFProfileRejectsOversizedMetadataWithoutTruncation(t *testing.T) {
	long := strings.Repeat("x", 2049)
	for _, body := range []string{
		`<Name>` + long + `</Name>`,
		`<vbox:Machine name="` + long + `"/>`,
		`<OperatingSystemSection><Description>` + long + `</Description></OperatingSystemSection>`,
		`<vbox:Machine OSType="` + long + `"/>`,
		`<OperatingSystemSection><Description>` + long + `</Description><vbox:OSType>Windows11_64</vbox:OSType></OperatingSystemSection>`,
		`<VirtualHardwareSection><Item><rasd:ResourceSubType>` + long + `</rasd:ResourceSubType></Item></VirtualHardwareSection>`,
		`<VirtualHardwareSection><Item><rasd:Description>` + long + `</rasd:Description></Item></VirtualHardwareSection>`,
	} {
		var report Report
		if err := parseOVF(profileDescriptor(body), "guest.ovf", &report); err == nil || !strings.Contains(err.Error(), "2048-character limit") {
			t.Fatalf("oversized metadata accepted: %v", err)
		}
	}
	// JSON Schema maxLength counts characters, not UTF-8 bytes.
	name := strings.Repeat("界", 2048)
	if got := inspectProfile(t, `<Name>`+name+`</Name>`).Name; got != name {
		t.Fatal("valid boundary name truncated")
	}
}
