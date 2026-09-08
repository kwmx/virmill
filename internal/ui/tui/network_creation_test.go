package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestNetworkCreationAndRecoveryUseSharedPreview(t *testing.T) {
	for _, tt := range []struct{ command, method, action, value string }{{"network create", "network.create", "create", "network.yaml"}, {"network creation resume", "network.creation.resume", "resume", "job-id"}, {"network creation result", "network.creation.result", "", "job-id"}} {
		r := &recorder{}
		m := New(r, "qemu:///system")
		found := false
		for section := range sections {
			m.Section = section
			for i, a := range m.actions() {
				if a.Command == tt.command {
					m.Selected = i
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatal("missing", tt.command)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if cmd != nil || !m.Editing {
			t.Fatal("expected input")
		}
		m.Input = tt.value
		_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("no request")
		}
		cmd()
		if r.method != tt.method || r.request.Action != tt.action || r.request.Apply != nil {
			t.Fatal(r)
		}
	}
}
