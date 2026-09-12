package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
)

func removalFixture(t *testing.T) GuidedForm {
	t.Helper()
	f, err := NewGuidedForm("remove-definition", workspaceVM(workspaceVMID, "Workstation café"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGuidedRemovalExactNameThenSeparatePreview(t *testing.T) {
	f := removalFixture(t)
	if len(f.Fields) != 1 || f.Fields[0].Name != "confirmation" || f.Fields[0].Value != "" || f.Fields[0].Toggle {
		t.Fatal("removal requires a blank typed confirmation field", f)
	}
	prior := f
	f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(f.VM.Name)})
	if submit || cancel || f.Fields[0].Value != f.VM.Name || prior.Fields[0].Value != "" {
		t.Fatal("typing mutated old state or requested removal")
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submit || cancel || f.Focus != 1 || !strings.Contains(f.View(80, 17), "> [ Preview ]") {
		t.Fatal("Enter on confirmation skipped explicit Preview")
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !submit || cancel || f.Error != "" {
		t.Fatal("explicit preview did not produce review intent", f.Error)
	}
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "vm.remove" || r.Action != "remove" || r.ID != workspaceVMID || r.Connection != "qemu:///system" || r.Path != "" || r.Apply != nil || r.Input == nil || len(r.Input) != 0 {
		t.Fatal("removal escaped definition-only preview contract", method, r, err)
	}
	f.Error = "The backend found a retained dependency. Inspect it before retrying."
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if submit || !cancel || f.Fields[0].Value != f.VM.Name || f.Error == "" {
		t.Fatal("Back lost confirmation or service error")
	}
}

func TestGuidedRemovalNameConfirmationIsLiteralAndRequired(t *testing.T) {
	for _, name := range []string{"", "Workstation", "workstation café", "Workstation café ", " Workstation café", "Workstation cafe\u0301", workspaceVMID} {
		t.Run(name, func(t *testing.T) {
			f := removalFixture(t)
			f.Fields[0].Value = name
			if method, r, err := f.Request("qemu:///system"); err == nil || method != "" || r.Action != "" {
				t.Fatal("nonliteral confirmation accepted", name)
			}
			f.Focus = 1
			f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if submit || cancel || f.Focus != 0 || !strings.Contains(f.Error, "exactly as shown") || f.Fields[0].Value != name {
				t.Fatal("mismatch error lost typed input")
			}
		})
	}
}

func TestGuidedRemovalEligibilityRejectsUnsafeTarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*domain.VM)
	}{
		{"running", func(vm *domain.VM) { vm.State = "running" }},
		{"paused", func(vm *domain.VM) { vm.State = "paused" }},
		{"unknown", func(vm *domain.VM) { vm.State = "unknown" }},
		{"transient", func(vm *domain.VM) { vm.PersistentXML = "" }},
		{"autostart", func(vm *domain.VM) { vm.Autostart = true }},
		{"managed save", func(vm *domain.VM) { vm.HasManagedSave = true }},
		{"remote", func(vm *domain.VM) { vm.Key.ConnectionID = "qemu+ssh://remote/system" }},
		{"other provider", func(vm *domain.VM) { vm.Key.ProviderID = "plugin" }},
		{"other resource", func(vm *domain.VM) { vm.Key.Kind = "network" }},
		{"missing ID", func(vm *domain.VM) { vm.Key.UUID = "" }},
		{"zero ID", func(vm *domain.VM) { vm.Key.UUID = "00000000-0000-0000-0000-000000000000" }},
		{"name ID", func(vm *domain.VM) { vm.Key.UUID = vm.Name }},
		{"unsafe name", func(vm *domain.VM) { vm.Name = "VM\x1b[2J" }},
		{"empty name", func(vm *domain.VM) { vm.Name = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := removalFixture(t)
			tc.edit(&f.VM)
			if _, err := NewGuidedForm("remove-definition", f.VM); err == nil {
				t.Fatal("ineligible VM opened removal")
			}
			f.Fields[0].Value = f.VM.Name
			if _, _, err := f.Request("qemu:///system"); err == nil {
				t.Fatal("altered ineligible VM could be previewed")
			}
		})
	}
	f := removalFixture(t)
	f.Fields[0].Value = f.VM.Name
	if _, _, err := f.Request("qemu:///session"); err == nil {
		t.Fatal("connection mismatch redirected removal")
	}
	f.VM.Key.ConnectionID = "qemu:///session"
	if method, _, err := f.Request("qemu:///session"); err != nil || method != "vm.remove" {
		t.Fatal("exact local session VM unnecessarily refused", err)
	}
}

