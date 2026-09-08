package cli

import (
	"context"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func TestLocalBackupCLIServiceParity(t *testing.T) {
	for _, tc := range []struct {
		args                     []string
		method, id, path, action string
	}{
		{[]string{"backup", "repository", "init", "/private/new repository", "--input", `{"passwordFile":"/private/credential"}`}, "backup.repository.init", "", "/private/new repository", "init"},
		{[]string{"backup", "repository", "check", "/private/repository", "--input", `{"passwordFile":"/private/credential"}`}, "backup.repository.check", "", "/private/repository", "check"},
		{[]string{"backup", "create", "capture-id", "--input", `{"repository":"/private/repository","passwordFile":"/private/credential"}`}, "backup.create", "capture-id", "", "create"},
		{[]string{"backup", "restore", "exact-snapshot-hash", "--input", `{"repository":"/private/repository","passwordFile":"/private/credential","captureID":"capture-id","manifestSHA256":"exact-manifest-hash"}`}, "backup.restore", "exact-snapshot-hash", "", "restore"},
		{[]string{"backup", "result", "operation-id"}, "backup.result", "operation-id", "", ""},
	} {
		t.Run(tc.method, func(t *testing.T) {
			c := &coldCLIClient{response: app.Response{APIVersion: domain.APIVersion, Error: domain.Fail("SOURCE_CHANGED", "reviewed repository changed")}}
			r, err := coldCLIExecute(t, context.Background(), c, "json", tc.args...)
			if err == nil || r.Error == nil || r.Error.Code != "SOURCE_CHANGED" || len(c.methods) != 1 || c.methods[0] != tc.method {
				t.Fatal(r, err, c.methods)
			}
			q := c.requests[0]
			if q.ID != tc.id || q.Path != tc.path || q.Action != tc.action || q.Apply != nil {
				t.Fatal(q)
			}
			if tc.action != "" && q.Input["passwordFile"] != "/private/credential" {
				t.Fatal("credential reference lost", q)
			}
		})
	}
}
