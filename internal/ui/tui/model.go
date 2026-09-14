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
	Client       ui.Client
	Connection   string
	Section      int
	Selected     int
	Width        int
	Height       int
	Offset       int
	Input        string
	Editing      bool
	Busy         bool
	Help         bool
	Output       string
	Plan         *domain.Plan
	Confirm      bool
	Quit         bool
	Search       string
	Searching    bool
	SearchNote   string
	selection    map[string]string
	dialogReview bool
	dialogOffset int
	formError    string
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

func (m *Model) formFailure(message string) {
	m.Output, m.formError = message, message
	m.dialogOffset = 0
	m.dialogReview = false
}

func (m Model) detailText() string {
	if !m.Confirm || m.Plan == nil {
		return m.Output
	}
	review, err := json.MarshalIndent(m.Plan, "", "  ")
	if err != nil {
		return "Plan review could not be rendered. Esc cancels this dialog."
	}
	text := fmt.Sprintf("Approve plan %s\nFull plan digest:\n%s\nRequired acknowledgements:\n%s\nAffected resources and complete plan:\n%s",
		m.Plan.ID, m.Plan.Digest, strings.Join(m.Plan.Acknowledgements, "\n"), review)
	if m.formError != "" {
		text = m.formError + "\n" + text
	}
	return text
}

