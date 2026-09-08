package cli

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestNetworkCreationUsesSharedPreview(t *testing.T) {
	for _, tt := range []struct {
		args                     []string
		method, action, id, path string
	}{{[]string{"network", "create", "network.yaml", "--plan"}, "network.create", "create", "", "network.yaml"}, {[]string{"network", "creation", "resume", "job-id", "--plan"}, "network.creation.resume", "resume", "job-id", ""}, {[]string{"network", "creation", "result", "job-id"}, "network.creation.result", "", "job-id", ""}} {
		r := &recorder{}
		var out bytes.Buffer
		c := New(r, &out, &out)
		c.SetArgs(append(tt.args, "--output", "json", "--non-interactive"))
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		if r.method != tt.method || r.request.Action != tt.action || r.request.ID != tt.id || (tt.path != "" && filepath.Base(r.request.Path) != tt.path) || r.request.Apply != nil {
			t.Fatal(r)
		}
	}
}
