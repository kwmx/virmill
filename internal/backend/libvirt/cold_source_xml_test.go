//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func coldSourceFixture(t testing.TB, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "protection", "cold-source-xml", name+".xml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func coldSourceTestDomain(disks string) string {
	return `<domain><uuid>aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</uuid><os><type>hvm</type></os><devices>` + disks + `</devices></domain>`
}

const coldSourceTestDisk = `<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='/fixture/leaf.qcow2'/><target dev='vda' bus='virtio'/></disk>`

func TestColdSourceXMLLocalChainAndMedia(t *testing.T) {
	raw := coldSourceFixture(t, "local-chain")
	got, err := InspectColdSourceXML(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.ColdDiskSource{
		{Target: "vda", Device: "disk", Bus: "virtio", Source: domain.ColdStorageSource{Type: "file", Format: "qcow2", File: "/fixture/cold/leaf.qcow2"}, Backing: []domain.ColdStorageSource{{Type: "volume", Format: "qcow2", Pool: "fixture-pool", Volume: "base:1"}, {Type: "file", Format: "raw", File: "/fixture/cold/base.raw"}}, BackingTerminated: true},
		{Target: "sda", Device: "disk", Bus: "scsi", Source: domain.ColdStorageSource{Type: "volume", Pool: "fixture-pool", Volume: "data"}, Backing: []domain.ColdStorageSource{}},
		{Target: "sdb", Device: "cdrom", Bus: "sata", ReadOnly: true, Empty: true, Source: domain.ColdStorageSource{Type: "file"}, Backing: []domain.ColdStorageSource{}},
		{Target: "fda", Device: "floppy", Bus: "fdc", ReadOnly: true, Source: domain.ColdStorageSource{Type: "file", File: "/fixture/media/boot.img"}, Backing: []domain.ColdStorageSource{}},
	}
	if got.Architecture != "x86_64" || got.Machine != "q35" || !reflect.DeepEqual(got.Disks, want) || len(got.External) != 0 {
		t.Fatalf("declared disk layout changed or lost: %#v", got)
	}
	state, err := InspectColdStateXML(raw)
	if err != nil || !reflect.DeepEqual(got.State, state) {
		t.Fatal("source inventory omitted cold firmware/TPM/secret state", got.State, err)
	}
	encoded, err := json.Marshal(got)
	if err != nil || wire.Validate(encoded) != nil {
		t.Fatal("projection is not valid shared wire JSON", err)
	}
	var decoded domain.ColdSourceLayout
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, got) {
		t.Fatal("wire round-trip changed source layout", err)
	}
}

func TestColdSourceXMLNativeSchemaFixtures(t *testing.T) {
	validator, err := exec.LookPath("xmllint")
	if err != nil {
		t.Skip("BLOCKED: xmllint unavailable for native schema fixture validation")
	}
	const schema = "/usr/share/libvirt/schemas/domain.rng"
	if _, err := os.Stat(schema); os.IsNotExist(err) {
		t.Skip("BLOCKED: installed libvirt domain schema unavailable")
	} else if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"local-chain", "unresolved", "structured-source", "configuration-only"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fixture := filepath.Join("..", "..", "..", "tests", "fixtures", "protection", "cold-source-xml", name+".xml")
			output, err := exec.CommandContext(ctx, validator, "--nonet", "--noout", "--relaxng", schema, fixture).CombinedOutput()
			if err != nil {
				t.Fatalf("generated fixture violates installed native schema: %v %s", err, output)
			}
		})
	}
}

