package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app/importer"
)

func importFocus(t *testing.T, f ImportForm, id string) ImportForm {
	t.Helper()
	for i, c := range f.controls() {
		if c.id == id {
			f.Focus = i
			return f
		}
	}
	t.Fatalf("missing %q control on page %d", id, f.Page)
	return f
}

func TestImportFormFitsSmallTerminal(t *testing.T) {
	for _, kind := range []string{"ova", "iso", "disks"} {
		for page := 0; page < 3; page++ {
			f := NewImportForm(kind)
			f.Page = page
			for i := range f.controls() {
				f.Focus = i
				for _, size := range [][2]int{{80, 24}, {60, 18}, {120, 36}, {20, 5}} {
					view := f.View(size[0], size[1])
					if len(strings.Split(view, "\n")) > size[1] {
						t.Fatal("view too tall")
					}
					for _, line := range strings.Split(view, "\n") {
						if ansi.StringWidth(line) > size[0] {
							t.Fatalf("view too wide: %q", line)
						}
					}
					if strings.Contains(view, "Settings file") || strings.Contains(view, "JSON") {
						t.Fatalf("settings-file prompt in native form: %s", view)
					}
				}
			}
		}
	}
}

func TestImportFormShowsValuesWhileEditing(t *testing.T) {
	f := NewImportForm("iso")
	f.Draft.Source = "/media/installer.iso"
	f = importFocus(t, f, "mediaID")
	for _, size := range [][2]int{{80, 24}, {60, 18}} {
		view := f.View(size[0], size[1])
		if !strings.Contains(view, "/media/installer.iso") || !strings.Contains(view, "[installer|]") {
			t.Fatal("short path or editable value disappeared", view)
		}
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new-media")})
	if !strings.Contains(f.View(80, 24), "[new-media|]") {
		t.Fatal("typed value invisible")
	}
	if got := importTail("very-long-name.iso", 8); got != "…ame.iso" {
		t.Fatal("filename suffix truncated incorrectly", got)
	}
}

func TestImportCycleUnknownNeedsExplicitChoice(t *testing.T) {
	if got := importCycle("", []string{"qcow2", "raw"}, 1); got != "qcow2" {
		t.Fatal(got)
	}
	if got := importCycle("qcow2", []string{"qcow2", "raw"}, -1); got != "raw" {
		t.Fatal(got)
	}
}

func TestImportFormEscBackAndCancel(t *testing.T) {
	f := NewImportForm("iso")
	f.Page = 2
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.Page != 1 || intent.Kind != "" {
		t.Fatalf("first Esc must go back: %+v", intent)
	}
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.Page != 0 || intent.Kind != "" {
		t.Fatalf("second Esc must go back: %+v", intent)
	}
	_, intent = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if intent.Kind != "cancel" {
		t.Fatalf("source Esc must cancel: %+v", intent)
	}
}

func TestImportFormPreviewExportAreSeparateButtons(t *testing.T) {
	for _, kind := range []string{"ova", "iso", "disks"} {
		f := NewImportForm(kind)
		f.Page = 2
		for _, id := range []string{"preview", "export"} {
			f = importFocus(t, f, id)
			_, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if intent.Kind != id {
				t.Fatalf("%s %s: %+v", kind, id, intent)
			}
		}
	}
}

func TestImportFormISOMultipleDisksAndExplicitOffline(t *testing.T) {
	f := NewImportForm("iso")
	f.Page = 2
	if f.Draft.Offline {
		t.Fatal("offline must require a user choice")
	}
	original := f
	f = importFocus(t, f, "size")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("8192")})
	if f.Draft.Disks[0].SizeMiB != "8192" || original.Draft.Disks[0].SizeMiB != "32768" {
		t.Fatal("editing must preserve prior model values")
	}
	f = importFocus(t, f, "addDisk")
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "" || len(f.Draft.Disks) != 2 || f.Disk != 1 || f.Draft.Disks[1].ID != "disk2" {
		t.Fatalf("add: %+v %+v", f, intent)
	}
	f = importFocus(t, f, "size")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4096")})
	f = importFocus(t, f, "disk")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if f.Disk != 0 || f.Draft.Disks[0].SizeMiB != "8192" || f.Draft.Disks[1].SizeMiB != "4096" {
		t.Fatal("each disk must retain its own options")
	}
	f = importFocus(t, f, "offline")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !f.Draft.Offline {
		t.Fatal("explicit toggle missing")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if f.Draft.Offline {
		t.Fatal("toggle off missing")
	}
	f = importFocus(t, f, "removeDisk")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(f.Draft.Disks) != 1 || f.Draft.Disks[0].ID != "disk2" {
		t.Fatal("remove must use selected disk")
	}
}

