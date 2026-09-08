package cli

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestBackupPolicyCommandsNormalizePathsAndNeverApply(t *testing.T) {
	for _, action := range []string{"validate", "preview"} {
		r := &recorder{}
		var out bytes.Buffer
		c := New(r, &out, &out)
		args := []string{"backup", "policy", action, "policy.yaml", "--output", "json", "--non-interactive"}
		if action == "preview" {
			args = append(args, "--input", `{"after":"2026-09-08T00:00:00Z","count":2}`)
		}
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		if r.method != "backup.policy."+action || !filepath.IsAbs(r.request.Path) || r.request.Apply != nil || r.request.Action != "" {
			t.Fatal("wrong policy service request", r)
		}
		if action == "preview" && r.request.Input["count"] != float64(2) {
			t.Fatal("preview options lost")
		}
	}
}
