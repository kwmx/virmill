package cli

import (
	"bytes"
	"testing"
)

func TestProviderConformanceUsesSharedPluginTest(t *testing.T) {
	r := &recorder{}
	var out bytes.Buffer
	command := New(r, &out, &out)
	command.SetArgs([]string{"plugin", "test", "/fixture/provider-conformance", "--output", "json", "--non-interactive"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if r.method != "plugin.test" || r.request.Path != "/fixture/provider-conformance" || r.request.Apply != nil {
		t.Fatal("provider conformance bypassed shared confined plugin test", r)
	}
}
