package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
)

func agentSetupFixture() (Workspace, *workspaceClient) {
	m := fixtureWorkspace()
	m.Section = 1
	m.Selected = 1
	client := &workspaceClient{response: app.Response{Data: guestAgentChannelReport{State: "stopped", CanEnable: true}}}
	m.Client = client
	return m, client
}
func TestGuestAgentSetupLoadsAndOffersReviewedEnable(t *testing.T) {
	m, client := agentSetupFixture()
	cmd := m.openGuestTools()
	if cmd == nil || m.GuestAgent == nil || !m.Busy || !m.GuestAgent.Loading || m.Form != nil {
		t.Fatal("missing prerequisite read")
	}
	reply := cmd().(workspaceReply)
	m.receiveGuestAgent(reply.Response.Data)
	if client.calls[0] != "vm.guest-agent.show" || client.requests[0].ID != workspaceVMID || m.GuestAgent == nil || m.Busy {
		t.Fatal("wrong observed target")
	}
	next, cmd := m.updateGuestAgent(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil || !m.Busy {
		t.Fatal("no reviewed enable request")
	}
	cmd()
	request := client.requests[1]
	if client.calls[1] != "vm.plan" || request.ID != workspaceVMID || request.Action != "set" || !reflect.DeepEqual(request.Input, map[string]any{"enableGuestAgent": true, "applyMode": "next-boot"}) || request.Apply != nil {
		t.Fatal("not exact shared plan", request)
	}
	if m.GuestAgent == nil || m.Form != nil {
		t.Fatal("review origin not retained")
	}
}
func TestGuestAgentSetupPresentContinuesToTools(t *testing.T) {
	m, client := agentSetupFixture()
	client.response.Data = map[string]any{"Present": true, "CanEnable": false, "State": "running", "HasManagedSave": false}
	cmd := m.openGuestTools()
	reply := cmd().(workspaceReply)
	m.receiveGuestAgent(reply.Response.Data)
	if m.GuestAgent != nil || m.Form == nil || m.Form.Kind != "guest-tools" || m.Form.VM.Key.UUID != workspaceVMID || m.Busy {
		t.Fatal("existing channel did not open tools")
	}
	if len(client.calls) != 1 || !strings.Contains(m.Notice, "checked separately") {
		t.Fatal("installed/responding agent implied")
	}
}
func TestGuestAgentSetupRunningOffersGracefulStopAndManualHelp(t *testing.T) {
	m, client := agentSetupFixture()
	m.openGuestTools()
	m.GuestAgent.VM.State = "running"
	m.receiveGuestAgent(guestAgentChannelReport{State: "running"})
	buttons := m.guestAgentButtons()
	if buttons[0].action != "stop" || buttons[1].action != "continue" {
		t.Fatal(buttons)
	}
	next, cmd := m.updateGuestAgent(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil {
		t.Fatal("no stop preview")
	}
	cmd()
	if client.requests[0].Action != "stop" || client.requests[0].Apply != nil {
		t.Fatal("unexpected automatic stop")
	}
	m.Busy = false
	m.GuestAgent.Index = 1
	next, cmd = m.updateGuestAgent(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd != nil || m.Form == nil || m.GuestAgent != nil {
		t.Fatal("manual options inaccessible")
	}
}
func TestGuestAgentSetupRefusalsAndRefresh(t *testing.T) {
	for _, bad := range []any{nil, map[string]any{}, map[string]any{"present": "true", "canEnable": false}, map[string]any{"present": false, "canEnable": nil}, guestAgentChannelReport{State: "stopped", Present: true, CanEnable: true}} {
		m, _ := agentSetupFixture()
		m.openGuestTools()
		m.receiveGuestAgent(bad)
		if m.GuestAgent == nil || m.GuestAgent.Report != nil || m.GuestAgent.Error == "" || m.Busy {
			t.Fatal("malformed report accepted", bad)
		}
		_, cmd := m.updateGuestAgent(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("refresh inaccessible")
		}
	}
	m, _ := agentSetupFixture()
	m.openGuestTools()
	m.GuestAgent.VM.HasManagedSave = true
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", HasManagedSave: true, CanEnable: true})
	if m.guestAgentButtons()[0].action == "enable" {
		t.Fatal("saved state enable offered")
	}
	m.GuestAgent.VM.HasManagedSave = false
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", CanEnable: false, Reason: "unsupported controller"})
	if m.guestAgentButtons()[0].action == "enable" {
		t.Fatal("unsupported enable offered")
	}
}
func TestGuestAgentSetupCancelInvalidatesReadWithoutMutation(t *testing.T) {
	m, _ := agentSetupFixture()
	m.openGuestTools()
	token := m.Pending["guest-agent-load"]
	if token == 0 {
		t.Fatal("no read token")
	}
	next, cmd := m.updateGuestAgent(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Workspace)
	if cmd != nil || m.GuestAgent != nil || m.Pending["guest-agent-load"] != 0 || m.Busy {
		t.Fatal("cancel retained pending read")
	}
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", CanEnable: true})
	if m.GuestAgent != nil || m.Form != nil {
		t.Fatal("late reply reopened setup")
	}
}
func TestGuestAgentSetupKeyboardAndResponsiveView(t *testing.T) {
	m, _ := agentSetupFixture()
	m.openGuestTools()
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", CanEnable: true})
	m.GuestAgent.VM.Name = "Guest\x1b[2J\u202e"
	for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyDown, tea.KeyShiftTab, tea.KeyUp} {
		next, cmd := m.updateGuestAgent(tea.KeyMsg{Type: key})
		m = next.(Workspace)
		if cmd != nil {
			t.Fatal("navigation executed")
		}
	}
	if m.GuestAgent.Index != 0 {
		t.Fatal("focus navigation drift")
	}
	for _, size := range [][2]int{{80, 24}, {120, 36}, {40, 10}} {
		lines := m.guestAgentView(size[0], size[1])
		if len(lines) > size[1] {
			t.Fatal("height overflow")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] || strings.ContainsAny(line, "\x1b\u202e") {
				t.Fatal("unsafe view", line)
			}
		}
		if size[0] >= 80 && !strings.Contains(strings.Join(lines, "\n"), "> [ Preview enable connection ]") {
			t.Fatal("primary action invisible")
		}
	}
	m.failGuestAgent(errors.New("service disconnected").Error())
	if m.Busy || m.GuestAgent.Error != "service disconnected" {
		t.Fatal("failure swallowed")
	}
}

func TestGuestAgentRefreshUsesFreshRuntimeState(t *testing.T) {
	m, client := agentSetupFixture()
	m.openGuestTools()
	m.receiveGuestAgent(guestAgentChannelReport{State: "running", CanEnable: true})
	if m.guestAgentButtons()[0].action != "stop" {
		t.Fatal("running guest should offer shutdown")
	}
	// Simulate the guest shutting down outside this view; the next shared report
	// must replace the captured inventory state before deciding which action fits.
	client.response.Data = guestAgentChannelReport{State: "stopped", HasManagedSave: true, CanEnable: true}
	m.GuestAgent.Report = nil
	next, cmd := m.updateGuestAgent(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil {
		t.Fatal("refresh unavailable")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if m.GuestAgent.VM.State != "stopped" || !m.GuestAgent.VM.HasManagedSave || m.guestAgentButtons()[0].action != "refresh" {
		t.Fatal("fresh saved-state restriction lost")
	}
	client.response.Data = guestAgentChannelReport{State: "stopped", CanEnable: true}
	next, cmd = m.updateGuestAgent(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil {
		t.Fatal("second refresh unavailable")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if m.GuestAgent.VM.HasManagedSave || m.guestAgentButtons()[0].action != "enable" {
		t.Fatal("fresh stopped state did not enable setup")
	}
	for _, bad := range []any{
		map[string]any{"present": false, "canEnable": true, "state": "stopped"},
		map[string]any{"present": false, "canEnable": true, "state": "invalid", "hasManagedSave": false},
		map[string]any{"present": false, "canEnable": true, "hasManagedSave": false},
	} {
		m.GuestAgent.Report = nil
		m.receiveGuestAgent(bad)
		if m.GuestAgent.Report != nil || m.GuestAgent.Error == "" {
			t.Fatal("incomplete runtime report accepted", bad)
		}
	}
}
