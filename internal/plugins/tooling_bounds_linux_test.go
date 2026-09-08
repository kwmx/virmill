//go:build linux

package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestWorkspaceManifestRefusesNonregularAndOversizedInputsBeforeExecution(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "fifo", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			root, cache := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "manifest.json")
			var err error
			switch kind {
			case "symlink":
				target := filepath.Join(root, "target")
				if err = os.WriteFile(target, []byte(`{}`), 0600); err == nil {
					err = os.Symlink(target, path)
				}
			case "directory":
				err = os.Mkdir(path, 0700)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "oversized":
				err = os.WriteFile(path, []byte(strings.Repeat(" ", (1<<20)+1)), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := WorkspaceManifest(root)
				if err == nil {
					done <- nil
					return
				}
				_, err = TestWorkspace(context.Background(), root, cache)
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("unsafe manifest accepted")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("manifest validation blocked on an untrusted file")
			}
			entries, err := os.ReadDir(cache)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected manifest created a conformance workspace", entries, err)
			}
		})
	}
}

func TestCanceledWorkspaceTestDoesNotReadInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := TestWorkspace(ctx, "/nonexistent", t.TempDir()); err != context.Canceled {
		t.Fatalf("canceled test returned %v", err)
	}
}