func TestImportFormExistingDiskBrowseAndBackingFiles(t *testing.T) {
	f := NewImportForm("disks")
	f.Page = 2
	f = importFocus(t, f, "addDisk")
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "browse" || intent.Target != "disk" || intent.Index != 0 || len(f.Draft.Disks) != 1 {
		t.Fatalf("root browse: %+v", intent)
	}
	f.Draft.Disks[0].Path = "root.qcow2"
	f.Draft.Files = []ImportFile{{Path: "root.qcow2"}, {Path: "base.raw"}}
	f = importFocus(t, f, "format")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if f.Draft.Disks[0].Format != "qcow2" {
		t.Fatal("format must be explicitly selectable")
	}
	f = importFocus(t, f, "addBacking")
	_, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "browse" || intent.Target != "backing" {
		t.Fatal(intent)
	}
	f = importFocus(t, f, "fileOptions")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f = importFocus(t, f, "removeFile")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Error == "" || len(f.Draft.Files) != 2 {
		t.Fatal("root-file removal must be refused")
	}
	f = importFocus(t, f, "file")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	f = importFocus(t, f, "removeFile")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(f.Draft.Files) != 1 || f.Draft.Files[0].Path != "root.qcow2" {
		t.Fatal("remove must use selected backing file")
	}
}