func (m *Model) scrollDialog(delta int) {
	rows := max(1, m.outputRows())
	last := max(0, len(wrap(validation.SafeText(m.detailText()), max(1, m.Width)))-rows)
	m.dialogOffset = max(0, min(m.dialogOffset+delta, last))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.clampSelection()
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = v.Width
		m.Height = v.Height
		rows := max(1, m.outputRows())
		m.Offset = max(0, min(m.Offset, len(wrap(validation.SafeText(m.Output), max(1, m.Width)))-rows))
		m.scrollDialog(0)
	case resultMsg:
		m.Busy = false
		m.Plan = nil
		m.Confirm = false
		m.Offset = 0
		m.dialogOffset = 0
		m.formError = ""
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
			// KeyRunes can contain an entire IME input or unbracketed paste.
			// Text such as "enter" must never become an Enter control event.
			if v.Type == tea.KeyRunes || v.Type == tea.KeySpace {
				if !m.dialogReview {
					incoming := string(v.Runes)
					if v.Type == tea.KeySpace {
						incoming = " "
					}
					if len(m.Input)+len(incoming) <= 128<<10 {
						m.Input += incoming
					} else {
						m.formFailure("Input exceeds the 128 KiB form limit; the new text was not added.")
					}
				}
				return m, nil
			}
			switch key {
			case "esc":
				m.Editing = false
				m.Confirm = false
				m.Input = ""
				m.dialogReview, m.dialogOffset, m.formError = false, 0, ""
			case "tab", "shift+tab":
				m.dialogReview = !m.dialogReview
			case "pgdown":
				m.scrollDialog(min(10, max(1, m.outputRows())))
			case "pgup":
				m.scrollDialog(-min(10, max(1, m.outputRows())))
			case "down":
				if m.dialogReview {
					m.scrollDialog(1)
				}
			case "up":
				if m.dialogReview {
					m.scrollDialog(-1)
				}
			case "backspace":
				if m.dialogReview {
					break
				}
				r := []rune(m.Input)
				if len(r) > 0 {
					m.Input = string(r[:len(r)-1])
				}
			case "enter":
				if m.dialogReview {
					m.dialogReview = false
					break
				}
				if m.Confirm {
					if m.Plan == nil || m.Input != m.Plan.Digest {
						m.formFailure("Plan digest did not match. No operation submitted.")
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
				if a.Method == "guest.recipe.run" || a.Argument == "parameters" || a.Mutation == "set" || a.Mutation == "autostart" || a.Method == "storage.access.grant" || a.Method == "snapshot.create" || a.Method == "snapshot.restore" || a.Method == "vm.recovery.auxiliary.inspect" || a.Method == "backup.verify-manifest" || a.Method == "backup.policy.preview" || a.Method == "vm.create" || a.Method == "vm.creation.cleanup" || a.Method == "vm.creation.accept" || (a.Mutation != "" && (strings.HasPrefix(a.Command, "plugin ") || strings.HasPrefix(a.Command, "import ") || strings.HasPrefix(a.Command, "backup "))) {
					var form struct {
						ID    string         `json:"id"`
						Path  string         `json:"path"`
						Input map[string]any `json:"input"`
					}
					if e := wire.Decode([]byte(m.Input), &form); e != nil {
						m.formFailure("Enter JSON with id or path, plus input parameters. See the selected command's generated reference.")
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
			}
			return m, nil
		}
		if m.Searching {
			m.searchKey(v)
			return m, nil
		}
		if v.Type == tea.KeyRunes && (v.Paste || len(v.Runes) != 1) {
			// Only explicit single-character shortcuts belong to navigation.
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
				m.dialogReview, m.dialogOffset, m.formError = false, 0, ""
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
				m.dialogReview, m.dialogOffset, m.formError = false, 0, ""
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
	dialog := m.Editing || m.Confirm
	compactDialog := dialog && height < 10
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
	if dialog {
		lines[1] = clip("Focus: input | Tab: details  Enter: submit  Esc: cancel  PgUp/PgDn: review")
		if m.dialogReview {
			lines[1] = clip("Focus: details | Tab/Enter: input  Up/Down/PgUp/PgDn: scroll  Esc: cancel")
		}
		if compactDialog {
			lines = lines[:1]
		}
	}
	if (m.Searching || m.Search != "") && !compactDialog {
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
	if m.SearchNote != "" && !dialog {
		lines = append(lines, clip(m.SearchNote))
	}
	if m.Help && !dialog {
		lines = append(lines, clip("Up/Down selects. / searches this section. Plans: a reviews digest approval. Jobs continue after detaching."))
	}
	var tail []string
	if m.Busy {
		tail = append(tail, clip("Request in progress; UI remains available."))
	}
	if dialog {
		input := wrap(validation.SafeText(m.Input), max(1, width-2))
		inputRows := 3
		if height < 12 {
			inputRows = 1
		}
		if len(input) > inputRows {
			if inputRows == 1 {
				input = input[len(input)-1:]
			} else {
				input = append([]string{"… earlier input hidden"}, input[len(input)-inputRows+1:]...)
			}
		}
		prompt := "Input: path/ID or JSON {id/path,input}; Esc cancels:"
		if m.Confirm {
			prompt = "Type the full plan digest; PgUp/PgDn reviews; Esc cancels:"
		}
		if compactDialog {
			prompt = "Focus: input; Esc cancels"
			if m.dialogReview {
				prompt = "Focus: details; Tab: input"
			}
		}
		tail = append(tail, clip(prompt))
		for i, line := range input {
			prefix := "  "
			if i == 0 && !m.dialogReview {
				prefix = "> "
			}
			tail = append(tail, clip(prefix+line))
		}
		if m.formError != "" && !compactDialog {
			tail = append(tail, clip(m.formError))
		}
	}
	if m.Plan != nil && !dialog {
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
	if m.Width < 12 || m.Height < 4 || ((m.Editing || m.Confirm) && m.Height < 6) {
		lines := []string{"Terminal too small; resize.", "Selection and input are retained."}
		lines = lines[:min(len(lines), m.Height)]
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], m.Width, "")
		}
		return strings.Join(lines, "\n") + "\n"
	}
	prefix := m.menuLines()
	lines := wrap(validation.SafeText(m.detailText()), m.Width)
	offset := m.Offset
	if m.Editing || m.Confirm {
		offset = m.dialogOffset
	}
	start := max(0, min(offset, len(lines)))
	end := min(start+m.outputRows(), len(lines))
	return strings.Join(append(prefix, lines[start:end]...), "\n") + "\n"
}

func wrap(s string, width int) []string {
	width = max(1, width)
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		plain := ansi.Strip(line)
		body := strings.TrimLeft(plain, " ")
		firstWord, _, _ := strings.Cut(body, " ")
		// Keep explicit layout in tables, preformatted output and JSON/XML.
		// Ordinary prose may be indented, but its leading spaces must not be
		// discarded when the first word is wider than the remaining row.
		structured := strings.ContainsAny(plain, "\t\r") ||
			strings.Contains(body, "  ") || strings.HasSuffix(plain, " ") ||
			strings.HasPrefix(body, "{") || strings.HasPrefix(body, "}") ||
			strings.HasPrefix(body, "[") || strings.HasPrefix(body, "]") ||
			strings.HasPrefix(body, "\"") || strings.HasPrefix(body, "<") ||
			(len(plain) != len(body) && len(plain)-len(body)+ansi.StringWidth(firstWord) > width)
		if structured {
			lines = append(lines, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
		} else {
			// Wordwrap first, then split only overlong tokens. The pinned
			// combined Wrap helper can split an ASCII base from its combining
			// accent when the base fills the row.
			lines = append(lines, strings.Split(ansi.Hardwrap(ansi.Wordwrap(line, width, ""), width, true), "\n")...)
		}
	}
	// Do not truncate: a grapheme wider than a one-cell viewport must remain
	// intact. Workspace renders an ASCII resize message at such tiny sizes.
	return lines
}
func Run(c ui.Client, connection string) error {
	return RunOptions(c, connection, false)
}

func RunOptions(c ui.Client, connection string, noColor bool) error {
	m := NewWorkspace(c, connection)
	m.NoColor = m.NoColor || noColor
	m.updateCheck, m.updatesOff = updateChecker()
	store, storeErr := NewDraftStore()
	if storeErr == nil {
		defer store.Close()
		m.AttachDraftStore(store)
	} else {
		m.Error = "Draft saving is unavailable: " + validation.SafeText(storeErr.Error())
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if w, ok := final.(Workspace); ok {
		if w.ImportCancel != nil {
			w.ImportCancel()
		}
		if saveErr := w.flushSetup(); saveErr != nil && err == nil {
			err = fmt.Errorf("TUI closed, but setup could not be saved: %w", saveErr)
		}
	}
	return err
}
