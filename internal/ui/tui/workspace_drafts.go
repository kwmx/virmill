package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// The writer serializes load/save/exit flush. Generation CAS prevents another
// terminal from replacing these choices; sequence checks discard delayed ticks.
type setupWriter struct {
	mu         sync.Mutex
	store      *DraftStore
	generation string
	sequence   uint64
	failed     bool
}

func (w *setupWriter) load(connection string) (*SavedSetupDocument, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var d SavedSetupDocument
	gen, err := w.store.Load("import", &d)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		w.failed = true
		return nil, err
	}
	raw, err := json.Marshal(d)
	if err == nil {
		d, err = decodeSetup(raw, connection)
	}
	if err != nil {
		w.failed = true
		return nil, fmt.Errorf("Saved setup was kept unchanged: %w", err)
	}
	w.generation = gen
	return &d, nil
}
func (w *setupWriter) save(seq uint64, d SavedSetupDocument) error { return w.write(seq, d, false) }
func (w *setupWriter) write(seq uint64, d SavedSetupDocument, barrier bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed {
		return fmt.Errorf("Draft saving paused after a storage conflict or error; existing saved choices are unchanged")
	}
	if seq <= w.sequence {
		if barrier {
			return fmt.Errorf("Setup submission was superseded; no operation was sent")
		}
		return nil
	}
	gen, err := w.store.Save("import", w.generation, d)
	if gen != "" {
		w.generation = gen
		w.sequence = seq
	}
	if err != nil {
		w.failed = true
	}
	return err
}

type setupLoaded struct {
	Document *SavedSetupDocument
	Err      error
}
type setupSaveTick struct{ Sequence uint64 }
type setupSaved struct{ Err error }

// AttachDraftStore is explicit: constructing a Workspace never touches home.
func (m *Workspace) AttachDraftStore(store *DraftStore) {
	m.draftWriter = &setupWriter{store: store}
	m.draftLoading = true
}
func (m Workspace) loadSetup() tea.Cmd {
	if m.draftWriter == nil {
		return nil
	}
	return func() tea.Msg { d, e := m.draftWriter.load(m.Connection); return setupLoaded{d, e} }
}
func (m *Workspace) offerSetup(kind string) bool {
	if m.draftWriter == nil || m.draftBypass {
		m.draftBypass = false
		return false
	}
	if m.draftLoading {
		m.Error = "Saved setup is still loading. Try again in a moment."
		return true
	}
	if m.draftSaved == nil {
		return false
	}
	m.draftModal = kind
	m.draftChoice = 0
	return true
}
func (m Workspace) setupView(width, height int) []string {
	lines := []string{"Continue your saved setup?", "", "Your image and hardware choices were saved on this computer.", "Sources and available hardware are checked again before review.", ""}
	labels := []string{"Resume saved setup", "Start new setup", "Back"}
	if m.draftSaved != nil && m.draftSaved.State != "editing" {
		lines = []string{"A setup was submitted", "", "Check Jobs before starting another import or VM creation.", "Saved choices were kept. No operation will be repeated.", ""}
		if m.draftSaved.State == "submitting" {
			lines = []string{"Check the previous submission", "", "The operation's outcome was not confirmed.", "Review Jobs before starting another import or VM creation.", "Saved choices were kept. No operation will be repeated.", ""}
		}
		labels[0] = "Open Jobs"
		if m.draftSaved.State == "submitted" && m.draftSaved.Import != nil && m.draftSaved.OperationID != "" {
			labels[0] = "Continue prepared setup"
		}
	}
	for i, label := range labels {
		mark := "  "
		if i == m.draftChoice {
			mark = "> "
		}
		lines = append(lines, mark+"[ "+label+" ]")
	}
	if m.draftError != "" {
		lines = append(lines, "", m.draftError)
	}
	return pageLines(lines, width, height, 0)
}
func (m Workspace) updateSetup(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		m.draftModal = ""
	case tea.KeyUp, tea.KeyShiftTab:
		m.draftChoice = max(0, m.draftChoice-1)
	case tea.KeyDown, tea.KeyTab:
		m.draftChoice = min(2, m.draftChoice+1)
	case tea.KeyEnter:
		kind := m.draftModal
		if m.draftChoice == 2 {
			m.draftModal = ""
			return m, nil
		}
		if m.draftChoice == 1 {
			m.draftModal = ""
			m.draftResume = nil
			m.draftSaved = nil
			m.draftSubmitted = false
			m.Import = nil
			m.Creation = nil
			m.SavedImport = nil
			m.SavedCreation = nil
			m.draftBypass = true
			m.draftError = ""
			if kind == "creation" {
				return m, m.openCreationSources()
			}
			return m, m.openImport(kind)
		}
		d := m.draftSaved
		if d == nil {
			m.draftModal = ""
			return m, nil
		}
		if d.State == "submitted" && d.Import != nil && d.OperationID != "" {
			m.draftModal = ""
			m.draftResume = d
			m.Busy = true
			return m, m.request("draft-preparation-job", "operation.get", app.Request{ID: d.OperationID})
		}
		if d.State != "editing" {
			m.draftModal = ""
			m.Section = 8
			m.NavIndex = 8
			m.Notice = "Review the previous submission in Jobs. No operation was repeated."
			if d.OperationID != "" {
				return m, m.request("detail", "operation.get", app.Request{ID: d.OperationID})
			}
			return m, m.refresh()
		}
		m.draftModal = ""
		m.draftResume = d
		if d.Import != nil {
			f := d.Import.form()
			m.Import = &f
			if f.Draft.SelectedSource == "" && f.Draft.Source == "" {
				m.draftResume = nil
				return m, m.browseImport("source", 0)
			}
			return m, m.describeImport()
		}
		if d.Creation != nil {
			return m, m.loadCreation(d.Creation.OperationID, d.Creation.Spec.Machine)
		}
	}
	return m, nil
}

