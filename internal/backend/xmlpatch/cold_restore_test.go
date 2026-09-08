package xmlpatch

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

const oldRestoreUUID = "12345678-1234-4234-8234-123456789abc"
const newRestoreUUID = "87654321-4321-4321-8321-cba987654321"

func restoreFixture(extraOS, extraDevices string) string {
	return `<?xml version="1.0"?>
<domain type='kvm' xmlns:x='urn:fixture' xmlns:qemu='http://libvirt.org/schemas/domain/qemu/1.0'>
 <name>captured-vm</name><uuid>` + oldRestoreUUID + `</uuid>
 <metadata><x:policy key='opaque'> exact &amp; retained </x:policy></metadata>
 <memory unit='KiB'>524288</memory><vcpu placement='static'>2</vcpu>
 <os><type arch='x86_64' machine='pc-q35-10.2'>hvm</type>` + extraOS + `<bootmenu enable='yes'/></os>
 <devices>
  <controller type='sata' index='0'><alias name='sata0'/></controller>
  <disk type='file' device='disk' snapshot='no'>
   <driver name='qemu' type='qcow2' cache='none' discard='unmap'><metadata_cache><max_size unit='KiB'>512</max_size></metadata_cache></driver>
   <source file='/original/root.qcow2' startupPolicy='mandatory'/>
   <backingStore type='file' index='1'><format type='qcow2'/><source file='/original/base.qcow2'/><backingStore type='file' index='2'><format type='raw'/><source file='/original/base.raw'/><backingStore/></backingStore></backingStore>
   <target dev='vda' bus='virtio'/><boot order='1'/><serial>same-serial</serial><alias name='ua-root'/>
   <x:device-policy exact='keep'/><!-- disk comment remains -->
  </disk>
  <disk type='file' device='cdrom'><driver name='qemu' type='raw'/><source file='/original/install.iso'/><target dev='sda' bus='sata'/><readonly/><boot order='2'/></disk>
  <disk type='file' device='cdrom'><source startupPolicy='optional'/><target dev='sdb' bus='sata'/><readonly/></disk>
  <interface type='network'><mac address='52:54:00:12:34:56'/><source network='original-net'/><model type='virtio'/><boot order='3'/></interface>
  <video><model type='virtio' heads='1' primary='yes'/></video>` + extraDevices + `
 </devices>
 <qemu:commandline><qemu:arg value='opaque-runtime-preserved'/></qemu:commandline>
</domain><!-- trailing preserved -->`
}

func restoreChange() ColdRestorePatch {
	return ColdRestorePatch{UUID: newRestoreUUID, Name: "restored-vm", Disks: []ColdRestoreDisk{{Target: "vda", Path: "/staged/root.raw", Format: "raw"}, {Target: "sda", Path: "/staged/install.raw", Format: "raw"}}}
}

func TestColdRestoreAllStorageAndOpaqueBytesPreserved(t *testing.T) {
	raw, change := restoreFixture("", ""), restoreChange()
	before := append([]ColdRestoreDisk{}, change.Disks...)
	out, err := ColdRestore(raw, change)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(raw, "<backingStore type=")
	end := strings.Index(raw, "\n   <target")
	want := raw[:start] + raw[end:]
	want = strings.NewReplacer(oldRestoreUUID, newRestoreUUID, "<name>captured-vm</name>", "<name>restored-vm</name>", "file='/original/root.qcow2'", "file='/staged/root.raw'", "file='/original/install.iso'", "file='/staged/install.raw'", "type='qcow2' cache=", "type='raw' cache=").Replace(want)
	if out != want {
		t.Fatalf("changed bytes outside selected spans\nGOT: %s\nWANT: %s", out, want)
	}
	if !reflect.DeepEqual(change.Disks, before) {
		t.Fatal("changed caller mappings")
	}
	if strings.Contains(out, "backingStore") || strings.Contains(out, "/original/") {
		t.Fatal("obsolete storage graph remains")
	}
	// Applying the same new identity again is stale, not an idempotent clone.
	if replay, err := ColdRestore(out, change); err == nil || replay != "" {
		t.Fatal("stale restore mapping replayed", replay, err)
	}
}

