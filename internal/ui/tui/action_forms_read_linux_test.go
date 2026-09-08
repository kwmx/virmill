//go:build linux

package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestActionFormParameterReaderRefusesSpecialLinksAndDirectory(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.json")
	if err := os.WriteFile(original, []byte(`{"selected":"original"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := readActionParameters(original); err != nil || string(data) != `{"selected":"original"}` {
		t.Fatal("original ordinary parameter file unreadable", err)
	}
	link := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(original, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo.json")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, fifo, dir} {
		done := make(chan error, 1)
		go func() { _, err := readActionParameters(path); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("unsafe parameter source accepted", path)
			}
		case <-time.After(time.Second):
			t.Fatal("special parameter source blocked the form", path)
		}
	}
	if err := os.Link(original, filepath.Join(dir, "second-link.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readActionParameters(original); err == nil {
		t.Fatal("multiply linked parameter source accepted")
	}
	parentLink := filepath.Join(t.TempDir(), "parent-link")
	if err := os.Symlink(dir, parentLink); err != nil {
		t.Fatal(err)
	}
	if _, err := readActionParameters(filepath.Join(parentLink, "original.json")); err == nil {
		t.Fatal("symlinked parent accepted")
	}
}
