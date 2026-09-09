package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/ui"
)

func TestImportOpensBrowserAndAutomaticallyDescribesSelection(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "appliance with spaces.ova")
	if err := os.WriteFile(source, []byte("picker metadata fixture, not OVA validation"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	m := fixtureWorkspace()
	m.Section = 1
	m, _ = wk(m, "i")
	m, cmd := wk(m, "enter")
	if m.Picker == nil || m.Import == nil || cmd == nil {
		t.Fatal("import must open explorer")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if !strings.Contains(m.View(), "Choose a file") || !strings.Contains(m.View(), filepath.Base(source)) {
		t.Fatal(m.View())
	}
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("select must observe source")
	}
	next, cmd = m.Update(cmd())
	m = next.(Workspace)
	if m.Picker != nil || m.Import.Draft.Source != source || !m.Busy || m.Plan != nil || cmd == nil {
		t.Fatal("selection must start a read-only description", m.View())
	}
	if len(m.Client.(*workspaceClient).calls) != 0 {
		t.Fatal("selecting file called service")
	}
	m, _ = wk(m, "esc") // cancel metadata read
	m, cmd = wk(m, "esc")
	if cmd != nil || m.Import != nil || !m.Advanced {
		t.Fatal("back must return to import choices")
	}
}
func TestPickerCancelAndResizeCannotSubmitUnderlyingForm(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	m := fixtureWorkspace()
	m.Section = 1
	m, _ = wk(m, "i")
	m, cmd := wk(m, "enter")
	pending := cmd()
	m, _ = wk(m, "esc")
	if m.Picker != nil || m.Import == nil {
		t.Fatal("picker cancel lost form")
	}
	n, _ := m.Update(pending)
	m = n.(Workspace)
	if m.Picker != nil || m.Import.Draft.Source != "" {
		t.Fatal("late reply changed canceled picker")
	}
	n, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = n.(Workspace)
	if m.Picker == nil || cmd == nil {
		t.Fatal("browse current path field")
	}
	n, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = n.(Workspace)
	m, cmd = wk(m, "enter")
	if cmd != nil || m.Busy {
		t.Fatal("hidden picker submitted")
	}
	n, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = n.(Workspace)
	m, _ = wk(m, "esc")
	if m.Picker != nil || m.Width != 80 {
		t.Fatal("resize/back lost state")
	}
}
func TestPickerPreservesInventoryRepliesAndOtherFields(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	m := fixtureWorkspace()
	m.Section = 1
	f := actionFixture(t, "import prepare")
	f.Form.Fields[1].Value = "/settings.json"
	m.ActionForm = &f
	cmd := m.browseField()
	if cmd == nil {
		t.Fatal("browse absent")
	}
	reply := m.request("jobs", "operation.list", app.Request{})
	n, _ := m.Update(reply())
	m = n.(Workspace)
	if m.Picker == nil || m.ActionForm.Form.Fields[1].Value != "/settings.json" || m.Pending["jobs"] != 0 {
		t.Fatal("lost independent state")
	}
}
func TestCommonTasksKeepAllAdvancedActionsReachable(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	m, _ = wk(m, "a")
	if len(m.catalog()) > 6 {
		t.Fatal("common menu too long")
	}
	for _, a := range m.catalog() {
		if a.Command == "vm creation cleanup" || a.Command == "vm save" {
			t.Fatal("specialist task in common menu")
		}
	}
	m.CatalogIndex = len(m.catalog())
	m, cmd := wk(m, "enter")
	if cmd != nil || !m.CatalogExpert {
		t.Fatal("Advanced entry did not open specialist menu")
	}
	count := 0
	for _, a := range ui.Actions {
		if a.Section == "VMs" {
			count++
		}
	}
	if len(m.catalog()) != count {
		t.Fatal("advanced menu lost actions")
	}
	m, _ = wk(m, "esc")
	if m.CatalogExpert || !m.Advanced {
		t.Fatal("back from Advanced must return to common")
	}
	m, _ = wk(m, "A")
	if !m.CatalogExpert {
		t.Fatal("documented advanced shortcut absent")
	}
}

func TestUnsupportedPickerPathReturnsEditableForm(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.ova ")
	if err := os.WriteFile(source, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	m := fixtureWorkspace()
	m.Section = 1
	m, _ = wk(m, "i")
	m, cmd := wk(m, "enter")
	n, _ := m.Update(cmd())
	m = n.(Workspace)
	m, cmd = wk(m, "enter")
	n, _ = m.Update(cmd())
	m = n.(Workspace)
	if m.Picker != nil || m.Import == nil || m.Import.Error == "" {
		t.Fatal("unsupported path trapped closed picker")
	}
	m, _ = wk(m, "esc")
	if m.Import != nil {
		t.Fatal("cannot cancel form after rejected picker result")
	}
}
