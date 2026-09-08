package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
)

func importWorkspace(t *testing.T) Workspace {
	t.Helper()
	m := fixtureWorkspace()
	f := NewImportForm("iso")
	f.Draft = importTestDraft(t, "iso")
	f.Page = 2
	m.Import = &f
	return m
}

func TestImportWorkspacePreviewRetainsOptionsAndUsesSharedService(t *testing.T) {
	m := importWorkspace(t)
	f := importFocus(t, *m.Import, "preview")
	m.Import = &f
	c := m.Client.(*workspaceClient)
	c.response = app.Response{Data: testWorkspacePlan(t)}
	m, cmd := wk(m, "enter")
	if cmd == nil || !m.Busy || m.Plan != nil {
		t.Fatal("preview must request a plan")
	}
	n, _ := m.Update(cmd())
	m = n.(Workspace)
	if m.Plan == nil || m.Import == nil || len(c.calls) != 1 || c.calls[0] != "import.prepare-install" || c.requests[0].Input["offlineSources"] != true {
		t.Fatal("plan must retain draft and use the existing service", c.calls)
	}
	m, _ = wk(m, "esc")
	if m.Plan != nil || m.Import == nil || m.Import.Page != 2 || len(m.Import.Draft.Disks) != 2 || m.Import.Draft.Disks[1].SizeMiB != "32" {
		t.Fatal("cancel review lost edited options")
	}
	if len(c.calls) != 1 {
		t.Fatal("cancel must not apply")
	}
	for range 3 {
		m, _ = wk(m, "esc")
	}
	if m.Import != nil || !m.Advanced {
		t.Fatal("back from the reviewed draft must restore its source catalog")
	}
}

func TestImportWorkspaceExportReturnsToDraftAndRefusesOverwrite(t *testing.T) {
	m := importWorkspace(t)
	t.Setenv("HOME", m.Import.Draft.DestinationParent)
	f := importFocus(t, *m.Import, "export")
	m.Import = &f
	m, cmd := wk(m, "enter")
	if cmd != nil || m.ExportForm == nil || !strings.Contains(m.View(), "Export settings") {
		t.Fatal("export must open save dialog")
	}
	path := filepath.Join(m.ExportForm.Fields[0].Value, m.ExportForm.Fields[1].Value)
	m, cmd = wk(m, "enter")
	if cmd == nil || !m.Busy {
		t.Fatal("explicit export missing")
	}
	n, _ := m.Update(cmd())
	m = n.(Workspace)
	if m.ExportForm != nil || m.Import == nil || m.Busy || !strings.Contains(m.Notice, "Settings exported") {
		t.Fatal(m.View())
	}
	before, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(before), "offlineSources") {
		t.Fatal("export absent", err)
	}
	m, _ = wk(m, "enter")
	m, cmd = wk(m, "enter")
	n, _ = m.Update(cmd())
	m = n.(Workspace)
	after, _ := os.ReadFile(path)
	if m.ExportForm == nil || !strings.Contains(m.ExportForm.Error, "already exists") || string(after) != string(before) {
		t.Fatal("export overwrote existing file")
	}
	if len(m.Client.(*workspaceClient).calls) != 0 {
		t.Fatal("export must not prepare or apply an import")
	}
}

func TestImportWorkspaceSourceChangeAndCanceledInspectionInvalidateState(t *testing.T) {
	m := importWorkspace(t)
	m.Import.Draft.SHA256 = strings.Repeat("a", 64)
	m.ImportPickerTarget = "source"
	m.importPicked(filepath.Join(m.Import.Draft.DestinationParent, "other.iso"))
	if m.Import.Draft.Offline || m.Import.Draft.SHA256 != "" || len(m.Import.Draft.Disks) != 2 {
		t.Fatal("new source retained old assurance or lost blank disks")
	}
	f := NewImportForm("ova")
	f.Draft.Source = "/media/appliance.ova"
	f = importFocus(t, f, "inspect")
	m.Import = &f
	c := m.Client.(*workspaceClient)
	c.response = app.Response{Data: importer.Report{Source: f.Draft.Source, Systems: []importer.System{{ID: "guest", DiskIDs: []string{"root"}}}, Disks: []importer.Disk{{ID: "root", Path: "root.raw", Format: "raw"}}}}
	m, cmd := wk(m, "enter")
	reply := cmd()
	m, _ = wk(m, "esc")
	n, _ := m.Update(reply)
	m = n.(Workspace)
	if m.Busy || m.Import.Draft.Report != nil || m.Pending["import-inspect"] != 0 {
		t.Fatal("canceled inspection changed form")
	}
	n, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = n.(Workspace)
	m, cmd = wk(m, "enter")
	if cmd != nil {
		t.Fatal("hidden form submitted")
	}
	m, _ = wk(m, "esc")
	if m.Import != nil {
		t.Fatal("cannot cancel hidden form")
	}
}