// Wrapper observes typed choice changes, including picker replies and nested
// hardware pages. The previous document survives leaving a page.
func (m Workspace) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case setupLoaded:
		m.draftLoading = false
		m.draftSaved = v.Document
		if v.Err != nil {
			m.draftError = "Could not load saved setup: " + validation.SafeText(v.Err.Error())
			m.Error = m.draftError
		}
		return m, nil
	case setupSaveTick:
		if v.Sequence != m.draftSequence || m.draftSaved == nil {
			return m, nil
		}
		d := *m.draftSaved
		writer := m.draftWriter
		return m, func() tea.Msg { return setupSaved{writer.save(v.Sequence, d)} }
	case setupSaved:
		if v.Err != nil {
			m.draftError = "Setup was not saved: " + validation.SafeText(v.Err.Error())
			m.Error = m.draftError
		}
		return m, nil
	case tea.KeyMsg:
		if m.draftModal != "" && v.Type != tea.KeyCtrlC {
			return m.updateSetup(v)
		}
	}
	next, cmd := m.updateWorkspace(msg)
	w, ok := next.(Workspace)
	if !ok || w.draftWriter == nil || w.draftLoading || w.draftResume != nil || w.draftModal != "" {
		return next, cmd
	}
	d := w.setupDocument()
	if d == nil || w.draftSubmitted {
		return w, cmd
	}
	if setupBinding(d) == setupBinding(w.draftSaved) {
		return w, cmd
	}
	w.draftSaved = d
	w.draftSequence++
	seq := w.draftSequence
	return w, tea.Batch(cmd, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return setupSaveTick{seq} }))
}
func (m *Workspace) guardSetupApply(cmd tea.Cmd) tea.Cmd {
	if m.draftWriter == nil || (m.Import == nil && m.Creation == nil) {
		return cmd
	}
	d := m.setupDocument()
	if d == nil {
		return cmd
	}
	d.State = "submitting"
	m.draftSaved = d
	m.draftSequence++
	m.draftSubmitted = true
	seq, writer, token := m.draftSequence, m.draftWriter, m.Pending["apply"]
	return func() tea.Msg {
		if err := writer.write(seq, *d, true); err != nil {
			return workspaceReply{Kind: "apply", Token: token, Err: fmt.Errorf("Could not safely save setup before submission: %w. No operation was sent", err)}
		}
		return cmd()
	}
}
func (m *Workspace) setupAccepted(id string) tea.Cmd {
	if m.draftWriter == nil || !m.draftSubmitted || m.draftSaved == nil {
		return nil
	}
	d := *m.draftSaved
	d.State = "submitted"
	d.OperationID = id
	m.draftSaved = &d
	m.draftSequence++
	seq, writer := m.draftSequence, m.draftWriter
	return func() tea.Msg { return setupSaved{writer.save(seq, d)} }
}
func (m Workspace) flushSetup() error {
	if m.draftWriter == nil || m.draftSaved == nil || m.draftLoading {
		return nil
	}
	return m.draftWriter.save(m.draftSequence+1, *m.draftSaved)
}

func (m *Workspace) resumePreparedJob(data any) tea.Cmd {
	d := m.draftResume
	if d == nil {
		return nil
	}
	var job domain.Job
	raw, _ := json.Marshal(data)
	if json.Unmarshal(raw, &job) != nil || job.ID != d.OperationID {
		m.Error = "Could not match the saved preparation job. Saved choices were kept."
		m.Busy = false
		return nil
	}
	if job.State == "succeeded" {
		machine := ""
		if d.Creation != nil {
			machine = d.Creation.Spec.Machine
		}
		return m.loadCreation(job.ID, machine)
	}
	m.draftResume = nil
	m.Busy = false
	m.Section = 8
	m.NavIndex = 8
	m.Detail = generic(data)
	m.DetailTitle = "Preparation job"
	m.Notice = "Preparation is not complete. Review its progress or recovery options here; your setup is saved."
	return nil
}
