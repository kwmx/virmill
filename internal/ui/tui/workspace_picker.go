package tui

import (
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

func actionBrowseKind(f ActionForm, name string) string {
	if name != "path" {
		return guidedBrowseKind(name)
	}
	switch f.Action.Method {
	case "import.prepare-disks", "plugin.validate", "plugin.test":
		return "directory"
	case "plugin.develop":
		return "directory"
	default:
		return "file"
	}
}
func (m *Workspace) browseField() tea.Cmd {
	if m.Busy || m.Pending["apply"] != 0 {
		return nil
	}
	var fields []GuidedField
	focus := 0
	kind := ""
	if m.ActionForm != nil {
		fields = m.ActionForm.Form.Fields
		focus = m.ActionForm.Form.Focus
		m.PickerAction = true
		if focus >= 0 && focus < len(fields) {
			kind = actionBrowseKind(*m.ActionForm, fields[focus].Name)
		}
	} else if m.Form != nil {
		fields = m.Form.Fields
		focus = m.Form.Focus
		m.PickerAction = false
		if focus >= 0 && focus < len(fields) {
			kind = guidedBrowseKind(fields[focus].Name)
		}
	}
	if focus < 0 || focus >= len(fields) || kind == "" {
		return nil
	}
	picker, cmd := NewFilePicker(fields[focus].Value, kind)
	m.Picker = &picker
	m.PickerField = focus
	m.Error = ""
	return cmd
}
func (m Workspace) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.Type == tea.KeyCtrlC {
			m.Quit = true
			return m, tea.Quit
		}
		if m.Width < 60 || m.Height < 18 {
			if key.Type == tea.KeyEsc {
				m.Picker = nil
			}
			return m, nil
		}
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width, m.Height = size.Width, size.Height
	}
	p, cmd, result := m.Picker.Update(msg)
	m.Picker = &p
	if result.Cancel {
		m.Picker = nil
		return m, cmd
	}
	if result.Path == "" {
		return m, cmd
	}
	var f *GuidedForm
	if m.PickerAction && m.ActionForm != nil {
		copy := *m.ActionForm
		m.ActionForm = &copy
		f = &m.ActionForm.Form
	} else if !m.PickerAction && m.Form != nil {
		copy := *m.Form
		m.Form = &copy
		f = m.Form
	}
	if f == nil || m.PickerField < 0 || m.PickerField >= len(f.Fields) {
		m.Picker = nil
		return m, nil
	}
	f.Fields = slices.Clone(f.Fields)
	field := &f.Fields[m.PickerField]
	if !guidedPath(result.Path) || len(result.Path) > field.Limit {
		m.Picker = nil
		f.Error = "Choose a path without leading or trailing spaces."
		return m, nil
	}
	field.Value = result.Path
	field.Cursor = utf8.RuneCountInString(result.Path)
	f.Error = ""
	m.Picker = nil
	// Choosing a file edits exactly one field. Planning still needs Enter on the
	// form, and the service rechecks file identity and permissions independently.
	return m, nil
}

func (m Workspace) formHints() string {
	var f *GuidedForm
	if m.ActionForm != nil {
		f = &m.ActionForm.Form
	} else {
		f = m.Form
	}
	browse := ""
	if f != nil && f.Focus >= 0 && f.Focus < len(f.Fields) && guidedBrowseKind(f.Fields[f.Focus].Name) != "" {
		browse = "Ctrl+O Browse   "
	}
	next := "Preview"
	if m.ActionForm != nil && m.ActionForm.Action.Mutation == "" {
		next = "Continue"
	}
	return strings.TrimSpace(browse + "Tab Next   Enter " + next + "   Esc Back")
}
