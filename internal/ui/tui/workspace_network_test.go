package tui

import (
	"encoding/json"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
)

func networkWorkspace() Workspace {
	m := fixtureWorkspace()
	m.Section = 2
	m.openAction(ui.Action{Command: "network create"})
	m.NetworkForm.SetViewport(m.Width, m.Height-7)
	m.NetworkForm.Name = "friendly-lab"
	m.NetworkForm.Focus = 4
	return m
}
func TestNetworkWorkspacePreviewBackAndCanceledRead(t *testing.T) {
	m := networkWorkspace()
	if m.NetworkForm == nil || m.ActionForm != nil {
		t.Fatal("file prompt replaced form")
	}
	c := &workspaceClient{response: app.Response{Data: testWorkspacePlan(t)}}
	m.Client = c
	saved := *m.NetworkForm
	m, cmd := wk(m, "enter")
	if cmd == nil || !m.Busy {
		t.Fatal("no preview")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Plan == nil || c.calls[0] != "network.create" || c.requests[0].Path != "" || c.requests[0].Input["document"] == nil || c.requests[0].Apply != nil {
		t.Fatal("not inline preview", c.requests)
	}
	m, _ = wk(m, "esc")
	if m.Plan != nil || !reflect.DeepEqual(*m.NetworkForm, saved) {
		t.Fatal("Back lost choices")
	}
	m, cmd = wk(m, "enter")
	m, _ = wk(m, "esc")
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if m.Plan != nil || m.Busy || m.NetworkForm == nil {
		t.Fatal("late preview replaced canceled state")
	}
	m, _ = wk(m, "esc")
	if m.NetworkForm != nil {
		t.Fatal("cannot leave form")
	}
}
func TestNetworkWorkspaceFailureAndSessionKeepChoices(t *testing.T) {
	m := networkWorkspace()
	c := &workspaceClient{err: errors.New("Network service unavailable")}
	m.Client = c
	m, cmd := wk(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Busy || !strings.Contains(m.NetworkForm.Error, "unavailable") || m.NetworkForm.Name != "friendly-lab" {
		t.Fatal("failure lost draft")
	}
	m.Connection = "qemu:///session"
	m, cmd = wk(m, "enter")
	if cmd != nil || !strings.Contains(m.NetworkForm.Error, "system") || len(c.calls) != 1 {
		t.Fatal("unsupported connection previewed")
	}
}
func TestNetworkWorkspaceExportFullDeclarationAndBrowse(t *testing.T) {
	m := networkWorkspace()
	m.NetworkForm.Focus = 5
	m, _ = wk(m, "enter")
	if m.ExportForm == nil {
		t.Fatal("no export form")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = next.(Workspace)
	if cmd == nil || m.Picker == nil || m.ImportPickerTarget != "export-parent" {
		t.Fatal("no folder browser")
	}
	dir := t.TempDir()
	m.importPicked(dir)
	m.ExportForm.Fields[1].Value = "network.json"
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("no export", m.View())
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	raw, err := os.ReadFile(filepath.Join(dir, "network.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := validation.Document(raw)
	if err != nil || doc["kind"] != "Network" {
		t.Fatal("not reusable declaration", string(raw), err)
	}
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	if value["document"] != nil || m.NetworkForm == nil || m.ExportForm != nil {
		t.Fatal("export wrapped document or lost form")
	}
}
func TestNetworkWorkspaceAdvancedFileAndPageCancel(t *testing.T) {
	m := networkWorkspace()
	m.NetworkForm.Advanced = true
	m.NetworkForm.Focus = 5
	m, cmd := wk(m, "enter")
	if cmd == nil || m.ActionForm == nil || m.NetworkForm == nil || m.Picker == nil {
		t.Fatal("advanced file unavailable", m.View())
	}
	m.Picker = nil
	m, _ = wk(m, "esc")
	if m.ActionForm != nil || m.NetworkForm == nil {
		t.Fatal("advanced file back lost choices")
	}
	m.NetworkForm.Advanced = false
	m.NetworkForm.Focus = 4
	c := &workspaceClient{response: app.Response{Data: testWorkspacePlan(t)}}
	m.Client = c
	m, cmd = wk(m, "enter")
	m.page(1)
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.NetworkForm != nil || m.Plan != nil || m.Section != 1 {
		t.Fatal("late network plan reopened page")
	}
}

func TestNetworkFormDoesNotYieldToBackgroundPreparation(t *testing.T) {
	m := networkWorkspace()
	m.Section = 8
	m.Detail = generic(domain.Job{ID: creationFormOperation, State: "running"})
	m.PendingPreparation = creationFormOperation
	m.Pending["preparation-job"] = 10
	next, cmd := m.Update(workspaceReply{Kind: "preparation-job", Token: 10, Response: app.Response{Data: domain.Job{ID: creationFormOperation, State: "succeeded"}}})
	m = next.(Workspace)
	if cmd != nil || m.Creation != nil || m.CreationPicking || m.NetworkForm == nil || m.PendingPreparation != "" {
		t.Fatal("background completion stole network form")
	}
}

func TestNetworkFormCannotCancelPendingApplyAsPreview(t *testing.T) {
	m := networkWorkspace()
	m.Pending["apply"] = 51
	m.Busy = true
	for _, key := range []string{"esc", "enter", "tab"} {
		var cmd tea.Cmd
		m, cmd = wk(m, key)
		if cmd != nil || !m.Busy || m.Pending["apply"] != 51 || m.NetworkForm == nil || !strings.Contains(m.Notice, "Submission is pending") {
			t.Fatal("pending apply unlocked or labeled canceled", m.Notice)
		}
	}
}
