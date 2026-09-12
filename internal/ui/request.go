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
	if method == "guest.recipe.run" && r.Path != "" && filepath.Clean(r.Path) != r.Path {
		return r, domain.Fail("INVALID_INPUT", "canonical guest recipe path required")
	}
	// Recovery verification rejects lexical aliases before Abs can clean away
	// a symlink/parent traversal. Canonical relative paths still resolve locally.
	if method == "backup.receipt.read" || method == "backup.verify-manifest" || strings.HasPrefix(method, "backup.repository.") {
		root, _ := r.Input["root"].(string)
		for _, value := range []string{r.Path, root} {
			if value != "" && filepath.Clean(value) != value {
				return r, domain.Fail("INVALID_INPUT", "canonical recovery manifest and member-root paths required")
			}
		}
	}
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
	case "import.prepare", "import.prepare-disks", "import.prepare-install":
		keys = []string{"destination"}
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
	if method == "vm.create" {
		if hardware, ok := r.Input["hardware"].(map[string]any); ok {
			if firmware, ok := hardware["firmware"].(map[string]any); ok {
				for _, key := range []string{"code", "template"} {
					if value, ok := firmware[key].(string); ok && value != "" {
						absolute, err := filepath.Abs(value)
						if err != nil {
							return r, err
						}
						firmware[key] = absolute
					}
				}
			}
		}
	}
	return r, nil
}
