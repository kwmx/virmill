package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"reflect"
	"strings"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func TestJobRefreshDoesNotOverlapOrReplaceAnotherPage(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 8
	m.Detail = generic(domain.Job{ID: workspaceVMID, State: "running"})
	next, cmd := m.Update(jobRefreshPulse{})
	m = next.(Workspace)
	if cmd == nil || m.Pending["jobs"] == 0 || m.Pending["job-update"] == 0 {
		t.Fatal("no live refresh")
	}
	token := m.Pending["job-update"]
	seq := m.sequence
	next, _ = m.Update(jobRefreshPulse{})
	m = next.(Workspace)
	if m.sequence != seq {
		t.Fatal("overlapping observations")
	}
	m.page(1)
	m.Detail = generic(workspaceVM(workspaceVMID, "keep this VM"))
	next, _ = m.Update(workspaceReply{Kind: "job-update", Token: token, Response: app.Response{Data: domain.Job{ID: workspaceVMID, State: "succeeded"}}})
	m = next.(Workspace)
	if field(m.Detail, "name") != "keep this VM" {
		t.Fatal("late job replaced another page")
	}
}
func TestJobRefreshPreservesScrollAndExplainsFailure(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 8
	m.Offset = 3
	m.Detail = generic(domain.Job{ID: workspaceVMID, State: "running"})
	next, _ := m.Update(jobRefreshPulse{})
	m = next.(Workspace)
	err := domain.Fail("DISK_FULL", "The destination has no free space.")
	err.SafeNextActions = []string{"Choose a destination with more free space."}
	next, _ = m.Update(workspaceReply{Kind: "job-update", Token: m.Pending["job-update"], Response: app.Response{Data: domain.Job{ID: workspaceVMID, State: "partial", Error: err}}})
	m = next.(Workspace)
	if m.Offset != 3 || rowState(m.Detail) != "partial" {
		t.Fatal("job refresh changed navigation or stayed stale")
	}
	text := strings.Join(m.jobDetails(80), "\n")
	for _, want := range []string{"Partly completed", "Some changes remain", "DISK_FULL", "Choose a destination"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	for _, button := range m.jobButtons() {
		if strings.Contains(button.label, "Retry") {
			t.Fatal("blind retry offered")
		}
	}
}
func TestEveryPreviewBackRestoresFormWithoutSubmission(t *testing.T) {
	for _, kind := range []string{"resources", "capture", "guest-recipe", "repository-init", "repository-check"} {
		t.Run(kind, func(t *testing.T) {
			m := fixtureWorkspace()
			f, e := NewGuidedForm(kind, workspaceVM(workspaceVMID, "guest"))
			if e != nil {
				t.Fatal(e)
			}
			f.Fields[0].Value = "retained input"
			f.Focus = len(f.Fields) - 1
			m.Form = &f
			m.Pending["plan"] = 77
			next, _ := m.Update(workspaceReply{Kind: "plan", Token: 77, Response: app.Response{Data: testWorkspacePlan(t)}})
			m = next.(Workspace)
			m, cmd := wk(m, "esc")
			if cmd != nil || m.Plan != nil || m.Form == nil || !reflect.DeepEqual(*m.Form, f) || m.SavedForm != nil {
				t.Fatal("preview lost form")
			}
		})
	}
}
func TestCreateEmptySourceListOpensUnifiedBrowser(t *testing.T) {
	m := fixtureWorkspace()
	m.CreationPicking = true
	next, cmd := m.updateCreation(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil || m.Picker == nil || m.Picker.kind != "source" || m.Advanced {
		t.Fatal("old source catalog returned")
	}
}
