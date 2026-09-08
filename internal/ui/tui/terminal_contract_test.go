package tui

import (
	"context"
	"errors"
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

type terminalClient struct {
	methods  []string
	requests []app.Request
}

func (c *terminalClient) Call(_ context.Context, method string, request app.Request) (app.Response, error) {
	c.methods = append(c.methods, method)
	c.requests = append(c.requests, request)
	return app.Response{APIVersion: domain.APIVersion, Data: map[string]string{"result": "terminal fixture"}}, nil
}

func terminalFixture(t *testing.T) (Model, *terminalClient) {
	t.Helper()
	previous := ui.Actions
	t.Cleanup(func() { ui.Actions = previous })
	ui.Actions = []ui.Action{
		{Command: "plugin test", Method: "plugin.test", Section: "Plugins", Argument: "path", Summary: "Run reviewed conformance workspace"},
		{Command: "fixture parameters", Method: "fixture.parameters", Section: "Plugins", Argument: "parameters", Summary: "Review parameter input"},
		{Command: "fixture read", Method: "fixture.read", Section: "Plugins", Summary: "Read fixture"},
	}
	c := &terminalClient{}
	m := New(c, "qemu:///system")
	m.Section = 9
	return m, c
}

func terminalStep(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, command := m.Update(msg)
	if command != nil {
		t.Fatal("navigation or text edit unexpectedly returned a command")
	}
	return next.(Model)
}

func terminalType(t *testing.T, m Model, text string) Model {
	t.Helper()
	return terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func terminalBounds(t *testing.T, m Model) string {
	t.Helper()
	view := m.View()
	if !utf8.ValidString(view) || strings.Contains(view, "\x1b") || strings.Count(view, "\n") > max(0, m.Height) {
		t.Fatalf("invalid or vertically overflowing terminal view at %dx%d: %q", m.Width, m.Height, view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > max(0, m.Width) {
			t.Fatalf("terminal line exceeds %d cells: %q", m.Width, line)
		}
	}
	return view
}

func TestTerminalPhysicalSpaceAndShortcutWordsRemainLiteralFormInput(t *testing.T) {
	for _, action := range []int{0, 1} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			m, client := terminalFixture(t)
			m.Selected = action
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			for _, word := range []string{"/source", "enter", "esc", "tab", "backspace", "界面"} {
				m = terminalType(t, m, word)
				m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeySpace})
			}
			want := "/source enter esc tab backspace 界面 "
			if !m.Editing || m.Input != want || len(client.methods) != 0 || m.Quit {
				t.Fatalf("literal text became a control or lost spaces: input=%q editing=%v calls=%v", m.Input, m.Editing, client.methods)
			}
			terminalBounds(t, m)
		})
	}
}

func TestTerminalPathWithPhysicalSpacesReachesOnlySelectedService(t *testing.T) {
	m, client := terminalFixture(t)
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = terminalType(t, m, "/fixture/provider")
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeySpace})
	m = terminalType(t, m, "workspace")
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !next.(Model).Busy {
		t.Fatal("explicit form submission did not return its request command")
	}
	terminalStep(t, next.(Model), command())
	if !reflect.DeepEqual(client.methods, []string{"plugin.test"}) || client.requests[0].Path != "/fixture/provider workspace" || client.requests[0].Apply != nil {
		t.Fatal("physical spaces changed the submitted path or introduced another operation", client)
	}
}

func terminalPlan() *domain.Plan {
	p := &domain.Plan{ID: "terminal-reviewed-plan", Digest: strings.Repeat("a", 64), Operation: "fixture.plan"}
	for i := 0; i < 35; i++ {
		p.Acknowledgements = append(p.Acknowledgements, fmt.Sprintf("ACK_%02d_preserve_the_named_resource_before_apply", i))
		p.ResourceIDs = append(p.ResourceIDs, fmt.Sprintf("resource-%02d-exact-identity", i))
	}
	return p
}

