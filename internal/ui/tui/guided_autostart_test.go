package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
)

func autostartFixture(t *testing.T, enabled bool, connection string) GuidedForm {
	t.Helper()
	vm := workspaceVM(workspaceVMID, "Workstation")
	vm.Key.ConnectionID, vm.Autostart = connection, enabled
	f, err := NewGuidedForm("autostart", vm)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGuidedAutostartUsesObservedCurrentSetting(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		f := autostartFixture(t, enabled, "qemu:///system")
		want := "false"
		current := "Off"
		if enabled {
			want, current = "true", "On"
		}
		if len(f.Fields) != 1 || f.Fields[0].Name != "enabled" || !f.Fields[0].Toggle || f.Fields[0].Value != want || f.Title() != "Start automatically" {
			t.Fatal("current observation not used", f)
		}
		view := f.View(80, 17)
		for _, text := range []string{"Current: " + current, "Requested: < " + current + " >", "system libvirt service", "does not start or stop the VM now", "[ Preview ]"} {
			if !strings.Contains(view, text) {
				t.Fatal("startup meaning/current value not visible", text, view)
			}
		}
	}
}

func TestGuidedAutostartRequiresSeparatePreviewButton(t *testing.T) {
	f := autostartFixture(t, false, "qemu:///system")
	original := f
	var submit, cancel bool
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submit || cancel || f.Fields[0].Value != "true" || f.Focus != 0 || original.Fields[0].Value != "false" {
		t.Fatal("Enter on toggle submitted or mutated previous model")
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyTab})
	if submit || cancel || f.Focus != 1 || !strings.Contains(f.View(80, 17), "> [ Preview ]") {
		t.Fatal("Preview is not separately focusable")
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !submit || cancel || f.Error != "" {
		t.Fatal("explicit Preview did not request review", f.Error)
	}
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "vm.plan" || r.Action != "autostart" || r.ID != workspaceVMID || r.Connection != "qemu:///system" || r.Path != "" || r.Apply != nil || !reflect.DeepEqual(r.Input, map[string]any{"enabled": true}) {
		t.Fatal("not an exact boolean plan request", method, r, err)
	}
	// The caller's Plan Back/error path reuses the same form; Escape retains its
	// requested value and never emits an immediate start or stop operation.
	f.Error = "The VM configuration changed. Review again."
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if submit || !cancel || f.Fields[0].Value != "true" || f.Error == "" {
		t.Fatal("Back lost the pending choice or error")
	}
}

func TestGuidedAutostartDisablingAndSessionSemantics(t *testing.T) {
	f := autostartFixture(t, true, "qemu:///session")
	f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if submit || cancel || f.Fields[0].Value != "false" {
		t.Fatal("Space did not disable requested startup only")
	}
	method, r, err := f.Request("qemu:///session")
	if err != nil || method != "vm.plan" || !reflect.DeepEqual(r.Input, map[string]any{"enabled": false}) {
		t.Fatal("session disable request lost explicit false", r, err)
	}
	view := f.View(80, 17)
	if !strings.Contains(view, "user libvirt service") || !strings.Contains(view, "not directly at host boot") || strings.Contains(view, "system libvirt service") {
		t.Fatal("session startup described as host startup", view)
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyUp})
	if f.Focus != 1 {
		t.Fatal("reverse navigation did not reach Preview")
	}
	_, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !submit || cancel {
		t.Fatal("Space on Preview did not request review")
	}
}