func TestColdSourceXMLUnresolvedDependenciesRedactContent(t *testing.T) {
	for _, name := range []string{"unresolved", "structured-source"} {
		t.Run(name, func(t *testing.T) {
			got, err := InspectColdSourceXML(coldSourceFixture(t, name))
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Disks) != 2 || len(got.External) == 0 || len(got.State.SecretReferences) != 1 {
				t.Fatal("disk, secret or unresolved dependency omitted", got)
			}
			var kinds []string
			for _, dependency := range got.External {
				kinds = append(kinds, dependency.Kind)
				if dependency.Target == "" {
					t.Fatal("unresolved dependency has no bounded location")
				}
			}
			want := []string{"storage-block", "storage-network", "filesystem", "hostdev", "shmem", "qemu-commandline"}
			if name == "structured-source" {
				want = []string{"storage-encryption", "storage-dataStore", "storage-mode"}
			}
			if !reflect.DeepEqual(kinds, want) {
				t.Fatal("unresolved requirements changed or disappeared", kinds)
			}
			encoded, err := json.Marshal(got)
			if err != nil || strings.Contains(string(encoded), "DO_NOT_ECHO") || strings.Contains(string(encoded), "example.invalid") || strings.Contains(string(encoded), "/dev/fixture-storage") {
				t.Fatal("opaque source content or credentials escaped into the projection", string(encoded), err)
			}
			if name == "unresolved" && (got.Disks[0].Source.File != "" || got.Disks[1].Source.File != "" || got.Disks[1].Source.Type != "network") {
				t.Fatal("unsupported storage became a local file", got.Disks)
			}
		})
	}
}

func TestColdSourceXMLUnknownsDefaultsAndOpaqueMetadata(t *testing.T) {
	base := `<disk><source file='/fixture/leaf'/><target dev='vda'/></disk>`
	got, err := InspectColdSourceXML(coldSourceTestDomain(base))
	if err != nil || len(got.Disks) != 1 || got.Disks[0].Device != "disk" || got.Disks[0].Source.Type != "file" || got.Disks[0].Source.Format != "" || got.Disks[0].Bus != "" || got.Disks[0].BackingTerminated || got.Architecture != "" || got.Machine != "" {
		t.Fatal("undocumented default or completeness inferred", got, err)
	}
	for _, media := range []string{"cdrom", "floppy"} {
		for _, source := range []string{"", "<source/>", "<source index='1' startupPolicy='optional'/>"} {
			raw := coldSourceTestDomain(`<disk device='` + media + `'>` + source + `<target dev='sda'/></disk>`)
			got, err := InspectColdSourceXML(raw)
			if err != nil || !got.Disks[0].Empty || got.Disks[0].ReadOnly != (media == "cdrom") {
				t.Fatal("valid empty removable media lost", got, err)
			}
		}
	}
	got, err = InspectColdSourceXML(coldSourceTestDomain(`<disk type='block' device='cdrom'><source startupPolicy='optional'/><target dev='sda'/></disk>`))
	if err != nil || !got.Disks[0].Empty || len(got.External) != 1 || got.External[0].Kind != "storage-block" {
		t.Fatal("empty removable media suppressed unsupported storage dependency", got, err)
	}
	chain := strings.Replace(base, "</disk>", `<backingStore><source file='/fixture/base'/></backingStore></disk>`, 1)
	got, err = InspectColdSourceXML(coldSourceTestDomain(chain))
	if err != nil || len(got.Disks[0].Backing) != 1 || got.Disks[0].Backing[0].Format != "" || got.Disks[0].BackingTerminated {
		t.Fatal("missing backing format or terminator was inferred", got, err)
	}
	chain = strings.Replace(chain, "</backingStore>", `<backingStore/></backingStore>`, 1)
	got, err = InspectColdSourceXML(coldSourceTestDomain(chain))
	if err != nil || !got.Disks[0].BackingTerminated {
		t.Fatal("explicit backing terminator was lost", got, err)
	}
	const metadata = `<metadata><x:opaque xmlns:x='urn:fixture'><disk><source file='not-a-native-source'/><target dev='vda'/></disk><secret usage='opaque'/></x:opaque></metadata>`
	raw := strings.Replace(coldSourceTestDomain(base), "<devices>", metadata+"<devices>", 1)
	withMetadata, err := InspectColdSourceXML(raw)
	plain, _ := InspectColdSourceXML(coldSourceTestDomain(base))
	if err != nil || !reflect.DeepEqual(withMetadata, plain) || !strings.Contains(raw, metadata) {
		t.Fatal("inert application metadata altered native inventory", withMetadata, err)
	}
	// Equal sources on different targets may be intentionally shared; preserve
	// both references. Only an actual native graph/writer check can qualify them.
	shared := base + strings.Replace(base, "vda", "vdb", 1)
	got, err = InspectColdSourceXML(coldSourceTestDomain(shared))
	if err != nil || len(got.Disks) != 2 {
		t.Fatal("shared file reference silently deduplicated", got, err)
	}
}

