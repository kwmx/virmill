package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/ui"
)

func actionFixture(t *testing.T, command string) ActionForm {
	t.Helper()
	for _, a := range ui.Actions {
		if a.Command == command {
			f, err := NewActionForm(a, "")
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
	}
	t.Fatal("action absent", command)
	return ActionForm{}
}

func actionFill(t *testing.T, f ActionForm, values map[string]string) ActionForm {
	t.Helper()
	f.Form = guidedFill(t, f.Form, values)
	return f
}

func actionParameterFixture(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("safe parameter-file reader is Linux-only")
	}
	path := filepath.Join(t.TempDir(), "input parameters.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// This inventory test follows the current registry instead of maintaining a
// second list. It verifies request routing, not each service's input schema.
func TestActionFormEveryRegisteredActionMapsExactRequest(t *testing.T) {
	for _, action := range ui.Actions {
		t.Run(action.Command, func(t *testing.T) {
			f, err := NewActionForm(action, "selected-action-specific-ID")
			if err != nil {
				t.Fatal("registered action lacks a visual form", err)
			}
			want := app.Request{Connection: "qemu:///session", Action: action.Mutation, Input: map[string]any{}}
			fields := map[string]string{}
			if action.Argument == "id" {
				want.ID = "selected-action-specific-ID"
			}
			if action.Argument == "path" || action.Method == "guest.recipe.run" {
				want.Path = "/private/catalog/selected document.json"
				fields["path"] = want.Path
			}
			if actionParameterFields(action) {
				body := `{"fixture":"literal $(touch never)","items":[1,"ضيف"],"id":"an-input-ID"}`
				fields["parametersFile"] = actionParameterFixture(t, body)
				if err := json.Unmarshal([]byte(body), &want.Input); err != nil {
					t.Fatal(err)
				}
			}
			if action.Method == "plugin.call" {
				fields["pluginAction"], want.Input["action"] = "describe", "describe"
			}
			if action.Method == "operation.watch" {
				fields["after"], want.After = "42", 42
			}
			f = actionFill(t, f, fields)
			method, request, err := f.Request("qemu:///session")
			if err != nil || method != action.Method || !reflect.DeepEqual(request, want) {
				t.Fatal("catalog changed ID/path/input/mutation or acquired apply authority", method, request, want, err)
			}
			f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if !submit || cancel || f.Form.Error != "" {
				t.Fatal("registered action cannot submit its form", f.Form.Error)
			}
			if !strings.HasPrefix(f.View(100, 20), f.Title()+"\n") || f.Title() == "" {
				t.Fatal("friendly action title missing", f.View(100, 20))
			}
		})
	}
}

func TestActionFormPrimaryIDNeverPrefillsPathOrParameters(t *testing.T) {
	for _, command := range []string{"plugin install", "guest recipe run", "network cidr check", "host inspect"} {
		f := actionFixture(t, command)
		f, err := NewActionForm(f.Action, "caller-scoped-ID")
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range f.Form.Fields {
			want := ""
			if field.Name == "id" {
				want = "caller-scoped-ID"
			}
			if field.Value != want {
				t.Fatal("selected ID escaped its primary identity field", command, field)
			}
		}
	}
}

func TestActionFormOptionalParametersAndExplicitCompoundArguments(t *testing.T) {
	for _, tc := range []struct {
		command string
		fields  map[string]string
		input   map[string]any
	}{
		{"network cidr check", map[string]string{}, map[string]any{}},
		{"vm autostart", map[string]string{"id": "vm-ID"}, map[string]any{}},
		{"plugin call", map[string]string{"id": "plugin-ID", "pluginAction": "summary"}, map[string]any{"action": "summary"}},
		{"guest recipe run", map[string]string{"id": "vm-ID", "path": "/private/recipe.json"}, map[string]any{}},
	} {
		f := actionFill(t, actionFixture(t, tc.command), tc.fields)
		_, r, err := f.Request("qemu:///system")
		if err != nil || !reflect.DeepEqual(r.Input, tc.input) || r.Apply != nil {
			t.Fatal("optional parameter input invented values or lost compound argument", tc.command, r, err)
		}
	}
}

func TestActionFormBadParametersRefusedWithoutSuccessfulRequest(t *testing.T) {
	for _, body := range []string{"", "null", "[]", "\"not an object\"", "{} {}", `{"token":"private-output-never-echoed","token":"duplicate"}`, `{"nested":{"a":1,"a":2}}`, `{"broken":`, strings.Repeat(" ", actionParametersLimit+1)} {
		file := actionParameterFixture(t, body)
		f := actionFill(t, actionFixture(t, "network cidr check"), map[string]string{"parametersFile": file})
		method, r, err := f.Request("qemu:///system")
		if err == nil || method != "" || !reflect.DeepEqual(r, app.Request{}) {
			t.Fatal("bad parameter file exposed a request", len(body), method, r, err)
		}
		f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if submit || cancel || f.Form.Error == "" || strings.Contains(f.Form.Error, "private-output-never-echoed") || f.Form.Fields[f.Form.Focus].Name != "parametersFile" {
			t.Fatal("file error hidden, payload echoed, or form submitted", f.Form.Error)
		}
	}
	f := actionFill(t, actionFixture(t, "plugin call"), map[string]string{"id": "plugin-ID", "pluginAction": "selected-action", "parametersFile": actionParameterFixture(t, `{"action":"other-action"}`)})
	if _, r, err := f.Request("qemu:///system"); err == nil || !reflect.DeepEqual(r, app.Request{}) {
		t.Fatal("duplicate plugin action silently won", r, err)
	}
}

func TestActionFormMissingBadFieldsAndCancel(t *testing.T) {
	for _, tc := range []struct {
		command string
		fields  map[string]string
		focus   string
	}{
		{"vm show", map[string]string{}, "id"},
		{"vm show", map[string]string{"id": " padded "}, "id"},
		{"network create", map[string]string{}, "path"},
		{"network create", map[string]string{"path": "relative.yaml"}, "path"},
		{"network create", map[string]string{"path": "/private/../network.yaml"}, "path"},
		{"guest recipe run", map[string]string{"id": "vm-ID"}, "path"},
		{"plugin call", map[string]string{"id": "plugin-ID"}, "pluginAction"},
		{"operation watch", map[string]string{"id": "job-ID", "after": "-1"}, "after"},
		{"operation watch", map[string]string{"id": "job-ID", "after": "01"}, "after"},
		{"operation watch", map[string]string{"id": "job-ID", "after": "9223372036854775808"}, "after"},
		{"network cidr check", map[string]string{"parametersFile": "/missing/explicit-file.json"}, "parametersFile"},
		{"network cidr check", map[string]string{"parametersFile": "relative.json"}, "parametersFile"},
	} {
		t.Run(tc.command+"/"+tc.focus+"/"+tc.fields[tc.focus], func(t *testing.T) {
			f := actionFill(t, actionFixture(t, tc.command), tc.fields)
			// Esc must work even when a chosen parameter file doesn't exist; it
			// neither reads the document nor invokes validation/service work.
			canceled, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if submit || !cancel || canceled.Form.Error != "" || !reflect.DeepEqual(canceled.Form.Fields, f.Form.Fields) {
				t.Fatal("cancel changed input or performed validation")
			}
			f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if submit || cancel || f.Form.Error == "" || f.Form.Fields[f.Form.Focus].Name != tc.focus {
				t.Fatal("invalid field submitted or lacked inline focus", f.Form.Error, f.Form.Focus)
			}
		})
	}
}

func TestActionFormFocusLiteralEditingAndDisplayBounds(t *testing.T) {
	f := actionFixture(t, "guest recipe run")
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("exact-ID")})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyTab})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/private/ضيف.json")})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyDown})
	if f.Form.Focus != 2 {
		t.Fatal("parameters field not reachable")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyUp})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if f.Form.Focus != 0 || f.Form.Fields[1].Value != "/private/ضيف.json" {
		t.Fatal("navigation changed literal path")
	}
	for _, size := range [][2]int{{80, 17}, {48, 10}, {24, 6}, {8, 3}, {0, 0}} {
		view := f.View(size[0], size[1])
		if !utf8.ValidString(view) || strings.Contains(view, "\x1b") || view != "" && len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("invalid catalog form display", size, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("catalog form line overflows", size, line)
			}
		}
	}
	for _, command := range []string{"operation cancel", "operation reconcile", "vm start"} {
		f := actionFixture(t, command)
		view := f.View(80, 17)
		if f.Action.Mutation != "" && !strings.Contains(view, "Enter preview") || f.Action.Mutation == "" && strings.Contains(view, "Enter preview") {
			t.Fatal("catalog mislabeled plan behavior", command, view)
		}
	}
}

func TestActionFormRejectsInventedMethodsAndCorruptForm(t *testing.T) {
	if _, err := NewActionForm(ui.Action{Command: "apply now", Method: "operation.apply", Argument: "id"}, "plan-ID"); err == nil {
		t.Fatal("unregistered apply action accepted")
	}
	f := actionFixture(t, "vm show")
	f.Form.Fields[0].Value = "id\n\x1b[31m"
	if method, r, err := f.Request("qemu:///system"); err == nil || method != "" || !reflect.DeepEqual(r, app.Request{}) {
		t.Fatal("unsafe direct field value accepted")
	}
	f = actionFill(t, actionFixture(t, "vm show"), map[string]string{"id": "exact-ID"})
	f.Action.Method = "operation.apply"
	if _, r, err := f.Request("qemu:///system"); err == nil || !reflect.DeepEqual(r, app.Request{}) {
		t.Fatal("method substitution accepted")
	}
}
