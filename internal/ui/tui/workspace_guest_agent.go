package tui

import (
	"encoding/json"
	"maps"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// This view consumes only the shared service's configuration report. It never
// parses VM XML or treats a configured channel as an installed/responding agent.
type guestAgentChannelReport struct {
	State           string `json:"state"`
	HasManagedSave  bool   `json:"hasManagedSave"`
	Present         bool   `json:"present"`
	CanEnable       bool   `json:"canEnable"`
	AddsController  bool   `json:"addsController"`
	ControllerIndex uint   `json:"controllerIndex"`
	Port            uint   `json:"port"`
	Reason          string `json:"reason"`
}
type guestAgentSetup struct {
	VM      domain.VM
	Report  *guestAgentChannelReport
	Loading bool
	Index   int
	Error   string
}
type guestAgentButton struct{ label, action, hint string }

func (m *Workspace) resetGuestAgent() {
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "guest-agent-load")
	m.GuestAgent = nil
}
func (m *Workspace) openGuestTools() tea.Cmd {
	vm := m.selectedVM()
	if vm.Key.UUID == "" {
		m.Error = "Choose a VM first, then open Guest tools."
		return nil
	}
	m.resetGuestAgent()
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	m.GuestAgent = &guestAgentSetup{VM: vm, Loading: true}
	m.Form = nil
	m.Advanced = false
	m.Busy = true
	m.Error, m.Notice = "", ""
	return m.request("guest-agent-load", "vm.guest-agent.show", app.Request{ID: vm.Key.UUID})
}
func (m *Workspace) failGuestAgent(message string) {
	m.Busy = false
	if m.GuestAgent != nil {
		m.GuestAgent.Loading = false
		m.GuestAgent.Error = validation.SafeText(message)
	}
}
func (m *Workspace) receiveGuestAgent(data any) {
	if m.GuestAgent == nil {
		return
	}
	m.Busy = false
	m.GuestAgent.Loading = false
	var report guestAgentChannelReport
	raw, err := json.Marshal(data)
	// Requiring explicit booleans avoids treating an empty/malformed service reply
	// as a legitimate missing channel. Legacy capitalized names decode identically.
	var fields map[string]json.RawMessage
	valid := err == nil && json.Unmarshal(raw, &fields) == nil
	if valid {
		present, canEnable, managedSave := false, false, false
		for name, value := range fields {
			if strings.EqualFold(name, "present") {
				present = string(value) == "true" || string(value) == "false"
			}
			if strings.EqualFold(name, "hasManagedSave") {
				managedSave = string(value) == "true" || string(value) == "false"
			}
			if strings.EqualFold(name, "canEnable") {
				canEnable = string(value) == "true" || string(value) == "false"
			}
		}
		valid = present && canEnable && managedSave
	}
	if !valid || json.Unmarshal(raw, &report) != nil || report.Present && report.CanEnable {
		m.failGuestAgent("Could not read the guest-agent connection. Refresh and try again.")
		return
	}
	switch report.State {
	case "unknown", "running", "blocked", "paused", "shutting-down", "stopped", "crashed", "suspended":
	default:
		m.failGuestAgent("Could not read the VM state. Refresh and try again.")
		return
	}
	m.GuestAgent.VM.State = report.State
	m.GuestAgent.VM.HasManagedSave = report.HasManagedSave
	m.GuestAgent.Report = &report
	m.GuestAgent.Error = ""
	m.GuestAgent.Index = 0
	m.Error = ""
	if report.Present && report.State == "running" && !report.HasManagedSave {
		m.continueGuestTools()
	}
}
func (m *Workspace) continueGuestTools() {
	if m.GuestAgent == nil {
		return
	}
	vm := m.GuestAgent.VM
	f, err := NewGuidedForm("guest-tools", vm)
	if err != nil {
		m.failGuestAgent(err.Error())
		return
	}
	present := m.GuestAgent.Report != nil && m.GuestAgent.Report.Present
	m.resetGuestAgent()
	m.Form = &f
	m.Busy = false
	m.Error = ""
	if present {
		m.Notice = "Guest-agent connection is configured. Software installation and agent response are checked separately."
	} else {
		m.Notice = "Automatic Linux setup needs the guest-agent connection. Windows instructions are available in Guest system."
	}
}
func (m Workspace) guestAgentButtons() []guestAgentButton {
	if m.GuestAgent == nil || m.GuestAgent.Loading {
		return nil
	}
	g := m.GuestAgent
	buttons := []guestAgentButton{}
	switch {
	case g.Report == nil:
		buttons = append(buttons, guestAgentButton{"Refresh connection status", "refresh", "Read the VM's current connection settings."})
	case g.VM.HasManagedSave:
		buttons = append(buttons, guestAgentButton{"Refresh connection status", "refresh", "Return after resuming the saved VM from VM controls."})
	case g.Report.Present && g.VM.State == "stopped":
		buttons = append(buttons,
			guestAgentButton{"Preview start VM", "start", "Review starting this VM. Return to Guest tools when it is running."},
			guestAgentButton{"Refresh connection status", "refresh", "Check whether the VM is running and ready for installation options."})
	case g.Report.Present:
		buttons = append(buttons, guestAgentButton{"Refresh connection status", "refresh", "Check the VM state again after resolving it in VM controls."})
	case g.VM.State == "running":
		buttons = append(buttons, guestAgentButton{"Preview shut down VM", "stop", "Ask the guest to shut down. Return to Guest tools when it has stopped."})
	case (g.VM.State == "stopped" || g.VM.State == "shut off" || g.VM.State == "shutoff") && g.Report.CanEnable:
		buttons = append(buttons, guestAgentButton{"Preview enable connection", "enable", "Review the host/guest connection change. No software is installed yet."})
	default:
		buttons = append(buttons, guestAgentButton{"Refresh connection status", "refresh", "Read current availability before trying again."})
	}
	options := guestAgentButton{"Installation options / Windows help", "continue", "Choose a guest system, read manual instructions, or prepare SSH settings."}
	if g.Report != nil && (g.VM.State != "running" || g.VM.HasManagedSave) {
		options = guestAgentButton{"Prepare options / Windows help", "continue", "Prepare settings or read Windows help. Installation needs a running VM."}
	}
	return append(buttons, options, guestAgentButton{"Back to VM", "back", "Return without changing this VM."})
}
func (m Workspace) updateGuestAgent(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.GuestAgent == nil {
		return m, nil
	}
	if key.Type == tea.KeyEsc {
		m.resetGuestAgent()
		m.Busy = false
		m.Error, m.Notice = "", ""
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "plan")
		return m, nil
	}
	if m.Busy || m.GuestAgent.Loading {
		return m, nil
	}
	buttons := m.guestAgentButtons()
	if len(buttons) == 0 {
		return m, nil
	}
	g := *m.GuestAgent
	m.GuestAgent = &g
	switch key.Type {
	case tea.KeyTab, tea.KeyDown, tea.KeyRight:
		g.Index = (g.Index + 1) % len(buttons)
	case tea.KeyShiftTab, tea.KeyUp, tea.KeyLeft:
		g.Index = (g.Index + len(buttons) - 1) % len(buttons)
	case tea.KeyEnter:
		switch buttons[max(0, min(g.Index, len(buttons)-1))].action {
		case "back":
			m.resetGuestAgent()
			m.Error, m.Notice = "", ""
		case "continue":
			m.continueGuestTools()
		case "refresh":
			g.Loading = true
			g.Report = nil
			g.Error = ""
			m.Busy = true
			m.Error = ""
			return m, m.request("guest-agent-load", "vm.guest-agent.show", app.Request{ID: g.VM.Key.UUID})
		case "stop":
			m.Busy = true
			m.Error = ""
			m.Notice = "Preparing a graceful shutdown review..."
			return m, m.request("plan", "vm.plan", app.Request{ID: g.VM.Key.UUID, Action: "stop"})
		case "start":
			m.Busy = true
			m.Error = ""
			m.Notice = "Preparing a VM start review..."
			return m, m.request("plan", "vm.plan", app.Request{ID: g.VM.Key.UUID, Action: "start"})
		case "enable":
			m.Busy = true
			m.Error = ""
			m.Notice = "Preparing a guest-agent connection review..."
			return m, m.request("plan", "vm.plan", app.Request{ID: g.VM.Key.UUID, Action: "set", Input: map[string]any{"enableGuestAgent": true, "applyMode": "next-boot"}})
		}
	}
	return m, nil
}
func (m Workspace) guestAgentView(width, height int) []string {
	if m.GuestAgent == nil {
		return nil
	}
	g := m.GuestAgent
	if g.Loading {
		return pageLines([]string{"Guest tools", "Checking the VM's guest-agent connection...", "Esc goes back."}, width, height, 0)
	}
	lines := []string{"Guest tools · " + validation.SafeText(g.VM.Name), "Install software inside a running guest.", ""}
	switch {
	case g.Report == nil:
		lines = append(lines, "Connection status could not be checked.")
	case g.Report.Present:
		lines = append(lines, "Connection configured; software and agent response are not checked.")
		lines = append(lines, guestAgentRuntimeHelp(g.VM)...)
	case g.VM.HasManagedSave:
		lines = append(lines, "This VM has saved runtime state.", "Resolve it before adding the guest-agent connection.")
	case g.VM.State == "running":
		lines = append(lines, "The guest-agent connection is missing.", "Shut down this VM before adding it.")
	case g.VM.State != "stopped":
		lines = append(lines, guestAgentRuntimeHelp(g.VM)...)
	case g.Report.CanEnable:
		lines = append(lines, "The guest-agent connection is missing.", "Enable it so this host can exchange management messages with the guest.")
	default:
		lines = append(lines, "The connection cannot be added to this configuration.")
		if g.Report.Reason != "" {
			lines = append(lines, validation.SafeText(g.Report.Reason))
		}
	}
	lines = append(lines, "")
	buttons := m.guestAgentButtons()
	for i, button := range buttons {
		mark := "  "
		if i == g.Index {
			mark = "> "
		}
		lines = append(lines, mark+"[ "+button.label+" ]")
	}
	lines = append(lines, "")
	if len(buttons) > 0 {
		lines = append(lines, buttons[max(0, min(g.Index, len(buttons)-1))].hint)
	}
	if g.Error != "" {
		lines = append(lines, "Issue: "+validation.SafeText(g.Error))
	}
	lines = append(lines, "", "Tab/Arrows Select   Enter Choose   Esc Back")
	return pageLines(lines, width, height, 0)
}

func guestAgentRuntimeHelp(vm domain.VM) []string {
	switch {
	case vm.HasManagedSave:
		return []string{"This VM has saved runtime state.", "Resume it from VM controls, then return here and refresh."}
	case vm.State == "stopped":
		return []string{"This VM is stopped. Start it before installing guest tools."}
	case vm.State == "paused" || vm.State == "suspended":
		return []string{"This VM is " + vm.State + ".", "Resume it from VM controls, then return here and refresh."}
	case vm.State == "shutting-down":
		return []string{"This VM is shutting down. Wait for it to stop, then refresh."}
	default:
		return []string{"VM state: " + validation.SafeText(vm.State) + ".", "Check its state in VM controls, then return here and refresh."}
	}
}
