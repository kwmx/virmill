package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"virmill.local/core/internal/ui"
)

func TestTaskMenuGroupsAndSearchByPlainLanguage(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	m, _ = wk(m, "a")
	view := m.View()
	for _, text := range []string{"VMs / More tasks", "Selected VM: Build guest", "Power", "Start VM", "Advanced tools..."} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing %s:\n%s", text, view)
		}
	}
	m, _ = wk(m, "/")
	m, _ = wk(m, "CPU")
	if len(m.catalog()) != 2 || m.catalog()[0].Command != "vm set" || m.catalog()[1].Command != "vm creation options" {
		t.Fatal("ordinary CPU search did not find editor", m.catalog())
	}
	m, _ = wk(m, "esc")
	if !m.Advanced || m.CatalogSearch != "" {
		t.Fatal("search clear lost menu")
	}
	m, _ = wk(m, "esc")
	if m.Advanced || m.Section != 1 {
		t.Fatal("back lost resource workspace")
	}
}
func TestPowerTaskUsesSelectedVMWithoutAnotherIDForm(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	m.Selected = 1
	m, _ = wk(m, "a")
	if m.catalog()[0].Command != "vm start" {
		t.Fatal(m.catalog()[0])
	}
	m, cmd := wk(m, "enter")
	if cmd == nil || m.ActionForm != nil || m.Advanced || !m.Busy {
		t.Fatal("power task must go straight to preview")
	}
	cmd()
	c := m.Client.(*workspaceClient)
	if len(c.calls) != 1 || c.calls[0] != "vm.plan" || c.requests[0].ID != workspaceVMID || c.requests[0].Action != "start" {
		t.Fatal(c.calls, c.requests)
	}
}
func TestTaskMenuEveryRowAndFocusedButtonVisibleWhenNarrow(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		m := fixtureWorkspace()
		m.Width, m.Height = size[0], size[1]
		m.Section = 1
		m, _ = wk(m, "a")
		for i, a := range m.catalog() {
			m.CatalogIndex = i
			view := m.View()
			if !strings.Contains(view, ">  "+actionLabel(a)) {
				t.Fatalf("selected task hidden %v %s:\n%s", size, a.Command, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > m.Width {
					t.Fatal("overflow", line)
				}
			}
		}
		m.Advanced = false
		m.ButtonFocus = true
		for i, b := range m.buttons() {
			m.ButtonIndex = i
			if !strings.Contains(m.View(), b.label) || !strings.Contains(m.View(), ">[") {
				t.Fatal("focused button hidden", size, b, m.View())
			}
		}
	}
}
func TestAllToolsCategoriesAndImportSources(t *testing.T) {
	m := fixtureWorkspace()
	m, _ = wk(m, ":")
	if len(m.catalog()) != len(ui.Actions) {
		t.Fatal("lost registry coverage")
	}
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = n.(Workspace)
	if m.CatalogSection != 0 || m.CatalogMode != "all" {
		t.Fatal("category navigation")
	}
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = n.(Workspace)
	if m.CatalogSection != -1 {
		t.Fatal("category did not return to all")
	}
	m, _ = wk(m, "esc")
	m, _ = wk(m, "i")
	if !strings.Contains(m.View(), "Choose a source") || len(m.catalog()) != 3 {
		t.Fatal("import choices missing", m.View())
	}
	for _, a := range m.catalog() {
		if !strings.HasPrefix(a.Command, "import ") {
			t.Fatal("unrelated import task", a)
		}
	}
}

func TestCancelReadOnlyTaskIgnoresLateDetail(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 8
	var a ui.Action
	for _, v := range ui.Actions {
		if v.Command == "operation watch" {
			a = v
		}
	}
	f, err := NewActionForm(a, "33345678-1234-4234-8234-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	m.ActionForm = &f
	m, cmd := wk(m, "enter")
	if cmd == nil {
		t.Fatal("expected read request")
	}
	token := m.Pending["detail"]
	m, _ = wk(m, "esc")
	n, _ := m.Update(workspaceReply{Kind: "detail", Token: token})
	m = n.(Workspace)
	if m.Detail != nil || m.ActionForm != nil || m.Busy || m.Pending["detail"] != 0 {
		t.Fatal("late canceled read changed page")
	}
}
