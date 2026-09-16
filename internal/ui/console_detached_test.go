//go:build linux

package ui

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func detachedSession(t *testing.T, script string) (*ConsoleSession, string) {
	t.Helper()
	dir := t.TempDir() + "/viewer"
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return &ConsoleSession{Command: exec.Command("/bin/sh", "-c", script), Graphical: true, configDir: dir}, dir
}

// A viewer that cannot attach exits at once; its own words reach the user and
// its private settings are removed.
func TestDetachedDisplayThatClosesAtOnceReportsWhy(t *testing.T) {
	s, dir := detachedSession(t, "echo 'Unable to connect to libvirt' >&2; exit 1")
	done, err := s.StartDetached()
	if done != nil || err == nil || !strings.Contains(err.Error(), "closed right away") || !strings.Contains(err.Error(), "Unable to connect to libvirt") {
		t.Fatal(done, err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatal("viewer settings kept after the viewer exited")
	}
}

// A viewer that stays open returns control to the TUI and reports its close
// later, removing its settings then and not before.
func TestDetachedDisplayRunsBesideTheTUI(t *testing.T) {
	s, dir := detachedSession(t, "sleep 30")
	started := time.Now()
	done, err := s.StartDetached()
	if err != nil || done == nil || time.Since(started) > 10*time.Second {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatal("viewer settings removed while the viewer is open")
	}
	if pgid, _ := syscallGetpgid(s.Command.Process.Pid); pgid != s.Command.Process.Pid {
		t.Fatal("viewer shares the TUI's process group; Ctrl-C would close it")
	}
	_ = s.Command.Process.Kill()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("close not reported")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatal("viewer settings kept after the viewer closed")
	}
}

func TestOnlyGraphicalSessionsDetach(t *testing.T) {
	s := &ConsoleSession{Command: exec.Command("/bin/true")}
	if _, err := s.StartDetached(); err == nil {
		t.Fatal("a serial console must take over the terminal, not detach")
	}
}

func syscallGetpgid(pid int) (int, error) { return syscall.Getpgid(pid) }
