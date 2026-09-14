package tui

import (
	"strings"
	"testing"

	"virmill.local/core/internal/app/importer"
)

func importFormatControl(f ImportForm) (importControl, bool) {
	for _, c := range f.controls() {
		if c.id == "format" {
			return c, true
		}
	}
	return importControl{}, false
}

// An OVA that declares no disk format keeps the format choice visible, with a
// labelled suggestion from the file name; a declared format moves behind Advanced.
func TestUndeclaredOVAFormatStaysVisibleAndIsSuggested(t *testing.T) {
	report := &importer.Report{Source: "/media/probe.ova", Disks: []importer.Disk{{ID: "boot", Path: "boot.vmdk", CapacityBytes: 16 << 20}}, Systems: []importer.System{{ID: "guest", Name: "probe", DiskIDs: []string{"boot"}, Items: []importer.Item{}}}}
	f := NewImportForm("ova")
	f.Draft.Source = report.Source
	if err := f.Draft.ApplyInspection(*report); err != nil {
		t.Fatal(err)
	}
	if len(f.Draft.Disks) != 1 || f.Draft.Disks[0].Format != "vmdk" {
		t.Fatal("undeclared vmdk format not suggested from the file name", f.Draft.Disks)
	}
	f.Page = 2
	c, ok := importFormatControl(f)
	if !ok || !strings.Contains(c.help, "Suggested from the file name") {
		t.Fatal("suggested format not visible and labelled", c)
	}
	f.Draft.Disks[0].Format = "qcow2"
	if c, ok = importFormatControl(f); !ok || strings.Contains(c.help, "Suggested") {
		t.Fatal("a changed undeclared format disappeared or kept the suggestion label")
	}
	f.Draft.Report.Disks[0].Format = "http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized"
	if _, ok = importFormatControl(f); ok {
		t.Fatal("declared format is not behind Advanced disk options")
	}
	for name, want := range map[string]string{"a.vmdk": "vmdk", "b.VHD": "vpc", "c.vhdx": "vhdx", "d.qcow2": "qcow2", "e.vdi": "vdi", "f.img": "", "g": ""} {
		if got := formatFromName(name); got != want {
			t.Fatalf("formatFromName(%q) = %q, want %q", name, got, want)
		}
	}
}
