package tui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

const recipeTUIVM = "12345678-1234-1234-1234-123456789abc"
const recipeTUIForm = `{"id":"12345678-1234-1234-1234-123456789abc","path":"/private/recipes/owner setup.json","input":{"address":"192.0.2.25","port":2222,"user":"operator","identityFile":"/private/keys/owner key","knownHostsFile":"/private/keys/known hosts","arguments":["literal $(touch never)","two words","ضيف","--not-a-host-flag"]}}`

// Only the guest extension is substituted. The registry, keyboard model and
// shared dispatcher are real; no transport or native readiness is exercised.
func recipeTUIService(fn func(context.Context, uint32, app.Request) (any, error)) *recoveryTUIServiceClient {
	return &recoveryTUIServiceClient{service: app.Service{Extensions: map[string]func(context.Context, uint32, app.Request) (any, error){
		"guest.recipe.run": fn, "guest.recipe.result": fn,
	}}}
}

func recipeTUIOpen(t *testing.T, m Model, action string) Model {
	t.Helper()
	found := false
	for section, name := range sections {
		if name != "VMs" {
			continue
		}
		m.Section = section
		for index, item := range m.actions() {
			if item.Command == "guest recipe "+action {
				m.Selected, found = index, true
				break
			}
		}
	}
	if !found {
		t.Fatal("guest recipe action unreachable", action)
	}
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Editing || m.Busy {
		t.Fatal("opening guest recipe form submitted work")
	}
	return m
}

