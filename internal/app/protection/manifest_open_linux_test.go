//go:build linux

package protection

import (
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestManifestRefusesSpecialFileBeforeReadableOpen(t *testing.T) {
	directory := t.TempDir()
	name := filepath.Join(directory, "replaced-member")
	if err := unix.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	notify, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(notify)
	if _, err := unix.InotifyAddWatch(notify, name, unix.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	root, err := openManifestRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.file.Close()
	held, err := root.openMember("replaced-member", true)
	if err == nil || held.file != nil {
		if held.file != nil {
			held.file.Close()
		}
		t.Fatal("special member accepted", err)
	}
	var events [4096]byte
	if n, err := unix.Read(notify, events[:]); n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("special file was opened for bytes before type refusal", n, err)
	}
	// Positive control establishes that this watch detects a readable FIFO
	// open; it touches only the generated FIFO, with no writer or bytes read.
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if n, err := unix.Read(notify, events[:]); n == 0 || err != nil {
		t.Fatal("inotify control did not observe readable open", n, err)
	}
}
