package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// Fixed responses at the UI transport seam test the real registered forms,
// model and confirmation flow. Native execution is covered separately.
type coldTUIClient struct {
	methods  []string
	requests []app.Request
	response app.Response
	err      error
}

func (c *coldTUIClient) Call(_ context.Context, method string, r app.Request) (app.Response, error) {
	c.methods = append(c.methods, method)
	c.requests = append(c.requests, r)
	return c.response, c.err
}

func coldTUIPlan(t *testing.T, action string) domain.Plan {
	t.Helper()
	p := domain.Plan{APIVersion: domain.APIVersion, ID: "860223f8-fbb4-4be7-a053-71a49818e768", CreatedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), ActorUID: 1000, ConnectionID: "qemu:///session", Operation: "snapshot." + action,
		ResourceIDs: []string{"libvirt|qemu:///session|vm|12345678-1234-1234-1234-123456789abc"}, Acknowledgements: []string{"offline-source-read", "private-recovery-state"},
		Review: map[string]any{"snapshotID": "a349c6aa-42fa-4931-99ab-091c315a7c6e", "catalog": "/private/recovery sets", "guestBootVerified": false, "sourceMutation": "none"}}
	if action == "restore" {
		p.Acknowledgements = []string{"all-network-interfaces-disconnected", "new-restored-identity"}
		p.Review = map[string]any{"snapshotID": "a349c6aa-42fa-4931-99ab-091c315a7c6e", "newName": "Restored ضيف", "newVMID": "12345678-1234-1234-1234-123456789abc", "disconnectAllNICs": true, "guestBootVerified": false}
	}
	var err error
	p.Digest, err = operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func coldTUISelect(t *testing.T, m Model, command string) Model {
	t.Helper()
	for section, name := range sections {
		if name != "Protection" {
			continue
		}
		m.Section = section
		for index, action := range m.actions() {
			if action.Command == command {
				m.Selected = index
				return m
			}
		}
	}
	t.Fatal("cold recovery action is not reachable", command)
	return m
}

func coldTUIOpen(t *testing.T, m Model, command string) Model {
	t.Helper()
	m = coldTUISelect(t, m, command)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || !m.Editing || m.Busy {
		t.Fatal("opening a form executed work")
	}
	return m
}

func coldTUIType(t *testing.T, m Model, input string) Model {
	t.Helper()
	for _, r := range input {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			key = tea.KeyMsg{Type: tea.KeySpace}
		}
		m = terminalStep(t, m, key)
	}
	if m.Input != input {
		t.Fatal("physical form input was changed", m.Input, input)
	}
	return m
}

func coldTUISubmit(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.Busy {
		t.Fatal("valid recovery form was inaccessible", m.View())
	}
	next, follow := m.Update(cmd())
	if follow != nil {
		t.Fatal("response auto-executed work")
	}
	return next.(Model)
}

