package tui

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"reflect"
	"testing"
	"virmill.local/core/internal/app"
)

func TestBootWorkspaceLoadsSelectedVMAndRestoresReview(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	m.Selected = 1
	f := bootFixture(t)
	client := &workspaceClient{response: app.Response{Data: f.Report}}
	m.Client = client
	cmd := m.openBootForm()
	if cmd == nil || !m.BootLoading {
		t.Fatal("missing observed boot request")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Boot == nil || m.BootLoading || m.Busy || client.calls[0] != "vm.boot.get" || client.requests[0].ID != workspaceVMID {
		t.Fatal("wrong boot selection", m.Error)
	}
	m.Boot.Focus = len(f.Report.Persistent.Devices)
	*m.Boot, _, _ = m.Boot.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Boot.Focus++
	wanted := *m.Boot
	client.response = app.Response{Data: testWorkspacePlan(t)}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil {
		t.Fatal("missing plan request")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if m.Plan == nil || client.calls[1] != "vm.plan" || client.requests[1].Input["ejectMedia"] != "sda" {
		t.Fatal("boot preview did not reach shared service")
	}
	m, cmd = wk(m, "esc")
	if cmd != nil || m.Plan != nil || m.Boot == nil || !reflect.DeepEqual(*m.Boot, wanted) {
		t.Fatal("boot review back lost edits")
	}
}
func TestApplyRetryRetainsIdempotencyKeyAfterLostResponse(t *testing.T) {
	m := fixtureWorkspace()
	p := testWorkspacePlan(t)
	m.Plan = &p
	m.Reviewing = true
	m.Approved = []bool{true, true}
	m.AckIndex = 2
	client := &workspaceClient{err: errors.New("response lost after submission")}
	m.Client = client
	m, cmd := wk(m, "enter")
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	m, _ = wk(m, "enter") // Return to confirmation after the uncertain result.
	m, cmd = wk(m, "enter")
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if len(client.requests) != 2 || client.requests[0].Apply.IdempotencyKey == "" || !reflect.DeepEqual(client.requests[0].Apply, client.requests[1].Apply) {
		t.Fatal("uncertain apply became a new operation", client.requests)
	}
}
