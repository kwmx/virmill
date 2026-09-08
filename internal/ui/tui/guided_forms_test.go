package tui

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func guidedFixture(t *testing.T, kind string) GuidedForm {
	t.Helper()
	vm := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: "12345678-1234-1234-1234-123456789abc"}, Name: "Owner guest ضيف", State: "stopped"}
	f, err := NewGuidedForm(kind, vm)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func guidedFill(t *testing.T, f GuidedForm, values map[string]string) GuidedForm {
	t.Helper()
	for name, value := range values {
		found := false
		for index, field := range f.Fields {
			if field.Name != name {
				continue
			}
			found = true
			f.Focus = index
			f.Fields[index].Value, f.Fields[index].Cursor = "", 0
			var submit, cancel bool
			f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
			if submit || cancel || f.Fields[index].Value != value || f.Error != "" {
				t.Fatal("editing changed input or submitted work", name, f.Fields[index].Value, f.Error)
			}
			break
		}
		if !found {
			t.Fatal("field missing", name)
		}
	}
	return f
}

func guidedGuestValues() map[string]string {
	return map[string]string{"recipe": "/private/recipes/owner setup.json", "address": "192.0.2.25", "user": "operator", "identityFile": "/private/keys/owner key", "knownHostsFile": "/private/keys/known hosts"}
}

func TestGuidedFormsExactPreviewRequests(t *testing.T) {
	for _, tc := range []struct {
		name, kind, method, path, action string
		values                           map[string]string
		input                            map[string]any
		vm                               bool
	}{
		{"resources-both", "resources", "vm.plan", "", "set", map[string]string{"vcpus": "4", "memoryMiB": "4096"}, map[string]any{"vcpus": float64(4), "memoryMiB": float64(4096), "applyMode": "next-boot"}, true},
		{"resources-cpu-only", "resources", "vm.plan", "", "set", map[string]string{"vcpus": "512"}, map[string]any{"vcpus": float64(512), "applyMode": "next-boot"}, true},
		{"resources-memory-only", "resources", "vm.plan", "", "set", map[string]string{"memoryMiB": "1048576"}, map[string]any{"memoryMiB": float64(1048576), "applyMode": "next-boot"}, true},
		{"capture", "capture", "snapshot.create", "", "create", map[string]string{"sourceRoot": "/private/source images"}, map[string]any{"sourceRoot": "/private/source images"}, true},
		{"capture-auxiliary", "capture", "snapshot.create", "", "create", map[string]string{"sourceRoot": "/private/source images", "auxiliaryRootID": "owner.UEFI-TPM_1"}, map[string]any{"sourceRoot": "/private/source images", "auxiliaryRootID": "owner.UEFI-TPM_1"}, true},
		{"guest-default-port-empty-arguments", "guest-recipe", "guest.recipe.run", "/private/recipes/owner setup.json", "run", guidedGuestValues(), map[string]any{"address": "192.0.2.25", "port": float64(22), "user": "operator", "identityFile": "/private/keys/owner key", "knownHostsFile": "/private/keys/known hosts", "arguments": []string{}}, true},
		{"repository-init", "repository-init", "backup.repository.init", "/private/new repository", "init", map[string]string{"repository": "/private/new repository", "passwordFile": "/private/password reference"}, map[string]any{"passwordFile": "/private/password reference"}, false},
		{"repository-check", "repository-check", "backup.repository.check", "/private/repository", "check", map[string]string{"repository": "/private/repository", "passwordFile": "/private/password reference"}, map[string]any{"passwordFile": "/private/password reference"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := guidedFill(t, guidedFixture(t, tc.kind), tc.values)
			method, request, err := f.Request("qemu:///system")
			want := app.Request{Connection: "qemu:///system", Path: tc.path, Action: tc.action, Input: tc.input}
			if tc.vm {
				want.ID = f.VM.Key.UUID
			}
			if err != nil || method != tc.method || !reflect.DeepEqual(request, want) {
				t.Fatal("form changed shared request or acquired apply authority", method, request, err)
			}
			f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if !submit || cancel || f.Error != "" {
				t.Fatal("valid form cannot open review", f.Error)
			}
			if title := f.Title(); title == "" || !strings.Contains(f.View(80, 24), title) {
				t.Fatal("missing labeled form title", title)
			}
		})
	}
}