func TestColdRestoreNICRemovalIsExplicitAndComplete(t *testing.T) {
	raw := restoreFixture("", `<interface type='bridge'><source bridge='other'/><x:opaque/></interface>`)
	change := restoreChange()
	kept, err := ColdRestore(raw, change)
	if err != nil || strings.Count(kept, "<interface ") != 2 {
		t.Fatal("default changed interfaces", kept, err)
	}
	change.DisconnectNICs = true
	removed, err := ColdRestore(raw, change)
	if err != nil {
		t.Fatal(err)
	}
	for _, nic := range []string{`<interface type='network'><mac address='52:54:00:12:34:56'/><source network='original-net'/><model type='virtio'/><boot order='3'/></interface>`, `<interface type='bridge'><source bridge='other'/><x:opaque/></interface>`} {
		kept = strings.Replace(kept, nic, "", 1)
	}
	if kept != removed || strings.Contains(removed, "<interface") || !strings.Contains(removed, "<boot order='1'/>") {
		t.Fatal("NIC removal changed unrelated boot/configuration", removed)
	}
}

func TestColdRestoreFirmwareAndTPMRemapPreservesStatePolicy(t *testing.T) {
	for _, typed := range []bool{false, true} {
		for _, backend := range []string{"dir", "file"} {
			t.Run(backend+map[bool]string{false: "/text", true: "/source"}[typed], func(t *testing.T) {
				nvram := `<nvram format='qcow2' template='/firmware/template.qcow2' templateFormat='qcow2'>/original/nvram.qcow2</nvram>`
				if typed {
					nvram = `<nvram type='file' format='qcow2' template='/firmware/template.qcow2' templateFormat='qcow2'><source file='/original/nvram.qcow2'/></nvram>`
				}
				firmware := `<loader type='pflash' readonly='yes' secure='yes' format='raw'>/firmware/code.fd</loader>` + nvram
				tpm := `<tpm model='tpm-crb'><backend type='emulator' version='2.0' persistent_state='yes'><source type='` + backend + `' path='/original/tpm'/><profile source='local:default' name='default-v1' removeDisabled='check'/><encryption secret='11111111-2222-4333-8444-555555555555'/><active_pcr_banks><sha256/></active_pcr_banks></backend><alias name='tpm0'/></tpm>`
				raw := restoreFixture(firmware, tpm)
				change := restoreChange()
				change.NVRAMPath, change.TPMPath = "/staged/nvram.qcow2", "/staged/tpm"
				out, err := ColdRestore(raw, change)
				if err != nil {
					t.Fatal(err)
				}
				for _, exact := range []string{strings.Replace(firmware, "/original/nvram.qcow2", "/staged/nvram.qcow2", 1), strings.Replace(tpm, "/original/tpm", "/staged/tpm", 1)} {
					if !strings.Contains(out, exact) {
						t.Fatal("firmware/TPM policy changed", out)
					}
				}
			})
		}
	}
}

