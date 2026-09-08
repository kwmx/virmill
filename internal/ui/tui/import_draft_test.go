package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"virmill.local/core/internal/app/importer"
)

func importTestDraft(t *testing.T, kind string) ImportDraft {
	t.Helper()
	root := t.TempDir()
	d := ImportDraft{Kind: kind, Source: filepath.Join(root, "source"), DestinationParent: root, DestinationName: "prepared", Offline: true, MediaID: "installer", Disks: []ImportDisk{{ID: "boot", Path: "boot.qcow2", Format: "qcow2", SizeMiB: "16"}, {ID: "data", Path: "data.vmdk", Format: "vmdk", SizeMiB: "32"}}, Files: []ImportFile{{Path: "boot.qcow2"}, {Path: "data.vmdk"}, {Path: "base.raw"}}}
	if kind == "ova" {
		d.SystemID = "machine"
		d.Report = &importer.Report{Source: d.Source, Systems: []importer.System{{ID: "machine", DiskIDs: []string{"boot", "data"}}}}
	}
	return d
}
func TestImportDraftEmitsCompleteExistingServiceSchemas(t *testing.T) {
	for _, kind := range []string{"ova", "iso", "disks"} {
		t.Run(kind, func(t *testing.T) {
			d := importTestDraft(t, kind)
			method, r, err := d.Request("qemu:///system")
			if err != nil {
				t.Fatal(err)
			}
			expected := map[string]string{"ova": "import.prepare", "iso": "import.prepare-install", "disks": "import.prepare-disks"}[kind]
			if method != expected || r.Path != d.Source || r.Input["destination"] != filepath.Join(d.DestinationParent, d.DestinationName) {
				t.Fatal(method, r)
			}
			raw, err := json.Marshal(r.Input)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "parametersFile") || strings.Contains(string(raw), "settingsFile") {
				t.Fatal("settings file required")
			}
			rows := r.Input["disks"].([]map[string]any)
			if len(rows) != 2 {
				t.Fatal("lost disk")
			}
			key := "maximumVirtualBytes"
			if kind == "iso" {
				key = "virtualBytes"
			}
			if rows[0][key] != int64(16<<20) || rows[1][key] != int64(32<<20) {
				t.Fatal(rows)
			}
			if kind != "ova" && r.Input["offlineSources"] != true {
				t.Fatal("offline confirmation lost")
			}
			if kind == "disks" && len(r.Input["files"].([]map[string]any)) != 3 {
				t.Fatal("backing file lost")
			}
		})
	}
}
func TestImportDraftRefusesMissingOrUnsafeOptions(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		change     func(*ImportDraft)
	}{
		{"offline", "iso", func(d *ImportDraft) { d.Offline = false }},
		{"invalid size", "iso", func(d *ImportDraft) { d.Disks[0].SizeMiB = "-1" }},
		{"overflow", "iso", func(d *ImportDraft) { d.Disks[0].SizeMiB = "999999999999999" }},
		{"duplicate", "iso", func(d *ImportDraft) { d.Disks[1].ID = d.Disks[0].ID }},
		{"escape folder", "ova", func(d *ImportDraft) { d.DestinationName = "../output" }},
		{"uninspected", "ova", func(d *ImportDraft) { d.Report = nil }},
		{"source changed", "ova", func(d *ImportDraft) { d.Source += "-changed" }},
		{"partial OVA", "ova", func(d *ImportDraft) { d.Disks = d.Disks[:1] }},
		{"missing rootfile", "disks", func(d *ImportDraft) { d.Files = d.Files[1:] }},
		{"escape disk", "disks", func(d *ImportDraft) { d.Disks[0].Path = "../outside" }},
		{"unsupported format", "disks", func(d *ImportDraft) { d.Disks[0].Format = "auto" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := importTestDraft(t, tc.kind)
			tc.change(&d)
			if _, _, err := d.Request("qemu:///system"); err == nil {
				t.Fatal("accepted unsafe input")
			}
		})
	}
}
func TestImportInspectionRequiresExplicitSystemAndAllDisks(t *testing.T) {
	d := importTestDraft(t, "ova")
	d.SystemID = ""
	d.Report = nil
	r := importer.Report{Source: d.Source, Systems: []importer.System{{ID: "a", DiskIDs: []string{"a1", "a2"}}, {ID: "b", DiskIDs: []string{"b1"}}}, Disks: []importer.Disk{{ID: "a1", Format: "vmdk", Path: "one.vmdk"}, {ID: "a2", Format: "qcow2", Path: "two.qcow2"}, {ID: "b1", Path: "b.raw"}}}
	if err := d.ApplyInspection(r); err != nil {
		t.Fatal(err)
	}
	if d.SystemID != "" || len(d.Disks) != 0 {
		t.Fatal("silently selected one appliance")
	}
	if err := d.SelectSystem("a"); err != nil {
		t.Fatal(err)
	}
	if len(d.Disks) != 2 || d.Disks[0].ID != "a1" || d.Disks[1].Format != "qcow2" {
		t.Fatal(d.Disks)
	}
	if d.Disks[0].SizeMiB != "" {
		t.Fatal("guessed descriptor capacity units")
	}
}
