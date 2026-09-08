package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestReadOnlyHostInventoryNavigation(t *testing.T) {
	for _, tc := range []struct {
		command, section, method string
		form                     bool
	}{
		{"host pci list", "Devices", "host.pci.list", false},
		{"device usb list", "Devices", "device.usb.list", false},
		{"network cidr check", "Networks", "network.cidr.check", true},
	} {
		r := &recorder{}
		m := New(r, "qemu:///session")
		for i, s := range sections {
			if s == tc.section {
				m.Section = i
			}
		}
		found := false
		for i, a := range m.actions() {
			if a.Command == tc.command {
				m.Selected = i
				found = true
			}
		}
		if !found {
			t.Fatal("inventory command inaccessible", tc.command)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if tc.form {
			if !m.Editing || cmd != nil {
				t.Fatal("CIDR form did not open")
			}
			m.Input = `{"input":{"candidates":["10.77.0.0/24"],"planned":[]}}`
			_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		if cmd == nil {
			t.Fatal("inventory did not dispatch")
		}
		cmd()
		if r.method != tc.method || r.request.ID != "" || r.request.Path != "" || r.request.Apply != nil || r.request.Action != "" || r.request.Connection != "qemu:///session" {
			t.Fatal("TUI inventory differs from CLI", r)
		}
		if tc.form && len(r.request.Input) != 2 {
			t.Fatal("CIDR form parameters lost")
		}
	}
}
