package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"maps"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
)

func (m *Workspace) openNetworkForm() {
	m.resetJobOutcome()
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	if m.NetworkForm == nil {
		f := NewNetworkForm()
		m.NetworkForm = &f
	}
	m.Advanced = false
	m.ActionForm = nil
	m.Form = nil
	m.Error, m.Notice = "", ""
	m.Offset = 0
}
func (m *Workspace) resetNetworkForm() {
	if m.NetworkForm == nil {
		return
	}
	m.NetworkForm = nil
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "plan")
	if m.Pending["apply"] == 0 {
		m.Busy = false
	}
}
func (m Workspace) updateNetworkForm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Pending["apply"] != 0 {
		m.Busy = true
		m.Notice = "Submission is pending. Wait for its result; check Jobs before another attempt."
		return m, nil
	}

	if key.Type == tea.KeyEsc && m.Busy {
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "plan")
		m.Busy = false
		m.Notice = "Preview canceled. Network options were kept."
		return m, nil
	}
	if m.Busy {
		return m, nil
	}
	m.NetworkForm.SetViewport(m.Width, m.Height-7)
	f, action := m.NetworkForm.Update(key)
	m.NetworkForm = &f
	switch action {
	case "back":
		m.resetNetworkForm()
		m.Error, m.Notice = "", ""
	case "preview":
		r, err := f.Request()
		if err != nil {
			f.Error = err.Error()
			return m, nil
		}
		if m.Connection != "qemu:///system" {
			f.Error = "Managed network creation uses the local system connection. Switch to qemu:///system first."
			return m, nil
		}
		f.Error = ""
		m.Busy = true
		m.Notice = "Checking subnet and preparing the network review..."
		return m, m.request("plan", "network.create", r)
	case "export":
		if _, err := f.Request(); err != nil {
			f.Error = err.Error()
			return m, nil
		}
		m.openSettingsExport("virmill-network.json")
	case "advanced-file":
		for _, a := range ui.Actions {
			if a.Command == "network create" {
				form, err := NewActionForm(a, "")
				if err != nil {
					f.Error = err.Error()
					return m, nil
				}
				m.ActionForm = &form
				m.ActionTitle = "Create network from a declaration"
				return m, m.browseField()
			}
		}
		f.Error = domain.Fail("UNSUPPORTED_CAPABILITY", "Network declaration action is unavailable").Message
	}
	return m, nil
}