func TestGuidedRemovalRejectsMalformedFieldsAndNeverOffersDiskDeletion(t *testing.T) {
	for _, edit := range []func(*GuidedForm){
		func(f *GuidedForm) { f.Fields = nil },
		func(f *GuidedForm) { f.Fields = append(f.Fields, GuidedField{Name: "deleteDisks", Value: "true"}) },
		func(f *GuidedForm) { f.Fields[0].Name = "deleteDisks" },
		func(f *GuidedForm) { f.Fields[0].Toggle = true },
		func(f *GuidedForm) { f.Fields[0].Choices = []string{f.VM.Name} },
		func(f *GuidedForm) { f.Fields[0].Value = "\x1b" + f.VM.Name },
	} {
		f := removalFixture(t)
		f.Fields[0].Value = f.VM.Name
		edit(&f)
		if method, r, err := f.Request("qemu:///system"); err == nil || method != "" || r.Input != nil {
			t.Fatal("malformed form emitted deletion request")
		}
	}
	f := removalFixture(t)
	view := f.View(80, 17)
	for _, text := range []string{"Remove VM", f.VM.Name, workspaceVMID, "State: stopped", "Disks and backups are kept", "configuration will be removed", "Back up or export it first"} {
		if !strings.Contains(view, text) {
			t.Fatal("removal consequences missing", text, view)
		}
	}
	if strings.Contains(view, "Delete disks") || strings.Contains(view, "automatically backed up") {
		t.Fatal("unsupported disk deletion or automatic backup promised")
	}
}

func TestGuidedRemovalKeyboardEditsAndEscapePreserveIntent(t *testing.T) {
	f := removalFixture(t)
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Wrong name")})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if f.Fields[0].Value != "" || f.Fields[0].Cursor != 0 {
		t.Fatal("Ctrl-U did not clear confirmation")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(f.VM.Name)})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if f.Focus != 1 {
		t.Fatal("reverse navigation lost Preview")
	}
	_, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !submit || cancel {
		t.Fatal("Space on Preview did not request review")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyUp})
	if f.Focus != 0 {
		t.Fatal("Up did not return to confirmation")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyLeft})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if f.Fields[0].Value == f.VM.Name {
		t.Fatal("confirmation cannot be corrected with cursor editing")
	}
	if _, _, err := f.Request("qemu:///system"); err == nil {
		t.Fatal("edited partial confirmation accepted")
	}
}

func TestGuidedRemoval80x24ErrorsFullyReachable(t *testing.T) {
	f := removalFixture(t)
	f.Fields[0].Value = f.VM.Name
	f.SetViewport(80, 17)
	f.Error = "Dependency refused. " + strings.Repeat("Keep the selected disks and inspect their references. ", 60) + "Recover the owning snapshot metadata before retrying removal."
	for _, width := range []int{80, 55} {
		for _, focus := range []int{0, 1} {
			f.Focus = focus
			view := f.View(width, 17)
			for _, text := range []string{workspaceVMID, "[ Preview ]", "F1 Read full issue", "Esc Back"} {
				if !strings.Contains(view, text) {
					t.Fatal("80x24 form clipped required action", text, view)
				}
			}
			if len(strings.Split(view, "\n")) > 17 {
				t.Fatal("body height overflow")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatal("body width overflow")
				}
			}
		}
	}
	f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeyF1})
	if submit || cancel || !f.removalIssueOpen {
		t.Fatal("full error reader unavailable")
	}
	var seen []string
	for i := 0; i < 100; i++ {
		view := f.View(80, 17)
		if !strings.Contains(view, "Esc Back to settings") || len(strings.Split(view, "\n")) > 17 {
			t.Fatal("issue reader clipped footer", view)
		}
		seen = append(seen, view)
		prior := f.removalIssueOffset
		f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		if submit || cancel {
			t.Fatal("issue scrolling emitted mutation")
		}
		if prior == f.removalIssueOffset {
			break
		}
		if i == 99 {
			t.Fatal("issue scrolling did not clamp")
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(strings.Join(seen, "\n")), " "), "Recover the owning snapshot metadata before retrying removal.") {
		t.Fatal("recovery instruction was lost")
	}
	before := f
	f.View(80, 17)
	if !reflect.DeepEqual(before, f) {
		t.Fatal("render changed confirmation or issue position")
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if submit || cancel || f.removalIssueOpen || f.Fields[0].Value != f.VM.Name {
		t.Fatal("issue Esc lost confirmation")
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if submit || !cancel {
		t.Fatal("form Esc did not return without removal")
	}
}

func TestGuidedRemovalNewIssueAndNewEditResetReader(t *testing.T) {
	f := removalFixture(t)
	f.Error = strings.Repeat("old error ", 250)
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyF1})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if f.removalIssueOffset == 0 {
		t.Fatal("reader never scrolled")
	}
	f.Error = "New source state: inspect current definition."
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !f.removalIssueOpen || f.removalIssueOffset != 0 || !strings.Contains(f.View(80, 17), "New source state") {
		t.Fatal("new refusal inherited old issue")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(f.VM.Name)})
	if f.Error != "" || f.removalIssueOpen || f.removalIssueOffset != 0 {
		t.Fatal("new edit retained stale issue")
	}
}
