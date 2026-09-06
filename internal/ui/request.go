package ui

import (
	"path/filepath"
	"strings"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// NormalizeRequest resolves user-entered paths on the client. The detached
// coordinator's working directory is unrelated to the terminal's directory.
func NormalizeRequest(method string, r app.Request) (app.Request, error) {
	if method == "plugin.develop" && r.Action == "new" && r.Path == "" {
		id, _ := r.Input["id"].(string)
		if id == "" {
			return r, domain.Fail("INVALID_INPUT", "plugin ID and destination are required")
		}
		parts := strings.Split(id, ".")
		r.Path = parts[len(parts)-1]
		if r.Path == "" || strings.ContainsAny(r.Path, "/\\") {
			return r, domain.Fail("INVALID_INPUT", "invalid default scaffold directory; supply a path")
		}
	}
	var err error
	if r.Path != "" {
		r.Path, err = filepath.Abs(r.Path)
		if err != nil {
			return r, err
		}
	}
	keys := []string{}
	switch method {
	case "plugin.develop":
		keys = []string{"sdkDirectory", "signingKeyPath", "output"}
	case "backup.verify-manifest":
		keys = []string{"root"}
	}
	for _, key := range keys {
		if value, ok := r.Input[key].(string); ok && value != "" {
			absolute, err := filepath.Abs(value)
			if err != nil {
				return r, err
			}
			r.Input[key] = absolute
		}
	}
	return r, nil
}
