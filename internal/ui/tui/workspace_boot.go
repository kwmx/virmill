package tui

import (
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"maps"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/validation"
)

func (m *Workspace) openBootForm() tea.Cmd {
	vm := m.selectedVM()
	if vm.Key.UUID == "" {
		m.Error = "Select a VM before editing its boot devices."
		return nil
	}
	m.BootVM = vm
	m.Boot = nil
	m.BootLoading = true
	m.Busy = true
	m.Advanced = false
	return m.request("boot-load", "vm.boot.get", app.Request{ID: vm.Key.UUID})
}
func (m *Workspace) receiveBoot(data any) {
	m.Busy = false
	m.BootLoading = false
	var r BootReport
	b, e := json.Marshal(data)
	if e != nil || json.Unmarshal(b, &r) != nil {
		m.Error = "Could not read boot devices. Refresh the VM and try again."
		return
	}
	f, e := NewBootForm(m.BootVM, r)
	if e != nil {
		m.Error = validation.SafeText(e.Error())
		return
	}
	m.Boot = &f
	m.Error = ""
}
func (m Workspace) updateBoot(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Busy {
		if key.Type == tea.KeyEsc {
			m.Pending = maps.Clone(m.Pending)
			delete(m.Pending, "boot-load")
			delete(m.Pending, "plan")
			m.Busy = false
			m.BootLoading = false
			m.Notice = ""
		}
		return m, nil
	}
	if m.Boot == nil {
		return m, nil
	}
	f, submit, cancel := m.Boot.Update(key)
	m.Boot = &f
	if cancel {
		m.Boot = nil
		m.Error = ""
		return m, nil
	}
	if submit {
		r, e := f.Request(m.Connection)
		if e != nil {
			m.Boot.Error = validation.SafeText(e.Error())
			return m, nil
		}
		m.Busy = true
		m.Notice = "Preparing reviewed boot changes..."
		m.Error = ""
		return m, m.request("plan", "vm.plan", r)
	}
	return m, nil
}
