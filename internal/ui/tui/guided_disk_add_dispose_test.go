package tui

import (
	"testing"

	"virmill.local/core/internal/domain"
)

const disposeJobID = "7602cb7a-6686-429b-a033-437cd5dc6247"

// ADR 0062: an unfinished disk addition is closed from Jobs, by accepting the
// new disk or deleting a volume no VM uses.
func TestDiskAddDisposeFormPlansTheChosenDisposition(t *testing.T) {
	f, err := NewGuidedForm("disk-add-dispose", domain.VM{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Fields[0].Value != "accept" || len(f.Fields[0].Choices) != 2 {
		t.Fatal(f.Fields[0])
	}
	// Without a job the form cannot submit: it acts on an operation.
	if _, _, err = f.Request("qemu:///system"); err == nil {
		t.Fatal("a disposition without a job was accepted")
	}
	f.JobID = disposeJobID
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "vm.disk.add.dispose" || r.ID != disposeJobID || r.Input["disposition"] != "accept" {
		t.Fatal(method, r, err)
	}
	f.Fields[0].Value = "delete"
	if _, r, err = f.Request("qemu:///system"); err != nil || r.Input["disposition"] != "delete" {
		t.Fatal(r.Input, err)
	}
	f.Fields[0].Value = "keep"
	if _, _, err = f.Request("qemu:///system"); err == nil {
		t.Fatal("an unknown disposition was accepted")
	}
}
