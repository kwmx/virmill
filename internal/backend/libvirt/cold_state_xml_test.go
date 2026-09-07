//go:build linux && cgo

package libvirt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func coldFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "protection", "cold-state-xml", name+".xml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestColdStateXMLExtractsDeclaredFirmwareAndTPM(t *testing.T) {
	directory, err := InspectColdStateXML(coldFixture(t, "uefi-tpm20-dir"))
	if err != nil {
		t.Fatal(err)
	}
	wantFirmware := domain.ColdFirmware{Loader: "/fixture/firmware/CODE.fd", LoaderType: "pflash", LoaderReadOnly: "yes", LoaderSecure: "yes", LoaderFormat: "raw", LoaderStateless: "no", NVRAM: &domain.ColdNVRAM{Path: "/fixture/cold/guest_VARS.fd", Format: "raw", Template: "/fixture/firmware/VARS.fd", TemplateFormat: "raw"}}
	wantTPM := &domain.ColdTPM{Model: "tpm-crb", Version: "2.0", SourceType: "dir", SourcePath: "/fixture/cold/tpm-state", PersistentState: "yes", Profile: "custom:restricted", ProfileSource: "local:restricted", ProfileRemoveDisabled: "check", EncryptionSecret: "11111111-2222-4333-8444-555555555555"}
	if directory.VMID != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" || !reflect.DeepEqual(directory.Firmware, wantFirmware) || !reflect.DeepEqual(directory.TPM, wantTPM) || !reflect.DeepEqual(directory.SecretReferences, []string{wantTPM.EncryptionSecret}) {
		t.Fatal("declared confidential layout changed or omitted", directory)
	}
	file, err := InspectColdStateXML(coldFixture(t, "uefi-tpm20-file"))
	if err != nil {
		t.Fatal(err)
	}
	if file.Firmware.LoaderFormat != "qcow2" || file.Firmware.NVRAM.Path != "/fixture/cold/guest_VARS.qcow2" || file.Firmware.NVRAM.Format != "qcow2" || file.Firmware.NVRAM.TemplateFormat != "raw" || file.TPM.SourceType != "file" || file.TPM.SourcePath != "/fixture/cold/tpm-state.bin" || file.TPM.Profile != "custom" || file.TPM.ProfileSource != "" || file.TPM.ProfileRemoveDisabled != "fips-host" || file.TPM.PersistentState != "no" || !reflect.DeepEqual(file.SecretReferences, []string{"99999999-aaaa-4bbb-8ccc-dddddddddddd"}) {
		t.Fatal("file-backed state or secret dependency was misidentified", file)
	}
	for _, layout := range []domain.ColdStateLayout{directory, file} {
		b, err := json.Marshal(layout)
		var decoded domain.ColdStateLayout
		if err != nil || wire.Decode(b, &decoded) != nil || !reflect.DeepEqual(layout, decoded) {
			t.Fatal("cold-state wire contract drift", err)
		}
	}
}

