package tui

import (
	"context"
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"maps"
	"strings"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
)

type consolePrepared struct {
	Token   uint64
	Session *ui.ConsoleSession
	Err     error
}
type consoleClosed struct{ Err error }

// displayOpened reports a graphical display that opened in its own window;
// Done reports when that window closes.
type displayOpened struct {
	Token uint64
	Done  <-chan error
	Err   error
}
type displayClosed struct{ Err error }

// soleDisplay is the VM's one usable SPICE display, which opens without a
// choice. A VM with several consoles, or none that can open here, keeps the
// chooser.
func soleDisplay(info domain.ConsoleInfo) (domain.ConsoleChoice, bool) {
	var found domain.ConsoleChoice
	count := 0
	for _, c := range info.Choices {
		if c.Available && c.Kind == "graphical" && c.Protocol == "spice" {
			found = c
			count++
		}
	}
	return found, count == 1 && ui.DisplayAvailable()
}

// launchConsole checks access again and opens choice.
func (m *Workspace) launchConsole(connection, id, fingerprint string, choice domain.ConsoleChoice) tea.Cmd {
	m.sequence++
	token := m.sequence
	m.Pending = maps.Clone(m.Pending)
	m.Pending["console-launch"] = token
	client := m.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		session, err := ui.PrepareConsole(ctx, client, connection, id, choice.ID, fingerprint)
		return consolePrepared{token, session, err}
	}
}

// openDisplayAfterStart opens the display of a VM that New VM just started
// (ADR 0065). It never blocks the screen: when the display cannot open here,
// the notice says where to open it.
func (m *Workspace) openDisplayAfterStart(vmID string) tea.Cmd {
	if !guidedUUID.MatchString(vmID) {
		return nil
	}
	m.DisplayAutoVM = vmID
	return m.request("display-auto", "vm.console.show", app.Request{ID: vmID})
}

func (m *Workspace) receiveDisplayAuto(data any) tea.Cmd {
	vmID := m.DisplayAutoVM
	m.DisplayAutoVM = ""
	var info domain.ConsoleInfo
	b, err := json.Marshal(data)
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "vm", UUID: vmID}
	if err != nil || json.Unmarshal(b, &info) != nil || vmID == "" || info.Resource != key || info.ConfigFingerprint == "" {
		return nil
	}
	name := validation.SafeText(info.Name)
	choice, ok := soleDisplay(info)
	if !ok {
		if !ui.DisplayAvailable() {
			m.Notice = name + " is running. Its display opens on this host's desktop: choose Display on the VM there."
		} else {
			m.Notice = name + " is running. Choose Display on the VM to open its screen."
		}
		return nil
	}
	m.Notice = name + " is running. Opening its display…"
	return m.launchConsole(m.Connection, vmID, info.ConfigFingerprint, choice)
}

func (m *Workspace) openConsole() tea.Cmd {
	vm := m.selectedVM()
	if vm.Key.UUID == "" {
		m.Error = "Choose a VM first, then open Console."
		return nil
	}
	m.ConsoleVM, m.Console, m.ConsoleIndex = vm, nil, 0
	m.ConsoleLoading, m.Busy, m.Advanced, m.Error = true, true, false, ""
	return m.request("console-load", "vm.console.show", app.Request{ID: vm.Key.UUID})
}
func (m *Workspace) receiveConsole(data any) {
	m.Busy, m.ConsoleLoading = false, false
	var info domain.ConsoleInfo
	b, err := json.Marshal(data)
	if err != nil || json.Unmarshal(b, &info) != nil || info.Resource != m.ConsoleVM.Key || info.ConfigFingerprint == "" {
		m.Error = "Could not verify console details for this VM. Refresh and try again."
		return
	}
	for i := range info.Choices {
		if info.Choices[i].Available && info.Choices[i].Kind == "graphical" && info.Choices[i].Protocol != "spice" {
			info.Choices[i].Available = false
			info.Choices[i].Reason = "VNC viewer support is not ready. Use a configured serial console or private SPICE display."
		}
	}
	m.Console, m.ConsoleIndex, m.Error = &info, 0, ""
}