func TestImportFormNavigationAndTextSafety(t *testing.T) {
	f := NewImportForm("iso")
	f = importFocus(t, f, "next")
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 0 || f.Error == "" || intent.Kind != "" {
		t.Fatal("missing source must remain on source page")
	}
	f.Draft.Source = "/media/install.iso"
	f = importFocus(t, f, "next")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 1 {
		t.Fatal("valid source should advance")
	}
	f = importFocus(t, f, "destination")
	_, intent = f.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if intent.Kind != "browse" || intent.Target != "destination" {
		t.Fatal(intent)
	}
	f = importFocus(t, f, "folder")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("備份")})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if f.Draft.DestinationName != "份" {
		t.Fatal("rune-aware edit lost text")
	}
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "" || f.Page != 1 {
		t.Fatal("Enter inside a text field must not submit")
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[2J")})
	if f.Draft.DestinationName != "份" || f.Error == "" {
		t.Fatal("terminal controls must be rejected")
	}
	f.Draft.DestinationName = "bad\x1b[2J\nline"
	if strings.Contains(f.View(80, 24), "\x1b") {
		t.Fatal("rendered unsafe escape")
	}
}

func TestImportFormOVAUsesEveryInspectedDiskAndInvalidatesSource(t *testing.T) {
	f := NewImportForm("ova")
	f.Draft.Source = "/media/appliance.ova"
	report := importer.Report{Source: f.Draft.Source, Systems: []importer.System{{ID: "first", DiskIDs: []string{"os", "data"}}, {ID: "other", DiskIDs: []string{"other-disk"}}}, Disks: []importer.Disk{{ID: "os", Path: "os.vmdk", Format: "vmdk"}, {ID: "data", Path: "data.raw", Format: "raw"}, {ID: "other-disk", Path: "other.qcow2", Format: "qcow2"}}}
	if err := f.Draft.ApplyInspection(report); err != nil {
		t.Fatal(err)
	}
	if f.Draft.SystemID != "" || len(f.Draft.Disks) != 0 {
		t.Fatal("collection requires explicit system choice")
	}
	f = importFocus(t, f, "system")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if f.Draft.SystemID != "first" || len(f.Draft.Disks) != 2 || f.Draft.Disks[0].ID != "os" || f.Draft.Disks[1].ID != "data" {
		t.Fatal("inspected attachment order lost")
	}
	f.Page = 2
	for _, c := range f.controls() {
		if c.id == "diskID" || c.id == "removeDisk" || c.id == "addDisk" {
			t.Fatal("OVA disk identity/list must stay tied to inspection")
		}
	}
	f = importFocus(t, f, "disk")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if f.Disk != 1 || !strings.Contains(f.View(80, 24), "data.raw") {
		t.Fatal("second disk unavailable")
	}
	f.Page = 0
	f = importFocus(t, f, "source")
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if f.Draft.Report != nil || len(f.Draft.Disks) != 0 || f.Draft.SystemID != "" {
		t.Fatal("source change must invalidate inspection")
	}
}

func TestImportFormContinueInspectsOVAWithoutExtraStep(t *testing.T) {
	f := NewImportForm("ova")
	f.Draft.Source = "/media/appliance.ova"
	for _, c := range f.controls() {
		if c.id == "inspect" {
			t.Fatal("uninspected source must not require a separate Inspect button")
		}
	}
	f = importFocus(t, f, "next")
	if f.controls()[f.Focus].label != "Continue" {
		t.Fatal("expected one clear next step")
	}
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "inspect" || f.Error != "" || f.Page != 0 {
		t.Fatalf("Continue must request inspection without an error: %+v %+v", f, intent)
	}
	report := importer.Report{Source: f.Draft.Source, Systems: []importer.System{{ID: "one", DiskIDs: []string{"disk1"}}}, Disks: []importer.Disk{{ID: "disk1", Path: "disk.vmdk", Format: "vmdk"}}}
	if err := f.Draft.ApplyInspection(report); err != nil {
		t.Fatal(err)
	}
	f = importFocus(t, f, "next")
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "" || f.Error != "" || f.Page != 3 {
		t.Fatalf("inspected source should continue: %+v %+v", f, intent)
	}
}

func TestImportFormContinueNeedsAnExplicitCollectionMember(t *testing.T) {
	f := NewImportForm("ova")
	f.Draft.Source = "/media/collection.ova"
	report := importer.Report{Source: f.Draft.Source, Systems: []importer.System{{ID: "a", DiskIDs: []string{"disk-a"}}, {ID: "b", DiskIDs: []string{"disk-b"}}}, Disks: []importer.Disk{{ID: "disk-a", Path: "a.vmdk", Format: "vmdk"}, {ID: "disk-b", Path: "b.raw", Format: "raw"}}}
	if err := f.Draft.ApplyInspection(report); err != nil {
		t.Fatal(err)
	}
	f = importFocus(t, f, "next")
	f, intent := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "" || f.Page != 0 || f.Error != "Choose which appliance to import." || f.controls()[f.Focus].id != "system" {
		t.Fatal("collection must focus the missing choice", f.Error, intent)
	}
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	f = importFocus(t, f, "next")
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.Page != 3 || f.Error != "" || intent.Kind != "" || f.Draft.SystemID != "a" || f.Draft.Disks[0].ID != "disk-a" {
		t.Fatal("selected member did not advance intact")
	}
	f.Page = 0
	f.Draft.Source = "/media/replacement.ova"
	f = importFocus(t, f, "next")
	f, intent = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "inspect" || f.Draft.Report != nil || len(f.Draft.Disks) != 0 || f.Draft.SystemID != "" {
		t.Fatal("stale report must be discarded before inspection")
	}
}

func TestImportFormPrimaryActionVisibleWithoutDuplicateKeyGuide(t *testing.T) {
	for _, kind := range []string{"ova", "iso", "disks"} {
		f := NewImportForm(kind)
		f.Draft.Disks = []ImportDisk{{ID: "one", Path: "disk.qcow2"}}
		f.Draft.Files = []ImportFile{{Path: "disk.qcow2"}, {Path: "backing.raw"}}
		f.advanced = true
		for page := 0; page < 3; page++ {
			f.Page = page
			for i := range f.controls() {
				f.Focus = i
				for _, size := range [][2]int{{80, 18}, {60, 12}, {120, 30}} {
					view := f.View(size[0], size[1])
					primary := "[ Continue ]"
					if page == 2 {
						primary = "[ Preview image preparation ]"
					}
					if !strings.Contains(view, primary) || strings.Count(view, primary) != 1 {
						t.Fatalf("primary action should remain visible once: %s", view)
					}
					if !strings.Contains(view, "Step ") || strings.Contains(view, "Tab Next option") || strings.Contains(view, "Enter Choose") {
						t.Fatalf("step heading or single keyboard guide violated: %s", view)
					}
					if len(strings.Split(view, "\n")) > size[1] {
						t.Fatal("tall primary-action view")
					}
					for _, line := range strings.Split(view, "\n") {
						if ansi.StringWidth(line) > size[0] {
							t.Fatal("wide primary-action view", line)
						}
					}
				}
			}
		}
	}
}
