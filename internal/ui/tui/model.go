package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

var sections = []string{"Overview", "VMs", "Networks", "Storage", "Templates", "Labs", "Protection", "Devices", "Jobs", "Plugins", "Settings"}

const maxSearchRunes = 128

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
	Search     string
	Searching  bool
	SearchNote string
	selection  map[string]string
}

func New(c ui.Client, connection string) Model {
	return Model{Client: c, Connection: connection, Width: 80, Height: 24, Output: "Development build. Hardware qualification and mandatory workflows remain incomplete. Press Tab to navigate; ? shows help."}
}
func (m Model) Init() tea.Cmd { return nil }
func (m Model) actions() []ui.Action {
	out := []ui.Action{}
	if m.Section < 0 || m.Section >= len(sections) {
		return out
	}
	terms := strings.Fields(searchText(m.Search))
	for _, a := range ui.Actions {
		if a.Section == sections[m.Section] {
			text := searchText(a.Command + " " + a.Summary)
			matches := true
			for _, term := range terms {
				if !strings.Contains(text, term) {
					matches = false
					break
				}
			}
			if matches {
				out = append(out, a)
			}
		}
	}
	return out
}

func searchText(text string) string {
	return strings.Map(func(r rune) rune {
		folded := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			folded = min(folded, next)
		}
		return folded
	}, text)
}

func (m *Model) clampSelection() {
	if m.Section < 0 || m.Section >= len(sections) {
		m.Section = 0
	}
	m.Selected = max(0, min(m.Selected, len(m.actions())-1))
}

func (m *Model) rememberSelection() {
	actions := m.actions()
	if m.Selected >= 0 && m.Selected < len(actions) {
		m.selection = maps.Clone(m.selection)
		if m.selection == nil {
			m.selection = make(map[string]string)
		}
		m.selection[sections[m.Section]] = actions[m.Selected].Command
	}
}

func (m *Model) restoreSelection() {
	m.Selected = 0
	for i, action := range m.actions() {
		if action.Command == m.selection[sections[m.Section]] {
			m.Selected = i
			return
		}
	}
}

func (m *Model) changeSection(delta int) {
	if m.Search == "" || m.selection[sections[m.Section]] == "" {
		m.rememberSelection()
	}
	m.Section = (m.Section + len(sections) + delta) % len(sections)
	m.restoreSelection()
}

func (m *Model) clearSearch() {
	m.Search, m.SearchNote = "", ""
	m.Searching = false
	m.restoreSelection()
}

