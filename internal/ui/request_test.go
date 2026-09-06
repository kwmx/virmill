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
}
