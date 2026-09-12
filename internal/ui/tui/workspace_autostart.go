package tui

import (
	"encoding/json"
	"maps"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// Autostart settings are read afresh; cached list values are never editable defaults.
func (m *Workspace) openAutostart() tea.Cmd {
	vm := m.selectedVM()
	if vm.Key.UUID == "" {
		m.Error = "Choose a VM before changing automatic startup."
		return nil
	}
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	m.AutostartTarget = &vm
	m.Form, m.ActionForm = nil, nil
	m.Advanced = false
	m.Busy = true
	m.Error, m.Notice = "", "Reading current automatic startup settings..."
	return m.request("autostart-load", "inventory.get", app.Request{ID: vm.Key.UUID})
}
func (m *Workspace) resetAutostartLoad() {
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "autostart-load")
	m.AutostartTarget = nil
}
func (m *Workspace) receiveAutostart(data any) {
	expected := m.AutostartTarget
	m.resetAutostartLoad()
	m.Busy, m.Notice = false, ""
	if expected == nil {
		return
	}
	var vm domain.VM
	raw, err := json.Marshal(data)
	if err != nil || json.Unmarshal(raw, &vm) != nil || vm.Key != expected.Key || vm.Key.ConnectionID != m.Connection || vm.Fingerprint == "" {
		m.Error = "Could not match the startup settings to this VM. Refresh and try again."
		return
	}
	f, err := NewGuidedForm("autostart", vm)
	if err != nil {
		m.Error = err.Error()
		return
	}
	m.Form = &f
	m.Error, m.Offset = "", 0
}