func TestGuidedAutostartRejectsNoChangeAndMalformedForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*GuidedForm)
	}{
		{"unchanged", func(f *GuidedForm) { f.Fields[0].Value = "false" }},
		{"missing", func(f *GuidedForm) { f.Fields = nil }},
		{"extra field", func(f *GuidedForm) { f.Fields = append(f.Fields, GuidedField{Name: "startNow", Value: "true"}) }},
		{"wrong field", func(f *GuidedForm) { f.Fields[0].Name = "autostart" }},
		{"not a toggle", func(f *GuidedForm) { f.Fields[0].Toggle = false }},
		{"unexpected choices", func(f *GuidedForm) { f.Fields[0].Choices = []string{"true", "false"} }},
		{"unparseable", func(f *GuidedForm) { f.Fields[0].Value = "yes" }},
		{"noncanonical", func(f *GuidedForm) { f.Fields[0].Value = "TRUE" }},
		{"blank", func(f *GuidedForm) { f.Fields[0].Value = "" }},
		{"terminal control", func(f *GuidedForm) { f.Fields[0].Value = "\x1btrue" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := autostartFixture(t, false, "qemu:///system")
			f.Fields[0].Value = "true"
			tc.edit(&f)
			method, r, err := f.Request("qemu:///system")
			if err == nil || method != "" || r.Action != "" || r.Input != nil {
				t.Fatal("invalid form produced a plan request", method, r, err)
			}
			f.Focus = 1
			f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if submit || cancel || f.Error == "" {
				t.Fatal("invalid Preview was not stopped visibly")
			}
		})
	}
	f := autostartFixture(t, true, "qemu:///system")
	f.Focus = 1
	f, submit, _ := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submit || f.Focus != 0 || !strings.Contains(f.Error, "No change selected") {
		t.Fatal("unchanged setting did not give useful next step")
	}
}

func TestGuidedAutostartRequiresExactPersistentLocalVM(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*domain.VM)
	}{
		{"other provider", func(vm *domain.VM) { vm.Key.ProviderID = "plugin" }},
		{"other resource", func(vm *domain.VM) { vm.Key.Kind = "network" }},
		{"missing ID", func(vm *domain.VM) { vm.Key.UUID = "" }},
		{"invalid ID", func(vm *domain.VM) { vm.Key.UUID = "workstation" }},
		{"zero ID", func(vm *domain.VM) { vm.Key.UUID = "00000000-0000-0000-0000-000000000000" }},
		{"remote", func(vm *domain.VM) { vm.Key.ConnectionID = "qemu+ssh://host/system" }},
		{"transient", func(vm *domain.VM) { vm.PersistentXML = "" }},
		{"empty definition", func(vm *domain.VM) { vm.PersistentXML = " \n" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := workspaceVM(workspaceVMID, "Workstation")
			tc.edit(&vm)
			if _, err := NewGuidedForm("autostart", vm); err == nil {
				t.Fatal("ineligible target opened automatic startup form")
			}
			f := autostartFixture(t, false, "qemu:///system")
			f.VM = vm
			f.Fields[0].Value = "true"
			if _, _, err := f.Request("qemu:///system"); err == nil {
				t.Fatal("Request trusted altered target")
			}
		})
	}
	f := autostartFixture(t, false, "qemu:///system")
	f.Fields[0].Value = "true"
	if _, _, err := f.Request("qemu:///session"); err == nil {
		t.Fatal("connection change redirected startup setting")
	}
}

func TestGuidedAutostartNoUnnecessaryStoppedStateRestriction(t *testing.T) {
	for _, state := range []string{"running", "stopped", "paused"} {
		f := autostartFixture(t, false, "qemu:///system")
		f.VM.State = state
		f.Fields[0].Value = "true"
		if _, r, err := f.Request("qemu:///system"); err != nil || r.Action != "autostart" {
			t.Fatal("startup policy incorrectly required shutdown", state, err)
		}
	}
}

func TestGuidedAutostart80x24CurrentTogglePreviewAndErrorVisible(t *testing.T) {
	for _, connection := range []string{"qemu:///system", "qemu:///session"} {
		f := autostartFixture(t, false, connection)
		f.Fields[0].Value = "true"
		f.Error = "Configuration changed. Your requested setting was kept; review again."
		for _, width := range []int{80, 55} {
			for _, focus := range []int{0, 1} {
				f.Focus = focus
				view := f.View(width, 17)
				for _, text := range []string{"Current: Off", "Requested: < On >", "[ Preview ]", "Configuration changed", "Esc Back"} {
					if !strings.Contains(view, text) {
						t.Fatal("80x24 full-width/sidebar content missing", text, view)
					}
				}
				lines := strings.Split(view, "\n")
				if len(lines) > 17 {
					t.Fatal("body height overflow")
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > width {
						t.Fatal("body width overflow", line)
					}
				}
			}
		}
	}
	f := autostartFixture(t, false, "qemu:///system")
	t.Log("Synthetic 80x24 body:\n" + f.View(80, 17))
}
