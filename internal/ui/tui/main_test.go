package tui

import (
	"os"
	"testing"
)

// Imports default to a folder under XDG_DATA_HOME. Point it at a temporary
// folder so tests never create folders in the real home directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "virmill-tui-test-")
	if err != nil {
		panic(err)
	}
	if err = os.Setenv("XDG_DATA_HOME", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
