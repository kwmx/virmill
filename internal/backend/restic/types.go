// Package restic adapts a system executable to local encrypted repositories.
// It does not authorize operations or certify a complete VM backup or restore.
package restic

import (
	"context"
	"os"
	"time"
)

type Repository struct {
	Path         string `json:"path"`
	PasswordFile string `json:"passwordFile"`
}
type Snapshot struct {
	ID    string    `json:"id"`
	Tags  []string  `json:"tags"`
	Paths []string  `json:"paths"`
	Time  time.Time `json:"time"`
}
type Identity struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// Tool's zero value uses only /usr/bin/restic. There is no public command,
// environment, backend-URL or executable override.
type Tool struct {
	command func(context.Context, invocation) ([]byte, error)
}
type invocation struct {
	args       []string
	files      []*os.File
	dir, label string
}

const executable = "/usr/bin/restic"
const maxOutput = 4 << 20
const maxDiagnostic = 64 << 10
const maxPassword = 4096
