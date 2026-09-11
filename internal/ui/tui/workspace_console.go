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
		m.sequence++
		token := m.sequence
		m.Pending = maps.Clone(m.Pending)
		m.Pending["console-launch"] = token
		m.Busy, m.Error, m.Notice = true, "", "Checking access before opening the console..."
		client, connection, id, fingerprint := m.Client, m.Connection, m.Console.Resource.UUID, m.Console.ConfigFingerprint
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			session, err := ui.PrepareConsole(ctx, client, connection, id, choice.ID, fingerprint)
			return consolePrepared{token, session, err}
		}
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
	return m, tea.ExecProcess(v.Session.Command, func(err error) tea.Msg {
		cleanup := v.Session.Close()
		if err == nil {
			err = cleanup
		}
		return consoleClosed{err}
	})
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