func TestColdRestoreVolumeAndMissingDriverBecomeExplicitFiles(t *testing.T) {
	raw := `<domain><name>old</name><uuid>` + oldRestoreUUID + `</uuid><os><type>hvm</type></os><devices><disk type='volume' device='disk'><source pool = 'pool-one' volume="disk-one" startupPolicy='optional'/><target dev='vda' bus='virtio'/><readonly/><boot order='1'/></disk><disk device='disk'><driver name='qemu' cache='none'/><source file='/old/second'/><target dev='vdb' bus='scsi'/></disk></devices></domain>`
	change := ColdRestorePatch{UUID: newRestoreUUID, Name: "new", Disks: []ColdRestoreDisk{{"vda", "/new/one.qcow2", "qcow2"}, {"vdb", "/new/a'b&c.raw", "raw"}}}
	out, err := ColdRestore(raw, change)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<disk type='file' device='disk'><driver name="qemu" type="qcow2"/>`, `startupPolicy='optional' file="/new/one.qcow2"`, `<target dev='vda' bus='virtio'/><readonly/><boot order='1'/>`, `<driver name='qemu' cache='none' type="raw"/>`, `file='/new/a&#39;b&amp;c.raw'`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
	if strings.Contains(out, "pool =") || strings.Contains(out, `volume=`) {
		t.Fatal("obsolete pool/volume source retained", out)
	}
}

func TestColdRestoreRejectsIncompleteStaleAndAliasedMappings(t *testing.T) {
	for name, mutate := range map[string]func(*ColdRestorePatch){
		"missing disk":            func(c *ColdRestorePatch) { c.Disks = c.Disks[:1] },
		"extra target":            func(c *ColdRestorePatch) { c.Disks = append(c.Disks, ColdRestoreDisk{"vdc", "/new/extra", "raw"}) },
		"empty media target":      func(c *ColdRestorePatch) { c.Disks = append(c.Disks, ColdRestoreDisk{"sdb", "/new/extra", "raw"}) },
		"duplicate target":        func(c *ColdRestorePatch) { c.Disks[1].Target = "vda" },
		"same staged path":        func(c *ColdRestorePatch) { c.Disks[1].Path = c.Disks[0].Path },
		"nested staged path":      func(c *ColdRestorePatch) { c.Disks[1].Path = c.Disks[0].Path + "/member" },
		"original top path":       func(c *ColdRestorePatch) { c.Disks[0].Path = "/original/root.qcow2" },
		"original backing path":   func(c *ColdRestorePatch) { c.Disks[0].Path = "/original/base.raw" },
		"path traversal":          func(c *ColdRestorePatch) { c.Disks[0].Path = "/new/../original/root" },
		"relative path":           func(c *ColdRestorePatch) { c.Disks[0].Path = "new/file" },
		"path control":            func(c *ColdRestorePatch) { c.Disks[0].Path = "/new/a\x00" },
		"path formatting control": func(c *ColdRestorePatch) { c.Disks[0].Path = "/new/a\u202e" },
		"unknown format":          func(c *ColdRestorePatch) { c.Disks[0].Format = "vmdk" },
		"missing UUID":            func(c *ColdRestorePatch) { c.UUID = "" },
		"zero UUID":               func(c *ColdRestorePatch) { c.UUID = "00000000-0000-0000-0000-000000000000" },
		"noncanonical UUID":       func(c *ColdRestorePatch) { c.UUID = strings.ToUpper(c.UUID) },
		"same UUID":               func(c *ColdRestorePatch) { c.UUID = oldRestoreUUID },
		"same name":               func(c *ColdRestorePatch) { c.Name = "captured-vm" },
		"missing name":            func(c *ColdRestorePatch) { c.Name = "" },
		"unsafe name":             func(c *ColdRestorePatch) { c.Name = "new/../old" },
		"invent NVRAM":            func(c *ColdRestorePatch) { c.NVRAMPath = "/new/nvram" },
		"invent TPM":              func(c *ColdRestorePatch) { c.TPMPath = "/new/tpm" },
	} {
		t.Run(name, func(t *testing.T) {
			c := restoreChange()
			mutate(&c)
			if out, err := ColdRestore(restoreFixture("", ""), c); err == nil || out != "" {
				t.Fatal("unsafe mapping accepted", out, err)
			}
		})
	}
}

func TestColdRestoreMalformedAmbiguousAndUnsupportedSourcesRefuse(t *testing.T) {
	base := restoreFixture("", "")
	for name, raw := range map[string]string{
		"DTD":                             "<!DOCTYPE domain []>" + base,
		"processing instruction":          strings.Replace(base, "<devices>", "<devices><?do bad?>", 1),
		"duplicate declaration":           "<?xml version='1.0'?>" + base,
		"unbound opaque namespace":        strings.Replace(base, "xmlns:x='urn:fixture'", "", 1),
		"reserved namespace reassignment": strings.Replace(base, "<domain type=", "<domain xmlns:xml='urn:wrong' type=", 1),
		"truncated":                       base[:len(base)/2],
		"extra root":                      base + "<domain/>",
		"duplicate UUID":                  strings.Replace(base, "</uuid>", "</uuid><uuid>"+oldRestoreUUID+"</uuid>", 1),
		"foreign UUID":                    strings.Replace(base, "</uuid>", "</uuid><x:uuid>"+oldRestoreUUID+"</x:uuid>", 1),
		"structured name":                 strings.Replace(base, "captured-vm", "captured<!--ambiguous-->-vm", 1),
		"foreign source":                  strings.Replace(base, "<source file='/original/root.qcow2'", "<x:source file='/original/root.qcow2'", 1),
		"duplicate source":                strings.Replace(base, "<source file='/original/root.qcow2'", "<source file='/other'/><source file='/original/root.qcow2'", 1),
		"duplicate path attribute":        strings.Replace(base, "file='/original/root.qcow2'", "file='/original/root.qcow2' file='/other'", 1),
		"foreign path attribute":          strings.Replace(base, "file='/original/root.qcow2'", "file='/original/root.qcow2' x:file='/other'", 1),
		"duplicate target":                strings.Replace(base, "dev='sda'", "dev='vda'", 1),
		"original source alias":           strings.Replace(base, "/original/install.iso", "/original/root.qcow2", 1),
		"network source":                  strings.Replace(base, "type='file' device='disk'", "type='network' device='disk'", 1),
		"block source":                    strings.Replace(base, "type='file' device='disk'", "type='block' device='disk'", 1),
		"missing data source":             strings.Replace(base, "<source file='/original/root.qcow2' startupPolicy='mandatory'/>", "", 1),
		"empty source attribute":          strings.Replace(base, "file='/original/root.qcow2'", "file=''", 1),
		"source auth":                     strings.Replace(base, "<target dev='vda'", "<auth username='existing'/><target dev='vda'", 1),
		"source encryption":               strings.Replace(base, "<target dev='vda'", "<encryption format='luks'/><target dev='vda'", 1),
		"external dataStore":              strings.Replace(base, "<source file='/original/root.qcow2' startupPolicy='mandatory'/>", "<source file='/original/root.qcow2'><dataStore type='file'><source file='/other'/></dataStore></source>", 1),
		"backing foreign source":          strings.Replace(base, "<source file='/original/base.raw'", "<x:source file='/original/base.raw'", 1),
		"backing cycle":                   strings.Replace(base, "/original/base.raw", "/original/root.qcow2", 1),
		"backing unsupported":             strings.Replace(base, "<backingStore type='file' index='1'", "<backingStore type='block' index='1'", 1),
		"oversized":                       base + strings.Repeat(" ", Limit),
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := ColdRestore(raw, restoreChange()); err == nil || out != "" {
				t.Fatal("unsafe source accepted", out, err)
			}
		})
	}
}

func TestColdRestoreAuxiliaryCannotBeSharedInventedOrImplicit(t *testing.T) {
	firmware := `<loader type='pflash'>/firmware/code</loader><nvram>/original/nvram</nvram>`
	tpm := `<tpm model='tpm-crb'><backend type='emulator' version='2.0'><source type='dir' path='/original/tpm'/></backend></tpm>`
	for _, scenario := range []string{"missing nvram mapping", "missing tpm mapping", "implicit nvram", "implicit tpm", "external tpm", "block nvram", "duplicate tpm", "foreign tpm source", "firmware alias", "auxiliary alias"} {
		t.Run(scenario, func(t *testing.T) {
			raw := restoreFixture(firmware, tpm)
			c := restoreChange()
			c.NVRAMPath, c.TPMPath = "/staged/nvram", "/staged/tpm"
			switch scenario {
			case "missing nvram mapping":
				c.NVRAMPath = ""
			case "missing tpm mapping":
				c.TPMPath = ""
			case "implicit nvram":
				raw = strings.Replace(raw, ">/original/nvram</nvram>", "></nvram>", 1)
			case "implicit tpm":
				raw = strings.Replace(raw, `<source type='dir' path='/original/tpm'/>`, "", 1)
			case "external tpm":
				raw = strings.Replace(raw, "type='emulator'", "type='external'", 1)
			case "block nvram":
				raw = strings.Replace(raw, "<nvram>", "<nvram type='block'>", 1)
			case "duplicate tpm":
				raw = strings.Replace(raw, "</devices>", tpm+"</devices>", 1)
			case "foreign tpm source":
				raw = strings.Replace(raw, "<source type='dir'", "<x:source type='dir'", 1)
			case "firmware alias":
				c.Disks[0].Path = "/firmware/code"
			case "auxiliary alias":
				c.TPMPath = c.NVRAMPath
			}
			if out, err := ColdRestore(raw, c); err == nil || out != "" {
				t.Fatal("unsafe auxiliary mapping accepted", out, err)
			}
		})
	}
}

func TestColdRestoreMalformedAuxiliaryFieldsRefuse(t *testing.T) {
	firmware := `<loader type='pflash' readonly='yes'>/firmware/code</loader><nvram>/original/nvram</nvram>`
	tpm := `<tpm model='tpm-crb'><backend type='emulator' version='2.0' persistent_state='yes'><source type='dir' path='/original/tpm'/><profile name='default-v1'/><encryption secret='11111111-2222-4333-8444-555555555555'/><active_pcr_banks><sha256/></active_pcr_banks></backend></tpm>`
	for _, pair := range [][2]string{
		{"readonly='yes'", "readonly='maybe'"},
		{"type='pflash'", "type='pflash' x:type='rom'"},
		{"model='tpm-crb'", "model='other'"},
		{"version='2.0'", "version='1.2'"},
		{"persistent_state='yes'", "persistent_state='sometimes'"},
		{"persistent_state='yes'", "persistent_state='yes' debug='999'"},
		{"name='default-v1'", "name='../profile'"},
		{"secret='11111111-2222-4333-8444-555555555555'", "secret='secret-alias'"},
		{"<sha256/>", "<sha256/><sha256/>"},
		{"<sha256/>", "<x:sha256/>"},
		{"<sha256/>", "<unknown/>"},
	} {
		t.Run(pair[1], func(t *testing.T) {
			raw := strings.Replace(restoreFixture(firmware, tpm), pair[0], pair[1], 1)
			c := restoreChange()
			c.NVRAMPath, c.TPMPath = "/staged/nvram", "/staged/tpm"
			if out, err := ColdRestore(raw, c); err == nil || out != "" {
				t.Fatal("malformed auxiliary policy accepted", out, err)
			}
		})
	}
}

func TestColdRestoreDisplayNamesPreserveUnicodeAndEscaping(t *testing.T) {
	raw := strings.Replace(restoreFixture("", ""), "captured-vm", "Virmill BIOS multi-disk probe", 1)
	c := restoreChange()
	c.Name = "Restored نسخ Café & <verified> \"one\" 'two'"
	out, err := ColdRestore(raw, c)
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Name string `xml:"name"`
	}
	if err := xml.Unmarshal([]byte(out), &observed); err != nil || observed.Name != c.Name {
		t.Fatal("display name lost Unicode or escaped content", observed.Name, err)
	}
	if strings.Contains(out, "<verified>") || !strings.Contains(out, "&lt;verified&gt;") {
		t.Fatal("new name was not escaped")
	}
	// Normalization-equivalent names still alias one reviewed identity.
	raw = strings.Replace(raw, "Virmill BIOS multi-disk probe", "Café", 1)
	c.Name = "Cafe\u0301"
	if out, err := ColdRestore(raw, c); err == nil || out != "" {
		t.Fatal("NFC-equivalent original/new names accepted", out, err)
	}
}
