package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app/importer"
)

func applianceSummaryFixture(t *testing.T) ImportForm {
	t.Helper()
	yes := true
	f := NewImportForm("ova")
	f.Draft.Source = "/media/windows.ova"
	report := importer.Report{Source: f.Draft.Source, FileReferences: map[string]string{"state": "Windows11.nvram"}, Members: []importer.Member{{Path: "Windows11.nvram"}, {Path: "unrelated.nvram"}}, Disks: []importer.Disk{{ID: "boot", Path: "Windows11-disk.vmdk", CapacityBytes: 64 << 30, Format: "vmdk"}}, Systems: []importer.System{{ID: "guest", Name: "Windows guest", OS: "Windows11_64", OVFOS: "Windows10_64", OSSource: "virtualbox", Firmware: "uefi", DiskIDs: []string{"boot"}, Items: []importer.Item{{ResourceType: "3", Quantity: "4"}, {ResourceType: "4", MemoryMiB: 8192}, {ResourceType: "20", ResourceSubType: "AHCI"}, {ResourceType: "10", ResourceSubType: "E1000"}, {ResourceType: "32768", HostResources: []string{"ovf:/file/state"}}, {ResourceType: "999", ResourceSubType: "odd-card", Description: "Custom card"}}, Devices: []importer.DeviceHint{{Kind: "usb", Model: "OHCI", Enabled: &yes}, {Kind: "audio", Model: "AC97", Enabled: &yes}}}}}
	if err := f.Draft.ApplyInspection(report); err != nil {
		t.Fatal(err)
	}
	f.Page = 3
	return f
}
func TestApplianceSummaryShowsAllMetadataWithoutClaimingNVRAMSupport(t *testing.T) {
	f := applianceSummaryFixture(t)
	text := []string{}
	for _, c := range f.controls() {
		text = append(text, c.label+" "+c.value)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"Windows 11 (64-bit)", "Windows10_64", "Windows11-disk.vmdk", "64.00 GiB", "SATA (AHCI)", "E1000", "OHCI", "AC97", "Firmware state: included; restoration not supported", "Windows11.nvram", "Needs review: Device type 999", "Custom card"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in %s", want, all)
		}
	}
	if strings.Contains(all, "unrelated.nvram") {
		t.Fatal("unattached NVRAM was claimed as guest firmware state")
	}
	f.Draft.Report.Members = []importer.Member{{Path: "unrelated.nvram"}}
	lines := []string{}
	for _, c := range f.controls() {
		lines = append(lines, c.value)
	}
	if strings.Contains(strings.Join(lines, "\n"), "Firmware state: included") {
		t.Fatal("missing attachment was claimed included")
	}
}
func TestApplianceSummaryEditsNavigationAndAdvancedIntent(t *testing.T) {
	f := applianceSummaryFixture(t)
	f = importFocus(t, f, "vmName")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Owner guest")})
	f = importFocus(t, f, "vcpus")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("8")})
	f = importFocus(t, f, "memoryMiB")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4096")})
	f = importFocus(t, f, "hardware")
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "hardware" || f.Page != 3 || f.Draft.VMName != "Owner guest" || f.Draft.VCPUs != "8" || f.Draft.MemoryMiB != "4096" {
		t.Fatal("advanced settings lost summary edits")
	}
	f = importFocus(t, f, "next")
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 1 || intent.Kind != "" {
		t.Fatal("summary did not advance to destination")
	}
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.Page != 3 || intent.Kind != "" {
		t.Fatal("destination Esc did not return to summary")
	}
	f.Draft.VCPUs = "bad"
	f = importFocus(t, f, "next")
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 3 || f.controls()[f.Focus].id != "vcpus" || f.Error == "" || intent.Kind != "" {
		t.Fatal("invalid summary CPU not focused")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.Page != 0 {
		t.Fatal("summary back did not return to source")
	}
	f = importFocus(t, f, "source")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if f.Draft.VMName != "" || f.Draft.VCPUs != "" || f.Draft.MemoryMiB != "" {
		t.Fatal("new source inherited summary fields")
	}
}
func TestApplianceSummaryEveryRowReachableAndActionsVisible(t *testing.T) {
	f := applianceSummaryFixture(t)
	for _, size := range [][2]int{{80, 24}, {80, 18}, {60, 18}, {120, 36}} {
		for i, c := range f.controls() {
			f.Focus = i
			view := f.View(size[0], size[1])
			if !strings.Contains(view, "Review appliance") || !strings.Contains(view, "[ Continue ]") || !strings.Contains(view, "[ Advanced settings ]") {
				t.Fatal("summary primary actions disappeared", view)
			}
			if c.kind == "summary" && !strings.Contains(view, "> "+c.value) {
				t.Fatal("metadata row unreachable", c.value, view)
			}
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("summary too tall")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] || strings.ContainsRune(line, '\x1b') {
					t.Fatal("unsafe summary", line)
				}
			}
		}
	}
	f.Draft.Report.Systems[0].OS = "bad\x1b[2Jvalue"
	if strings.ContainsRune(f.View(80, 24), '\x1b') {
		t.Fatal("metadata escaped terminal")
	}
}

func TestApplianceSummaryTabSkipsReadOnlyMetadata(t *testing.T) {
	f := applianceSummaryFixture(t)
	if f.controls()[0].id != "vmName" {
		t.Fatal("summary should focus VM name first")
	}
	wanted := []string{"vcpus", "memoryMiB", "hardware", "back", "next", "vmName"}
	for _, id := range wanted {
		f, _ = f.Update(tea.KeyMsg{Type: tea.KeyTab})
		if f.controls()[f.Focus].id != id {
			t.Fatalf("Tab should reach %s without crossing read-only rows; got %s", id, f.controls()[f.Focus].id)
		}
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if f.Focus == 0 || f.controls()[f.Focus].kind != "summary" {
		t.Fatal("metadata must remain reachable with page navigation")
	}
}

func TestApplianceSummaryProgressUsesSelectedResourcesAndShowsHiddenRows(t *testing.T) {
	f := applianceSummaryFixture(t)
	view := f.View(80, 18)
	if !strings.Contains(view, "rows below") || !strings.Contains(view, "PgDn reads more") {
		t.Fatal("hidden metadata has no read hint", view)
	}
	if !strings.Contains(view, "[ Continue ]") || !strings.Contains(view, "[ Advanced settings ]") {
		t.Fatal("read hint displaced primary actions")
	}
	f.Draft.VCPUs, f.Draft.MemoryMiB = "8", "4096"
	for _, page := range []int{1, 2} {
		f.Page = page
		f.Focus = 0
		view = f.View(80, 24)
		if strings.Contains(view, "come next") || strings.Contains(view, "VM setup follows") {
			t.Fatal("progress contradicts configured VM options", view)
		}
	}
	if !strings.Contains(view, "Selected VM: 8 CPUs · 4096 MiB RAM") || strings.Contains(view, "8192 MiB RAM") {
		t.Fatal("progress showed original rather than selected hardware", view)
	}
}