func TestColdSourceXMLUnsupportedSourcesRemainVisible(t *testing.T) {
	for kind, source := range map[string]string{
		"block": `<source dev='/dev/fixture'/>`, "dir": `<source dir='/fixture/directory'/>`,
		"network":   `<source protocol='nbd'><host name='example.invalid' port='10809'/></source>`,
		"nvme":      `<source type='pci' managed='yes' namespace='1'><address domain='0x0000' bus='0x01' slot='0x00' function='0x0'/></source>`,
		"vhostuser": `<source type='unix' path='/fixture/block.sock'/>`, "vhostvdpa": `<source dev='/dev/fixture'/>`,
		"ctl": `<source dev='/dev/fixture'/>`, "future-native": `<source identity='opaque'/>`,
	} {
		t.Run(kind, func(t *testing.T) {
			raw := coldSourceTestDomain(`<disk type='file'><source file='/fixture/leaf'/><target dev='vda'/><backingStore type='` + kind + `'>` + source + `<backingStore/></backingStore></disk>`)
			got, err := InspectColdSourceXML(raw)
			if err != nil || len(got.Disks) != 1 || len(got.Disks[0].Backing) != 1 || got.Disks[0].Backing[0].Type != kind || len(got.External) != 1 || got.External[0].Target != "vda/backing[1]" || !got.Disks[0].BackingTerminated {
				t.Fatal("unsupported backing storage was omitted or resolved", got, err)
			}
		})
	}
	for _, extra := range []string{
		`<x:runtime xmlns:x='urn:fixture'><x:arg value='DO_NOT_ECHO'/></x:runtime>`,
		`<futureRuntime value='DO_NOT_ECHO'/>`,
		`<features><x:runtime xmlns:x='urn:fixture'/></features>`,
		`<bootloader>/fixture/DO_NOT_ECHO</bootloader>`,
	} {
		raw := strings.Replace(coldSourceTestDomain(coldSourceTestDisk), "</domain>", extra+"</domain>", 1)
		got, err := InspectColdSourceXML(raw)
		if err != nil || len(got.External) != 1 {
			t.Fatal("runtime extension or boot artifact was omitted", got, err)
		}
		encoded, _ := json.Marshal(got)
		if strings.Contains(string(encoded), "DO_NOT_ECHO") {
			t.Fatal("runtime extension content escaped redaction")
		}
	}
}

