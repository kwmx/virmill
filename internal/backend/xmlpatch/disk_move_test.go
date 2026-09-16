package xmlpatch

import (
	"strings"
	"testing"
)

func movableXML(disks string) string {
	return `<domain type="kvm"><name>moved</name><uuid>7e472a89-a207-4615-af5f-2a10a734943b</uuid>` +
		`<metadata><private:keep xmlns:private="urn:example:opaque" private:value="unchanged"/></metadata>` +
		`<memory unit="KiB">2097152</memory><vcpu>2</vcpu><os><type arch="x86_64" machine="q35">hvm</type></os>` +
		`<devices><emulator>/usr/bin/qemu-system-x86_64</emulator><controller type="sata" index="0"/>` + disks +
		`<interface type="network"><source network="default"/></interface></devices></domain>`
}

const movableDisks = `<disk type="volume" device="disk"><driver name="qemu" type="qcow2" cache="writethrough" error_policy="stop"/><source pool="images" volume="app-disk-000.qcow2"/><target dev="sda" bus="sata"/><boot order="1"/><address type="drive" controller="0" bus="0" target="0" unit="0"/></disk>` +
	`<disk type="volume" device="cdrom"><source pool="images" volume="seed.iso"/><target dev="sdb" bus="sata"/><readonly/><address type="drive" controller="0" bus="0" target="0" unit="1"/></disk>`

// Moving a disk rewrites its source and nothing else, so unlike an inserted
// disk the result keeps the document's child ordering and a whole-definition
// digest binds it.
func TestRetargetDiskReplacesOnlyTheSource(t *testing.T) {
	raw := movableXML(movableDisks)
	out, err := RetargetDisk(raw, "sda", "archive", "virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-000.qcow2")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(raw, `<source pool="images" volume="app-disk-000.qcow2"/>`,
		`<source pool="archive" volume="virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-000.qcow2"/>`, 1)
	if out != want {
		t.Fatal(out)
	}
	// The disk keeps its place, its boot order, its drive port and its driver,
	// and no other device is touched.
	for _, keep := range []string{`<target dev="sda" bus="sata"/>`, `<boot order="1"/>`,
		`<address type="drive" controller="0" bus="0" target="0" unit="0"/>`,
		`<driver name="qemu" type="qcow2" cache="writethrough" error_policy="stop"/>`,
		`private:value="unchanged"`, `<source pool="images" volume="seed.iso"/>`} {
		if !strings.Contains(out, keep) {
			t.Fatal("the move changed more than the source:", keep)
		}
	}
	if strings.Count(out, "<disk ") != 2 || strings.Count(out, "<controller ") != 1 {
		t.Fatal("device inventory changed", out)
	}
	// An in-place swap is confirmable by a digest of the whole definition,
	// which an inserted disk never is.
	if targets, err := DiskTargets(out); err != nil || strings.Join(targets, ",") != "sda,sdb" {
		t.Fatal(targets, err)
	}
	before, err := HardwareDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	after, err := HardwareDigest(out)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("the digest must change when the source changes")
	}
	// Reindented exactly as libvirt stores it, the result still matches.
	reindented := strings.Replace(out, `<disk type="volume" device="disk">`, "\n    "+`<disk type="volume" device="disk">`, 1)
	if digest, err := HardwareDigest(reindented); err != nil || digest != after {
		t.Fatal("libvirt's own formatting must not change the verdict", err)
	}
	// Preserved source attributes travel with the disk.
	withMode := strings.Replace(raw, `<source pool="images" volume="app-disk-000.qcow2"/>`,
		`<source pool="images" volume="app-disk-000.qcow2" mode="direct" index="3"/>`, 1)
	moved, err := RetargetDisk(withMode, "sda", "archive", "copy.qcow2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(moved, `<source pool="archive" volume="copy.qcow2" mode="direct" index="3"/>`) {
		t.Fatal("source policy attributes were dropped", moved)
	}
}

func TestRetargetDiskRefusesWhatItCannotMoveSafely(t *testing.T) {
	raw := movableXML(movableDisks)
	fileDisk := movableXML(`<disk type="file" device="disk"><driver name="qemu" type="qcow2"/><source file="/srv/images/a.qcow2"/><target dev="sda" bus="sata"/></disk>`)
	layered := movableXML(`<disk type="volume" device="disk"><driver name="qemu" type="qcow2"/><source pool="images" volume="a.qcow2"/><backingStore type="volume"><source pool="images" volume="base.qcow2"/></backingStore><target dev="sda" bus="sata"/></disk>`)
	structured := movableXML(`<disk type="volume" device="disk"><driver name="qemu" type="qcow2"/><source pool="images" volume="a.qcow2"><privateHint value="1"/></source><target dev="sda" bus="sata"/></disk>`)
	unknownAttr := movableXML(`<disk type="volume" device="disk"><driver name="qemu" type="qcow2"/><source pool="images" volume="a.qcow2" unexpected="x"/><target dev="sda" bus="sata"/></disk>`)
	encrypted := movableXML(`<disk type="volume" device="disk"><driver name="qemu" type="qcow2"/><source pool="images" volume="a.qcow2"/><encryption format="luks"/><target dev="sda" bus="sata"/></disk>`)
	for name, test := range map[string]struct {
		raw, target, pool, volume, want string
	}{
		"installer media":     {raw, "sdb", "archive", "copy.qcow2", "UNSUPPORTED_CAPABILITY"},
		"absent target":       {raw, "sdz", "archive", "copy.qcow2", "INVALID_INPUT"},
		"same volume":         {raw, "sda", "images", "app-disk-000.qcow2", "STALE_PLAN"},
		"plain file disk":     {fileDisk, "sda", "archive", "copy.qcow2", "UNSUPPORTED_CAPABILITY"},
		"backing chain":       {layered, "sda", "archive", "copy.qcow2", "UNSUPPORTED_CAPABILITY"},
		"structured source":   {structured, "sda", "archive", "copy.qcow2", "UNSUPPORTED_CAPABILITY"},
		"unknown source attr": {unknownAttr, "sda", "archive", "copy.qcow2", "UNSUPPORTED_CAPABILITY"},
		"encrypted disk":      {encrypted, "sda", "archive", "copy.qcow2", "UNSUPPORTED_CAPABILITY"},
		"empty pool name":     {raw, "sda", "", "copy.qcow2", "INVALID_INPUT"},
		"bad target name":     {raw, "sd/a", "archive", "copy.qcow2", "INVALID_INPUT"},
	} {
		_, err := RetargetDisk(test.raw, test.target, test.pool, test.volume)
		if code(t, err) != test.want {
			t.Fatal(name, err)
		}
	}
}
