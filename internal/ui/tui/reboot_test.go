package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestRebootFormUsesSharedPreview(t *testing.T) {
	r := &recorder{}
	m := New(r, "qemu:///session")
	m.Section = 1
	found := false
	for i, a := range m.actions() {
		if a.Command == "vm reboot" {
			m.Selected = i
			found = true
		}
	}
	if !found {
		t.Fatal("reboot action missing")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || !m.Editing {
		t.Fatal("reboot must ask for stable UUID")
	}
	m.Input = "selected-uuid"
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no preview")
	}
	cmd()
	if r.method != "vm.plan" || r.request.Action != "reboot" || r.request.ID != "selected-uuid" || r.request.Connection != "qemu:///session" || r.request.Apply != nil {
		t.Fatal("reboot bypassed preview", r)
	}
}