func TestColdStateXMLKeepsUnknownDefaultsAndOpaqueMetadata(t *testing.T) {
	bios, err := InspectColdStateXML(coldFixture(t, "bios-no-aux"))
	if err != nil || bios.Firmware != (domain.ColdFirmware{}) || bios.TPM != nil || bios.SecretReferences == nil || len(bios.SecretReferences) != 0 {
		t.Fatal("BIOS fixture gained invented auxiliary state", bios, err)
	}
	rom, err := InspectColdStateXML(coldFixture(t, "rom-tpm12-default"))
	if err != nil || rom.Firmware.Loader != "/fixture/firmware/bios.bin" || rom.Firmware.LoaderType != "rom" || rom.TPM == nil || rom.TPM.Version != "1.2" || rom.TPM.SourceType != "" || rom.TPM.SourcePath != "" || rom.TPM.PersistentState != "" || rom.TPM.Profile != "" {
		t.Fatal("TPM defaults became guessed state paths/policies", rom, err)
	}
	base := coldFixture(t, "bios-no-aux")
	for _, addition := range []string{"<loader/>", "<nvram/>", "<nvram type='file'/>", "<loader stateless='yes'/>"} {
		got, err := InspectColdStateXML(strings.Replace(base, "</os>", addition+"</os>", 1))
		if err != nil || got.Firmware.Loader != "" || got.Firmware.NVRAM != nil && got.Firmware.NVRAM.Path != "" {
			t.Fatal("unresolved firmware default invented a path", got, err)
		}
	}
	data := strings.Replace(base, "<devices/>", `<devices><tpm><backend type='emulator'/></tpm></devices>`, 1)
	got, err := InspectColdStateXML(data)
	if err != nil || got.TPM == nil || *got.TPM != (domain.ColdTPM{}) {
		t.Fatal("missing TPM model/version/source inferred from host", got, err)
	}
	const opaque = `<metadata><x:state xmlns:x='urn:foreign'><uuid>not-the-vm</uuid><loader>not-a-loader</loader><nvram>not-state</nvram><tpm/><secret usage='not-a-native-reference'/></x:state></metadata>`
	data = strings.Replace(base, "<devices/>", opaque+`<devices/>`, 1)
	got, err = InspectColdStateXML(data)
	if err != nil || !reflect.DeepEqual(bios, got) || !strings.Contains(data, opaque) {
		t.Fatal("opaque extension data affected extraction", got, err)
	}
}

func TestColdStateXMLCanonicalizesAllNativeSecretUUIDs(t *testing.T) {
	base := coldFixture(t, "uefi-tpm20-dir")
	base = strings.Replace(base, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "AAAAAAAABBBB4CCC8DDDEEEEEEEEEEEE", 1)
	base = strings.Replace(base, "11111111-2222-4333-8444-555555555555", "11111111222243338444555555555555", 1)
	const secrets = `<disk><encryption format='luks'><secret type='passphrase' uuid='99999999-AAAA-4BBB-8CCC-DDDDDDDDDDDD'/></encryption><auth><secret type='ceph' uuid='11111111-2222-4333-8444-555555555555'/></auth></disk><disk><auth><secret type='iscsi' uuid='22222222-3333-4444-8555-666666666666'/></auth></disk>`
	got, err := InspectColdStateXML(strings.Replace(base, "</devices>", secrets+"</devices>", 1))
	if err != nil || got.VMID != "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee" || !reflect.DeepEqual(got.SecretReferences, []string{"11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666", "99999999-aaaa-4bbb-8ccc-dddddddddddd"}) {
		t.Fatal("secret UUIDs were missed, duplicated or not canonicalized", got, err)
	}
}