func TestGuestRecipeTUIExactFormReviewedPlanAndCancel(t *testing.T) {
	p := coldTUIPlan(t, "create")
	p.Operation = "guest.recipe.run"
	p.InputDigest = strings.Repeat("a", 64)
	p.Acknowledgements = []string{"guest-execution", "guest-host-key-binding", "non-root-guest-setup", "non-idempotent-recipe"}
	p.Review = map[string]any{"recipeSHA256": strings.Repeat("b", 64), "privilege": "non-root", "reboot": "never", "nativeAddressBindingVerified": false, "rawOutputRetained": false}
	var err error
	p.Digest, err = operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range []string{"qemu:///system", "qemu:///session"} {
		t.Run(connection, func(t *testing.T) {
			var received app.Request
			calls := 0
			c := recipeTUIService(func(_ context.Context, uid uint32, r app.Request) (any, error) {
				calls++
				received = r
				if uid != 1000 {
					t.Fatal("actor changed", uid)
				}
				return p, nil
			})
			m := recipeTUIOpen(t, New(c, connection), "run")
			m = coldTUIType(t, m, recipeTUIForm)
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.Editing || m.Busy || c.calls != 0 || calls != 0 {
				t.Fatal("form cancellation called guest service")
			}
			m = recipeTUIOpen(t, m, "run")
			m = coldTUIType(t, m, recipeTUIForm)
			m = coldTUISubmit(t, m)
			var form struct {
				ID    string         `json:"id"`
				Path  string         `json:"path"`
				Input map[string]any `json:"input"`
			}
			if err := json.Unmarshal([]byte(recipeTUIForm), &form); err != nil {
				t.Fatal(err)
			}
			wantRequest := app.Request{Connection: connection, ID: form.ID, Path: form.Path, Action: "run", Input: form.Input}
			if c.calls != 1 || calls != 1 || c.method != "guest.recipe.run" || !reflect.DeepEqual(received, wantRequest) || !reflect.DeepEqual(c.request, wantRequest) {
				t.Fatal("VM/path/arguments changed or implicit apply authority appeared", received, c.request)
			}
			want, _ := json.MarshalIndent(app.Response{APIVersion: domain.APIVersion, Data: p, Warnings: []string{}}, "", "  ")
			if m.Output != string(want) || m.Plan == nil || m.Plan.Digest != p.Digest || !reflect.DeepEqual(m.Plan.Acknowledgements, p.Acknowledgements) || m.Busy || m.Confirm {
				t.Fatal("preview changed shared plan or granted approval", m.Output)
			}
			// Paging the content hashes and explicit non-root/host-key review is
			// navigation only, including at a narrower window size.
			for _, size := range [][2]int{{80, 24}, {48, 12}} {
				m = terminalStep(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.Offset = 0
				pages := ""
				for i := 0; m.Offset <= len(wrap(m.Output, m.Width)); i++ {
					if i > 100 {
						t.Fatal("unbounded guest recipe review")
					}
					// Join only newly displayed detail rows; repeating menu rows
					// between pages must not split a wrapped digest in this check.
					visible := strings.Split(strings.TrimSuffix(terminalBounds(t, m), "\n"), "\n")[len(m.menuLines()):]
					rows := min(10, m.outputRows(), len(visible))
					pages += strings.Join(visible[:rows], "\n") + "\n"
					m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
				}
				for _, text := range append([]string{p.ID, p.Digest, p.InputDigest, "recipeSHA256", "nativeAddressBindingVerified", "rawOutputRetained"}, p.Acknowledgements...) {
					if !strings.Contains(strings.ReplaceAll(pages, "\n", ""), text) {
						t.Fatal("guest recipe review field inaccessible", size, text)
					}
				}
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			if !m.Confirm || m.Plan.Digest != p.Digest {
				t.Fatal("exact reviewed plan not available for separate approval")
			}
			m = coldTUIType(t, m, "wrong-digest")
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if !m.Confirm || c.calls != 1 {
				t.Fatal("wrong digest authorized guest execution")
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.Confirm || m.Busy || c.calls != 1 || calls != 1 {
				t.Fatal("approval cancellation or navigation executed guest work")
			}
		})
	}
}

func TestGuestRecipeTUIMalformedFormNeverDispatches(t *testing.T) {
	for _, input := range []string{
		`not-json`, `[]`, `{} {}`, `{"id":"a","id":"b"}`, `{"id":"a","path":"/private/recipe.json","input":[]}`, `{"id":"a","path":"/private/recipe.json","apply":true}`,
	} {
		t.Run(input, func(t *testing.T) {
			c := &coldTUIClient{}
			m := recipeTUIOpen(t, New(c, "qemu:///session"), "run")
			m = coldTUIType(t, m, input)
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(Model)
			if cmd != nil || len(c.requests) != 0 || !m.Editing || m.Busy || m.Confirm || m.Plan != nil || !strings.Contains(m.Output, "Enter JSON") {
				t.Fatal("malformed form reached service or hid corrective guidance", m.View())
			}
			m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.Editing || len(c.requests) != 0 {
				t.Fatal("malformed form could not be canceled")
			}
		})
	}
}

func TestGuestRecipeTUIResultReadOnlyAndUnknownEffectVisible(t *testing.T) {
	operationID := "860223f8-fbb4-4be7-a053-71a49818e768"
	result := map[string]any{"operationID": operationID, "state": "recovery-required", "complete": false, "rawOutputRetained": false, "nativeAddressBindingVerified": false,
		"stages": []any{map[string]any{"stage": "apply", "complete": false, "effectUnknown": true, "receipt": nil}}}
	var received app.Request
	c := recipeTUIService(func(_ context.Context, _ uint32, r app.Request) (any, error) { received = r; return result, nil })
	m := recipeTUIOpen(t, New(c, "qemu:///session"), "result")
	m = coldTUIType(t, m, operationID)
	m = coldTUISubmit(t, m)
	wantRequest := app.Request{Connection: "qemu:///session", ID: operationID}
	if c.calls != 1 || c.method != "guest.recipe.result" || !reflect.DeepEqual(received, wantRequest) || !reflect.DeepEqual(c.request, wantRequest) {
		t.Fatal("result changed operation identity or acquired recipe authority", received, c.request)
	}
	want, _ := json.MarshalIndent(app.Response{APIVersion: domain.APIVersion, Data: result, Warnings: []string{}}, "", "  ")
	if m.Output != string(want) || m.Plan != nil || m.Confirm || m.Busy {
		t.Fatal("result hid uncertain effect or gained approval", m.Output)
	}
	m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.Confirm || c.calls != 1 {
		t.Fatal("read-only result authorized guest work")
	}
}

func TestGuestRecipeTUIFailuresClearSuccessfulPreviewAndAuthority(t *testing.T) {
	for _, action := range []string{"run", "result"} {
		for _, failure := range []string{"SOURCE_CHANGED", "PERMISSION_DENIED", "INVALID_INPUT", "RECOVERY_REQUIRED", "canceled", "transport"} {
			t.Run(action+"/"+failure, func(t *testing.T) {
				c := recipeTUIService(func(ctx context.Context, _ uint32, _ app.Request) (any, error) {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					return nil, domain.Fail(failure, "guest recipe fixture refused")
				})
				want := failure
				if failure == "canceled" {
					c.cancelCall = true
					want = "OPERATION_FAILED"
				}
				if failure == "transport" {
					want = "guest recipe transport unavailable"
					c.transportError = errors.New(want)
				}
				m := New(c, "qemu:///session")
				p := coldTUIPlan(t, "create")
				m.Plan, m.Output = &p, "old preview "+p.Digest
				m = recipeTUIOpen(t, m, action)
				input := recipeTUIVM
				if action == "run" {
					input = recipeTUIForm
				}
				m = coldTUIType(t, m, input)
				m = coldTUISubmit(t, m)
				if m.Plan != nil || m.Confirm || m.Busy || strings.Contains(m.Output, p.Digest) || !strings.Contains(m.Output, want) || c.calls != 1 || c.method != "guest.recipe."+action || c.request.Apply != nil {
					t.Fatal("failure hid refusal or retained preview/authority", m.Output, c.request)
				}
				if failure != "transport" {
					var r app.Response
					if err := json.Unmarshal([]byte(m.Output), &r); err != nil || r.Error == nil || r.Error.Code != want || r.Data != nil {
						t.Fatal("failure displayed successful data", m.Output, err)
					}
				}
				m = terminalStep(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
				if m.Confirm || c.calls != 1 {
					t.Fatal("failure enabled a hidden retry or guest mutation")
				}
			})
		}
	}
}
