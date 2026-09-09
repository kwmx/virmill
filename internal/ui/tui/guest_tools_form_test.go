package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
)

// These tests exercise form and coordinator boundaries, not SSH or guest hardware.
func guestToolsFormFixture(t *testing.T) GuidedForm {
	t.Helper()
	f := guidedFixture(t, "guest-tools")
	f.VM.State = "running"
	return guidedFill(t, f, map[string]string{"address": "192.0.2.25", "user": "operator", "identityFile": "/private/keys/owner key", "knownHostsFile": "/private/keys/known hosts"})
}
func guestToolsFocus(t *testing.T, f GuidedForm, name string) GuidedForm {
	t.Helper()
	f.Focus = slices.IndexFunc(f.Fields, func(field GuidedField) bool { return field.Name == name })
	if f.Focus < 0 {
		t.Fatal("missing guest-tools field", name)
	}
	return f
}
func guestToolsValue(t *testing.T, f GuidedForm, name string) string {
	t.Helper()
	f = guestToolsFocus(t, f, name)
	return f.Fields[f.Focus].Value
}

func TestGuestToolsFormProfilesDefaultsAndDesktopToggle(t *testing.T) {
	f := guestToolsFormFixture(t)
	if guestToolsValue(t, f, "profile") != "linux-auto" || guestToolsValue(t, f, "desktop") != "false" || guestToolsValue(t, f, "port") != "22" {
		t.Fatal("unsafe or missing defaults")
	}
	f = guestToolsFocus(t, f, "profile")
	for _, want := range []string{"debian", "ubuntu", "fedora", "windows", "linux-auto"} {
		var submit, cancel bool
		f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyRight})
		if submit || cancel || guestToolsValue(t, f, "profile") != want {
			t.Fatal("profile selection submitted or lost choice", want)
		}
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if guestToolsValue(t, f, "profile") != "windows" {
		t.Fatal("reverse profile selection failed")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("unsupported")})
	if guestToolsValue(t, f, "profile") != "windows" {
		t.Fatal("free text replaced a supported choice")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	f = guestToolsFocus(t, f, "desktop")
	original := f
	f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if submit || cancel || guestToolsValue(t, f, "desktop") != "true" || guestToolsValue(t, original, "desktop") != "false" {
		t.Fatal("desktop toggle failed or mutated prior model")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if guestToolsValue(t, f, "desktop") != "false" {
		t.Fatal("desktop tools cannot be disabled")
	}
}

func TestGuestToolsFormExactPreviewRequest(t *testing.T) {
	for _, connection := range []string{"qemu:///system", "qemu:///session"} {
		for _, profile := range []string{"linux-auto", "debian", "ubuntu", "fedora"} {
			t.Run(connection+"/"+profile, func(t *testing.T) {
				f := guestToolsFormFixture(t)
				f.VM.Key.ConnectionID = connection
				f = guestToolsFocus(t, f, "profile")
				for guestToolsValue(t, f, "profile") != profile {
					f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
				}
				f = guestToolsFocus(t, f, "desktop")
				f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
				f = guidedFill(t, f, map[string]string{"port": "2222", "address": "fd00::25"})
				method, request, err := f.Request(connection)
				want := app.Request{Connection: connection, ID: f.VM.Key.UUID, Action: "install", Input: map[string]any{"profile": profile, "desktop": true, "address": "fd00::25", "port": float64(2222), "user": "operator", "identityFile": "/private/keys/owner key", "knownHostsFile": "/private/keys/known hosts"}}
				if err != nil || method != "guest.tools.install" || !reflect.DeepEqual(request, want) {
					t.Fatal("request changed or acquired apply authority", method, request, err)
				}
				f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if !submit || cancel || f.Error != "" {
					t.Fatal("valid request cannot open review", f.Error)
				}
			})
		}
	}
}

func TestGuestToolsFormFailuresStayEditable(t *testing.T) {
	for _, state := range []string{"stopped", "paused", "unknown", ""} {
		t.Run("state/"+state, func(t *testing.T) {
			f := guestToolsFormFixture(t)
			f.VM.State = state
			method, request, err := f.Request(f.VM.Key.ConnectionID)
			if err == nil || method != "" || !reflect.DeepEqual(request, app.Request{}) || !strings.Contains(err.Error(), "Start this VM") {
				t.Fatal("non-running guest accepted", method, request, err)
			}
			f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if submit || cancel || f.Fields[f.Focus].Name != "address" || !strings.Contains(f.Error, "Start this VM") {
				t.Fatal("running requirement not actionable", f.Error)
			}
		})
	}
	for _, tc := range []struct{ name, value, hint string }{
		{"profile", "other", "supported"}, {"desktop", "yes", "supported"},
		{"address", "127.0.0.1", "guest IPv4"}, {"user", "root", "non-root"}, {"port", "0", "1 to 65535"},
		{"identityFile", "private-key-contents", "absolute file paths"}, {"knownHostsFile", "/private/../hosts", "absolute file paths"},
	} {
		t.Run(tc.name+"/"+tc.value, func(t *testing.T) {
			f := guestToolsFocus(t, guestToolsFormFixture(t), tc.name)
			f.Fields[f.Focus].Value = tc.value
			f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if submit || cancel || !strings.Contains(f.Error, tc.hint) {
				t.Fatal("invalid option submitted or lost guidance", f.Error)
			}
		})
	}
}

func TestGuestToolsFormWindowsManualGuidance(t *testing.T) {
	f := guestToolsFocus(t, guestToolsFormFixture(t), "profile")
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	method, request, err := f.Request(f.VM.Key.ConnectionID)
	if err == nil || method != "" || !reflect.DeepEqual(request, app.Request{}) || !strings.Contains(err.Error(), "VirtIO") {
		t.Fatal("Windows must not produce an automatic SSH installation request", method, request, err)
	}
	f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submit || cancel || f.Error != "" || f.Focus != 0 {
		t.Fatal("Windows guidance should remain available without an invalid form submission", f.Error)
	}
	for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
		view := f.View(size[0], size[1])
		if !strings.Contains(view, "VirtIO") || strings.Contains(view, "Guest IP address") || strings.Contains(view, "SSH key") {
			t.Fatal("Windows guidance hides manual steps or requests inapplicable SSH details", view)
		}
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("Windows guidance too tall", size)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("Windows guidance too wide", size, line)
			}
		}
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	if guestToolsValue(t, f, "profile") != "linux-auto" || !strings.Contains(f.View(80, 24), "Guest IP address") {
		t.Fatal("Linux options cannot be restored from Windows guidance")
	}
}

