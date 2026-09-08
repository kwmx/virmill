package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
)

type searchClient struct {
	methods  []string
	requests []app.Request
}

func (c *searchClient) Call(_ context.Context, method string, request app.Request) (app.Response, error) {
	c.methods = append(c.methods, method)
	c.requests = append(c.requests, request)
	return app.Response{APIVersion: domain.APIVersion, Data: map[string]string{"result": "read fixture"}}, nil
}

func searchFixture(t *testing.T) (Model, *searchClient) {
	t.Helper()
	old := ui.Actions
	t.Cleanup(func() { ui.Actions = old })
	ui.Actions = []ui.Action{
		{Command: "host inspect", Method: "host.inspect", Section: "Overview", Summary: "Read host prerequisites"},
		{Command: "vm list", Method: "inventory.list", Section: "VMs", Summary: "Read all virtual machines"},
		{Command: "vm show", Method: "inventory.get", Section: "VMs", Summary: "Read one machine", Argument: "id"},
		{Command: "vm pause", Method: "vm.plan", Section: "VMs", Summary: "Plan pause", Argument: "id", Mutation: "pause"},
		{Command: "vm résumé", Method: "fixture.resume", Section: "VMs", Summary: "CAFÉ Σ 界面"},
		{Command: "network list", Method: "network.list", Section: "Networks", Summary: "Read networks"},
		{Command: "network show", Method: "network.get", Section: "Networks", Summary: "Read one network", Argument: "id"},
	}
	c := &searchClient{}
	m := New(c, "qemu:///session")
	m.Section = 1
	return m, c
}

func searchUpdate(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, command := m.Update(msg)
	if command != nil {
		t.Fatal("navigation unexpectedly returned a service or quit command")
	}
	return next.(Model)
}

