package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app/importer"
)

func sourceDescriptionFixture() importer.SourceDescription {
	return importer.SourceDescription{Source: "/media/disk.qcow2", Kind: "disk", Root: "/media", Name: "disk", Format: "qcow2", PhysicalBytes: 8 << 20, Files: []string{"disk.qcow2", "base.raw"}, Disks: []importer.SourceDisk{{ID: "boot", Path: "disk.qcow2", Format: "qcow2", VirtualBytes: (32 << 20) + 1, PhysicalBytes: 8 << 20, BackingPath: "base.raw", BackingFormat: "raw"}}, Warnings: []string{"Boot compatibility is not known."}}
}
func sourceSummaryFixture(t *testing.T, description importer.SourceDescription) ImportForm {
	t.Helper()
	f := NewImportForm("auto")
	f.Draft.SelectedSource = description.Source
	f.Draft.Source = description.Source
	if err := f.Draft.ApplySourceDescription(description); err != nil {
		t.Fatal(err)
	}
	f.Page = 3
	return f
}
func TestSourceSummaryDiskDescriptionKeepsSelectedFileAndCompleteInput(t *testing.T) {
	r := sourceDescriptionFixture()
	f := sourceSummaryFixture(t, r)
	d := f.Draft
	if d.Kind != "disks" || d.Source != "/media" || d.SelectedSource != "/media/disk.qcow2" || !d.HasSourceDescription() {
		t.Fatal("selected file and service root were conflated", d)
	}
	if d.VMName != "disk" || d.VCPUs != "2" || d.MemoryMiB != "2048" || d.Offline {
		t.Fatal("invalid suggested defaults")
	}
	if len(d.Disks) != 1 || d.Disks[0].Format != "qcow2" || d.Disks[0].SizeMiB != "33" || len(d.Files) != 2 {
		t.Fatal("metadata disk size/format or backing list lost", d)
	}
	d.DestinationParent = "/output"
	d.DestinationName = "prepared"
	d.Offline = true
	method, request, err := d.Request("qemu:///system")
	if err != nil || method != "import.prepare-disks" || request.Path != "/media" {
		t.Fatal("existing service input changed", method, request, err)
	}
	rows := request.Input["disks"].([]map[string]any)
	if rows[0]["maximumVirtualBytes"] != int64(33<<20) || len(request.Input["files"].([]map[string]any)) != 2 {
		t.Fatal("input mapping incomplete")
	}
	data, _ := json.Marshal(request.Input)
	if strings.Contains(string(data), "description") || strings.Contains(string(data), "selectedSource") || strings.Contains(string(data), "memoryMiB") {
		t.Fatal("UI metadata leaked into legacy preparation schema", string(data))
	}
	controls := f.controls()
	all := ""
	for _, c := range controls {
		all += c.value + "\n"
	}
	for _, want := range []string{"/media/disk.qcow2", "Disk image (qcow2)", "Backing dependency: base.raw", "Included backing / extent: base.raw", "Operating system: Not determined", "Boot compatibility is not known."} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in summary %s", want, all)
		}
	}
	f = importFocus(t, f, "vcpus")
	if !strings.Contains(f.controls()[f.Focus].help, "Suggested") || strings.Contains(f.controls()[f.Focus].help, "Detected") {
		t.Fatal("fallback CPU claimed detected")
	}
}
func TestSourceSummaryISOUsesExplicitBlankDiskOptions(t *testing.T) {
	r := importer.SourceDescription{Source: "/media/installer.iso", Kind: "iso", Name: "Installer", Format: "iso9660", PhysicalBytes: 2 << 30}
	f := sourceSummaryFixture(t, r)
	if f.Draft.Kind != "iso" || f.Draft.Source != r.Source || f.Draft.MediaID != "installer" || len(f.Draft.Disks) != 1 || f.Draft.Disks[0].SizeMiB != "32768" || f.Draft.Offline {
		t.Fatal("ISO suggested options wrong")
	}
	text := ""
	for _, c := range f.controls() {
		text += c.value + "\n"
	}
	if !strings.Contains(text, "32768 MiB (VM option; editable)") || !strings.Contains(text, "Operating system: Not determined") || strings.Contains(text, "boot verified") {
		t.Fatal("ISO summary makes unsupported inference", text)
	}
	f.Draft.VMName = "Owner VM"
	f.Draft.VCPUs = "6"
	f.Draft.MemoryMiB = "4096"
	f.Draft.Disks[0].SizeMiB = "65536"
	if err := f.Draft.ApplySourceDescription(r); err != nil {
		t.Fatal(err)
	}
	if f.Draft.VMName != "Owner VM" || f.Draft.VCPUs != "6" || f.Draft.MemoryMiB != "4096" || f.Draft.Disks[0].SizeMiB != "65536" {
		t.Fatal("same-source refresh lost user choices")
	}
}
func TestSourceSummaryDescriptionRejectsMismatchEscapeAndUnsupported(t *testing.T) {
	for _, change := range []func(*importer.SourceDescription){
		func(r *importer.SourceDescription) { r.Source = "/other/disk.qcow2" },
		func(r *importer.SourceDescription) { r.Root = "/" },
		func(r *importer.SourceDescription) { r.Files = []string{"../escape"} },
		func(r *importer.SourceDescription) { r.Files = []string{"disk.qcow2", "disk.qcow2"} },
		func(r *importer.SourceDescription) { r.Disks[0].VirtualBytes = -1 },
		func(r *importer.SourceDescription) { r.Kind = "unknown" },
	} {
		d := ImportDraft{Kind: "auto", SelectedSource: "/media/disk.qcow2", Source: "/media/disk.qcow2"}
		before := d
		r := sourceDescriptionFixture()
		change(&r)
		if err := d.ApplySourceDescription(r); err == nil || !reflect.DeepEqual(d, before) {
			t.Fatal("bad metadata accepted or partial state mutation", err)
		}
	}
	r := sourceDescriptionFixture()
	r.Kind = "disks"
	r.Source = r.Root
	f := sourceSummaryFixture(t, r)
	if f.Draft.SelectedSource != "/media" || f.Draft.Kind != "disks" {
		t.Fatal("folder description not supported")
	}
}
func TestSourceSummaryOVADelegatesApplianceInspection(t *testing.T) {
	r := importer.SourceDescription{Source: "/media/appliance.ova", Kind: "ova", Name: "Appliance", Appliance: &importer.Report{Source: "/media/appliance.ova", Systems: []importer.System{{ID: "guest", Name: "Original VM", DiskIDs: []string{"boot"}, Items: []importer.Item{{ResourceType: "3", Quantity: "4"}, {ResourceType: "4", MemoryMiB: 8192}}}}, Disks: []importer.Disk{{ID: "boot", Path: "boot.vmdk", Format: "vmdk", CapacityBytes: 64 << 30}}}}
	f := sourceSummaryFixture(t, r)
	if f.Draft.Kind != "ova" || f.Draft.SystemID != "guest" || f.Draft.VCPUs != "4" || f.Draft.MemoryMiB != "8192" || !strings.Contains(f.View(80, 24), "Review appliance") {
		t.Fatal("appliance path lost original metadata")
	}
}
func TestSourceSummaryInspectNavigationAndInvalidation(t *testing.T) {
	for _, kind := range []string{"auto", "iso", "disks"} {
		f := NewImportForm(kind)
		f.Draft.Source = "/media/selected"
		f = importFocus(t, f, "next")
		_, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if intent.Kind != "inspect" {
			t.Fatal("every source needs automatic description", kind, intent)
		}
	}
	f := sourceSummaryFixture(t, sourceDescriptionFixture())
	f.Page = 0
	f = importFocus(t, f, "next")
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 3 || intent.Kind != "" {
		t.Fatal("described source did not open summary")
	}
	f = importFocus(t, f, "next")
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 1 || intent.Kind != "" {
		t.Fatal("summary did not continue to destination")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.Page != 3 {
		t.Fatal("destination did not return to summary")
	}
	f = importFocus(t, f, "hardware")
	_, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "hardware" {
		t.Fatal("advanced VM settings unavailable")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	f = importFocus(t, f, "source")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if f.Draft.Description != nil || f.Draft.SelectedSource != "" || f.Draft.VMName != "" || f.Draft.VCPUs != "" || f.Draft.MemoryMiB != "" {
		t.Fatal("source change retained stale metadata")
	}
}
func TestSourceSummaryTerminalBoundsAndExplicitUnknownFields(t *testing.T) {
	r := sourceDescriptionFixture()
	r.Disks[0].Format = ""
	r.Disks[0].VirtualBytes = 0
	f := sourceSummaryFixture(t, r)
	if f.Draft.Disks[0].Format != "" || f.Draft.Disks[0].SizeMiB != "" {
		t.Fatal("unknown disk metadata was guessed")
	}
	for i := range f.controls() {
		f.Focus = i
		for _, size := range [][2]int{{80, 24}, {60, 18}, {120, 36}} {
			view := f.View(size[0], size[1])
			if !strings.Contains(view, "Review source") || !strings.Contains(view, "[ Advanced settings ]") || !strings.Contains(view, "[ Continue ]") {
				t.Fatal("summary navigation hidden", view)
			}
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("summary too tall")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] || strings.ContainsRune(line, '\x1b') {
					t.Fatal("unsafe summary width", line)
				}
			}
		}
	}
	f.Page = 2
	for _, id := range []string{"format", "addBacking", "size"} {
		_ = importFocus(t, f, id)
	}
}