func TestColdSourceXMLReviewedConfigurationDevices(t *testing.T) {
	raw := coldSourceFixture(t, "configuration-only")
	got, err := InspectColdSourceXML(raw)
	if err != nil || len(got.Disks) != 1 || len(got.External) != 0 {
		t.Fatal("ordinary configuration-only devices became external source dependencies", got, err)
	}
	for _, device := range []string{
		`<controller type='scsi' model='virtio-scsi'><driver queues='4'/><acpi index='1'/></controller>`,
		`<controller type='usb' model='ich9-uhci1'><master startport='0'/></controller>`,
		`<memballoon model='virtio'><driver iommu='on'/><address type='pci' bus='0x00' slot='0x04' function='0x0'/></memballoon>`,
		`<sound model='ich9'><codec type='duplex'/><codec type='micro'/><audio id='1'/></sound>`,
	} {
		got, err := InspectColdSourceXML(coldSourceTestDomain(coldSourceTestDisk + device))
		if err != nil || len(got.External) != 0 {
			t.Fatal("reviewed configuration shape was not recognized", got, err)
		}
	}
	for name, device := range map[string]string{
		"controller extra source":      `<controller type='pci'><source file='/fixture/DO_NOT_ECHO'/></controller>`,
		"controller unknown attribute": `<controller type='pci' futureFile='/fixture/DO_NOT_ECHO'/>`,
		"controller unknown child":     `<controller type='pci'><futureRuntime/></controller>`,
		"controller foreign extension": `<controller type='pci'><x:hook xmlns:x='urn:fixture'/></controller>`,
		"controller nested external":   `<controller type='pci'><target port='0x10'><source file='/fixture/DO_NOT_ECHO'/></target></controller>`,
		"controller foreign attribute": `<controller type='pci' x:model='future' xmlns:x='urn:fixture'/>`,
		"controller duplicate child":   `<controller type='pci'><target port='1'/><target port='2'/></controller>`,
		"input evdev":                  `<input type='evdev'><source dev='/dev/DO_NOT_ECHO'/></input>`,
		"input future mode":            `<input type='future'/>`,
		"video host GPU":               `<video><model type='virtio'><acceleration accel3d='yes'/></model></video>`,
		"video explicit GPU":           `<video><model type='virtio'><acceleration rendernode='/dev/dri/DO_NOT_ECHO'/></model></video>`,
		"video vhostuser":              `<video><driver name='vhostuser'/><model type='virtio'/></video>`,
		"video blobs":                  `<video><model type='virtio' blob='on'/></video>`,
		"video unknown model":          `<video><model type='future'/></video>`,
		"video nested foreign":         `<video><model type='virtio'><x:resolution xmlns:x='urn:fixture'/></model></video>`,
		"balloon backend":              `<memballoon model='virtio'><backend path='/fixture/DO_NOT_ECHO'/></memballoon>`,
		"watchdog unknown config":      `<watchdog model='itco'><futureState/></watchdog>`,
		"panic unknown config":         `<panic model='isa' future='enabled'/>`,
		"hub path":                     `<hub type='usb' path='/fixture/DO_NOT_ECHO'/>`,
		"sound nested backend":         `<sound model='ich9'><audio id='1'><backend/></audio></sound>`,
		"audio pipewire":               `<audio id='1' type='pipewire'/>`,
		"audio file":                   `<audio id='1' type='file' path='/fixture/DO_NOT_ECHO'/>`,
		"rng host source":              `<rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>`,
		"serial host endpoint":         `<serial type='unix'><source mode='connect' path='/fixture/DO_NOT_ECHO'/></serial>`,
		"console host file":            `<console type='file'><source path='/fixture/DO_NOT_ECHO'/></console>`,
		"channel host endpoint":        `<channel type='unix'><source path='/fixture/DO_NOT_ECHO'/></channel>`,
		"graphics host endpoint":       `<graphics type='vnc' socket='/fixture/DO_NOT_ECHO'/>`,
		"emulator version dependency":  `<emulator>/fixture/DO_NOT_ECHO</emulator>`,
		"unknown device":               `<futureDevice/>`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := InspectColdSourceXML(coldSourceTestDomain(coldSourceTestDisk + device))
			if err != nil || len(got.External) != 1 {
				t.Fatal("external device escaped dependency classification", got, err)
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "DO_NOT_ECHO") {
				t.Fatal("dependency content escaped redaction")
			}
		})
	}
}