func TestGuidedGuestArgumentsAreLiteralAndBounded(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"", []string{}},
		{`alpha "two words" 'ضيف' escaped\ space '' ""`, []string{"alpha", "two words", "ضيف", "escaped space", "", ""}},
		{"'$(touch never)' '$HOME' '`id`' '*' ';' '--flag'", []string{"$(touch never)", "$HOME", "`id`", "*", ";", "--flag"}},
		{`'a\b' "c\\d" a"b"c`, []string{`a\b`, `c\d`, "abc"}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			values := guidedGuestValues()
			values["arguments"], values["port"], values["address"] = tc.text, "2222", "fd00::25"
			f := guidedFill(t, guidedFixture(t, "guest-recipe"), values)
			method, r, err := f.Request("qemu:///system")
			if err != nil || method != "guest.recipe.run" || !reflect.DeepEqual(r.Input["arguments"], tc.want) || r.Input["port"] != float64(2222) || r.Input["address"] != "fd00::25" || len(r.Input) != 6 {
				t.Fatal("literal arguments or explicit transport fields changed", r, err)
			}
		})
	}
	for _, text := range []string{`'unfinished`, `"unfinished`, `trailing\`, strings.Repeat("a ", 65), strings.Repeat("a", 4097), strings.Repeat(strings.Repeat("a", 4096)+" ", 5)} {
		if args, err := guidedArguments(text); err == nil || args != nil {
			t.Fatal("invalid argument list accepted", len(text), err)
		}
	}
}

func TestGuidedFormsInvalidFieldsStayInlineAndDoNotSubmit(t *testing.T) {
	for _, tc := range []struct {
		kind, field, bad string
	}{
		{"resources", "vcpus", ""}, {"resources", "vcpus", "0"}, {"resources", "vcpus", "513"}, {"resources", "vcpus", "1.5"}, {"resources", "vcpus", "+1"}, {"resources", "vcpus", "01"},
		{"resources", "memoryMiB", "1048577"}, {"resources", "memoryMiB", "-2"},
		{"capture", "sourceRoot", "relative"}, {"capture", "sourceRoot", "/private/../other"}, {"capture", "sourceRoot", "/private/source/"}, {"capture", "sourceRoot", " /private/source"}, {"capture", "auxiliaryRootID", "../other"},
		{"guest-recipe", "recipe", "relative.json"}, {"guest-recipe", "address", "host.example"}, {"guest-recipe", "address", "127.0.0.1"}, {"guest-recipe", "address", "fe80::1"}, {"guest-recipe", "address", "::ffff:192.0.2.25"},
		{"guest-recipe", "port", "0"}, {"guest-recipe", "port", "65536"}, {"guest-recipe", "user", "ROOT"}, {"guest-recipe", "user", "-option"}, {"guest-recipe", "identityFile", "paste-a-key"}, {"guest-recipe", "knownHostsFile", "/private/../hosts"}, {"guest-recipe", "arguments", "'unfinished"},
		{"repository-init", "repository", "relative"}, {"repository-check", "passwordFile", "plaintext-password"}, {"repository-check", "passwordFile", "/private/repository/key"}, {"repository-check", "passwordFile", "/private/repository"},
	} {
		t.Run(tc.kind+"/"+tc.field+"/"+tc.bad, func(t *testing.T) {
			values := map[string]string{}
			switch tc.kind {
			case "capture":
				values["sourceRoot"] = "/private/source"
			case "guest-recipe":
				values = guidedGuestValues()
			case "repository-init", "repository-check":
				values = map[string]string{"repository": "/private/repository", "passwordFile": "/private/key"}
			}
			values[tc.field] = tc.bad
			f := guidedFill(t, guidedFixture(t, tc.kind), values)
			method, r, err := f.Request("qemu:///system")
			if err == nil || method != "" || !reflect.DeepEqual(r, app.Request{}) {
				t.Fatal("invalid form exposed a usable request", method, r, err)
			}
			f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if submit || cancel || f.Error == "" || f.Fields[f.Focus].Name != tc.field || !strings.Contains(f.View(80, 24), "Error:") {
				t.Fatal("validation did not focus and explain invalid field", f.Focus, f.Error)
			}
			f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if submit || !cancel || f.Fields[f.Focus].Value != tc.bad {
				t.Fatal("cancel submitted or discarded form input")
			}
		})
	}
}

func TestGuidedFormsFocusUnicodeEditingAndValueIsolation(t *testing.T) {
	f := guidedFixture(t, "capture")
	for _, tc := range []struct {
		key  tea.KeyType
		want int
	}{{tea.KeyTab, 1}, {tea.KeyDown, 0}, {tea.KeyUp, 1}, {tea.KeyShiftTab, 0}} {
		var submit, cancel bool
		f, submit, cancel = f.Update(tea.KeyMsg{Type: tc.key})
		if submit || cancel || f.Focus != tc.want {
			t.Fatal("field focus changed request state", f.Focus)
		}
	}
	original := f
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/private/ضيف")})
	if original.Fields[0].Value != "" {
		t.Fatal("value update mutated prior model")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if f.Fields[0].Value != "/private/ضف" {
		t.Fatal("backspace split Unicode or ignored cursor", f.Fields[0].Value)
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ي")})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnd})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("disk")})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyHome})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyDelete})
	if f.Fields[0].Value != "private/ضيف disk" {
		t.Fatal("home/delete/space editing changed text", f.Fields[0].Value)
	}
}

func TestGuidedFormsRejectControlsAndExcessWithoutLosingInput(t *testing.T) {
	for _, text := range []string{"\x1b[31mred", "line\nbreak", "tab\there", "\u202eevil", strings.Repeat("a", 4097)} {
		f := guidedFill(t, guidedFixture(t, "capture"), map[string]string{"sourceRoot": "/private/source"})
		f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
		if submit || cancel || f.Error == "" || f.Fields[0].Value != "/private/source" {
			t.Fatal("unsafe or excessive paste changed the form", len(text), f.Error)
		}
	}
	f := guidedFixture(t, "resources")
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1234")})
	if f.Fields[0].Value != "" || f.Error == "" {
		t.Fatal("numeric field ignored its byte bound")
	}
}

func TestGuidedFormsIdentityAndConnectionRefusals(t *testing.T) {
	if _, err := NewGuidedForm("unknown", domain.VM{}); err == nil {
		t.Fatal("unknown form accepted")
	}
	for _, kind := range []string{"resources", "capture", "guest-recipe"} {
		if _, err := NewGuidedForm(kind, domain.VM{}); err == nil {
			t.Fatal("missing VM accepted", kind)
		}
		f := guidedFixture(t, kind)
		for _, connection := range []string{"qemu:///session", "qemu+ssh://remote/system", ""} {
			if method, r, err := f.Request(connection); err == nil || method != "" || !reflect.DeepEqual(r, app.Request{}) {
				t.Fatal("wrong connection accepted", kind, connection)
			}
		}
	}
	for _, kind := range []string{"repository-init", "repository-check"} {
		if _, err := NewGuidedForm(kind, domain.VM{}); err != nil {
			t.Fatal("repository incorrectly requires VM selection", err)
		}
	}
	if _, submit, cancel := (GuidedForm{}).Update(tea.KeyMsg{Type: tea.KeyEnter}); submit || cancel {
		t.Fatal("empty form submitted")
	}
}

func TestGuidedFormsResizeSanitizationAndFocusVisible(t *testing.T) {
	for _, kind := range []string{"resources", "capture", "guest-recipe", "repository-init", "repository-check"} {
		f := guidedFixture(t, kind)
		for i := range f.Fields {
			f.Focus = i
			f.Fields[i].Value = strings.Repeat("界", 90)
			f.Fields[i].Cursor = 90
			for _, size := range [][2]int{{80, 24}, {80, 17}, {48, 10}, {24, 6}, {8, 3}, {1, 1}, {0, 0}} {
				view := f.View(size[0], size[1])
				if !utf8.ValidString(view) || strings.Contains(view, "\x1b") || view != "" && len(strings.Split(view, "\n")) > size[1] {
					t.Fatal("invalid or overflowing form", kind, size, view)
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("form exceeds width", kind, size, line)
					}
				}
				if size[0] >= 24 && size[1] >= 6 && !strings.Contains(view, "> ") {
					t.Fatal("focus has no textual cue", kind, size, view)
				}
				if size[0] >= 48 && size[1] >= 6 && !strings.Contains(view, f.Fields[i].Label) {
					t.Fatal("focused field scrolled away", kind, size, view)
				}
			}
		}
		f.Error = "bad\n\x1b[31merror\ttext\u202e"
		f.Fields[f.Focus].Label = "label\n\x1b[31mtext"
		if view := f.View(80, 17); strings.ContainsAny(view, "\x1b\t\u202e") || len(strings.Split(view, "\n")) > 17 {
			t.Fatal("unsafe display text escaped bounds", view)
		}
	}
}

func TestGuidedFormsCompactLayoutShowsOnlyFocusedHelp(t *testing.T) {
	f := guidedFixture(t, "guest-recipe")
	for focus := range f.Fields {
		f.Focus = focus
		view := f.View(80, 24)
		if !strings.Contains(view, f.Fields[focus].Hint) || !strings.Contains(view, "[Enter Preview]") || !strings.Contains(view, "Esc Back") {
			t.Fatal("missing focused explanation or next step", view)
		}
		for i, field := range f.Fields {
			if i != focus && strings.Contains(view, field.Hint) {
				t.Fatal("unfocused field repeats instructions", field.Name, view)
			}
			if !strings.Contains(view, field.Label) {
				t.Fatal("normal terminal hides a form field", field.Name, view)
			}
		}
		if len(strings.Split(view, "\n")) != len(f.Fields)+4 {
			t.Fatal("form should use one row per field and one help line", view)
		}
		wantBrowse := guidedBrowseKind(f.Fields[focus].Name) != ""
		if strings.Contains(view, "Ctrl+O Browse") != wantBrowse {
			t.Fatal("browse shortcut does not match focused field", f.Fields[focus].Name, view)
		}
	}
}

func TestGuidedFormCompactInputKeepsCursorAndInlineErrorVisible(t *testing.T) {
	f := guidedFixture(t, "guest-recipe")
	f.Focus = len(f.Fields) - 1
	f.Fields[f.Focus].Value = strings.Repeat("界", 90)
	f.Fields[f.Focus].Cursor = 90
	f.Error = "Check the recipe inputs."
	for _, size := range [][2]int{{80, 24}, {48, 10}, {24, 6}} {
		view := f.View(size[0], size[1])
		focused := ""
		for _, line := range strings.Split(view, "\n") {
			if strings.HasPrefix(line, "> ") {
				focused = line
			}
		}
		if !strings.Contains(focused, "|") || !strings.HasSuffix(focused, "]") || !strings.Contains(view, "Error:") || !strings.Contains(view, "Esc Back") {
			t.Fatal("compact input lost cursor, error or escape", size, view)
		}
		if got := f.Fields[f.Focus].Value; got != strings.Repeat("界", 90) {
			t.Fatal("rendering changed field value")
		}
	}
}

func TestGuidedBrowseKindsAndEssentialContext(t *testing.T) {
	for field, want := range map[string]string{
		"repository": "directory", "sourceRoot": "directory",
		"path": "file", "parametersFile": "file", "recipe": "file", "identityFile": "file", "knownHostsFile": "file", "passwordFile": "file",
		"vcpus": "", "address": "", "auxiliaryRootID": "", "unknown": "",
	} {
		if got := guidedBrowseKind(field); got != want {
			t.Fatal("wrong picker target", field, got, want)
		}
	}
	for _, kind := range []string{"resources", "capture"} {
		if !strings.Contains(guidedFixture(t, kind).View(80, 24), "VM must be stopped") {
			t.Fatal("required stopped state was hidden", kind)
		}
	}
	f := guidedFixture(t, "guest-recipe")
	f.Focus = 4
	if view := f.View(80, 24); !strings.Contains(view, "never paste a private key") || !strings.Contains(view, "VM must be running") {
		t.Fatal("SSH credential or running-state guidance was hidden", view)
	}
	f = guidedFixture(t, "repository-init")
	f.Focus = 1
	if view := f.View(80, 24); !strings.Contains(view, "outside the repository") || !strings.Contains(view, "no password text") {
		t.Fatal("password-file guidance was hidden", view)
	}
}
