package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"virmill.local/core/internal/app"
)

func TestImportInspectionWaitHonorsExplicitTimeout(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		var observed time.Duration
		client := streamClientFunc(func(ctx context.Context, _ string, _ app.Request) (app.Response, error) {
			deadline, _ := ctx.Deadline()
			observed = time.Until(deadline)
			return app.Response{Data: map[string]any{}}, nil
		})
		var out bytes.Buffer
		cmd := New(client, &out, &out)
		args := []string{"import", "inspect", "/media/appliance.ova", "--output", "json"}
		if explicit {
			args = append(args, "--timeout", "7s")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if explicit && (observed > 7*time.Second || observed < 6*time.Second) {
			t.Fatal("explicit timeout ignored", observed)
		}
		if !explicit && observed < 19*time.Minute {
			t.Fatal("default import deadline too short", observed)
		}
	}
}