func TestColdSourceXMLRejectsAmbiguousLayout(t *testing.T) {
	base := coldSourceTestDomain(coldSourceTestDisk)
	replace := func(old, next string) string { return strings.Replace(base, old, next, 1) }
	cases := map[string]string{
		"foreign disk":             replace("<disk ", "<disk xmlns='urn:foreign' "),
		"duplicate disk target":    coldSourceTestDomain(coldSourceTestDisk + coldSourceTestDisk),
		"foreign target":           replace("<target ", "<target xmlns='urn:foreign' "),
		"duplicate target":         replace("</disk>", "<target dev='vdb'/></disk>"),
		"missing target":           replace("<target dev='vda' bus='virtio'/>", ""),
		"target path":              replace("dev='vda'", "dev='/dev/vda'"),
		"target credentials":       replace("dev='vda'", "dev='user:password@host'"),
		"empty target":             replace("dev='vda'", "dev=''"),
		"target child":             replace("<target dev='vda' bus='virtio'/>", "<target dev='vda'><source/></target>"),
		"foreign target attr":      replace("bus='virtio'", "x:bus='virtio' xmlns:x='urn:foreign'"),
		"duplicate attribute":      replace("bus='virtio'", "bus='virtio' bus='scsi'"),
		"unknown bus":              replace("bus='virtio'", "bus='future'"),
		"empty bus":                replace("bus='virtio'", "bus=''"),
		"foreign source":           replace("<source ", "<source xmlns='urn:foreign' "),
		"duplicate source":         replace("</disk>", "<source file='/fixture/other'/></disk>"),
		"missing source":           replace("<source file='/fixture/leaf.qcow2'/>", ""),
		"empty data disk":          replace("<source file='/fixture/leaf.qcow2'/>", "<source/>"),
		"empty lun":                replace("device='disk'", "device='lun'"),
		"empty explicit file":      replace("file='/fixture/leaf.qcow2'", "file=''"),
		"mixed file volume":        replace("file='/fixture/leaf.qcow2'", "file='/fixture/leaf.qcow2' pool='p' volume='v'"),
		"relative path":            replace("/fixture/leaf.qcow2", "leaf.qcow2"),
		"parent traversal":         replace("/fixture/leaf.qcow2", "/fixture/../leaf.qcow2"),
		"noncanonical path":        replace("/fixture/leaf.qcow2", "/fixture//leaf.qcow2"),
		"root path":                replace("/fixture/leaf.qcow2", "/"),
		"control path":             replace("/fixture/leaf.qcow2", "/fixture/bad&#10;path"),
		"bidi path":                replace("/fixture/leaf.qcow2", "/fixture/bad&#x202e;path"),
		"source text":              replace("<source file='/fixture/leaf.qcow2'/>", "<source file='/fixture/leaf.qcow2'>text</source>"),
		"unknown source structure": replace("<source file='/fixture/leaf.qcow2'/>", "<source file='/fixture/leaf.qcow2'><other/></source>"),
		"foreign encryption":       replace("<source file='/fixture/leaf.qcow2'/>", "<source file='/fixture/leaf.qcow2'><x:encryption xmlns:x='urn:foreign'/></source>"),
		"duplicate encryption":     replace("<source file='/fixture/leaf.qcow2'/>", "<source file='/fixture/leaf.qcow2'><encryption/><encryption/></source>"),
		"driver duplicate":         replace("</disk>", "<driver type='raw'/></disk>"),
		"driver foreign":           replace("<driver ", "<driver xmlns='urn:foreign' "),
		"driver empty format":      replace("type='qcow2'", "type=''"),
		"driver nested source":     replace("<driver name='qemu' type='qcow2'/>", "<driver type='qcow2'><source file='/fixture/other'/></driver>"),
		"readonly content":         replace("</disk>", "<readonly value='no'/></disk>"),
		"duplicate readonly":       replace("</disk>", "<readonly/><readonly/></disk>"),
		"foreign readonly":         replace("</disk>", "<readonly xmlns='urn:foreign'/></disk>"),
		"unknown disk device":      replace("device='disk'", "device='other'"),
		"empty disk type":          replace("type='file'", "type=''"),
		"unknown disk element":     replace("</disk>", "<futureSource/></disk>"),
		"foreign backing":          replace("</disk>", "<backingStore xmlns='urn:foreign'/></disk>"),
		"duplicate backing":        replace("</disk>", "<backingStore/><backingStore/></disk>"),
		"false terminator":         replace("</disk>", "<backingStore type='file'/></disk>"),
		"backing text":             replace("</disk>", "<backingStore>text</backingStore></disk>"),
		"backing no source":        replace("</disk>", "<backingStore type='file'><format type='raw'/></backingStore></disk>"),
		"backing repeated source":  replace("</disk>", "<backingStore type='file'><source file='/fixture/leaf.qcow2'/></backingStore></disk>"),
		"backing duplicate source": replace("</disk>", "<backingStore type='file'><source file='/fixture/a'/><source file='/fixture/b'/></backingStore></disk>"),
		"backing empty format":     replace("</disk>", "<backingStore type='file'><format/><source file='/fixture/a'/></backingStore></disk>"),
		"backing bad index":        replace("</disk>", "<backingStore type='file' index='01'><source file='/fixture/a'/></backingStore></disk>"),
		"architecture duplicate":   replace("<type>hvm</type>", "<type>hvm</type><type>hvm</type>"),
		"architecture foreign":     replace("<type>", "<type xmlns='urn:foreign'>"),
		"architecture structure":   replace("<type>hvm</type>", "<type><arch>x86_64</arch></type>"),
		"architecture empty":       replace("<type>", "<type arch=''>"),
		"machine control":          replace("<type>", "<type machine='bad&#10;machine'>"),
		"DTD":                      "<!DOCTYPE domain>" + base,
		"malformed":                base[:len(base)-1],
		"multiple roots":           base + base,
	}
	// A file-backed LUN is unsupported independently of missing media.
	cases["empty lun"] = coldSourceTestDomain(`<disk type='block' device='lun'><target dev='sda'/></disk>`)
	cases["file backed lun"] = replace("device='disk'", "device='lun'")
	cases["aliased target"] = coldSourceTestDomain(coldSourceTestDisk + strings.Replace(coldSourceTestDisk, "vda", "ioemu:vda", 1))
	cases["duplicate backing index"] = replace("</disk>", `<backingStore type='file' index='1'><source file='/fixture/a'/><backingStore type='file' index='1'><source file='/fixture/b'/></backingStore></backingStore></disk>`)
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := InspectColdSourceXML(raw)
			if err == nil || !reflect.DeepEqual(got, domain.ColdSourceLayout{}) {
				t.Fatal("ambiguous input returned a successful or partial layout", got, err)
			}
		})
	}
}

