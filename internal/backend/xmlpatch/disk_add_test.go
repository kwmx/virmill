package xmlpatch

import (
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func diskAdditionXML(disks string) string {
	return `<domain type="kvm"><name>added</name><uuid>7e472a89-a207-4615-af5f-2a10a734943b</uuid>` +
		`<metadata><private:keep xmlns:private="urn:example:opaque" private:value="unchanged"/></metadata>` +
		`<memory unit="KiB">2097152</memory><vcpu>2</vcpu><os><type arch="x86_64" machine="q35">hvm</type></os>` +
		`<devices><emulator>/usr/bin/qemu-system-x86_64</emulator><controller type="sata" index="0"/>` + disks +
		`<interface type="network"><source network="default"/></interface></devices></domain>`
}

const sataDisk = `<disk type="volume" device="disk"><driver name="qemu" type="qcow2"/><source pool="images" volume="app-disk-000.qcow2"/><target dev="sda" bus="sata"/><address type="drive" controller="0" bus="0" target="0" unit="0"/></disk>` +
	`<disk type="volume" device="cdrom"><source pool="images" volume="seed.iso"/><target dev="sdb" bus="sata"/><readonly/><address type="drive" controller="0" bus="0" target="0" unit="1"/></disk>`

func code(t *testing.T, err error) string {
	t.Helper()
	var d *domain.Error
	if !errors.As(err, &d) {
		t.Fatal("expected a domain error, got", err)
	}
	return d.Code
}

func TestInspectDiskAdditionPicksTheNextFreeTargetAndPort(t *testing.T) {
	raw := diskAdditionXML(sataDisk)
	add, err := InspectDiskAddition(raw, "")
	if err != nil || add != (DiskAddition{Bus: "sata", Target: "sdc", Unit: 2}) {
		t.Fatal(add, err)
	}
	virtio := diskAdditionXML(`<disk type="volume" device="disk"><source pool="images" volume="a.qcow2"/><target dev="vda" bus="virtio"/></disk>`)
	add, err = InspectDiskAddition(virtio, "")
	if err != nil || add != (DiskAddition{Bus: "virtio", Target: "vdb", Unit: -1}) {
		t.Fatal(add, err)
	}
}

func TestAddDiskInsertsOneDiskAndKeepsEverythingElse(t *testing.T) {
	raw := diskAdditionXML(sataDisk)
	add, err := InspectDiskAddition(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	add.Pool, add.Volume = "images", "virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-002.qcow2"
	out, err := AddDisk(raw, add)
	if err != nil {
		t.Fatal(err)
	}
	inserted := `<disk type="volume" device="disk"><driver name="qemu" type="qcow2" cache="writethrough" error_policy="stop"/><source pool="images" volume="virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-002.qcow2"/><target dev="sdc" bus="sata"/><address type="drive" controller="0" bus="0" target="0" unit="2"/></disk>`
	if out != strings.Replace(raw, "</devices>", inserted+"</devices>", 1) {
		t.Fatal(out)
	}
	if !strings.Contains(out, `private:value="unchanged"`) || strings.Count(out, "<disk ") != 3 {
		t.Fatal("unrelated XML changed", out)
	}
	// The new disk is not bootable and no controller was added.
	if strings.Count(out, "<controller ") != 1 || strings.Contains(inserted, "<boot") {
		t.Fatal("device inventory changed", out)
	}
	virtio := diskAdditionXML(`<disk type="volume" device="disk"><source pool="images" volume="a.qcow2"/><target dev="vda" bus="virtio"/></disk>`)
	out, err = AddDisk(virtio, DiskAddition{Pool: "images", Volume: "b.qcow2", Bus: "virtio", Target: "vdb", Unit: -1})
	if err != nil || strings.Count(out, "<address") != 0 {
		t.Fatal(out, err)
	}
}

func TestAddDiskRefusesStaleAndUnsupportedRequests(t *testing.T) {
	raw := diskAdditionXML(sataDisk)
	for name, test := range map[string]struct {
		raw  string
		add  DiskAddition
		want string
	}{
		"target in use":     {raw, DiskAddition{Pool: "images", Volume: "b.qcow2", Bus: "sata", Target: "sda", Unit: 3}, "STALE_PLAN"},
		"port in use":       {raw, DiskAddition{Pool: "images", Volume: "b.qcow2", Bus: "sata", Target: "sdc", Unit: 1}, "STALE_PLAN"},
		"bus mismatch":      {raw, DiskAddition{Pool: "images", Volume: "b.qcow2", Bus: "sata", Target: "vdc", Unit: 2}, "INVALID_INPUT"},
		"unknown bus":       {raw, DiskAddition{Pool: "images", Volume: "b.qcow2", Bus: "ide", Target: "sdc", Unit: 2}, "INVALID_INPUT"},
		"port out of range": {raw, DiskAddition{Pool: "images", Volume: "b.qcow2", Bus: "sata", Target: "sdc", Unit: 9}, "INVALID_INPUT"},
		"empty volume":      {raw, DiskAddition{Pool: "images", Bus: "sata", Target: "sdc", Unit: 2}, "INVALID_INPUT"},
		"no controller": {strings.Replace(raw, `<controller type="sata" index="0"/>`, "", 1),
			DiskAddition{Pool: "images", Volume: "b.qcow2", Bus: "sata", Target: "sdc", Unit: 2}, "UNSUPPORTED_CAPABILITY"},
	} {
		if _, err := AddDisk(test.raw, test.add); code(t, err) != test.want {
			t.Fatal(name, err)
		}
	}
	if _, err := InspectDiskAddition(`<domain type="kvm"><name>x</name></domain>`, "sata"); err == nil {
		t.Fatal("a definition without devices was accepted")
	}
	mixed := diskAdditionXML(sataDisk + `<disk type="volume" device="disk"><source pool="images" volume="c.qcow2"/><target dev="vda" bus="virtio"/></disk>`)
	if code(t, mustFail(t, mixed, "")) != "INVALID_INPUT" {
		t.Fatal("mixed buses did not ask for an explicit bus")
	}
	full := `<controller type="sata" index="0"/>`
	for unit, dev := range []string{"sda", "sdb", "sdc", "sdd", "sde", "sdf"} {
		full += `<disk type="volume" device="disk"><source pool="images" volume="` + dev + `.qcow2"/><target dev="` + dev + `" bus="sata"/><address type="drive" controller="0" bus="0" target="0" unit="` + string(rune('0'+unit)) + `"/></disk>`
	}
	if code(t, mustFail(t, strings.Replace(diskAdditionXML(""), `<controller type="sata" index="0"/>`, full, 1), "sata")) != "UNSUPPORTED_CAPABILITY" {
		t.Fatal("an exhausted controller was accepted")
	}
}

func mustFail(t *testing.T, raw, bus string) error {
	t.Helper()
	add, err := InspectDiskAddition(raw, bus)
	if err == nil {
		t.Fatal("expected a refusal, got", add)
	}
	return err
}
