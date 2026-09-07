package tui

import (
	"context"
	"encoding/json"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

var sections = []string{"Overview", "VMs", "Networks", "Storage", "Templates", "Labs", "Protection", "Devices", "Jobs", "Plugins", "Settings"}

type resultMsg struct {
	response app.Response
	err      error
}
type Model struct {
	Client     ui.Client
	Connection string
	Section    int
	Selected   int
	Width      int
	Height     int
	Offset     int
	Input      string
	Editing    bool
	Busy       bool
	Help       bool
	Output     string
	Plan       *domain.Plan
	Confirm    bool
	Quit       bool
}

func New(c ui.Client, connection string) Model {
	return Model{Client: c, Connection: connection, Width: 80, Height: 24, Output: "Development build. Hardware qualification and mandatory workflows remain incomplete. Press Tab to navigate; ? shows help."}
}
func (m Model) Init() tea.Cmd { return nil }
func (m Model) actions() []ui.Action {
	out := []ui.Action{}
	for _, a := range ui.Actions {
		if a.Section == sections[m.Section] {
			out = append(out, a)
		}
	}
	return out
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = v.Width
		m.Height = v.Height
	case resultMsg:
		m.Busy = false
		if v.err != nil {
			m.Output = validation.SafeText(v.err.Error())
			break
		}
		b, _ := json.MarshalIndent(v.response, "", "  ")
		m.Output = validation.SafeText(string(b))
		m.Offset = 0
		m.Plan = nil
		if v.response.Error == nil {
			raw, _ := json.Marshal(v.response.Data)
			var p domain.Plan
			if json.Unmarshal(raw, &p) == nil && p.ID != "" && p.Digest != "" {
				m.Plan = &p
			}
		}
	case tea.KeyMsg:
		key := v.String()
		if key == "ctrl+c" {
			m.Quit = true
			return m, tea.Quit
		}
		if m.Editing || m.Confirm {
			switch key {
			case "esc":
				m.Editing = false
				m.Confirm = false
				m.Input = ""
			case "backspace":
				r := []rune(m.Input)
				if len(r) > 0 {
					m.Input = string(r[:len(r)-1])
				}
			case "enter":
				if m.Confirm {
					if m.Plan == nil || m.Input != m.Plan.Digest {
						m.Output = "Plan digest did not match. No operation submitted."
						break
					}
					p := m.Plan
					r := app.Request{Apply: &operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements}}
					m.Busy = true
					m.Confirm = false
					m.Input = ""
					return m, m.call("operation.apply", r)
				}
				actions := m.actions()
				if len(actions) == 0 {
					break
				}
				a := actions[m.Selected]
				r := app.Request{Connection: m.Connection, Action: a.Mutation}
				if a.Argument == "path" {
					r.Path = m.Input
				} else {
					r.ID = m.Input
				}
				if a.Argument == "parameters" || a.Mutation == "set" || a.Mutation == "autostart" || a.Method == "storage.access.grant" || a.Method == "vm.create" || a.Method == "vm.creation.cleanup" || a.Method == "vm.creation.accept" || (a.Mutation != "" && (strings.HasPrefix(a.Command, "plugin ") || strings.HasPrefix(a.Command, "import "))) {
					var form struct {
						ID    string         `json:"id"`
						Path  string         `json:"path"`
						Input map[string]any `json:"input"`
					}
					if e := wire.Decode([]byte(m.Input), &form); e != nil {
						m.Output = "Enter JSON with id or path, plus input parameters. See the selected command's generated reference."
						break
					}
					r.ID = form.ID
					r.Path = form.Path
					r.Input = form.Input
				}
				m.Editing = false
				m.Busy = true
				m.Input = ""
				return m, m.call(a.Method, r)
			default:
				if v.Type == tea.KeyRunes {
					incoming := string(v.Runes)
					if len(m.Input)+len(incoming) <= 128<<10 {
						m.Input += incoming
					} else {
						m.Output = "Input exceeds the 128 KiB form limit; the new text was not added."
					}
				}
			}
			return m, nil
		}
		switch key {
		case "q":
			m.Quit = true
			return m, tea.Quit
		case "?":
			m.Help = !m.Help
		case "tab":
			m.Section = (m.Section + 1) % len(sections)
			m.Selected = 0
		case "shift+tab":
			m.Section = (m.Section + len(sections) - 1) % len(sections)
			m.Selected = 0
		case "up", "k":
			if m.Selected > 0 {
				m.Selected--
			}
		case "down", "j":
			if m.Selected+1 < len(m.actions()) {
				m.Selected++
			}
		case "pgdown":
			m.Offset += 10
		case "pgup":
			m.Offset = max(0, m.Offset-10)
		case "esc":
			m.Help = false
			m.Plan = nil
			m.Offset = 0
		case "a":
			if m.Plan != nil && !m.Busy {
				m.Confirm = true
				m.Input = ""
			}
		case "enter":
			if m.Busy {
				break
			}
			a := m.actions()
			if len(a) == 0 {
				break
			}
			action := a[m.Selected]
			if action.Argument != "" {
				m.Editing = true
				m.Input = ""
			} else {
				m.Busy = true
				return m, m.call(action.Method, app.Request{Connection: m.Connection})
			}
		}
	}
	return m, nil
}
func (m Model) call(method string, r app.Request) tea.Cmd {
	return func() tea.Msg {
		var e error
		r, e = ui.NormalizeRequest(method, r)
		if e != nil {
			return resultMsg{err: e}
		}
		resp, e := m.Client.Call(context.Background(), method, r)
		return resultMsg{resp, e}
	}
}
func (m Model) View() string {
	if m.Quit {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Virmill | %s | %s\n", validation.SafeText(m.Connection), sections[m.Section])
	b.WriteString("Tab: section  Enter: action  ?: help  PgUp/PgDn: details  q: detach\n")
	if m.Help {
		b.WriteString("Choose an action with Up/Down. Inputs use stable VM UUIDs. Plans show exact effects and acknowledgements. Press a on a plan to review and confirm its digest. Jobs continue after leaving the interface. Use Jobs to cancel or reconcile.\n")
	}
	actions := m.actions()
	if len(actions) == 0 {
		b.WriteString("This section has no completed workflow yet; 1.0 release remains blocked.\n")
	}
	// Keep keyboard-selected actions visible without overflowing an 80x24 terminal.
	menuRows := max(3, min(8, m.Height/3))
	first := max(0, m.Selected-menuRows+1)
	last := min(len(actions), first+menuRows)
	for i := first; i < last; i++ {
		a := actions[i]
		prefix := "  "
		if i == m.Selected {
			prefix = "> "
		}
		line := prefix + a.Command + " — " + a.Summary
		if len([]rune(line)) > max(20, m.Width) {
			line = string([]rune(line)[:max(20, m.Width)-1]) + "…"
		}
		fmt.Fprintln(&b, line)
	}
	if len(actions) > menuRows {
		fmt.Fprintf(&b, "Actions %d–%d of %d; Up/Down scrolls\n", first+1, last, len(actions))
	}
	if m.Busy {
		b.WriteString("Request in progress; UI remains available.\n")
	}
	if m.Editing {
		lines := wrap(validation.SafeText(m.Input), max(20, m.Width-2))
		if len(lines) > 3 {
			lines = append([]string{"… earlier input hidden"}, lines[len(lines)-2:]...)
		}
		b.WriteString("Input: path/ID, or JSON {id/path,input} for parameter forms (CIDR checks use {input}). Esc cancels:\n> " + strings.Join(lines, "\n  ") + "\n")
	}
	if m.Confirm {
		fmt.Fprintf(&b, "Approve plan %s. Required acknowledgements: %s\nType the full plan digest to authorize these exact effects; Esc cancels:\n%s\n> %s\n", m.Plan.ID, strings.Join(m.Plan.Acknowledgements, ", "), m.Plan.Digest, validation.SafeText(m.Input))
	}
	if m.Plan != nil && !m.Confirm {
		b.WriteString("Plan is a preview. Press a to review authorization.\n")
	}
	available := max(3, m.Height-strings.Count(b.String(), "\n")-1)
	lines := wrap(m.Output, max(20, m.Width))
	start := min(m.Offset, len(lines))
	end := min(start+available, len(lines))
	b.WriteString(strings.Join(lines[start:end], "\n"))
	return b.String() + "\n"
}
func wrap(s string, width int) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		r := []rune(line)
		for len(r) > width {
			out = append(out, string(r[:width]))
			r = r[width:]
		}
		out = append(out, string(r))
	}
	return out
}
func Run(c ui.Client, connection string) error {
	_, e := tea.NewProgram(New(c, connection), tea.WithAltScreen()).Run()
	return e
}