func TestColdStateXMLRejectsMalformedAndAmbiguousLayout(t *testing.T) {
	base := coldFixture(t, "uefi-tpm20-dir")
	replace := func(old, next string) string { return strings.Replace(base, old, next, 1) }
	cases := map[string]string{
		"missing UUID":                 replace("<uuid>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</uuid>", ""),
		"duplicate UUID":               replace("</uuid>", "</uuid><uuid>11111111-2222-4333-8444-555555555555</uuid>"),
		"invalid UUID":                 replace("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeeG"),
		"structured UUID":              replace("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "<value>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</value>"),
		"foreign UUID":                 replace("<uuid>", "<uuid xmlns='urn:foreign'>"),
		"foreign root":                 replace("<domain ", "<domain xmlns='urn:foreign' "),
		"foreign OS":                   replace("<os>", "<os xmlns='urn:foreign'>"),
		"foreign devices":              replace("<devices>", "<devices xmlns='urn:foreign'>"),
		"duplicate OS":                 replace("</os>", "</os><os/>"),
		"duplicate devices":            replace("</devices>", "</devices><devices/>"),
		"missing OS":                   `<domain><uuid>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</uuid><devices/></domain>`,
		"missing devices":              `<domain><uuid>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</uuid><os/></domain>`,
		"duplicate loader":             replace("</loader>", "</loader><loader>/fixture/other</loader>"),
		"foreign loader":               replace("<loader ", "<loader xmlns='urn:foreign' "),
		"foreign loader attr":          replace("readonly='yes'", "x:readonly='yes' xmlns:x='urn:foreign'"),
		"duplicate loader attr":        replace("readonly='yes'", "readonly='yes' readonly='no'"),
		"structured loader":            replace("/fixture/firmware/CODE.fd", "<path>/fixture/firmware/CODE.fd</path>"),
		"relative loader":              replace("/fixture/firmware/CODE.fd", "fixture/CODE.fd"),
		"loader traversal":             replace("/fixture/firmware/CODE.fd", "/fixture/../firmware/CODE.fd"),
		"empty loader attr":            replace("readonly='yes'", "readonly=''"),
		"unknown loader type":          replace("type='pflash'", "type='network'"),
		"unknown loader format":        replace("format='raw'", "format='vmdk'"),
		"bad loader boolean":           replace("readonly='yes'", "readonly='true'"),
		"stateless with NVRAM":         replace("stateless='no'", "stateless='yes'"),
		"duplicate NVRAM":              replace("</nvram>", "</nvram><nvram>/fixture/other</nvram>"),
		"foreign NVRAM":                replace("<nvram ", "<nvram xmlns='urn:foreign' "),
		"relative NVRAM":               replace("/fixture/cold/guest_VARS.fd", "guest_VARS.fd"),
		"relative template":            replace("template='/fixture/firmware/VARS.fd'", "template='VARS.fd'"),
		"root path":                    replace("/fixture/cold/guest_VARS.fd", "/"),
		"noncanonical path":            replace("/fixture/cold/guest_VARS.fd", "/fixture//cold/guest_VARS.fd"),
		"path whitespace":              replace("/fixture/cold/guest_VARS.fd", " /fixture/cold/guest_VARS.fd"),
		"control path":                 replace("/fixture/cold/guest_VARS.fd", "/fixture/cold/bad&#10;name"),
		"bidi path":                    replace("/fixture/cold/guest_VARS.fd", "/fixture/cold/bad\u202ename"),
		"long path":                    replace("/fixture/cold/guest_VARS.fd", "/"+strings.Repeat("x", 4096)),
		"typed NVRAM text":             replace("<nvram ", "<nvram type='file' "),
		"unsupported NVRAM type":       replace("<nvram ", "<nvram type='network' "),
		"unsupported template format":  replace("templateFormat='raw'", "templateFormat='vmdk'"),
		"NVRAM text and source":        replace("</nvram>", "<source file='/fixture/other'/></nvram>"),
		"varstore":                     replace("</os>", "<varstore path='/fixture/cold/vars.json'/></os>"),
		"pSeries NVRAM":                replace("</devices>", "<nvram><address type='spapr-vio' reg='0x3000'/></nvram></devices>"),
		"duplicate TPM":                replace("</devices>", "<tpm><backend type='emulator'/></tpm></devices>"),
		"foreign TPM":                  replace("<tpm ", "<tpm xmlns='urn:foreign' "),
		"duplicate backend":            replace("</backend>", "</backend><backend type='emulator'/>"),
		"foreign backend":              replace("<backend ", "<backend xmlns='urn:foreign' "),
		"missing backend":              strings.Replace(coldFixture(t, "bios-no-aux"), "<devices/>", "<devices><tpm/></devices>", 1),
		"TPM passthrough":              replace("type='emulator'", "type='passthrough'"),
		"TPM external":                 replace("type='emulator'", "type='external'"),
		"unsupported TPM model":        replace("model='tpm-crb'", "model='spapr-tpm-proxy'"),
		"bad TPM version":              replace("version='2.0'", "version='3.0'"),
		"CRB version 1.2":              replace("version='2.0'", "version='1.2'"),
		"bad persistent policy":        replace("persistent_state='yes'", "persistent_state='true'"),
		"bad debug":                    replace("debug='1'", "debug='256'"),
		"TPM source missing path":      replace(" path='/fixture/cold/tpm-state'", ""),
		"TPM source missing type":      replace("type='dir' ", ""),
		"TPM source relative path":     replace("/fixture/cold/tpm-state", "relative/state"),
		"TPM source unix":              replace("type='dir'", "type='unix'"),
		"TPM source extra attr":        replace("type='dir'", "type='dir' mode='connect'"),
		"duplicate TPM source":         replace("<source type='dir' path='/fixture/cold/tpm-state'/>", "<source type='dir' path='/fixture/cold/tpm-state'/><source type='dir' path='/fixture/other'/>"),
		"foreign TPM source":           replace("<source type='dir'", "<source xmlns='urn:foreign' type='dir'"),
		"missing encryption reference": replace("secret='11111111-2222-4333-8444-555555555555'", ""),
		"bad encryption UUID":          replace("secret='11111111-2222-4333-8444-555555555555'", "secret='not-a-uuid'"),
		"encryption value":             replace("<encryption secret='11111111-2222-4333-8444-555555555555'/>", "<encryption secret='11111111-2222-4333-8444-555555555555'>forbidden-secret-value</encryption>"),
		"encryption extra attr":        replace("secret='11111111-2222-4333-8444-555555555555'", "secret='11111111-2222-4333-8444-555555555555' value='forbidden-secret-value'"),
		"duplicate encryption":         replace("<encryption secret='11111111-2222-4333-8444-555555555555'/>", "<encryption secret='11111111-2222-4333-8444-555555555555'/><encryption secret='22222222-3333-4444-8555-666666666666'/>"),
		"foreign encryption":           replace("<encryption ", "<encryption xmlns='urn:foreign' "),
		"profile path":                 replace("source='local:restricted'", "source='/etc/profile.json'"),
		"profile invalid name":         replace("name='custom:restricted'", "name='custom_bad'"),
		"profile bad remove policy":    replace("removeDisabled='check'", "removeDisabled='yes'"),
		"profile empty source":         replace("source='local:restricted'", "source=''"),
		"duplicate profile":            replace("<profile source='local:restricted' name='custom:restricted' removeDisabled='check'/>", "<profile name='custom'/><profile name='null'/>"),
		"foreign profile":              replace("<profile ", "<profile xmlns='urn:foreign' "),
		"duplicate PCR":                replace("<sha256/>", "<sha256/><sha256/>"),
		"unknown PCR":                  replace("<sha256/>", "<unknown/>"),
		"foreign PCR":                  replace("<sha256/>", "<sha256 xmlns='urn:foreign'/>"),
		"unknown TPM backend child":    replace("</backend>", "<future-state path='/fixture/unmodeled'/></backend>"),
		"DTD":                          "<!DOCTYPE domain>" + base,
		"external entity":              "<!DOCTYPE domain [<!ENTITY secret SYSTEM 'file:///never-opened'>]>" + base,
		"processing instruction":       "<?untrusted action?>" + base,
		"malformed":                    "<domain>",
		"multiple roots":               base + "<domain/>",
		"outside text":                 base + "not-XML",
	}
	file := coldFixture(t, "uefi-tpm20-file")
	for name, replacement := range map[string]string{
		"missing file":       "<source/>",
		"foreign source":     "<source xmlns='urn:foreign' file='/fixture/state'/>",
		"foreign file attr":  "<source xmlns:x='urn:foreign' x:file='/fixture/state'/>",
		"duplicate source":   "<source file='/fixture/one'/><source file='/fixture/two'/>",
		"duplicate file":     "<source file='/fixture/one' file='/fixture/two'/>",
		"external data file": "<source file='/fixture/state'><dataStore/></source>",
		"encrypted source":   "<source file='/fixture/state'><encryption format='luks'/></source>",
		"descriptor group":   "<source file='/fixture/state' fdgroup='opaque'/>",
	} {
		cases["NVRAM "+name] = strings.Replace(file, "<source file='/fixture/cold/guest_VARS.qcow2'/>", replacement, 1)
	}
	for name, addition := range map[string]string{"profile": "<profile source='null'/>", "PCR banks": "<active_pcr_banks><sha256/></active_pcr_banks>"} {
		cases["TPM 1.2 "+name] = strings.Replace(coldFixture(t, "rom-tpm12-default"), "<backend type='emulator' version='1.2'/>", "<backend type='emulator' version='1.2'>"+addition+"</backend>", 1)
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := InspectColdStateXML(raw)
			if err == nil || !reflect.DeepEqual(got, domain.ColdStateLayout{}) {
				t.Fatalf("ambiguous layout returned partial success: %#v; %v", got, err)
			}
			if strings.Contains(err.Error(), "forbidden-secret-value") {
				t.Fatal("secret value leaked in error")
			}
		})
	}
}

