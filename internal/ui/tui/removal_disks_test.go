package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRemovalDiskTogglesAreExplicitAndPreserved(t *testing.T) {
	vm := workspaceVM(workspaceVMID, "delete fixture")
	vm.PersistentXML = `<domain><devices><disk type="file" device="disk"><source file="/fixture/a.qcow2"/><target dev="vda"/></disk><disk type="file" device="disk"><source file="/fixture/b.qcow2"/><target dev="vdb"/></disk><disk type="file" device="cdrom"><source file="/fixture/install.iso"/><target dev="sda"/><readonly/></disk></devices></domain>`
	f, e := NewGuidedForm("remove-definition", vm)
	if e != nil || len(f.Fields) != 3 {
		t.Fatal(f, e)
	}
	f.Fields[0].Value = vm.Name
	_, r, e := f.Request(vm.Key.ConnectionID)
	if e != nil || len(r.Input) != 0 {
		t.Fatal("default deleted disks", r, e)
	}
	f.Focus = 1
	f, submit, cancel := f.Update(tea.KeyMsg{Type: tea.KeySpace})
	if submit || cancel || f.Fields[1].Value != "true" {
		t.Fatal("toggle applied instead of selected")
	}
	_, r, e = f.Request(vm.Key.ConnectionID)
	if e != nil || !reflect.DeepEqual(r.Input["deleteDisks"], []string{"vda"}) {
		t.Fatal(r, e)
	}
	f.Focus = 2
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f.Focus = 3
	if !strings.Contains(f.View(80, 17), "> [ Preview ]") || !strings.Contains(f.View(80, 17), "2 of 2 disks") {
		t.Fatal(f.View(80, 17))
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !submit || cancel {
		t.Fatal("preview not emitted")
	}
	f, submit, cancel = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if submit || !cancel || f.Fields[1].Value != "true" || f.Fields[2].Value != "true" {
		t.Fatal("Back lost selection")
	}
}
func TestRemovalDiskOptionsExcludeParentsSharedAndMedia(t *testing.T) {
	raw := `<domain><devices><disk type="file" device="disk"><source file="/base"/><target dev="vda"/></disk><disk type="file" device="disk"><source file="/leaf"/><target dev="vdb"/><backingStore><source file="/base"/></backingStore></disk><disk type="file" device="disk"><source file="/shared"/><target dev="vdc"/><shareable/></disk><disk type="file" device="disk"><source file="/readonly"/><target dev="vdd"/><readonly/></disk></devices></domain>`
	f := removalDiskFields(raw)
	if len(f) != 1 || f[0].Name != "delete:vdb" {
		t.Fatal(f)
	}
}
