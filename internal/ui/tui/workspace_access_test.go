package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
)

func TestProtectionWorkspaceSelectedCaptureReviewBackAndPicker(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 6
	m.Data["captures"] = generic([]string{protectionCapture})
	client := &workspaceClient{response: app.Response{Data: []domain.StoragePool{protectionTestPool()}}}
	m.Client = client
	cmd := m.openProtection("snapshot restore")
	if cmd == nil || m.Protection == nil {
		t.Fatal(m.Error)
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Busy || m.Protection.Fields[1].Value != protectionPool {
		t.Fatal(m.Error, m.Protection)
	}
	protectionSet(m.Protection, "name", "Recovered guest")
	m.Protection.Focus = len(m.Protection.Fields)
	wanted := *m.Protection
	client.response = app.Response{Data: testWorkspacePlan(t)}
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("no preview", m.Error)
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if client.calls[1] != "snapshot.restore" || client.requests[1].Input["name"] != "Recovered guest" || client.requests[1].ID != protectionCapture {
		t.Fatal(client.requests)
	}
	m, _ = wk(m, "esc")
	if m.Plan != nil || !reflect.DeepEqual(*m.Protection, wanted) {
		t.Fatal("review back lost form")
	}
	m, _ = wk(m, "esc")
	if m.Protection != nil {
		t.Fatal("back stuck")
	}
	_ = m.openProtection("backup create")
	if m.Picker == nil || !m.PickerProtection || m.ActionForm != nil {
		t.Fatal("backup requires manual settings", m.Error)
	}
	m.Picker = nil
	protectionSet(m.Protection, "repository", "/tmp/backups")
	protectionSet(m.Protection, "passwordFile", "/tmp/private/password")
	m.Protection.Focus = 2
	client.response = app.Response{Data: testWorkspacePlan(t)}
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal(m.Protection.Error)
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	last := client.requests[len(client.requests)-1]
	if last.Input["passwordFile"] != "/tmp/private/password" || last.Path != "" {
		t.Fatal(last)
	}
}
func TestProtectionDismissedPoolReadAndFailure(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 6
	m.Data["captures"] = generic([]string{protectionCapture})
	c := &workspaceClient{err: errors.New("pool service unavailable")}
	m.Client = c
	cmd := m.openProtection("snapshot restore")
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Busy || !strings.Contains(m.Error, "pool service unavailable") {
		t.Fatal(m.Error)
	}
	cmd = m.openProtection("snapshot restore")
	m, _ = wk(m, "esc")
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if m.Protection != nil || m.Busy {
		t.Fatal("dismissed pool response reopened form")
	}
}
func TestConsoleWorkspaceSelectedObservationAndFailureReturn(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	m.Selected = 1
	info := domain.ConsoleInfo{Resource: m.selectedVM().Key, Name: "Recovery workstation", State: "running", ConfigFingerprint: strings.Repeat("a", 64), Choices: []domain.ConsoleChoice{{ID: "serial:0", Kind: "serial", Protocol: "serial", Label: "Serial console", Available: false, Reason: "Start this VM first."}}}
	c := &workspaceClient{response: app.Response{Data: info}}
	m.Client = c
	cmd := m.openAction(ui.Action{Command: "vm console show"})
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Console == nil || m.ConsoleLoading || m.Busy || c.requests[0].ID != workspaceVMID {
		t.Fatal("console read", m.Error)
	}
	m, cmd = wk(m, "enter")
	if cmd != nil || !strings.Contains(m.Error, "Start this VM") {
		t.Fatal("launched unavailable console")
	}
	for _, size := range [][2]int{{80, 24}, {120, 36}} {
		m.Width, m.Height = size[0], size[1]
		if !strings.Contains(m.View(), "Ctrl+]") {
			t.Fatal(m.View())
		}
	}
	m.Busy = true
	next, _ = m.Update(consoleClosed{errors.New("viewer exited")})
	m = next.(Workspace)
	if m.Busy || !strings.Contains(m.Error, "viewer exited") || m.Console == nil {
		t.Fatal("viewer failure corrupted screen")
	}
	m, _ = wk(m, "esc")
	if m.Console != nil {
		t.Fatal("console back stuck")
	}
	cmd = m.openConsole()
	m, _ = wk(m, "esc")
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if m.Console != nil || m.ConsoleLoading {
		t.Fatal("late console response reopened screen")
	}
}
