package tui

import (
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"maps"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

func (m *Workspace) openProtection(command string) tea.Cmd {
	if m.Section != 6 {
		m.Error = "Open Protection and select a recovery point first."
		return nil
	}
	row := m.Detail
	if row == nil {
		rows := m.rows()
		if m.Selected >= 0 && m.Selected < len(rows) {
			row = rows[m.Selected]
		}
	}
	kind := "backup-create"
	if command == "snapshot restore" {
		kind = "snapshot-restore"
	}
	f, err := NewProtectionForm(kind, resourceID(row))
	if err != nil {
		m.Error = validation.SafeText(err.Error())
		return nil
	}
	m.Protection, m.Advanced, m.Error = &f, false, ""
	if kind == "snapshot-restore" {
		m.Busy = true
		return m.request("protection-pools", "storage.pool.list", app.Request{})
	}
	return m.browseField()
}
func (m *Workspace) receiveProtectionPools(data any) {
	m.Busy = false
	if m.Protection == nil {
		return
	}
	var pools []domain.StoragePool
	b, err := json.Marshal(data)
	if err != nil || json.Unmarshal(b, &pools) != nil {
		m.Protection.Error = "Could not read storage pools. Go Back and retry."
		return
	}
	m.Protection.SetPools(pools)
	if len(m.Protection.Pools) == 0 {
		m.Protection.Error = "No active directory pool is available. Check Storage, then retry."
	}
}
func (m Workspace) updateProtection(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyEsc {
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "plan")
		delete(m.Pending, "protection-pools")
		m.Protection, m.Busy, m.Error, m.Notice = nil, false, "", ""
		return m, nil
	}
	if m.Busy {
		return m, nil
	}
	if key.Type == tea.KeyCtrlO {
		return m, m.browseField()
	}
	f, preview, _ := m.Protection.Update(key)
	m.Protection = &f
	if !preview {
		return m, nil
	}
	method, request, err := f.Request(m.Connection)
	if err != nil {
		m.Protection.Error = validation.SafeText(err.Error())
		return m, nil
	}
	m.Busy, m.Error, m.Notice = true, "", "Checking the recovery point and destination..."
	return m, m.request("plan", method, request)
}