// consoleLoaded opens the VM's only display straight away; otherwise the
// chooser stays open.
func (m *Workspace) consoleLoaded() tea.Cmd {
	if m.Console == nil {
		return nil
	}
	choice, ok := soleDisplay(*m.Console)
	if !ok {
		return nil
	}
	m.Busy, m.Notice = true, "Opening the display…"
	return m.launchConsole(m.Connection, m.Console.Resource.UUID, m.Console.ConfigFingerprint, choice)
}
func (m Workspace) updateConsole(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyEsc {
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "console-load")
		delete(m.Pending, "console-launch")
		m.Console, m.ConsoleLoading, m.Busy, m.Error, m.Notice = nil, false, false, "", ""
		return m, nil
	}
	if m.Busy || m.Console == nil {
		return m, nil
	}
	n := len(m.Console.Choices)
	switch key.Type {
	case tea.KeyDown, tea.KeyTab:
		if n > 0 {
			m.ConsoleIndex = (m.ConsoleIndex + 1) % n
		}
	case tea.KeyUp, tea.KeyShiftTab:
		if n > 0 {
			m.ConsoleIndex = (m.ConsoleIndex + n - 1) % n
		}
	case tea.KeyEnter:
		if n == 0 {
			return m, nil
		}
		choice := m.Console.Choices[m.ConsoleIndex]
		if !choice.Available {
			m.Error = validation.SafeText(choice.Reason)
			return m, nil
		}
		m.Busy, m.Error, m.Notice = true, "", "Checking access before opening the console..."
		return m, m.launchConsole(m.Connection, m.Console.Resource.UUID, m.Console.ConfigFingerprint, choice)
	}
	return m, nil
}
func (m Workspace) receiveConsolePrepared(v consolePrepared) (tea.Model, tea.Cmd) {
	if m.Pending["console-launch"] != v.Token {
		if v.Session != nil {
			_ = v.Session.Close()
		}
		return m, nil
	}
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "console-launch")
	m.Notice = ""
	if v.Err != nil {
		m.Busy = false
		m.Error = validation.SafeText(v.Err.Error())
		return m, nil
	}
	if v.Session == nil || v.Session.Command == nil {
		m.Busy = false
		m.Error = "Console launcher returned no session."
		return m, nil
	}
	if v.Session.Graphical {
		// The display opens in its own window, so Virmill stays usable.
		session, token := v.Session, v.Token
		m.Pending["console-launch"] = token
		return m, func() tea.Msg {
			done, err := session.StartDetached()
			return displayOpened{token, done, err}
		}
	}
	return m, tea.ExecProcess(v.Session.Command, func(err error) tea.Msg {
		cleanup := v.Session.Close()
		if err == nil {
			err = cleanup
		}
		return consoleClosed{err}
	})
}
func (m Workspace) receiveDisplayOpened(v displayOpened) (tea.Model, tea.Cmd) {
	if m.Pending["console-launch"] != v.Token {
		return m, waitDisplay(v.Done)
	}
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "console-launch")
	m.Busy = false
	if v.Err != nil {
		m.Notice, m.Error = "", validation.SafeText(v.Err.Error())
		return m, nil
	}
	m.Console, m.Error = nil, ""
	m.Notice = "The display opened in its own window. Closing it does not stop the VM."
	return m, waitDisplay(v.Done)
}

func waitDisplay(done <-chan error) tea.Cmd {
	if done == nil {
		return nil
	}
	return func() tea.Msg { return displayClosed{<-done} }
}

func (m Workspace) consoleView(width, height int) []string {
	if m.ConsoleLoading {
		return []string{"Checking guest access...", "Esc goes back."}
	}
	if m.Console == nil {
		return nil
	}
	info := m.Console
	lines := []string{"Open console · " + validation.SafeText(info.Name), "Use the guest display for installation, or its configured serial port.", ""}
	count := max(1, height-11)
	start := max(0, m.ConsoleIndex-count+1)
	for i := start; i < min(len(info.Choices), start+count); i++ {
		c := info.Choices[i]
		mark := "  "
		if i == m.ConsoleIndex {
			mark = "> "
		}
		suffix := ""
		if !c.Available {
			suffix = " (unavailable)"
		}
		lines = append(lines, mark+"[ "+validation.SafeText(c.Label)+" ]"+suffix)
		if i == m.ConsoleIndex && c.Reason != "" {
			lines = append(lines, "  "+validation.SafeText(c.Reason))
		}
	}
	if len(info.Choices) == 0 {
		lines = append(lines, "No supported console is configured for this VM.")
	}
	lines = append(lines, "", "Graphical: opens on this host's desktop; a plain SSH terminal has no display.", "Serial: Ctrl+] returns here. A serial port may have no guest login.", "Clipboard, audio, USB redirection and resize stay off.", "Closing the console does not stop the VM.")
	if len(info.Warnings) > 0 {
		lines = append(lines, "", strings.Join(info.Warnings, " "))
	}
	return pageLines(lines, width, height, 0)
}