func (m *Model) searchKey(v tea.KeyMsg) {
	if v.Type == tea.KeyRunes || v.Type == tea.KeySpace {
		incoming := v.Runes
		if v.Type == tea.KeySpace {
			incoming = []rune{' '}
		}
		if len([]rune(m.Search))+len(incoming) > maxSearchRunes {
			m.SearchNote = "Search limit: 128 characters; new text was not added."
			return
		}
		text := string(incoming)
		if validation.SafeText(text) != text || strings.ContainsAny(text, "\r\n\t") {
			m.SearchNote = "Search accepts printable text; new text was not added."
			return
		}
		for _, r := range incoming {
			if unicode.IsControl(r) {
				m.SearchNote = "Search accepts printable text; new text was not added."
				return
			}
		}
		m.Search += text
		m.SearchNote = ""
		m.restoreSelection()
		return
	}
	switch v.String() {
	case "esc":
		m.clearSearch()
	case "enter":
		m.rememberSelection()
		m.Searching = false
		m.SearchNote = ""
	case "backspace":
		runes := []rune(m.Search)
		if len(runes) > 0 {
			m.Search = string(runes[:len(runes)-1])
		}
		m.SearchNote = ""
		m.restoreSelection()
	case "tab":
		m.changeSection(1)
	case "shift+tab":
		m.changeSection(-1)
	case "up":
		m.Selected = max(0, m.Selected-1)
		m.rememberSelection()
	case "down":
		m.Selected = max(0, min(m.Selected+1, len(m.actions())-1))
		m.rememberSelection()
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.clampSelection()
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = v.Width
		m.Height = v.Height
	case resultMsg:
		m.Busy = false
		m.Plan = nil
		m.Confirm = false
		m.Offset = 0
		if v.err != nil {
			m.Output = validation.SafeText(v.err.Error())
			break
		}
		b, _ := json.MarshalIndent(v.response, "", "  ")
		m.Output = validation.SafeText(string(b))
		if v.response.Error == nil {
			raw, _ := json.Marshal(v.response.Data)
			var p domain.Plan
			if json.Unmarshal(raw, &p) == nil && p.ID != "" && p.Digest != "" {
				m.Plan = &p
			}
		}
	case tea.KeyMsg:
		key := v.String()
		if v.Type == tea.KeyCtrlC {
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
				if a.Argument == "parameters" || a.Mutation == "set" || a.Mutation == "autostart" || a.Method == "storage.access.grant" || a.Method == "vm.recovery.auxiliary.inspect" || a.Method == "backup.verify-manifest" || a.Method == "backup.policy.preview" || a.Method == "vm.create" || a.Method == "vm.creation.cleanup" || a.Method == "vm.creation.accept" || (a.Mutation != "" && (strings.HasPrefix(a.Command, "plugin ") || strings.HasPrefix(a.Command, "import "))) {
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
		if m.Searching {
			m.searchKey(v)
			return m, nil
		}
		switch key {
		case "/":
			m.rememberSelection()
			m.Searching = true
			m.SearchNote = ""
		case "q":
			m.Quit = true
			return m, tea.Quit
		case "?":
			m.Help = !m.Help
		case "tab":
			m.changeSection(1)
		case "shift+tab":
			m.changeSection(-1)
		case "up", "k":
			if m.Selected > 0 {
				m.Selected--
				m.rememberSelection()
			}
		case "down", "j":
			if m.Selected+1 < len(m.actions()) {
				m.Selected++
				m.rememberSelection()
			}
		case "pgdown":
			m.Offset += min(10, max(1, m.outputRows()))
		case "pgup":
			m.Offset = max(0, m.Offset-min(10, max(1, m.outputRows())))
		case "esc":
			if m.Search != "" {
				m.clearSearch()
				break
			}
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
func (m Model) menuLines() []string {
	width, height := max(1, m.Width), max(1, m.Height)
	clip := func(text string) string {
		tail := "…"
		if width == 1 {
			tail = ""
		}
		return ansi.Truncate(validation.SafeText(text), width, tail)
	}
	lines := []string{
		clip(fmt.Sprintf("Virmill | %s | %s", m.Connection, sections[m.Section])),
		clip("Tab: section  /: search  Enter: action  ?: help  PgUp/PgDn: details  q: detach"),
	}
	if m.Searching || m.Search != "" {
		prefix := "Filter: "
		if m.Searching {
			prefix = "Search> "
			lines[1] = clip("Type to filter; Up/Down: select  Enter: leave search  Esc: clear  Tab: section")
		}
		query := validation.SafeText(m.Search)
		if query == "" {
			query = "(all actions)"
		}
		room := max(1, width-ansi.StringWidth(prefix))
		if cells := ansi.StringWidth(query); cells > room {
			query = "…" + ansi.Cut(query, cells-room+1, cells)
		}
		lines = append(lines, clip(prefix+query))
	}
	if m.SearchNote != "" {
		lines = append(lines, clip(m.SearchNote))
	}
	if m.Help {
		lines = append(lines, clip("Up/Down selects. / searches this section. Plans: a reviews digest approval. Jobs continue after detaching."))
	}
	var tail []string
	if m.Busy {
		tail = append(tail, clip("Request in progress; UI remains available."))
	}
	if m.Editing {
		input := wrap(validation.SafeText(m.Input), max(1, width-2))
		if len(input) > 3 {
			input = append([]string{"… earlier input hidden"}, input[len(input)-2:]...)
		}
		tail = append(tail, clip("Input: path/ID or JSON {id/path,input}; Esc cancels:"))
		for i, line := range input {
			prefix := "  "
			if i == 0 {
				prefix = "> "
			}
			tail = append(tail, clip(prefix+line))
		}
	}
	if m.Confirm && m.Plan != nil {
		text := fmt.Sprintf("Approve plan %s. Required acknowledgements: %s\nType the full plan digest; Esc cancels:\n%s\n> %s",
			m.Plan.ID, strings.Join(m.Plan.Acknowledgements, ", "), m.Plan.Digest, m.Input)
		tail = append(tail, wrap(validation.SafeText(text), width)...)
	}
	if m.Plan != nil && !m.Confirm {
		tail = append(tail, clip("Plan is a preview. Press a to review authorization."))
	}
	actions := m.actions()
	// Reserve room for state and result details. Even a compact menu keeps the
	// selected action visible; scrolling never changes the selected identity.
	menuRows := max(1, min(8, height/3, height-len(lines)-len(tail)-3))
	first := max(0, m.Selected-menuRows+1)
	last := min(len(actions), first+menuRows)
	if len(actions) == 0 {
		message := "This section has no completed workflow yet; 1.0 release remains blocked."
		if strings.TrimSpace(m.Search) != "" {
			message = "No actions match the search. Esc clears; Tab changes section."
		}
		lines = append(lines, clip(message))
	}
	for i := first; i < last; i++ {
		prefix := "  "
		if i == m.Selected {
			prefix = "> "
		}
		lines = append(lines, clip(prefix+actions[i].Command+" — "+actions[i].Summary))
	}
	if len(actions) > menuRows && len(lines)+len(tail) < height-1 {
		lines = append(lines, clip(fmt.Sprintf("Actions %d–%d of %d; Up/Down scrolls", first+1, last, len(actions))))
	}
	lines = append(lines, tail...)
	return lines[:min(len(lines), height)]
}

func (m Model) outputRows() int {
	return max(0, m.Height-len(m.menuLines())-1)
}

func (m Model) View() string {
	if m.Quit || m.Width <= 0 || m.Height <= 0 {
		return ""
	}
	m.clampSelection()
	if m.Width < 12 || m.Height < 4 {
		lines := []string{"Terminal too small; resize.", "Selection and input are retained."}
		lines = lines[:min(len(lines), m.Height)]
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], m.Width, "")
		}
		return strings.Join(lines, "\n") + "\n"
	}
	prefix := m.menuLines()
	lines := wrap(validation.SafeText(m.Output), m.Width)
	start := max(0, min(m.Offset, len(lines)))
	end := min(start+m.outputRows(), len(lines))
	return strings.Join(append(prefix, lines[start:end]...), "\n") + "\n"
}

func wrap(s string, width int) []string {
	width = max(1, width)
	lines := strings.Split(ansi.Hardwrap(s, width, true), "\n")
	for i := range lines {
		// A single grapheme can be wider than an extremely narrow terminal.
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return lines
}
func Run(c ui.Client, connection string) error {
	_, e := tea.NewProgram(New(c, connection), tea.WithAltScreen()).Run()
	return e
}