func TestGuestToolsFormCredentialPickerRoutingAndCancel(t *testing.T) {
	for _, name := range []string{"identityFile", "knownHostsFile"} {
		t.Run(name, func(t *testing.T) {
			f := guestToolsFocus(t, guestToolsFormFixture(t), name)
			original := f
			m := fixtureWorkspace()
			m.Form = &f
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
			m = next.(Workspace)
			if cmd == nil || m.Picker == nil || m.Picker.kind != "file" || m.PickerAction || m.PickerField != f.Focus || m.ImportPickerTarget != "" || m.Busy || m.Plan != nil {
				t.Fatal("credential picker misrouted or started work")
			}
			// Do not execute the observation command: cancellation must need no reads.
			m, cmd = wk(m, "esc")
			if cmd != nil || m.Picker != nil || m.Form == nil || !reflect.DeepEqual(*m.Form, original) || len(m.Client.(*workspaceClient).calls) != 0 {
				t.Fatal("picker cancellation changed form or contacted service")
			}
		})
	}
	f := guestToolsFocus(t, guestToolsFormFixture(t), "address")
	m := fixtureWorkspace()
	m.Form = &f
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if cmd != nil || next.(Workspace).Picker != nil {
		t.Fatal("IP address must not open a file picker")
	}
}

func TestGuestToolsFormWorkspacePlanCancelRestoresEdits(t *testing.T) {
	m := fixtureWorkspace()
	f := guestToolsFormFixture(t)
	m.Detail = generic(f.VM)
	if cmd := m.openAction(ui.Action{Command: "guest tools install", Mutation: "install"}); cmd != nil || m.Form == nil || m.Form.Kind != "guest-tools" || m.Advanced {
		t.Fatal("action did not open native guest-tools form")
	}
	f = guestToolsFocus(t, f, "profile")
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
	f = guestToolsFocus(t, f, "desktop")
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Form = &f
	p := testWorkspacePlan(t)
	p.Operation = "guest.tools.install"
	p.ResourceIDs = []string{"libvirt|qemu:///system|vm|" + f.VM.Key.UUID}
	p.Acknowledgements = []string{"guest-execution", "guest-sudo"}
	var err error
	p.Digest, err = operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	client := m.Client.(*workspaceClient)
	client.response = app.Response{Data: p}
	m, cmd := wk(m, "enter")
	if cmd == nil || !m.Busy || m.Plan != nil {
		t.Fatal("preview request missing")
	}
	next, follow := m.Update(cmd())
	m = next.(Workspace)
	if follow != nil || m.Plan == nil || m.Form != nil || m.SavedGuestTools == nil || m.Busy || m.Reviewing || len(client.calls) != 1 || client.calls[0] != "guest.tools.install" || client.requests[0].Apply != nil {
		t.Fatal("preview failed or approved installation implicitly", m.Error)
	}
	m, cmd = wk(m, "esc")
	if cmd != nil || m.Plan != nil || m.SavedGuestTools != nil || m.Form == nil || !reflect.DeepEqual(*m.Form, f) || len(client.calls) != 1 {
		t.Fatal("cancel lost edited guest-tools form or executed guest work")
	}
}

func TestGuestToolsFormTerminalBounds(t *testing.T) {
	f := guestToolsFormFixture(t)
	for focus := range f.Fields {
		f.Focus = focus
		for _, size := range [][2]int{{60, 18}, {80, 24}, {120, 36}} {
			view := f.View(size[0], size[1])
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("form too tall", size, focus)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("form too wide", size, focus, line)
				}
			}
			if !strings.Contains(view, "Install guest tools") || strings.Contains(view, "JSON") {
				t.Fatal("native guest-tools title missing or settings upload requested", view)
			}
		}
	}
}
