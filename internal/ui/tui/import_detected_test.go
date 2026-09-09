package tui

import (
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app/importer"
)

func detectedImportDraft(t *testing.T, capacity int64) ImportDraft {
	t.Helper()
	d := ImportDraft{Kind: "ova", Source: "/media/appliance.ova", DestinationParent: "/media", DestinationName: "prepared"}
	r := importer.Report{Source: d.Source, Systems: []importer.System{{ID: "guest", DiskIDs: []string{"disk"}, Items: []importer.Item{{ResourceType: "3", Quantity: "2"}, {ResourceType: "4", Quantity: "8192", AllocationUnits: "byte * 2^20", MemoryMiB: 8192}}}}, Disks: []importer.Disk{{ID: "disk", Path: "disk.vmdk", Format: "vmdk", CapacityBytes: capacity}}}
	if err := d.ApplyInspection(r); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestImportDetectedCapacityDefaultsWithoutGuessingOrReducing(t *testing.T) {
	for _, tc := range []struct {
		capacity int64
		want     string
	}{
		{120 << 30, "122880"}, {1<<20 + 1, "2"}, {1, "1"}, {512 << 30, "524288"},
		{0, ""}, {-1, ""}, {512<<30 + 1, ""}, {math.MaxInt64, ""},
	} {
		d := detectedImportDraft(t, tc.capacity)
		if d.Disks[0].SizeMiB != tc.want {
			t.Fatalf("capacity %d defaulted to %s; want %s", tc.capacity, d.Disks[0].SizeMiB, tc.want)
		}
	}
	d := detectedImportDraft(t, 120<<30)
	_, request, err := d.Request("qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	if request.Input["disks"].([]map[string]any)[0]["maximumVirtualBytes"] != int64(120<<30) {
		t.Fatal("recognized disk limit changed")
	}
	d.Disks[0].SizeMiB = "1024"
	if _, _, err := d.Request("qemu:///system"); err == nil || !strings.Contains(err.Error(), "cannot be smaller") {
		t.Fatalf("lower capacity bypassed storage requirement: %v", err)
	}
	d = detectedImportDraft(t, 0)
	if _, _, err := d.Request("qemu:///system"); err == nil {
		t.Fatal("unknown units did not require a disk limit")
	}
}

func TestImportDetectedResourcesRejectAmbiguousOrRawMemoryGuess(t *testing.T) {
	d := detectedImportDraft(t, 1<<30)
	if cpu, memory := d.DetectedResources(); cpu != 2 || memory != 8192 {
		t.Fatalf("lost known resources: %d %d", cpu, memory)
	}
	d.Report.Systems[0].Items[1].MemoryMiB = 0
	if _, memory := d.DetectedResources(); memory != 0 {
		t.Fatal("guessed memory from raw quantity without normalized units")
	}
	d.Report.Systems[0].Items = append(d.Report.Systems[0].Items, importer.Item{ResourceType: "3", Quantity: "4"}, importer.Item{ResourceType: "4", MemoryMiB: 16384})
	if cpu, memory := d.DetectedResources(); cpu != 0 || memory != 0 {
		t.Fatal("ambiguous hardware items were silently combined")
	}
	d.SystemID = "other"
	if cpu, memory := d.DetectedResources(); cpu != 0 || memory != 0 {
		t.Fatal("resources leaked across system selection")
	}
}

func TestImportDiskPageExplainsPreparationAndPlacesFormatBehindAdvanced(t *testing.T) {
	f := ImportForm{Draft: detectedImportDraft(t, 120<<30), Page: 2}
	view := f.View(80, 24)
	for _, text := range []string{"Review disk conversion, then confirm the VM before creation.", "Selected VM: 2 CPUs", "8192 MiB RAM", "Preview image preparation", "122880"} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing clear preparation detail %q: %s", text, view)
		}
	}
	for _, c := range f.controls() {
		if c.id == "format" {
			t.Fatal("recognized source format is a front-page choice")
		}
	}
	f = importFocus(t, f, "advanced")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = importFocus(t, f, "format")
	if f.controls()[f.Focus].value != "vmdk" {
		t.Fatal("advanced format choice lost detected value")
	}
	f.Draft.Disks[0].Format = ""
	f.advanced = false
	f = importFocus(t, f, "format")
	if f.controls()[f.Focus].id != "format" {
		t.Fatal("missing source format was hidden")
	}
}

func TestImportSpaceRefusalKeepsFullMessageAndDestinationAction(t *testing.T) {
	f := ImportForm{Draft: detectedImportDraft(t, 120<<30), Page: 2, Error: "INSUFFICIENT_SPACE: Needs 190.40 GiB; 55.00 GiB available (135.40 GiB short). Choose another destination or free at least 135.40 GiB."}
	for _, size := range [][2]int{{80, 18}, {120, 30}, {60, 12}} {
		current := f
		views := ""
		for range 12 {
			view := current.View(size[0], size[1])
			if !strings.Contains(view, "[ Back: Destination ]") || !strings.Contains(view, "[ Preview image preparation ]") {
				t.Fatalf("storage refusal hid an action: %s", view)
			}
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("error overflowed terminal height")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("error overflowed terminal width")
				}
			}
			views += "\n" + view
			current, _ = current.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		}
		for _, required := range []string{"Not enough storage", "190.40 GiB", "55.00 GiB available", "135.40 GiB short", "free at least 135.40 GiB"} {
			if !strings.Contains(views, required) {
				t.Fatalf("error discarded required detail %q: %s", required, views)
			}
		}
	}
	f = importFocus(t, f, "back")
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 1 || f.Error != "" || intent.Kind != "" || f.Draft.Disks[0].SizeMiB != "122880" {
		t.Fatal("destination action changed draft or started work")
	}
	f.Error = "Source inspection failed."
	if strings.Contains(f.View(80, 24), "Not enough storage") {
		t.Fatal("unrelated error mislabeled as insufficient space")
	}
}
