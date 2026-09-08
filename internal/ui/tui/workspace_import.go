package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/validation"
)

type importExportReply struct {
	Token uint64
	Path  string
	Err   error
}

func (m *Workspace) openImport(kind string) tea.Cmd {
	f := NewImportForm(kind)
	m.Import = &f
	m.ActionForm = nil
	m.Form = nil
	return m.browseImport("source", 0)
}
func (m *Workspace) browseImport(target string, index int) tea.Cmd {
	if m.Busy || m.Import == nil {
		return nil
	}
	kind, start := "file", ""
	d := m.Import.Draft
	switch target {
	case "source":
		start = d.Source
		if d.Kind == "disks" {
			kind = "directory"
		}
	case "destination":
		kind, start = "directory", d.DestinationParent
	case "disk":
		start = d.Source
		if index >= 0 && index < len(d.Disks) && d.Disks[index].Path != "" {
			start = filepath.Join(d.Source, d.Disks[index].Path)
		}
	case "backing":
		start = d.Source
	case "export-parent":
		kind = "directory"
		if m.ExportForm != nil {
			start = m.ExportForm.Fields[0].Value
		}
	default:
		return nil
	}
	p, cmd := NewFilePicker(start, kind)
	m.Picker = &p
	m.ImportPickerTarget = target
	m.PickerField = index
	return cmd
}
func (m *Workspace) importPicked(path string) {
	target, index := m.ImportPickerTarget, m.PickerField
	m.ImportPickerTarget = ""
	m.Picker = nil
	if m.Import == nil {
		return
	}
	copy := *m.Import
	copy.Draft.Disks = slices.Clone(copy.Draft.Disks)
	copy.Draft.Files = slices.Clone(copy.Draft.Files)
	m.Import = &copy
	d := &m.Import.Draft
	if !guidedPath(path) {
		m.Import.Error = "Choose a path without leading or trailing spaces."
		return
	}
	switch target {
	case "source":
		if d.Source != path {
			d.Source = path
			d.Report = nil
			d.SystemID = ""
			d.Offline = false
			d.SHA256 = ""
			if d.Kind != "iso" {
				d.Disks = nil
				d.Files = nil
			}
		}
	case "destination":
		d.DestinationParent = path
	case "export-parent":
		if m.ExportForm != nil {
			c := *m.ExportForm
			c.Fields = slices.Clone(c.Fields)
			c.Fields[0].Value = path
			c.Fields[0].Cursor = utf8.RuneCountInString(path)
			m.ExportForm = &c
		}
	case "disk", "backing":
		rel, err := filepath.Rel(d.Source, path)
		if err != nil || importer.SafePath(filepath.ToSlash(rel)) != nil {
			m.Import.Error = "Choose files inside the source folder."
			return
		}
		rel = filepath.ToSlash(rel)
		found := false
		for _, f := range d.Files {
			found = found || f.Path == rel
		}
		if !found {
			d.Files = append(d.Files, ImportFile{Path: rel})
		}
		if target == "disk" {
			if index < 0 || index >= len(d.Disks) {
				m.Import.Error = "Choose a disk row first."
				return
			}
			d.Disks[index].Path = rel
		}
	}
	m.Import.Error = ""
}
func (m Workspace) updateImport(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Busy {
		if key.Type == tea.KeyEsc {
			m.Pending = maps.Clone(m.Pending)
			delete(m.Pending, "import-inspect")
			delete(m.Pending, "plan")
			m.Busy = false
			m.Notice = ""
		}
		return m, nil
	}
	f, intent := m.Import.Update(key)
	m.Import = &f
	switch intent.Kind {
	case "cancel":
		m.Import = nil
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "import-inspect")
		delete(m.Pending, "plan")
		m.Error = ""
		m.Notice = ""
	case "browse":
		return m, m.browseImport(intent.Target, intent.Index)
	case "inspect":
		if !guidedPath(f.Draft.Source) {
			m.Import.Error = "Choose an OVA file first."
			return m, nil
		}
		m.Busy = true
		m.Error = ""
		m.Import.Error = ""
		m.Notice = "Inspecting appliance..."
		return m, m.request("import-inspect", "import.inspect", app.Request{Path: f.Draft.Source})
	case "preview", "export":
		method, r, err := f.Draft.Request(m.Connection)
		if err != nil {
			m.Import.Error = err.Error()
			return m, nil
		}
		m.Import.Error = ""
		m.Error = ""
		if intent.Kind == "preview" {
			m.Busy = true
			m.Notice = "Preparing reviewed import..."
			return m, m.request("plan", method, r)
		}
		parent, _ := os.UserHomeDir()
		e := GuidedForm{Kind: "import-export", Fields: []GuidedField{{Name: "exportParent", Label: "Folder", Value: parent, Cursor: utf8.RuneCountInString(parent), Limit: 4096}, {Name: "exportName", Label: "File name", Value: "virmill-import-settings.json", Cursor: len("virmill-import-settings.json"), Limit: 255}}}
		m.ExportForm = &e
	}
	return m, nil
}
func (m Workspace) updateImportExport(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Busy {
		return m, nil
	}
	if key.Type == tea.KeyEsc {
		m.ExportForm = nil
		return m, nil
	}
	if key.Type == tea.KeyCtrlO && m.ExportForm.Focus == 0 {
		return m, m.browseImport("export-parent", 0)
	}
	if key.Type != tea.KeyEnter {
		f, _, _ := m.ExportForm.Update(key)
		m.ExportForm = &f
		return m, nil
	}
	parent, name := m.ExportForm.Fields[0].Value, m.ExportForm.Fields[1].Value
	if !guidedPath(parent) || name == "" || !guidedPrintable(name) || strings.TrimSpace(name) != name || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		m.ExportForm.Error = "Choose a folder and a new file name."
		return m, nil
	}
	_, r, err := m.Import.Draft.Request(m.Connection)
	if err != nil {
		m.ExportForm.Error = err.Error()
		return m, nil
	}
	path := filepath.Join(parent, name)
	m.sequence++
	token := m.sequence
	m.Pending = maps.Clone(m.Pending)
	m.Pending["import-export"] = token
	m.Busy = true
	m.Notice = "Exporting settings..."
	return m, func() tea.Msg {
		return importExportReply{Token: token, Path: path, Err: exportImportSettings(path, r.Input)}
	}
}
func (m Workspace) importExportView(width, height int) []string {
	view := strings.Split(m.ExportForm.View(width, height), "\n")
	if len(view) > 0 {
		view[0] = "Export settings"
	}
	if len(view) > 1 {
		view[1] = "Save these options for reuse. Existing files are kept."
	}
	if len(view) > 0 {
		view[len(view)-1] = "Ctrl+O Choose folder   Enter Export   Esc Back"
	}
	return view
}
func (m *Workspace) importInspection(data any) {
	m.Busy = false
	m.Notice = ""
	if m.Import == nil {
		return
	}
	var report importer.Report
	b, err := json.Marshal(data)
	if err == nil {
		err = json.Unmarshal(b, &report)
	}
	if err == nil {
		err = m.Import.Draft.ApplyInspection(report)
	}
	if err != nil {
		m.Import.Error = validation.SafeText(fmt.Sprint(err))
		return
	}
	m.Import.Error = ""
	m.Notice = "Appliance inspected. Choose its system and disk options."
}
