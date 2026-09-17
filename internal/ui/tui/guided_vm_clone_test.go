package tui

import (
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

// ADR 0066: the clone form suggests a name and plans one clone of the stopped VM.
func TestCloneFormSuggestsANameAndPlansTheClone(t *testing.T) {
	vm := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: workspaceVMID},
		Name: "web", State: "stopped", PersistentXML: `<domain><name>web</name></domain>`}
	f, err := NewGuidedForm("vm-clone", vm)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title() != "Clone a VM" || f.Fields[0].Value != "web-clone" {
		t.Fatal(f.Title(), f.Fields[0].Value)
	}
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "vm.plan" || r.ID != workspaceVMID || r.Action != "clone" || r.Input["name"] != "web-clone" {
		t.Fatal(method, r, err)
	}
	if _, present := r.Input["pool"]; present {
		t.Fatal("an empty pool keeps each copy in its disk's own pool", r.Input)
	}
	f.Fields[1].Value = "archive"
	if _, r, err = f.Request("qemu:///system"); err != nil || r.Input["pool"] != "archive" {
		t.Fatal(r.Input, err)
	}
	for _, bad := range []string{"", "web", "a/b"} {
		f.Fields[0].Value = bad
		if _, _, err = f.Request("qemu:///system"); err == nil {
			t.Fatal("accepted name", bad)
		}
	}
	if long := cloneNameSuggestion(strings.Repeat("x", 200)); len([]rune(long)) > 128 || !strings.HasSuffix(long, "-clone") {
		t.Fatal(long)
	}
	vm.State = "running"
	if _, err = NewGuidedForm("vm-clone", vm); err == nil {
		t.Fatal("a running VM was offered for cloning")
	}
}
