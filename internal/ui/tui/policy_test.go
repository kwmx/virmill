package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"path/filepath"
	"testing"
)

func TestBackupPolicyFormsUseSharedReadOnlyMethods(t *testing.T) {
	for _, action := range []string{"validate", "preview"} {
		r := &recorder{}
		m := New(r, "qemu:///session")
		for i, name := range sections {
			if name == "Protection" {
				m.Section = i
			}
		}
		found := false
		for i, a := range m.actions() {
			if a.Command == "backup policy "+action {
				m.Selected = i
				found = true
			}
		}
		if !found {
			t.Fatal("policy action missing")
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if cmd != nil || !m.Editing {
			t.Fatal("policy form missing")
		}
		m.Input = "policy.yaml"
		if action == "preview" {
			m.Input = `{"path":"policy.yaml","input":{"after":"2026-09-08T00:00:00Z","count":2}}`
		}
		_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("no policy dispatch")
		}
		cmd()
		if r.method != "backup.policy."+action || !filepath.IsAbs(r.request.Path) || r.request.Apply != nil || r.request.Action != "" {
			t.Fatal("policy bypassed shared service", r)
		}
		if action == "preview" && r.request.Input["count"] != float64(2) {
			t.Fatal("preview options lost", r)
		}
	}
}
