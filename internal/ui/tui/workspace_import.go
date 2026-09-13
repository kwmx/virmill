package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

type importExportReply struct {
	Token uint64
	Path  string
	Err   error
}

type importPulse struct{ Token uint64 }

func importPulseCommand(token uint64) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return importPulse{Token: token} })
}

func (m Workspace) importBusyView(width, height int) []string {
	seconds := int(m.ImportElapsed.Seconds())
	source := m.Import.Draft.SelectedSource
	if source == "" {
		source = m.Import.Draft.Source
	}
	return pageLines([]string{
		"Reading image settings", "", filepath.Base(source), "",
		"Detecting the source type and available settings.",
		"Disk checksums are verified during preparation.", "",
		fmt.Sprintf("Elapsed %d:%02d", seconds/60, seconds%60), "",
		"> [ Cancel inspection ]",
	}, width, height, 0)
}

func importError(err error) string {
	if e, ok := err.(*domain.Error); ok {
		switch e.Code {
		case "INSUFFICIENT_SPACE":
			return "INSUFFICIENT_SPACE: " + validation.SafeText(e.Message)
		case "WAIT_TIMEOUT":
			return "The appliance check took too long. Retry when the disk is less busy, or choose another file."
		case "BUSY", "RESOURCE_BUSY":
			return "Another image check is finishing. Wait a moment, then Continue."
		default:
			return validation.SafeText(e.Message)
		}
	}
	return validation.SafeText(err.Error())
}

