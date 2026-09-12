package cli

import (
	"bytes"
	"reflect"
	"testing"
)

func TestRemoveDiskFlagUsesSelectedTargetsOnly(t *testing.T) {
	for _, flags := range [][]string{{"--delete-disk", "vda,vdb"}, {"--delete-disk", "vda", "--delete-disk", "vdb"}} {
		r := &recorder{}
		var out bytes.Buffer
		c := New(r, &out, &out)
		c.SetArgs(append([]string{"vm", "remove", "vm-id", "--output", "json", "--non-interactive"}, flags...))
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		if r.method != "vm.remove" || r.request.Action != "remove" || r.request.ID != "vm-id" || r.request.Apply != nil || !reflect.DeepEqual(r.request.Input["deleteDisks"], []string{"vda", "vdb"}) {
			t.Fatal("selected removal did not use the shared preview service", r.request)
		}
	}
}

func TestRemoveDiskFlagRefusesEmptyAmbiguousOrDuplicateSelection(t *testing.T) {
	for _, flags := range [][]string{{"--delete-disk="}, {"--delete-disk", "vda,"}, {"--delete-disk", ",vda"}, {"--delete-disk", "vda,vda"}, {"--delete-disk", "vda", "--delete-disk", "vda"}, {"--delete-disk", "/tmp/disk.qcow2"}, {"--delete-disk", " vda"}, {"--delete-disk", "vda", "--input", `{"deleteDisks":[]}`}} {
		r := &recorder{}
		var out bytes.Buffer
		c := New(r, &out, &out)
		c.SetArgs(append([]string{"vm", "remove", "vm-id", "--output", "json", "--non-interactive"}, flags...))
		if err := c.Execute(); err == nil || r.method != "" {
			t.Fatal("ambiguous deletion reached service", flags, err)
		}
	}
}

func TestRemoveKeepsDisksUnlessExplicitlySelected(t *testing.T) {
	for _, flags := range [][]string{nil, {"--input", `{"deleteDisks":["vda"]}`}} {
		r := &recorder{}
		var out bytes.Buffer
		c := New(r, &out, &out)
		c.SetArgs(append([]string{"vm", "remove", "vm-id", "--output", "json"}, flags...))
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		if len(flags) == 0 && len(r.request.Input) != 0 {
			t.Fatal("default removal selected disks")
		}
		if len(flags) != 0 && !reflect.DeepEqual(r.request.Input["deleteDisks"], []any{"vda"}) {
			t.Fatal("explicit JSON selection lost")
		}
	}
}
