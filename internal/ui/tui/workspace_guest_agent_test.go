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

func TestGuestAgentPresentStoppedRequiresReviewedStart(t *testing.T) {
	m, client := agentSetupFixture()
	m.openGuestTools()
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", Present: true})
	if m.GuestAgent == nil || m.Form != nil || m.Busy {
		t.Fatal("stopped VM bypassed readiness gateway")
	}
	buttons := m.guestAgentButtons()
	if len(buttons) != 4 || buttons[0].action != "start" || buttons[1].action != "refresh" || buttons[2].action != "continue" {
		t.Fatal("missing start/refresh/preparation choices", buttons)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil || !m.Busy || m.Form != nil {
		t.Fatal("missing start review")
	}
	cmd()
	if len(client.calls) != 1 || client.calls[0] != "vm.plan" || client.requests[0].Action != "start" || client.requests[0].ID != workspaceVMID || client.requests[0].Apply != nil {
		t.Fatal("start was not an exact reviewed request", client.requests)
	}
	next, _ = m.Update(workspaceReply{Kind: "plan", Token: m.Pending["plan"], Response: app.Response{Data: testWorkspacePlan(t)}})
	m = next.(Workspace)
	if m.Plan == nil || m.GuestAgent == nil {
		t.Fatal("review lost readiness gateway")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Workspace)
	if m.Plan != nil || m.GuestAgent == nil || m.Form != nil || m.guestAgentButtons()[0].action != "start" {
		t.Fatal("review back did not retain gateway")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	next, _ = m.Update(workspaceReply{Kind: "plan", Token: m.Pending["plan"], Err: errors.New("VM state changed")})
	m = next.(Workspace)
	if m.Busy || m.GuestAgent == nil || !strings.Contains(m.View(), "VM state changed") {
		t.Fatal("review failure lost context or error", m.View())
	}
}

func TestGuestAgentPresentNonrunningExplainsReadiness(t *testing.T) {
	for _, tc := range []struct {
		state, want string
		saved       bool
	}{
		{"stopped", "saved runtime state", true},
		{"running", "saved runtime state", true},
		{"paused", "Resume it from VM controls", false},
		{"suspended", "Resume it from VM controls", false},
		{"shutting-down", "Wait for it to stop", false},
		{"blocked", "Check its state in VM controls", false},
		{"crashed", "Check its state in VM controls", false},
		{"unknown", "Check its state in VM controls", false},
	} {
		t.Run(tc.state+tc.want, func(t *testing.T) {
			m, client := agentSetupFixture()
			m.openGuestTools()
			m.receiveGuestAgent(guestAgentChannelReport{State: tc.state, Present: true, HasManagedSave: tc.saved})
			if m.GuestAgent == nil || m.Form != nil || m.Busy {
				t.Fatal("not-ready VM requested credentials")
			}
			for _, b := range m.guestAgentButtons() {
				if b.action == "enable" || b.action == "start" || b.action == "stop" {
					t.Fatal("inappropriate mutation offered", b)
				}
			}
			view := strings.Join(m.guestAgentView(80, 17), "\n")
			if !strings.Contains(view, tc.want) || !strings.Contains(view, "software and agent response are not checked") || strings.Contains(view, "connection is missing") || !strings.Contains(view, "Esc Back") {
				t.Fatal("readiness unclear in 80x24 body", view)
			}
			if len(client.calls) != 0 {
				t.Fatal("readiness observation caused side effects")
			}
		})
	}
}

func TestGuestAgentPresentRefreshOpensInstallationOnlyWhenRunning(t *testing.T) {
	m, client := agentSetupFixture()
	m.openGuestTools()
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", Present: true})
	m.GuestAgent.Index = 1
	client.response.Data = guestAgentChannelReport{State: "running", Present: true}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil || !m.Busy || !m.GuestAgent.Loading || m.Form != nil || !strings.Contains(m.View(), "Checking the VM") {
		t.Fatal("missing refresh loading state")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if m.GuestAgent != nil || m.Form == nil || m.Form.VM.State != "running" || m.Form.Kind != "guest-tools" || m.Busy {
		t.Fatal("fresh running VM did not reach installation")
	}
	if len(client.calls) != 1 || client.calls[0] != "vm.guest-agent.show" {
		t.Fatal("refresh mutated VM")
	}
}

func TestGuestAgentPresentGatewayFailureCancelAndOptionalHelp(t *testing.T) {
	m, _ := agentSetupFixture()
	m.openGuestTools()
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", Present: true})
	m.GuestAgent.Index = 1
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	next, _ = m.Update(workspaceReply{Kind: "guest-agent-load", Token: m.Pending["guest-agent-load"], Err: errors.New("service unavailable")})
	m = next.(Workspace)
	if m.Busy || m.GuestAgent == nil || !strings.Contains(m.View(), "service unavailable") || m.guestAgentButtons()[0].action != "refresh" {
		t.Fatal("refresh failure did not remain recoverable", m.View())
	}
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", Present: true})
	m.GuestAgent.Index = 2
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd != nil || m.Form == nil || m.Form.Kind != "guest-tools" || m.Form.VM.State != "stopped" {
		t.Fatal("optional preparation/help unavailable")
	}
	// A late start review must not reopen the gateway after cancellation.
	m, _ = agentSetupFixture()
	m.openGuestTools()
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", Present: true})
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	token := m.Pending["plan"]
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Workspace)
	next, _ = m.Update(workspaceReply{Kind: "plan", Token: token, Response: app.Response{Data: testWorkspacePlan(t)}})
	m = next.(Workspace)
	if m.GuestAgent != nil || m.Plan != nil || m.Form != nil || m.Busy {
		t.Fatal("late start review survived cancel")
	}
}

func TestGuestAgentPresentStoppedViewFits80x24(t *testing.T) {
	m, _ := agentSetupFixture()
	m.openGuestTools()
	m.receiveGuestAgent(guestAgentChannelReport{State: "stopped", Present: true})
	m.Width, m.Height = 80, 24
	for i := range m.guestAgentButtons() {
		m.GuestAgent.Index = i
		body := strings.Join(m.guestAgentView(80, 17), "\n")
		for _, want := range []string{"This VM is stopped", "Preview start VM", "Refresh connection status", "Prepare options / Windows help", "Back to VM", "Enter Choose", "Esc Back"} {
			if !strings.Contains(body, want) {
				t.Fatal("80x24 body missing", want, body)
			}
		}
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) > 24 {
			t.Fatal("80x24 height overflow")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > 80 {
				t.Fatal("80x24 width overflow", line)
			}
		}
	}
	t.Log("Synthetic 80x24 view:\n" + m.View())
}
