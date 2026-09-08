package tui

import (
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func TestLocalBackupTUIServiceParity(t *testing.T) {
	for _, tc := range []struct{ command, method, input, id, path, action string }{
		{"backup repository init", "backup.repository.init", `{"path":"/private/new repository","input":{"passwordFile":"/private/credential"}}`, "", "/private/new repository", "init"},
		{"backup repository check", "backup.repository.check", `{"path":"/private/repository","input":{"passwordFile":"/private/credential"}}`, "", "/private/repository", "check"},
		{"backup create", "backup.create", `{"id":"capture-id","input":{"repository":"/private/repository","passwordFile":"/private/credential"}}`, "capture-id", "", "create"},
		{"backup restore", "backup.restore", `{"id":"exact-snapshot-hash","input":{"repository":"/private/repository","passwordFile":"/private/credential","captureID":"capture-id","manifestSHA256":"exact-manifest-hash"}}`, "exact-snapshot-hash", "", "restore"},
		{"backup result", "backup.result", "operation-id", "operation-id", "", ""},
	} {
		t.Run(tc.method, func(t *testing.T) {
			c := &coldTUIClient{response: app.Response{APIVersion: domain.APIVersion, Error: domain.Fail("SOURCE_CHANGED", "reviewed repository changed")}}
			m := coldTUIOpen(t, New(c, "qemu:///session"), tc.command)
			m = coldTUIType(t, m, tc.input)
			m = coldTUISubmit(t, m)
			if len(c.methods) != 1 || c.methods[0] != tc.method || m.Busy {
				t.Fatal(c.methods, m.View())
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
