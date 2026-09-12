package tui

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

func (m *Workspace) resetBackupRecovery() {
	if m.PickerBackupRecovery || m.PickerBackupReceipt {
		m.Picker = nil
	}
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "backup-receipts")
	delete(m.Pending, "backup-receipt-read")
	m.BackupRecovery = nil
	m.BackupRecoveryLoading = false
	m.PickerBackupRecovery = false
	m.PickerBackupReceipt = false
}

func (m *Workspace) openBackupRecovery() tea.Cmd {
	m.resetBackupRecovery()
	f := NewBackupRecoveryForm(nil)
	m.BackupRecovery = &f
	m.Protection = nil
	m.Form = nil
	m.ActionForm = nil
	m.Advanced = false
	m.Busy = true
	m.BackupRecoveryLoading = true
	m.Error = ""
	m.Notice = "Reading recent verified backups..."
	return m.request("backup-receipts", "backup.receipts.list", app.Request{})
}

func (m *Workspace) receiveBackupReceipts(data any) {
	m.Busy = false
	m.BackupRecoveryLoading = false
	m.Notice = ""
	if m.BackupRecovery == nil {
		return
	}
	raw, err := json.Marshal(data)
	var receipts []domain.BackupReceipt
	if err != nil || wire.Decode(raw, &receipts) != nil || len(receipts) > 1000 {
		m.BackupRecovery.Error = "Could not read recent backup receipts. Go Back and retry."
		return
	}
	for _, receipt := range receipts {
		if !validRecoveryReceipt(receipt) || receipt.Connection != m.Connection {
			m.BackupRecovery.Error = "A recent backup receipt is incomplete or belongs to another connection. Import a saved receipt instead."
			return
		}
	}
	f := NewBackupRecoveryForm(receipts)
	m.BackupRecovery = &f
}

func (m *Workspace) receiveBackupReceipt(data any) {
	m.Busy = false
	m.Notice = ""
	if m.BackupRecovery == nil {
		return
	}
	raw, err := json.Marshal(data)
	var receipt domain.BackupReceipt
	if err != nil || wire.Decode(raw, &receipt) != nil || !validRecoveryReceipt(receipt) {
		m.BackupRecovery.Error = "Choose a valid saved BackupReceipt file."
		return
	}
	receipts := slices.Clone(m.BackupRecovery.Receipts)
	selected := slices.IndexFunc(receipts, func(r domain.BackupReceipt) bool { return r == receipt })
	if selected < 0 {
		receipts = append(receipts, receipt)
		selected = len(receipts) - 1
	}
	f := NewBackupRecoveryForm(receipts)
	f.Selected = selected
	f.Fields[1].Value = receipt.Repository
	f.Fields[1].Cursor = len([]rune(receipt.Repository))
	f.Focus = 1
	m.BackupRecovery = &f
	m.Notice = "Receipt loaded. Choose the backup folder and its private password file."
}

func (m *Workspace) browseBackupReceipt() tea.Cmd {
	if m.BackupRecovery == nil || m.Busy || m.Pending["apply"] != 0 {
		return nil
	}
	picker, cmd := NewFilePicker("", "file")
	m.Picker = &picker
	m.PickerBackupReceipt = true
	m.PickerBackupRecovery = false
	m.PickerProtection = false
	m.PickerAction = false
	m.ImportPickerTarget = ""
	m.Error = ""
	return cmd
}

func (m Workspace) updateBackupRecovery(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyEsc {
		m.resetBackupRecovery()
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "plan")
		m.Busy = m.Pending["apply"] != 0
		m.Error = ""
		m.Notice = ""
		return m, nil
	}
	if m.Busy {
		return m, nil
	}
	if key.Type == tea.KeyCtrlO {
		if m.BackupRecovery.Focus == 0 || len(m.BackupRecovery.Receipts) == 0 {
			return m, m.browseBackupReceipt()
		}
		return m, m.browseField()
	}
	if len(m.BackupRecovery.Receipts) == 0 && key.Type == tea.KeyEnter {
		return m, m.browseBackupReceipt()
	}
	if m.BackupRecovery.Focus == 4 && (key.Type == tea.KeyEnter || key.Type == tea.KeySpace) && len(m.BackupRecovery.Receipts) > 0 {
		m.openSettingsExport("virmill-backup-receipt.json")
		return m, nil
	}
	f, preview, _ := m.BackupRecovery.Update(key)
	m.BackupRecovery = &f
	if !preview {
		return m, nil
	}
	method, request, err := f.Request(m.Connection)
	if err != nil {
		m.BackupRecovery.Error = validation.SafeText(err.Error())
		return m, nil
	}
	m.Busy = true
	m.Error = ""
	m.Notice = "Checking the repository and recovered-file destination..."
	return m, m.request("plan", method, request)
}

func (m Workspace) backupRecoveryView(width, height int) []string {
	if m.BackupRecoveryLoading {
		return []string{"Recover backup files", "Reading recent verified backup receipts...", "Esc Back"}
	}
	if m.BackupRecovery == nil {
		return nil
	}
	lines := strings.Split(m.BackupRecovery.View(width, height), "\n")
	if len(lines) > 0 {
		if len(m.BackupRecovery.Receipts) == 0 {
			lines[len(lines)-1] = "Enter Choose saved receipt   Ctrl+O Browse   Esc Back"
		} else if m.BackupRecovery.Focus == 0 {
			lines[len(lines)-1] = "Left/Right Choose   Ctrl+O Saved receipt file   Enter Next   Esc Back"
		}
	}
	return lines
}
