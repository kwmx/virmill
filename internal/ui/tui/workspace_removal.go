package tui

import (
	"encoding/json"
	"maps"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// Removal always reads the current VM before presenting its exact-name confirmation.
func (m *Workspace) openRemoval() tea.Cmd {
	vm := m.selectedVM()
	if vm.Key.UUID == "" {
		m.Error = "Choose the VM you want to remove."
		return nil
	}
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	m.RemovalTarget = &vm
	m.Form, m.ActionForm = nil, nil
	m.Advanced = false
	m.Busy = true
	m.Error, m.Notice = "", "Reading the selected VM..."
	return m.request("removal-load", "inventory.get", app.Request{ID: vm.Key.UUID})
}
func (m *Workspace) resetRemovalLoad() {
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "removal-load")
	m.RemovalTarget = nil
}
func (m *Workspace) receiveRemoval(data any) {
	expected := m.RemovalTarget
	m.resetRemovalLoad()
	m.Busy, m.Notice = false, ""
	if expected == nil {
		return
	}
	var vm domain.VM
	raw, err := json.Marshal(data)
	if err != nil || json.Unmarshal(raw, &vm) != nil || vm.Key != expected.Key || vm.Key.ConnectionID != m.Connection || vm.Fingerprint == "" {
		m.Error = "Could not match the removal details to this VM. Refresh and try again."
		return
	}
	f, err := NewGuidedForm("remove-definition", vm)
	if err != nil {
		m.Error = err.Error()
		return
	}
	m.Form = &f
	m.Error, m.Offset = "", 0
}