func TestTerminalLongApprovalRetainsVisibleInputAndPageableExactReview(t *testing.T) {
	m, client := terminalFixture(t)
	m.Plan = terminalPlan()
	plan := m.Plan
	m = terminalType(t, m, "a")
	m = terminalType(t, m, "typed-prefix")
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 40, Height: 10}, {Width: 20, Height: 6}, {Width: 2, Height: 2}, {Width: 120, Height: 40}, {Width: 80, Height: 24}} {
		m = terminalStep(t, m, size)
		view := terminalBounds(t, m)
		if size.Height >= 6 && !strings.Contains(view, "typed-prefix") {
			t.Fatalf("approval input disappeared at %+v: %q", size, view)
		}
		if !m.Confirm || m.Input != "typed-prefix" || m.Plan != plan || len(client.methods) != 0 {
			t.Fatal("resize lost approval input, binding or focus")
		}
	}
	seen := ""
	for i := 0; i < 100; i++ {
		seen += terminalBounds(t, m)
		m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	for _, text := range append(append([]string{plan.ID, plan.Digest}, plan.Acknowledgements...), plan.ResourceIDs...) {
		if !strings.Contains(seen, text) {
			t.Fatalf("exact approval detail cannot be reached: %q", text)
		}
	}
	if m.Plan != plan || !m.Confirm || m.Input != "typed-prefix" || len(client.methods) != 0 {
		t.Fatal("review paging submitted or changed authorization")
	}
}

func TestTerminalResizeRevealsDetailsAfterPriorNarrowScroll(t *testing.T) {
	m, _ := terminalFixture(t)
	m.Output = strings.Repeat("wrapped fixture output ", 100) + "FINAL_RESOURCE"
	m = terminalStep(t, m, tea.WindowSizeMsg{Width: 20, Height: 8})
	for i := 0; i < 60; i++ {
		m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	m = terminalStep(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if !strings.Contains(terminalBounds(t, m), "FINAL_RESOURCE") {
		t.Fatal("resize kept an out-of-range details offset and blanked the result")
	}
}

func TestTerminalDialogFocusCannotRetargetSubmitOrLoseCancellation(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		t.Run(fmt.Sprint(confirm), func(t *testing.T) {
			m, client := terminalFixture(t)
			if confirm {
				m.Plan = terminalPlan()
				m = terminalType(t, m, "a")
			} else {
				m.Selected = 1
				m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			}
			m = terminalType(t, m, "retained form text")
			section, selected, plan := m.Section, m.Selected, m.Plan
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyTab})
			if !strings.Contains(terminalBounds(t, m), "Focus: details") {
				t.Fatal("Tab did not expose dialog details focus")
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyDown})
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if m.Section != section || m.Selected != selected || m.Input != "retained form text" || m.Plan != plan || len(client.methods) != 0 {
				t.Fatal("dialog review navigation retargeted or submitted the form")
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.Editing || m.Confirm || m.Input != "" || m.Plan != plan || len(client.methods) != 0 {
				t.Fatal("dialog cancellation submitted, discarded the preview, or retained input")
			}
		})
	}
}

func TestTerminalApprovalMismatchRemainsReadableAfterReviewPaging(t *testing.T) {
	m, client := terminalFixture(t)
	m.Plan = terminalPlan()
	m = terminalType(t, m, "a")
	for i := 0; i < 10; i++ {
		m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	m = terminalType(t, m, "wrong-digest")
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(terminalBounds(t, m), "did not match") || !m.Confirm || len(client.methods) != 0 {
		t.Fatal("invalid authorization was hidden, submitted or dismissed", m.View())
	}
}

func TestTerminalBackendErrorsPreserveNavigationAndHaveTextMeaning(t *testing.T) {
	for _, result := range []resultMsg{{err: context.Canceled}, {err: errors.New("connection lost\x1b[2J; retry requires review")},
		{response: app.Response{APIVersion: domain.APIVersion, Error: &domain.Error{Code: "PLUGIN_FAILED", Message: "fixture plugin stopped"}}}} {
		m, client := terminalFixture(t)
		m.Search, m.Searching, m.Busy = "fixture", true, true
		m.Selected = 1
		m.Plan = terminalPlan()
		m = terminalStep(t, m, result)
		if m.Busy || m.Plan != nil || !m.Searching || m.Search != "fixture" || m.Selected != 1 || len(client.methods) != 0 {
			t.Fatal("backend error lost focus/navigation or retained actionable authorization")
		}
		view := terminalBounds(t, m)
		if result.err != nil && !strings.Contains(view, strings.Split(result.err.Error(), "\x1b")[0]) {
			t.Fatal("transport/cancellation error has no visible text meaning", view)
		}
		if result.response.Error != nil && !strings.Contains(view, result.response.Error.Code) {
			t.Fatal("domain error code is not visible as text", view)
		}
		m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		if len(client.methods) != 0 {
			t.Fatal("leaving failure initiated a service call")
		}
	}
}

func TestTerminalTopLevelTextDoesNotMasqueradeAsControl(t *testing.T) {
	m, client := terminalFixture(t)
	m.Selected = 2
	for _, text := range []string{"enter", "tab", "esc", "down", "ctrl+c"} {
		m = terminalType(t, m, text)
	}
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}, Paste: true})
	if m.Section != 9 || m.Selected != 2 || m.Editing || m.Searching || m.Busy || m.Quit || len(client.methods) != 0 {
		t.Fatal("text events became navigation or execution")
	}
}