func TestColdRecoveryTUIExactFormsPreviewAndApproval(t *testing.T) {
	for _, action := range []string{"create", "restore"} {
		t.Run(action, func(t *testing.T) {
			p := coldTUIPlan(t, action)
			c := &coldTUIClient{response: app.Response{APIVersion: domain.APIVersion, Data: p, Warnings: []string{"fixture: no native execution"}}}
			original, _ := json.Marshal(c.response)
			input := `{"id":"12345678-1234-1234-1234-123456789abc","input":{"sourceRoot":"/private/source images","auxiliaryRootID":"uefi-tpm"}}`
			if action == "restore" {
				input = `{"id":"12345678-1234-1234-1234-123456789abc","input":{"name":"Restored ضيف","poolID":"a349c6aa-42fa-4931-99ab-091c315a7c6e"}}`
			}
			m := coldTUIOpen(t, New(c, "qemu:///session"), "snapshot "+action)
			m = coldTUIType(t, m, input)
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.Editing || m.Busy || len(c.requests) != 0 {
				t.Fatal("canceling input executed work")
			}
			m = coldTUIOpen(t, m, "snapshot "+action)
			m = coldTUIType(t, m, input)
			m = coldTUISubmit(t, m)
			var form struct {
				ID    string         `json:"id"`
				Input map[string]any `json:"input"`
			}
			_ = json.Unmarshal([]byte(input), &form)
			if len(c.requests) != 1 || c.methods[0] != "snapshot."+action {
				t.Fatal("unexpected dispatch", c.methods)
			}
			q := c.requests[0]
			if q.ID != form.ID || q.Path != "" || q.Action != action || q.Apply != nil || q.Connection != "qemu:///session" || !reflect.DeepEqual(q.Input, form.Input) {
				t.Fatal("form lost required fields or acquired authority", q)
			}
			want, _ := json.MarshalIndent(c.response, "", "  ")
			if m.Output != string(want) || m.Plan == nil || m.Plan.Digest != p.Digest || !reflect.DeepEqual(m.Plan.Review, p.Review) {
				t.Fatal("preview changed service result", m.Output)
			}
			// Critical review and exact acknowledgement text remain reachable at
			// 80x24 and after resize. Paging and resizing cannot submit a request.
			for _, size := range [][2]int{{80, 24}, {48, 12}, {120, 40}} {
				m = terminalStep(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.Offset = 0
				pages := ""
				for i := 0; m.Offset <= len(wrap(m.Output, m.Width)); i++ {
					if i > 150 {
						t.Fatal("unbounded review")
					}
					pages += terminalBounds(t, m)
					m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
				}
				for _, text := range append([]string{p.ID, p.Digest, "guestBootVerified"}, p.Acknowledgements...) {
					if !strings.Contains(strings.ReplaceAll(pages, "\n", ""), text) {
						t.Fatal("review field inaccessible", size, text)
					}
				}
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			if !m.Confirm {
				t.Fatal("approval not reachable")
			}
			m = coldTUIType(t, m, "wrong-digest")
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if !m.Confirm || len(c.requests) != 1 {
				t.Fatal("wrong digest authorized capture")
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.Confirm {
				t.Fatal("approval cannot cancel")
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			m = coldTUIType(t, m, p.Digest)
			c.response = app.Response{APIVersion: domain.APIVersion, Error: domain.Fail("STALE_PLAN", "source changed; no restore submitted"), Warnings: []string{}}
			m = coldTUISubmit(t, m)
			if !reflect.DeepEqual(c.methods, []string{"snapshot." + action, "operation.apply"}) {
				t.Fatal("hidden execution", c.methods)
			}
			q = c.requests[1]
			if q.Apply == nil || q.Apply.PlanID != p.ID || q.Apply.PlanDigest != p.Digest || q.Apply.IdempotencyKey == "" || !reflect.DeepEqual(q.Apply.Acknowledgements, p.Acknowledgements) || q.ID != "" || len(q.Input) != 0 {
				t.Fatal("immutable approval changed", q)
			}
			if m.Plan != nil || m.Confirm || m.Busy || !strings.Contains(m.Output, "STALE_PLAN") || !strings.Contains(m.Output, "source changed") {
				t.Fatal("apply error retained authority or hid cause", m.Output)
			}
			still, _ := json.Marshal(app.Response{APIVersion: domain.APIVersion, Data: p, Warnings: []string{"fixture: no native execution"}})
			if !bytes.Equal(original, still) {
				t.Fatal("UI mutated original service plan")
			}
		})
	}
}

func TestColdRecoveryTUIReadOnlyMappingsAndErrorClearsApproval(t *testing.T) {
	for _, action := range []string{"list", "show"} {
		t.Run(action, func(t *testing.T) {
			c := &coldTUIClient{response: app.Response{APIVersion: domain.APIVersion, Data: map[string]any{"guestBootVerified": false}}}
			m := coldTUISelect(t, New(c, "qemu:///session"), "snapshot "+action)
			id := ""
			if action == "show" {
				id = "a349c6aa-42fa-4931-99ab-091c315a7c6e"
				m = coldTUIOpen(t, m, "snapshot show")
				m = coldTUIType(t, m, id)
			}
			m = coldTUISubmit(t, m)
			if len(c.requests) != 1 || c.methods[0] != "snapshot."+action || c.requests[0].ID != id || c.requests[0].Action != "" || c.requests[0].Apply != nil || m.Plan != nil {
				t.Fatal("read-only mapping changed", c.requests)
			}
		})
	}
	for _, fault := range []string{"canceled", "transport", "incomplete"} {
		t.Run(fault, func(t *testing.T) {
			p := coldTUIPlan(t, "create")
			c := &coldTUIClient{}
			want := "context canceled"
			c.err = context.Canceled
			if fault == "transport" {
				want = "fixture disconnected"
				c.err = errors.New(want)
			}
			if fault == "incomplete" {
				want = "INCOMPLETE_BACKUP"
				c.err = nil
				c.response = app.Response{APIVersion: domain.APIVersion, Error: domain.Fail(want, "required member missing")}
			}
			m := New(c, "qemu:///session")
			m.Plan = &p
			m.Output = "prior " + p.Digest
			m = coldTUIOpen(t, m, "snapshot show")
			m = coldTUIType(t, m, "a349c6aa-42fa-4931-99ab-091c315a7c6e")
			m = coldTUISubmit(t, m)
			if !strings.Contains(m.Output, want) || strings.Contains(m.Output, p.Digest) || m.Plan != nil || m.Confirm || m.Busy {
				t.Fatal("failure retained approval or hid error", m.Output)
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			if m.Confirm || len(c.requests) != 1 {
				t.Fatal("failure left apply accessible")
			}
		})
	}
}

func TestColdRecoveryTUIMalformedFormsNeverDispatch(t *testing.T) {
	for _, action := range []string{"create", "restore"} {
		for _, input := range []string{`not-json`, `{"id":"one","id":"two","input":{}}`, `{"id":"one","input":{"name":"first","name":"second"}}`, `{"id":"one","apply":{"planID":"hidden"}}`} {
			t.Run(action+"/"+input, func(t *testing.T) {
				c := &coldTUIClient{}
				m := coldTUIOpen(t, New(c, "qemu:///session"), "snapshot "+action)
				m = coldTUIType(t, m, input)
				m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
				if !m.Editing || m.Busy || len(c.requests) != 0 || !strings.Contains(m.formError, "Enter JSON") {
					t.Fatal("malformed form dispatched or hid error", m.View())
				}
			})
		}
	}
}
