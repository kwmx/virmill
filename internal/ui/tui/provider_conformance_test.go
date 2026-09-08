package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestProviderConformanceUsesSharedPluginTest(t *testing.T) {
	r := &recorder{}
	m := New(r, "qemu:///system")
	m.Section = 9
	found := false
	for i, action := range m.actions() {
		if action.Command == "plugin test" {
			m.Selected, found = i, true
		}
	}
	if !found {
		t.Fatal("provider conformance is absent from Plugins")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	m.Input = "/fixture/provider-conformance"
	_, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("provider workspace form did not submit")
	}
	command()
	if r.method != "plugin.test" || r.request.Path != "/fixture/provider-conformance" || r.request.Apply != nil {
		t.Fatal("provider conformance differs from CLI", r)
	}
}