func searchType(t *testing.T, m Model, text string) Model {
	t.Helper()
	return searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func searchSelected(m Model) string {
	actions := m.actions()
	if m.Selected < 0 || m.Selected >= len(actions) {
		return ""
	}
	return actions[m.Selected].Command
}

func TestSearchEnterSelectsWithoutExecutingAndNextEnterUsesRegistry(t *testing.T) {
	m, c := searchFixture(t)
	m = searchType(t, m, "/")
	if !m.Searching || m.Search != "" {
		t.Fatal("slash did not focus an empty search")
	}
	m = searchType(t, m, "READ")
	if len(m.actions()) != 2 || searchSelected(m) != "vm list" {
		t.Fatal("command/summary search did not filter the current section")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Searching || m.Search != "READ" || m.Busy || m.Editing || len(c.methods) != 0 {
		t.Fatal("search Enter executed or lost the filter")
	}
	if !strings.Contains(m.View(), "Filter: READ") || !strings.Contains(m.View(), "> vm list") {
		t.Fatal("retained filter or text selection is not visible", m.View())
	}
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !next.(Model).Busy {
		t.Fatal("ordinary action Enter did not dispatch")
	}
	m = searchUpdate(t, next.(Model), command())
	if !reflect.DeepEqual(c.methods, []string{"inventory.list"}) || c.requests[0].Connection != "qemu:///session" || c.requests[0].Apply != nil {
		t.Fatal("filtered action bypassed its registry service", c)
	}
}

func TestSearchPreservesSelectedIdentityWhileFilteringAndClearing(t *testing.T) {
	m, _ := searchFixture(t)
	m.Selected = 2
	m = searchType(t, m, "/")
	m = searchType(t, m, "show")
	if searchSelected(m) != "vm show" {
		t.Fatal("available filter result was not selected")
	}
	m = searchType(t, m, " nonexistent")
	if len(m.actions()) != 0 || !strings.Contains(m.View(), "No actions match the search") {
		t.Fatal("no-results state is not explicit")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Searching || m.Search != "" || searchSelected(m) != "vm pause" {
		t.Fatal("transient filtering lost the prior selected action")
	}
	m = searchType(t, m, "/")
	m = searchType(t, m, "read")
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if searchSelected(m) != "vm show" {
		t.Fatal("clearing lost the explicitly selected search result")
	}
}

func TestSearchKeepsSelectionAndFocusAcrossSections(t *testing.T) {
	m, _ := searchFixture(t)
	m.Selected = 1
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if searchSelected(m) != "network show" {
		t.Fatal("network selection failed")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if searchSelected(m) != "vm show" {
		t.Fatal("section change lost the VM selection")
	}
	m = searchType(t, m, "/")
	m = searchType(t, m, "show")
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.Searching || m.Search != "show" || searchSelected(m) != "network show" {
		t.Fatal("section change lost search focus/query or selected identity")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.Searching || len(m.actions()) != 0 || !strings.Contains(m.View(), "No actions match") {
		t.Fatal("empty filtered section lost search focus")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.Searching || searchSelected(m) != "network show" || m.Search != "show" {
		t.Fatal("retained filter failed outside search editing")
	}
}

func TestSearchUnicodeCaseFoldingBackspaceAndWordMatching(t *testing.T) {
	for _, query := range []string{"résumé", "RÉSUMÉ", "café", "ς", "界面", "VM café"} {
		t.Run(query, func(t *testing.T) {
			m, _ := searchFixture(t)
			m = searchType(t, m, "/")
			m = searchType(t, m, query)
			if len(m.actions()) != 1 || searchSelected(m) != "vm résumé" || m.Search != query {
				t.Fatal("Unicode/word matching changed or lost the query")
			}
		})
	}
	m, _ := searchFixture(t)
	m = searchType(t, m, "/")
	m = searchType(t, m, "界面")
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Search != "界" || !utf8.ValidString(m.Search) {
		t.Fatal("backspace split a Unicode code point")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Search != "" || !m.Searching || len(m.actions()) != 4 {
		t.Fatal("backspace did not restore the menu")
	}
	m = searchType(t, m, "cafe\u0301")
	if len(m.actions()) != 0 {
		t.Fatal("search unexpectedly introduced Unicode normalization")
	}
}

func TestSearchSpaceEventsAndShortcutTextAreInput(t *testing.T) {
	m, c := searchFixture(t)
	m.Plan = &domain.Plan{ID: "plan", Digest: "digest"}
	m = searchType(t, m, "/")
	m = searchType(t, m, "vm")
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeySpace})
	m = searchType(t, m, "show")
	if searchSelected(m) != "vm show" || m.Search != "vm show" {
		t.Fatal("a physical space was lost")
	}
	m = searchType(t, m, "qa/?enterctrl+c")
	if m.Quit || m.Confirm || m.Help || m.Editing || !m.Searching || len(c.methods) != 0 || m.Plan == nil {
		t.Fatal("search text became a shortcut or lost the plan")
	}
}

func TestSearchInputLimitsAndControlsRefuseWholePaste(t *testing.T) {
	m, _ := searchFixture(t)
	m.Plan = &domain.Plan{ID: "plan", Digest: "digest"}
	output := m.Output
	m = searchType(t, m, "/")
	m = searchType(t, m, strings.Repeat("界", maxSearchRunes))
	before := m.Search
	m = searchType(t, m, "extra")
	if m.Search != before || !strings.Contains(m.SearchNote, "limit") || m.Output != output || m.Plan == nil {
		t.Fatal("oversized search was partially appended or disturbed the result")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.SearchNote != "" {
		t.Fatal("successful edit did not clear the search notice")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = searchType(t, m, "/")
	m = searchType(t, m, "safe")
	for _, text := range []string{"line\nnext", "\x1b[2J", "\u202eevil", "\x00", "\t"} {
		before := m.Search
		m = searchType(t, m, text)
		if m.Search != before || !strings.Contains(m.SearchNote, "printable") || strings.Contains(m.View(), "\x1b") {
			t.Fatalf("unsafe query %q was not refused as one edit", text)
		}
	}
}

func TestSearchNoResultsCannotExecuteOrDiscardPlan(t *testing.T) {
	m, c := searchFixture(t)
	plan := &domain.Plan{ID: "plan", Digest: "digest"}
	m.Plan, m.Offset = plan, 7
	output := m.Output
	m = searchType(t, m, "/")
	m = searchType(t, m, "not an available action")
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Editing || m.Busy || m.Plan != plan || len(c.methods) != 0 {
		t.Fatal("no-results Enter changed operation state")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Plan != plan || m.Offset != 7 || m.Output != output || m.Search != "" {
		t.Fatal("clearing search acted as page cancellation")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Plan != nil || m.Offset != 0 {
		t.Fatal("ordinary page Escape behavior changed")
	}
}

func TestSearchDoesNotInterceptFormOrDigestEditing(t *testing.T) {
	m, c := searchFixture(t)
	m = searchType(t, m, "/")
	m = searchType(t, m, "show")
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = searchType(t, m, "/literal?qa")
	if !m.Editing || m.Searching || m.Search != "show" || m.Input != "/literal?qa" {
		t.Fatal("search consumed form characters")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Editing || m.Input != "" || m.Search != "show" {
		t.Fatal("form cancellation cleared the surrounding filter")
	}
	m.Plan = &domain.Plan{ID: "plan", Digest: "digest"}
	m = searchType(t, m, "a")
	m = searchType(t, m, "/")
	if !m.Confirm || m.Input != "/" || m.Searching {
		t.Fatal("search intercepted digest editing")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Confirm || !strings.Contains(m.Output, "did not match") || len(c.methods) != 0 {
		t.Fatal("an invalid digest reached apply")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Confirm || m.Input != "" || len(c.methods) != 0 {
		t.Fatal("approval cancellation executed a command")
	}
}

func TestSearchResultAndBusyStateKeepFocusWithoutReexecution(t *testing.T) {
	m, c := searchFixture(t)
	m.Busy = true
	m = searchType(t, m, "/")
	m = searchType(t, m, "show")
	m = searchUpdate(t, m, resultMsg{err: fmt.Errorf("fixture unavailable")})
	if !m.Searching || m.Search != "show" || searchSelected(m) != "vm show" || m.Busy || !strings.Contains(m.Output, "fixture unavailable") {
		t.Fatal("async failure lost surrounding search/focus")
	}
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(c.methods) != 0 || m.Editing {
		t.Fatal("leaving search replayed the failed request")
	}
}

func TestSearchNarrowResizeBoundsAndSelectedActionRemainVisible(t *testing.T) {
	m, c := searchFixture(t)
	for i := 0; i < 20; i++ {
		ui.Actions = append(ui.Actions, ui.Action{Command: fmt.Sprintf("vm fixture-%02d", i), Method: "fixture.read", Section: "VMs", Summary: "界面 👩‍💻 long description"})
	}
	m.Selected = len(m.actions()) - 1
	m = searchType(t, m, "/")
	m = searchType(t, m, "vm")
	selected := searchSelected(m)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 40, Height: 9}, {Width: 20, Height: 6}, {Width: 12, Height: 4}, {Width: 1, Height: 1}, {Width: 0, Height: 0}, {Width: 80, Height: 24}} {
		m = searchUpdate(t, m, size)
		view := m.View()
		if !utf8.ValidString(view) || strings.Count(view, "\n") > size.Height {
			t.Fatalf("view overflow at %+v: %q", size, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("wide text overflow at %+v: %q", size, line)
			}
		}
		if size.Width >= 12 && size.Height >= 4 && !strings.Contains(view, "> vm") {
			t.Fatalf("selected action is outside resized menu at %+v: %q", size, view)
		}
		if !m.Searching || m.Search != "vm" || searchSelected(m) != selected || len(c.methods) != 0 {
			t.Fatal("resize changed search, selection, focus or operation state")
		}
	}
	m.Search = strings.Repeat("界", 128)
	m = searchUpdate(t, m, tea.WindowSizeMsg{Width: 20, Height: 8})
	if !strings.Contains(m.View(), "Search> …") {
		t.Fatal("long Unicode query lacks an explicit truncated-tail indicator", m.View())
	}
}

func TestSearchCompactResultPagingHasNoGaps(t *testing.T) {
	m, _ := searchFixture(t)
	m = searchType(t, m, "/")
	m = searchType(t, m, "pause")
	m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = searchUpdate(t, m, tea.WindowSizeMsg{Width: 24, Height: 8})
	var output []string
	for i := 0; i < 30; i++ {
		output = append(output, fmt.Sprintf("row-%02d 界", i))
	}
	m.Output = strings.Join(output, "\n")
	seen := map[string]bool{}
	for m.Offset < len(output) {
		for _, line := range strings.Split(m.View(), "\n") {
			seen[line] = true
		}
		before := m.Offset
		m = searchUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
		if m.Offset <= before || m.Offset-before > m.outputRows() {
			t.Fatal("compact paging skipped more than a visible page")
		}
	}
	for _, line := range output {
		if !seen[line] {
			t.Fatalf("result line became inaccessible after resize: %q", line)
		}
	}
}

func TestSearchCtrlCOnlyDetaches(t *testing.T) {
	m, c := searchFixture(t)
	m.Busy = true
	m = searchType(t, m, "/")
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if command == nil || !next.(Model).Quit || !next.(Model).Busy || len(c.methods) != 0 {
		t.Fatal("Ctrl-C did not detach independently of the job")
	}
}