func (m *Workspace) openImport(kind string) tea.Cmd {
	// Leaving an unused import returns to whichever view opened it.
	m.ImportFromCatalog = m.Advanced
	if m.offerSetup(kind) {
		return nil
	}
	m.draftSubmitted = false
	if m.SavedImport != nil && m.SavedImport.Draft.Kind == kind {
		m.Import = m.SavedImport
		m.SavedImport = nil
		m.ActionForm = nil
		m.Form = nil
		m.Notice = "Recovered import options. Check Jobs before reviewing another attempt."
		return nil
	}
	f := NewImportForm(kind)
	m.Import = &f
	m.ActionForm = nil
	m.Form = nil
	return m.browseImport("source", 0)
}
func (m *Workspace) browseImport(target string, index int) tea.Cmd {
	if m.Busy || (m.Import == nil && !((m.NetworkForm != nil || m.Creation != nil || m.BackupRecovery != nil) && target == "export-parent")) {
		return nil
	}
	kind, start := "file", ""
	d := ImportDraft{}
	if m.Import != nil {
		d = m.Import.Draft
	}
	switch target {
	case "source":
		start = d.SelectedSource
		if start == "" {
			start = d.Source
		}
		kind = "source"
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
	if target == "export-parent" && m.ExportForm != nil {
		if !guidedPath(path) {
			m.ExportForm.Error = "Choose a path without leading or trailing spaces."
			return
		}
		c := *m.ExportForm
		c.Fields = slices.Clone(c.Fields)
		c.Fields[0].Value = path
		c.Fields[0].Cursor = utf8.RuneCountInString(path)
		c.Error = ""
		m.ExportForm = &c
		return
	}
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
		if d.SelectedSource != path || d.Source != path {
			d.SelectedSource = path
			d.Source = path
			d.Description = nil
			d.Report = nil
			d.VMName, d.VCPUs, d.MemoryMiB = "", "", ""
			m.Import.VM = nil
			m.Import.VMBinding = ""
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
	if m.draftResume != nil && !m.Busy {
		if key.Type == tea.KeyEsc {
			m.Import = nil
			m.draftResume = nil
			m.draftModal = "auto"
			m.draftChoice = 0
		}
		return m, nil
	}
	if m.Busy {
		if key.Type == tea.KeyEsc || (key.Type == tea.KeyEnter && m.Pending["import-inspect"] != 0) {
			inspection := m.Pending["import-inspect"] != 0
			if m.ImportCancel != nil {
				m.ImportCancel()
				m.ImportCancel = nil
			}
			m.Pending = maps.Clone(m.Pending)
			delete(m.Pending, "import-inspect")
			delete(m.Pending, "plan")
			m.Busy = false
			m.Notice = "Inspection canceled."
			if !inspection {
				m.Notice = "Preview canceled."
			}
			m.Error = ""
			m.Import.Error = ""
			if m.draftResume != nil && inspection {
				m.Import = nil
				m.draftResume = nil
				m.draftModal = "auto"
				m.draftChoice = 0
				m.Notice = "Inspection canceled. Your saved choices were kept."
			}
		}
		return m, nil
	}
	f, intent := m.Import.Update(key)
	m.Import = &f
	switch intent.Kind {
	case "cancel":
		if m.ImportCancel != nil {
			m.ImportCancel()
			m.ImportCancel = nil
		}
		m.Import = nil
		m.Advanced = m.ImportFromCatalog
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "import-inspect")
		delete(m.Pending, "plan")
		m.Error = ""
		m.Notice = ""
	case "browse":
		return m, m.browseImport(intent.Target, intent.Index)
	case "inspect":
		return m, m.describeImport()
	case "hardware":
		return m, m.configureImportHardware()
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
		m.openSettingsExport("virmill-import-settings.json")
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
	r, err := m.settingsExportRequest()
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
	m.Notice = "Saving file..."
	return m, func() tea.Msg {
		return importExportReply{Token: token, Path: path, Err: exportImportSettings(path, r.Input)}
	}
}
func (m Workspace) importExportView(width, height int) []string {
	view := strings.Split(m.ExportForm.View(width, height), "\n")
	if len(view) > 0 {
		view[0] = "Export settings"
		if m.BackupRecovery != nil {
			view[0] = "Save recovery receipt"
		}
	}
	if len(view) > 1 {
		view[1] = "Save these options for reuse. Existing files are kept."
		if m.BackupRecovery != nil {
			view[1] = "Keep this with your backups. Store the password separately."
		}
	}
	if len(view) > 0 {
		view[len(view)-1] = "Ctrl+O Choose folder   Enter Export   Esc Back"
	}
	return view
}
func (m *Workspace) importInspection(data any) {
	m.Busy = false
	m.ImportCancel = nil
	m.Notice = ""
	if m.Import == nil {
		return
	}
	var report importer.SourceDescription
	b, err := json.Marshal(data)
	if err == nil {
		err = json.Unmarshal(b, &report)
	}
	if err == nil && m.draftResume != nil && m.draftResume.Import != nil {
		saved := m.draftResume
		if saved.SourceBinding != "" && saved.SourceBinding != setupBinding(&report) {
			m.Import.Error = "The source settings changed. Your saved choices were kept. Go back and Start new setup to use the changed source."
			return
		}
		fresh := NewImportForm(saved.Import.Kind)
		fresh.Draft.SelectedSource = m.Import.Draft.SelectedSource
		fresh.Draft.Source = m.Import.Draft.Source
		if err = fresh.Draft.ApplySourceDescription(report); err == nil {
			restored := saved.Import.form()
			if saved.SourceBinding == "" {
				// Inspection had not completed when the terminal closed. Use
				// detected defaults, retaining only explicit ordinary choices.
				restored = fresh
				restored.Draft.DestinationParent = saved.Import.DestinationParent
				restored.Draft.DestinationName = saved.Import.DestinationName
				if saved.Import.VMName != "" {
					restored.Draft.VMName = saved.Import.VMName
				}
				if saved.Import.VCPUs != "" {
					restored.Draft.VCPUs = saved.Import.VCPUs
				}
				if saved.Import.MemoryMiB != "" {
					restored.Draft.MemoryMiB = saved.Import.MemoryMiB
				}
			}
			restored.Draft.Description = fresh.Draft.Description
			restored.Draft.Report = fresh.Draft.Report
			restored.Draft.selectionSource = restored.Draft.Source
			restored.Draft.selectionSystem = restored.Draft.SystemID
			m.Import = &restored
			if saved.Creation != nil && saved.SourceBinding != "" {
				f := CreationForm{BeforePreparation: true, Source: importCreationSource(restored.Draft)}
				saved.Creation.restore(&f)
				m.Import.VM = &f
				m.Import.VMBinding = creationDraftBinding(m.Import.Draft)
			}
			m.draftResume = nil
			m.Notice = "Saved choices restored. Review available hardware before continuing."
		}
	} else if err == nil {
		err = m.Import.Draft.ApplySourceDescription(report)
	}
	if err != nil {
		m.Import.Error = validation.SafeText(fmt.Sprint(err))
		return
	}
	m.Import.Error = ""
	if m.Import.Draft.Kind != "ova" || m.Import.Draft.SystemID != "" {
		m.Import.Page = 3
		m.Import.Focus = 0
	} else {
		m.Notice = "Choose which appliance to import."
	}
}

func (m *Workspace) openSettingsExport(name string) {
	parent, _ := os.UserHomeDir()
	e := GuidedForm{Kind: "import-export", Fields: []GuidedField{{Name: "exportParent", Label: "Folder", Value: parent, Cursor: utf8.RuneCountInString(parent), Limit: 4096}, {Name: "exportName", Label: "File name", Value: name, Cursor: utf8.RuneCountInString(name), Limit: 255}}}
	m.ExportForm = &e
}
func (m Workspace) settingsExportRequest() (app.Request, error) {
	if m.NetworkForm != nil {
		r, err := m.NetworkForm.Request()
		if err != nil {
			return r, err
		}
		r.Input = r.Input["document"].(map[string]any)
		return r, nil
	}
	if m.BackupRecovery != nil {
		f := m.BackupRecovery
		if f.Selected < 0 || f.Selected >= len(f.Receipts) {
			return app.Request{}, fmt.Errorf("Choose a verified backup receipt first.")
		}
		b, err := json.Marshal(f.Receipts[f.Selected])
		if err != nil {
			return app.Request{}, err
		}
		if err = validation.Schema("backup-receipt", b); err != nil {
			return app.Request{}, err
		}
		var input map[string]any
		if err = json.Unmarshal(b, &input); err != nil {
			return app.Request{}, err
		}
		return app.Request{Input: input}, nil
	}
	if m.Creation != nil {
		return m.Creation.Request(m.Connection)
	}
	if m.Import == nil {
		return app.Request{}, fmt.Errorf("Open import or VM settings first.")
	}
	_, r, err := m.Import.Draft.Request(m.Connection)
	return r, err
}

func (m *Workspace) describeImport() tea.Cmd {
	if m.Import == nil {
		return nil
	}
	source := m.Import.Draft.SelectedSource
	if source == "" {
		source = m.Import.Draft.Source
	}
	if !guidedPath(source) {
		return nil
	}
	m.Busy = true
	m.Error, m.Import.Error, m.Notice = "", "", ""
	m.ImportStarted = time.Now()
	m.ImportElapsed = 0
	cmd := m.request("import-inspect", "import.source.describe", app.Request{Path: source})
	return tea.Batch(cmd, importPulseCommand(m.Pending["import-inspect"]))
}
