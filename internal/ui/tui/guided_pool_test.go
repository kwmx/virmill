package tui

import (
	"reflect"
	"testing"

	"virmill.local/core/internal/domain"
)

// Custom pool is the advanced path; Create pool keeps libvirt's defaults.
func TestCustomPoolFormMapsToPoolCreation(t *testing.T) {
	f, err := NewGuidedForm("pool-create", domain.VM{})
	if err != nil {
		t.Fatal(err)
	}
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "storage.pool.create" || r.Action != "create" || r.ID != "" || r.Path != "" || !reflect.DeepEqual(r.Input, map[string]any{"name": "default", "autostart": true}) {
		t.Fatal("default custom pool request", method, r, err)
	}
	f.Fields[0].Value, f.Fields[1].Value, f.Fields[2].Value = "vms", "/srv/vms", "false"
	if method, r, err = f.Request("qemu:///session"); err != nil || method != "storage.pool.create" || !reflect.DeepEqual(r.Input, map[string]any{"name": "vms", "path": "/srv/vms", "autostart": false}) {
		t.Fatal("custom pool request", r, err)
	}
	for _, bad := range []struct{ name, folder string }{{"bad name", ""}, {"", ""}, {"-lead", ""}, {"vms", "relative/dir"}, {"vms", "/srv/../etc"}} {
		g := f
		g.Fields = append([]GuidedField{}, f.Fields...)
		g.Fields[0].Value, g.Fields[1].Value = bad.name, bad.folder
		if _, _, err := g.Request("qemu:///system"); err == nil {
			t.Fatal("invalid custom pool accepted", bad)
		}
	}
	if guidedBrowseKind("folder") != "directory" || f.Title() != "Create a storage pool" {
		t.Fatal("folder field is not a directory picker")
	}
	m := fixtureWorkspace()
	m.Section, m.NavIndex = 3, 3
	found := false
	for _, b := range m.buttons() {
		found = found || b.label == "Custom pool" && b.key == "n"
	}
	if !found {
		t.Fatal("Storage lacks Custom pool")
	}
	m, _ = wk(m, "n")
	if m.Form == nil || m.Form.Kind != "pool-create" {
		t.Fatal("Custom pool did not open its form")
	}
}
