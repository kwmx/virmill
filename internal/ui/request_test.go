package ui

import (
	"path/filepath"
	"testing"
	"virmill.local/core/internal/app"
)

func TestClientResolvesPathsBeforeDetachedServiceCall(t *testing.T) {
	r, err := NormalizeRequest("plugin.develop", app.Request{Action: "new", Input: map[string]any{"id": "example.virmill.vm-report", "sdkDirectory": "sdk/go"}})
	if err != nil {
		t.Fatal(err)
	}
	destination, _ := filepath.Abs("vm-report")
	sdk, _ := filepath.Abs("sdk/go")
	if r.Path != destination || r.Input["sdkDirectory"] != sdk {
		t.Fatal("client path was left relative", r)
	}
	r, err = NormalizeRequest("lab.validate", app.Request{Path: "examples/lab.yaml"})
	if err != nil || !filepath.IsAbs(r.Path) {
		t.Fatal("declarative path not resolved", r, err)
	}
	r, err = NormalizeRequest("import.prepare", app.Request{Path: "appliance.ova", Input: map[string]any{"destination": "prepared/appliance"}})
	destination, _ = filepath.Abs("prepared/appliance")
	if err != nil || !filepath.IsAbs(r.Path) || r.Input["destination"] != destination {
		t.Fatal("import paths not resolved on client", r, err)
	}
	r, err = NormalizeRequest("import.prepare-disks", app.Request{Path: "selected", Input: map[string]any{"destination": "prepared/disks", "files": []any{map[string]any{"path": "disks/boot.qcow2"}}}})
	destination, _ = filepath.Abs("prepared/disks")
	if err != nil || !filepath.IsAbs(r.Path) || r.Input["destination"] != destination || r.Input["files"].([]any)[0].(map[string]any)["path"] != "disks/boot.qcow2" {
		t.Fatal("source-relative file selection changed during client normalization", r, err)
	}
}