func TestTerminalExactApprovalDigestAndAcknowledgementsUnchanged(t *testing.T) {
	m, client := terminalFixture(t)
	m.Plan = terminalPlan()
	plan := m.Plan
	m = terminalType(t, m, "a")
	m = terminalType(t, m, plan.Digest)
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeySpace})
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Confirm || !strings.Contains(m.Output, "did not match") || len(client.methods) != 0 {
		t.Fatal("physical trailing space was silently trimmed from authorization")
	}
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Confirm || len(client.methods) != 0 {
		t.Fatal("Enter in review focus executed approval")
	}
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !next.(Model).Busy || next.(Model).Confirm {
		t.Fatal("explicit exact approval failed to submit")
	}
	terminalStep(t, next.(Model), command())
	if !reflect.DeepEqual(client.methods, []string{"operation.apply"}) || client.requests[0].Apply == nil {
		t.Fatal("approval did not use the shared apply service", client)
	}
	got := client.requests[0].Apply
	if got.PlanID != plan.ID || got.PlanDigest != plan.Digest || got.IdempotencyKey == "" || !reflect.DeepEqual(got.Acknowledgements, plan.Acknowledgements) {
		t.Fatal("approval changed the reviewed binding or acknowledgement set", got)
	}
}

func TestTerminalFormsPreserveInputAcrossResizeAndExposeErrors(t *testing.T) {
	for _, selected := range []int{0, 1} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			m, client := terminalFixture(t)
			m.Selected = selected
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			input := strings.Repeat("界面 👩‍💻 /source ", 60) + "FINAL_INPUT"
			m = terminalType(t, m, input)
			for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 30, Height: 9}, {Width: 12, Height: 4}, {Width: 0, Height: 0}, {Width: 120, Height: 40}, {Width: 80, Height: 24}} {
				m = terminalStep(t, m, size)
				terminalBounds(t, m)
				if !m.Editing || m.Input != input || m.Selected != selected || len(client.methods) != 0 {
					t.Fatal("form resize lost input/selection or executed a request")
				}
			}
			if !strings.Contains(m.View(), "FINAL_INPUT") {
				t.Fatal("large form lost the visible input tail")
			}
			if selected == 1 {
				m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
				if !m.Editing || !strings.Contains(m.View(), "Enter JSON") || len(client.methods) != 0 {
					t.Fatal("malformed parameter form hid validation or dispatched")
				}
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.Editing || m.Input != "" || len(client.methods) != 0 {
				t.Fatal("canceling resized input submitted a service call")
			}
		})
	}
}

func TestTerminalBusyDetachDoesNotCancelOrResubmit(t *testing.T) {
	m, client := terminalFixture(t)
	m.Selected = 2
	next, pending := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if pending == nil {
		t.Fatal("fixture read did not start")
	}
	m = next.(Model)
	for _, message := range []tea.Msg{tea.WindowSizeMsg{Width: 40, Height: 10}, tea.KeyMsg{Type: tea.KeyTab},
		tea.KeyMsg{Type: tea.KeyEsc}, tea.KeyMsg{Type: tea.KeyEnter}} {
		m = terminalStep(t, m, message)
	}
	if !m.Busy || len(client.methods) != 0 || !strings.Contains(m.View(), "Request in progress") {
		t.Fatal("busy navigation submitted, canceled or hid operation state")
	}
	next, quit := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quit == nil || !next.(Model).Quit || !next.(Model).Busy || len(client.methods) != 0 {
		t.Fatal("detach canceled or resubmitted a request")
	}
	pending()
	if !reflect.DeepEqual(client.methods, []string{"fixture.read"}) {
		t.Fatal("detachment introduced a cancellation or replay", client.methods)
	}
}
