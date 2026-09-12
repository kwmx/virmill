//go:build linux

package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app/importer"
)

func setupTestWorkspace(t *testing.T) Workspace {
	t.Helper()
	store, _ := newTestDraftStore(t)
	m := NewWorkspace(nil, "qemu:///system")
	m.AttachDraftStore(store)
	next, _ := m.Update(m.loadSetup()())
	return next.(Workspace)
}
func TestSetupAutosaveDebouncedExitAndResumeCreation(t *testing.T) {
	m := setupTestWorkspace(t)
	f := creationFormFixture()
	f.CPUText = "17"
	f.MemoryText = "24576"
	f.Spec.GuestAgent = true
	m.Creation = &f
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Workspace)
	if m.draftSequence != 1 || m.draftSaved == nil {
		t.Fatal("editing did not queue draft")
	}
	if err := m.flushSetup(); err != nil {
		t.Fatal(err)
	}
	fresh := NewWorkspace(&creationWorkspaceClient{}, m.Connection)
	fresh.AttachDraftStore(m.draftWriter.store)
	next, _ = fresh.Update(fresh.loadSetup()())
	fresh = next.(Workspace)
	if cmd := fresh.openCreationSources(); cmd != nil || fresh.draftModal == "" {
		t.Fatal("resume choice missing")
	}
	if !strings.Contains(fresh.View(), "Resume saved setup") {
		t.Fatal(fresh.View())
	}
	next, cmd := fresh.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fresh = next.(Workspace)
	if fresh.Creation != nil || cmd == nil {
		t.Fatal("restored without rereading source")
	}
	next, _ = fresh.Update(cmd())
	fresh = next.(Workspace)
	if fresh.Creation == nil || !reflect.DeepEqual(fresh.Creation.Spec, f.Spec) || fresh.Creation.CPUText != "17" {
		t.Fatal("lost choices on validated resume", fresh.Error)
	}
	if fresh.Plan != nil {
		t.Fatal("saved approval restored")
	}
}
func TestSetupStaleImportRetainsOriginalChoices(t *testing.T) {
	m := setupTestWorkspace(t)
	description := importer.SourceDescription{Source: "/tmp/installer.iso", Kind: "iso", Name: "original"}
	f := NewImportForm("iso")
	f.Draft.Source = description.Source
	if err := f.Draft.ApplySourceDescription(description); err != nil {
		t.Fatal(err)
	}
	f.Draft.VCPUs = "9"
	f.Draft.MemoryMiB = "12345"
	m.Import = &f
	m.draftSaved = m.setupDocument()
	m.draftResume = m.draftSaved
	m.Import = nil
	restored := m.draftSaved.Import.form()
	m.Import = &restored
	description.Name = "changed"
	m.importInspection(description)
	if m.draftResume == nil || m.Import.Draft.VCPUs != "9" || !strings.Contains(m.Import.Error, "changed") {
		t.Fatal("stale source silently replaced saved choices")
	}
	next, cmd := m.updateImport(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || next.(Workspace).Plan != nil {
		t.Fatal("stale source continued")
	}
	if m.draftSaved.Import.VCPUs != "9" {
		t.Fatal("original saved choices lost")
	}
}
func TestSetupImportResumeRetainsAdvancedSettings(t *testing.T) {
	m := setupTestWorkspace(t)
	description := importer.SourceDescription{Source: "/tmp/installer.iso", Kind: "iso", Name: "original"}
	f := NewImportForm("iso")
	f.Draft.Source = description.Source
	if err := f.Draft.ApplySourceDescription(description); err != nil {
		t.Fatal(err)
	}
	f.Draft.VCPUs = "9"
	f.Draft.MemoryMiB = "12345"
	hardware := creationFormFixture()
	hardware.BeforePreparation = true
	hardware.OperationID = ""
	hardware.Spec.GuestAgent = true
	f.VM = &hardware
	f.VMBinding = creationDraftBinding(f.Draft)
	m.Import = &f
	m.draftSaved = m.setupDocument()
	m.draftResume = m.draftSaved
	resumed := m.draftSaved.Import.form()
	m.Import = &resumed
	m.importInspection(description)
	if m.draftResume != nil || m.Import.VM == nil || !reflect.DeepEqual(m.Import.VM.Spec, hardware.Spec) || m.Import.Draft.VCPUs != "9" {
		t.Fatal("advanced settings lost", m.Import.Error)
	}
	if !m.Import.Draft.HasSourceDescription() {
		t.Fatal("source was not rebound")
	}
}
func TestSetupSubmissionSavedBeforeApplyAndNeverAutoRepeated(t *testing.T) {
	m := setupTestWorkspace(t)
	f := creationFormFixture()
	m.Creation = &f
	sent := 0
	cmd := m.guardSetupApply(func() tea.Msg { sent++; return nil })
	if sent != 0 {
		t.Fatal("apply started before save")
	}
	cmd()
	if sent != 1 {
		t.Fatal("apply not dispatched")
	}
	var saved SavedSetupDocument
	if _, err := m.draftWriter.store.Load("import", &saved); err != nil || saved.State != "submitting" {
		t.Fatal("submission fence absent", err)
	}
	fresh := NewWorkspace(nil, m.Connection)
	fresh.AttachDraftStore(m.draftWriter.store)
	next, _ := fresh.Update(fresh.loadSetup()())
	fresh = next.(Workspace)
	fresh.openCreationSources()
	if !strings.Contains(fresh.View(), "Open Jobs") || strings.Contains(fresh.View(), "Resume saved setup") {
		t.Fatal("restart suggested replay", fresh.View())
	}
	if sent != 1 {
		t.Fatal("operation replayed")
	}
}
func TestSetupConflictBlocksDispatchAndPreservesOtherClient(t *testing.T) {
	m := setupTestWorkspace(t)
	f := creationFormFixture()
	m.Creation = &f
	other := m.setupDocument()
	other.Creation.CPUText = "other-client"
	if _, err := m.draftWriter.store.Save("import", "", *other); err != nil {
		t.Fatal(err)
	}
	sent := false
	msg := m.guardSetupApply(func() tea.Msg { sent = true; return nil })()
	reply, ok := msg.(workspaceReply)
	if !ok || reply.Err == nil || sent {
		t.Fatal("conflicting client overwrote or applied")
	}
	var saved SavedSetupDocument
	if _, err := m.draftWriter.store.Load("import", &saved); err != nil || saved.Creation.CPUText != "other-client" {
		t.Fatal("other client lost", err)
	}
	if !errors.Is(reply.Err, ErrDraftConflict) {
		t.Fatal(reply.Err)
	}
}

func TestSetupResumeBeforeInspectionUsesDetectedDefaults(t *testing.T) {
	m := setupTestWorkspace(t)
	f := NewImportForm("auto")
	f.Draft.Source = "/tmp/installer.iso"
	f.Draft.SelectedSource = f.Draft.Source
	f.Draft.DestinationName = "my-images"
	m.Import = &f
	m.draftSaved = m.setupDocument()
	m.draftResume = m.draftSaved
	m.importInspection(importer.SourceDescription{Source: f.Draft.Source, Kind: "iso", Name: "detected"})
	if m.draftResume != nil || m.Import.Draft.Kind != "iso" || len(m.Import.Draft.Disks) == 0 || !m.Import.Draft.HasSourceDescription() || m.Import.Draft.DestinationName != "my-images" {
		t.Fatal("uninspected source resumed with invalid defaults", m.Import.Draft)
	}
}
func TestSetupExitSupersedesSubmissionBarrierWithoutDispatch(t *testing.T) {
	m := setupTestWorkspace(t)
	f := creationFormFixture()
	m.Creation = &f
	sent := false
	cmd := m.guardSetupApply(func() tea.Msg { sent = true; return nil })
	if err := m.flushSetup(); err != nil {
		t.Fatal(err)
	}
	reply := cmd().(workspaceReply)
	if sent || reply.Err == nil || !strings.Contains(reply.Err.Error(), "superseded") {
		t.Fatal("stale barrier dispatched", reply.Err)
	}
}
func TestSetupStartNewClearsRetainedChoicesAndSubmission(t *testing.T) {
	m := setupTestWorkspace(t)
	f := NewImportForm("iso")
	f.Draft.VMName = "old"
	m.Import = &f
	m.SavedImport = &f
	m.draftSaved = m.setupDocument()
	m.draftSubmitted = true
	m.draftModal = "iso"
	m.draftChoice = 1
	next, _ := m.updateSetup(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if m.draftSubmitted || m.SavedImport != nil || m.Import == nil || m.Import.Draft.VMName == "old" {
		t.Fatal("Start new resurrected old setup")
	}
}
func TestSetupSubmittedPreparationContinuesAdvancedChoices(t *testing.T) {
	m := setupTestWorkspace(t)
	f := creationFormFixture()
	f.OperationID = ""
	f.BeforePreparation = true
	f.CPUText = "19"
	f.Spec.GuestAgent = true
	imp := NewImportForm("iso")
	imp.Draft.Source = "/tmp/installer.iso"
	imp.VM = &f
	imp.VMBinding = creationDraftBinding(imp.Draft)
	m.Import = &imp
	d := m.setupDocument()
	d.State = "submitted"
	d.OperationID = creationFormOperation
	m.Import = nil
	m.draftSaved = d
	m.draftResume = d
	m.Client = &creationWorkspaceClient{}
	cmd := m.resumePreparedJob(map[string]any{"operationID": creationFormOperation, "state": "succeeded"})
	if cmd == nil {
		t.Fatal("did not load prepared source", m.Error)
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Creation == nil || m.Creation.OperationID != creationFormOperation || m.Creation.CPUText != "19" || !m.Creation.Spec.GuestAgent || m.Creation.BeforePreparation {
		t.Fatal("prepared continuation lost settings", m.Error)
	}
	if m.draftSaved.State != "editing" || m.draftSaved.Import != nil || m.draftSaved.Creation.OperationID != creationFormOperation {
		t.Fatal("prepared continuation did not save new binding")
	}
}

func TestSetupApplyUpdateRetainsDurableBarrierState(t *testing.T) {
	m := setupTestWorkspace(t)
	f := creationFormFixture()
	m.Creation = &f
	p := testWorkspacePlan(t)
	m.Plan = &p
	m.Reviewing = true
	m.Approved = make([]bool, len(p.Acknowledgements))
	for i := range m.Approved {
		m.Approved[i] = true
	}
	m.AckIndex = len(m.Approved)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil || !m.draftSubmitted || m.draftSaved == nil || m.draftSaved.State != "submitting" {
		t.Fatal("apply barrier state lost across Update return")
	}
	if m.draftSequence != 1 {
		t.Fatal("editing autosave overwrote submission fence")
	}
}

func TestSetupPreparationReadFailureOffersRetryAndKeepsChoices(t *testing.T) {
	m := setupTestWorkspace(t)
	f := NewImportForm("iso")
	f.Draft.Source = "/tmp/installer.iso"
	f.Draft.VCPUs = "11"
	m.Import = &f
	d := m.setupDocument()
	d.State = "submitted"
	d.OperationID = creationFormOperation
	m.Import = nil
	m.draftSaved = d
	m.draftResume = d
	m.Busy = true
	m.Pending["draft-preparation-job"] = 12
	next, _ := m.Update(workspaceReply{Kind: "draft-preparation-job", Token: 12, Err: errors.New("coordinator unavailable")})
	m = next.(Workspace)
	if m.Busy || m.draftResume != nil || m.draftModal == "" || m.draftSaved.Import.VCPUs != "11" {
		t.Fatal("failed read stuck or discarded saved state")
	}
	view := m.View()
	if !strings.Contains(view, "Continue prepared setup") || !strings.Contains(view, "Could not check the saved preparation") {
		t.Fatal("retry/error was hidden", view)
	}
}
func TestSetupUnconfirmedMarkerDoesNotClaimSubmission(t *testing.T) {
	m := setupTestWorkspace(t)
	f := creationFormFixture()
	m.Creation = &f
	d := m.setupDocument()
	d.State = "submitting"
	m.draftSaved = d
	m.draftModal = "creation"
	view := m.View()
	if strings.Contains(view, "A setup was submitted") || !strings.Contains(view, "outcome was not confirmed") {
		t.Fatal("unconfirmed marker made a success claim", view)
	}
}
func TestSetupCanceledResumeInspectionReturnsToResumeChoices(t *testing.T) {
	m := setupTestWorkspace(t)
	f := NewImportForm("iso")
	f.Draft.Source = "/tmp/installer.iso"
	f.Draft.VCPUs = "11"
	m.Import = &f
	m.draftSaved = m.setupDocument()
	m.draftResume = m.draftSaved
	m.Busy = true
	m.Pending["import-inspect"] = 2
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Workspace)
	if m.Busy || m.Import != nil || m.draftResume != nil || m.draftModal == "" || m.draftSaved.Import.VCPUs != "11" {
		t.Fatal("canceled resume left inert form or lost choices")
	}
	if !strings.Contains(m.View(), "Resume saved setup") || !strings.Contains(m.Notice, "saved choices were kept") {
		t.Fatal(m.View())
	}
}