func TestColdStateXMLRejectsUnresolvedSecretFormsAndLimits(t *testing.T) {
	base := coldFixture(t, "bios-no-aux")
	for name, secret := range map[string]string{
		"usage":          `<secret type='passphrase' usage='key-name'/>`,
		"both selectors": `<secret type='passphrase' usage='key-name' uuid='11111111-2222-4333-8444-555555555555'/>`,
		"missing type":   `<secret uuid='11111111-2222-4333-8444-555555555555'/>`,
		"invalid UUID":   `<secret type='passphrase' uuid='invalid'/>`,
		"foreign secret": `<x:secret xmlns:x='urn:foreign' type='passphrase' uuid='11111111-2222-4333-8444-555555555555'/>`,
		"foreign UUID":   `<secret xmlns:x='urn:foreign' type='passphrase' x:uuid='11111111-2222-4333-8444-555555555555'/>`,
		"value":          `<secret type='passphrase' uuid='11111111-2222-4333-8444-555555555555'>forbidden-secret-value</secret>`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(base, "<devices/>", "<devices><disk><encryption format='luks'>"+secret+"</encryption></disk></devices>", 1)
			got, err := InspectColdStateXML(raw)
			if err == nil || !reflect.DeepEqual(got, domain.ColdStateLayout{}) {
				t.Fatal("unresolved reference accepted", got, err)
			}
			var typed *domain.Error
			if (name == "usage" || name == "both selectors") && (!errors.As(err, &typed) || typed.Code != "UNSUPPORTED_CAPABILITY") {
				t.Fatal("usage lookup limitation was not explicit", err)
			}
		})
	}
	var secrets strings.Builder
	for i := 0; i < 257; i++ {
		fmt.Fprintf(&secrets, "<secret type='passphrase' uuid='%08x-2222-4333-8444-555555555555'/>", i)
	}
	for name, raw := range map[string]string{
		"XML bytes": base + strings.Repeat(" ", coldStateXMLLimit),
		"depth":     strings.Replace(base, "<devices/>", "<devices>"+strings.Repeat("<x>", 32)+strings.Repeat("</x>", 32)+"</devices>", 1),
		"nodes":     strings.Replace(base, "<devices/>", "<devices>"+strings.Repeat("<x/>", 16384)+"</devices>", 1),
		"tokens":    strings.Replace(base, "<devices/>", "<devices>"+strings.Repeat("<!--token-->", 65536)+"</devices>", 1),
		"secrets":   strings.Replace(base, "<devices/>", "<devices><disk><encryption format='luks'>"+secrets.String()+"</encryption></disk></devices>", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := InspectColdStateXML(raw); err == nil || !reflect.DeepEqual(got, domain.ColdStateLayout{}) {
				t.Fatal("layout bound failed", got, err)
			}
		})
	}
}

func FuzzColdStateXML(f *testing.F) {
	f.Add(`<domain><uuid>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</uuid><os/><devices/></domain>`)
	f.Add(`<domain><uuid>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</uuid><os><loader type='pflash'>/fixture/code</loader><nvram>/fixture/vars</nvram></os><devices><tpm model='tpm-crb'><backend type='emulator' version='2.0'><source type='dir' path='/fixture/tpm'/><encryption secret='11111111-2222-4333-8444-555555555555'/></backend></tpm></devices></domain>`)
	f.Add(`<!DOCTYPE domain><domain/>`)
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := InspectColdStateXML(raw)
		if err != nil {
			if !reflect.DeepEqual(got, domain.ColdStateLayout{}) {
				t.Fatal("error returned partial layout")
			}
			return
		}
		if len(got.VMID) != 36 || got.SecretReferences == nil || len(got.SecretReferences) > 256 || !sort.StringsAreSorted(got.SecretReferences) {
			t.Fatal("invalid layout identity/reference set")
		}
		b, err := json.Marshal(got)
		if err != nil || wire.Validate(b) != nil {
			t.Fatal("layout cannot safely cross wire")
		}
	})
}