func TestColdSourceXMLRejectsExternalIdentityAmbiguities(t *testing.T) {
	for _, disk := range []string{
		`<disk type='block'><source dev='/dev/fixture' file='/fixture/other'/><target dev='vda'/></disk>`,
		`<disk type='block'><source file='/fixture/other'/><target dev='vda'/></disk>`,
		`<disk type='network'><source protocol='nbd' dev='/dev/fixture'/><target dev='vda'/></disk>`,
		`<disk type='network'><source protocol='nbd'><auth/><auth/></source><target dev='vda'/></disk>`,
		`<disk type='network'><source protocol='nbd'><x:host xmlns:x='urn:foreign'/></source><target dev='vda'/></disk>`,
		`<disk type='volume'><source pool='p'/><target dev='vda'/></disk>`,
		`<disk type='volume'><source pool='p' volume='user:password@host'/><target dev='vda'/></disk>`,
		`<disk type='volume'><source pool='p' volume='..'/><target dev='vda'/></disk>`,
		`<filesystem><source dir='/fixture/a'/><source dir='/fixture/b'/><target dir='mount'/></filesystem>`,
		`<filesystem><x:target xmlns:x='urn:foreign' dir='mount'/></filesystem>`,
	} {
		got, err := InspectColdSourceXML(coldSourceTestDomain(disk))
		if err == nil || !reflect.DeepEqual(got, domain.ColdSourceLayout{}) {
			t.Fatal("ambiguous external identity accepted", got, err)
		}
	}
}

func TestColdSourceXMLBounds(t *testing.T) {
	chain := func(count int) string {
		var b strings.Builder
		for i := 0; i < count; i++ {
			fmt.Fprintf(&b, `<backingStore type='file'><source file='/fixture/base%d'/>`, i)
		}
		b.WriteString("<backingStore/>")
		b.WriteString(strings.Repeat("</backingStore>", count))
		return b.String()
	}
	var disks strings.Builder
	for i := 0; i < coldSourceDiskLimit; i++ {
		disks.WriteString(strings.Replace(coldSourceTestDisk, "vda", fmt.Sprintf("vd%d", i), 1))
	}
	got, err := InspectColdSourceXML(coldSourceTestDomain(disks.String()))
	if err != nil || len(got.Disks) != coldSourceDiskLimit {
		t.Fatal("exact disk bound refused", err)
	}
	maxChain := strings.Replace(coldSourceTestDisk, "</disk>", chain(coldSourceBackingLimit)+"</disk>", 1)
	got, err = InspectColdSourceXML(coldSourceTestDomain(maxChain))
	if err != nil || len(got.Disks[0].Backing) != coldSourceBackingLimit || !got.Disks[0].BackingTerminated {
		t.Fatal("exact backing bound refused", got, err)
	}
	var manySources strings.Builder
	for i := 0; i < coldSourceEntryLimit/coldSourceBackingLimit; i++ {
		disk := strings.Replace(coldSourceTestDisk, "vda", fmt.Sprintf("vd%d", i), 1)
		manySources.WriteString(strings.Replace(disk, "</disk>", chain(coldSourceBackingLimit-1)+"</disk>", 1))
	}
	got, err = InspectColdSourceXML(coldSourceTestDomain(manySources.String()))
	if err != nil || len(got.Disks)*(1+len(got.Disks[0].Backing)) != coldSourceEntryLimit {
		t.Fatal("exact aggregate source bound refused", err)
	}
	got, err = InspectColdSourceXML(coldSourceTestDomain(strings.Repeat("<filesystem/>", coldSourceDependencyLimit)))
	if err != nil || len(got.External) != coldSourceDependencyLimit {
		t.Fatal("exact dependency bound refused", err)
	}
	for _, raw := range []string{
		coldSourceTestDomain(disks.String() + strings.Replace(coldSourceTestDisk, "vda", "vdover", 1)),
		coldSourceTestDomain(strings.Replace(coldSourceTestDisk, "</disk>", chain(coldSourceBackingLimit+1)+"</disk>", 1)),
		coldSourceTestDomain(strings.Repeat("<filesystem/>", coldSourceDependencyLimit+1)),
		coldSourceTestDomain(manySources.String() + coldSourceTestDisk),
		coldSourceTestDomain(strings.Replace(coldSourceTestDisk, "/fixture/leaf.qcow2", "/"+strings.Repeat("x", 4096), 1)),
		strings.Repeat(" ", coldStateXMLLimit+1),
		coldSourceTestDomain(strings.Repeat("<x>", 32) + strings.Repeat("</x>", 32)),
		coldSourceTestDomain("<metadata>" + strings.Repeat("<x/>", 16384) + "</metadata>"),
		coldSourceTestDomain("<metadata>" + strings.Repeat("<!---->", 65536) + "</metadata>"),
	} {
		got, err := InspectColdSourceXML(raw)
		if err == nil || !reflect.DeepEqual(got, domain.ColdSourceLayout{}) {
			t.Fatal("over-limit source XML accepted", got, err)
		}
	}
}

func FuzzColdSourceXML(f *testing.F) {
	f.Add(coldSourceTestDomain(coldSourceTestDisk))
	f.Add(coldSourceTestDomain(`<disk device='cdrom'><target dev='sda'/></disk>`))
	f.Add(coldSourceTestDomain(`<disk type='network'><source protocol='nbd'><host name='example.invalid'/></source><target dev='vda'/></disk>`))
	for _, name := range []string{"local-chain", "unresolved", "structured-source", "configuration-only"} {
		f.Add(coldSourceFixture(f, name))
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := InspectColdSourceXML(raw)
		if err != nil {
			if !reflect.DeepEqual(got, domain.ColdSourceLayout{}) {
				t.Fatal("failed extraction returned partial authority", got)
			}
			return
		}
		if len(got.Disks) > coldSourceDiskLimit || len(got.External) > coldSourceDependencyLimit {
			t.Fatal("projection exceeds bounds")
		}
		seen := map[string]bool{}
		for _, disk := range got.Disks {
			if seen[disk.Target] || !coldDiskTarget.MatchString(disk.Target) || len(disk.Backing) > coldSourceBackingLimit || disk.Empty && disk.Device != "cdrom" && disk.Device != "floppy" {
				t.Fatal("successful projection is inconsistent", disk)
			}
			seen[disk.Target] = true
			for _, source := range append([]domain.ColdStorageSource{disk.Source}, disk.Backing...) {
				if source.File != "" && coldPath(source.File) != nil {
					t.Fatal("unsafe local file projected")
				}
			}
		}
		encoded, err := json.Marshal(got)
		if err != nil || wire.Validate(encoded) != nil {
			t.Fatal("successful layout is not valid bounded wire JSON", err)
		}
		again, err := InspectColdSourceXML(raw)
		if err != nil || !reflect.DeepEqual(got, again) {
			t.Fatal("extraction is nondeterministic")
		}
	})
}
