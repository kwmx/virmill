//go:build linux && amd64

package coldfiles

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
)

func fixture(t *testing.T) (string, string, fileidentity.Identity) {
	t.Helper()
	root := t.TempDir()
	relative := "disk.raw"
	if err := os.WriteFile(filepath.Join(root, relative), []byte("original-source-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := fileidentity.Observe(filepath.Join(root, relative), false)
	if err != nil {
		t.Fatal(err)
	}
	return root, relative, id
}

func openFixture(t *testing.T, root, relative string, id fileidentity.Identity) *Source {
	t.Helper()
	s, err := Open(context.Background(), root, relative, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func refuse(t *testing.T, root, relative string, id fileidentity.Identity) {
	t.Helper()
	s, err := Open(context.Background(), root, relative, id)
	if s != nil {
		s.Close()
		t.Fatal("refused source returned a usable handle")
	}
	if err == nil {
		t.Fatal("unsafe source accepted")
	}
}

func TestReadPinnedBytesAndExactIdentity(t *testing.T) {
	root, relative, id := fixture(t)
	s := openFixture(t, root, relative, id)
	if s.Identity() != id {
		t.Fatal("source identity differs from expected identity")
	}
	buf := make([]byte, 8)
	if n, err := s.ReadAt(buf, 9); err != nil || n != len(buf) || string(buf) != "source-b" {
		t.Fatal("wrong offset data", n, string(buf), err)
	}
	if n, err := s.ReadAt(buf, int64(id.Size)-2); !errors.Is(err, io.EOF) || n != 2 || string(buf[:n]) != "es" {
		t.Fatal("short read did not report EOF", n, err)
	}
	if _, err := s.ReadAt(buf, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if err := s.Recheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	observed, err := fileidentity.Observe(filepath.Join(root, relative), false)
	if err != nil || observed != id {
		t.Fatal("reader changed source metadata", observed, err)
	}
}

func TestRefuseMissingOrDifferentExpectedIdentity(t *testing.T) {
	root, relative, id := fixture(t)
	refuse(t, root, relative, fileidentity.Identity{})
	for name, change := range map[string]func(*fileidentity.Identity){
		"generation": func(i *fileidentity.Identity) { i.Generation += "-different" },
		"size":       func(i *fileidentity.Identity) { i.Size++ },
		"modified":   func(i *fileidentity.Identity) { i.Modified += "0" },
		"changed":    func(i *fileidentity.Identity) { i.Changed += "0" },
		"links":      func(i *fileidentity.Identity) { i.Links = 2 },
		"mode":       func(i *fileidentity.Identity) { i.Mode ^= 0400 },
		"partial":    func(i *fileidentity.Identity) { i.Changed = "" },
	} {
		t.Run(name, func(t *testing.T) { different := id; change(&different); refuse(t, root, relative, different) })
	}
	if err := os.WriteFile(filepath.Join(root, "empty"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	empty, err := fileidentity.Observe(filepath.Join(root, "empty"), false)
	if err != nil {
		t.Fatal(err)
	}
	s := openFixture(t, root, "empty", empty)
	if n, err := s.ReadAt(make([]byte, 1), 0); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatal("explicit empty ordinary file mishandled", n, err)
	}
}

func TestRefuseNoncanonicalPathsAndSymlinks(t *testing.T) {
	root, relative, id := fixture(t)
	for _, name := range []string{"", ".", "..", "../disk.raw", "/disk.raw", "sub/../disk.raw", "sub//disk.raw", "disk.raw/", "disk.raw\x00other"} {
		t.Run(name, func(t *testing.T) { refuse(t, root, name, id) })
	}
	for _, name := range []string{"", "relative", root + "/.", root + "/../" + filepath.Base(root)} {
		refuse(t, name, relative, id)
	}
	if err := os.Symlink(relative, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	refuse(t, root, "alias", id)
	if err := os.Symlink(root, filepath.Join(root, "directory-alias")); err != nil {
		t.Fatal(err)
	}
	refuse(t, root, "directory-alias/"+relative, id)
	refuse(t, filepath.Join(root, "directory-alias"), relative, id)
	if err := os.Link(filepath.Join(root, relative), filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	refuse(t, root, relative, id)
}

func TestFIFOIsRejectedBeforeReadableOpen(t *testing.T) {
	root, relative, id := fixture(t)
	path := filepath.Join(root, relative)
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	notifications, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(notifications)
	if _, err = unix.InotifyAddWatch(notifications, path, unix.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	refuse(t, root, relative, id)
	buffer := make([]byte, 4096)
	if n, err := unix.Read(notifications, buffer); n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("FIFO was opened for bytes before its type was rejected", n, err)
	}
	// Establish that this kernel watch detects a real readable FIFO open, so an
	// empty notification queue above is an observable boundary, not a mock.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	unix.Close(fd)
	if n, err := unix.Read(notifications, buffer); err != nil || n < unix.SizeofInotifyEvent {
		t.Fatal("FIFO open notification control failed", n, err)
	}
}

func TestRecheckRejectsSourceAndRootDrift(t *testing.T) {
	for name, change := range map[string]func(string, string) error{
		"replace-same-bytes": func(root, path string) error {
			if err := os.Rename(path, path+".retained"); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("original-source-bytes"), 0600)
		},
		"write-same-inode": func(root, path string) error { return os.WriteFile(path, []byte("different-file-bytes!"), 0600) },
		"truncate":         func(root, path string) error { return os.Truncate(path, 1) },
		"chmod":            func(root, path string) error { return os.Chmod(path, 0400) },
		"unlink":           func(root, path string) error { return os.Remove(path) },
		"new-hardlink":     func(root, path string) error { return os.Link(path, path+".link") },
		"sibling-created":  func(root, path string) error { return os.WriteFile(filepath.Join(root, "sibling"), nil, 0600) },
		"root-chmod":       func(root, path string) error { return os.Chmod(root, 0500) },
	} {
		t.Run(name, func(t *testing.T) {
			root, relative, id := fixture(t)
			s := openFixture(t, root, relative, id)
			if err := change(root, filepath.Join(root, relative)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(root, 0700) })
			if err := s.Recheck(context.Background()); err == nil {
				t.Fatal("source/root drift accepted")
			}
			if duplicate, err := s.DupForTransfer(); err == nil || duplicate != nil {
				if duplicate != nil {
					duplicate.Close()
				}
				t.Fatal("drifted source transferred", err)
			}
		})
	}
}

func TestPathReplacementNeverRedirectsBytes(t *testing.T) {
	root, relative, id := fixture(t)
	s := openFixture(t, root, relative, id)
	path := filepath.Join(root, relative)
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("attacker-replacement!"), 0600); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, id.Size)
	if n, err := s.ReadAt(buf, 0); err != nil || n != len(buf) || string(buf) != "original-source-bytes" {
		t.Fatal("selected pathname redirected held reader", n, string(buf), err)
	}
	if err := s.Recheck(context.Background()); err == nil {
		t.Fatal("replacement passed final capture check")
	}
}

func TestRootPathReplacementIsRejected(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := fileidentity.Observe(filepath.Join(root, "source"), false)
	if err != nil {
		t.Fatal(err)
	}
	s := openFixture(t, root, "source", id)
	if err = os.Rename(root, filepath.Join(parent, "retained")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "source"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Recheck(context.Background()); err == nil {
		t.Fatal("root replacement accepted")
	}
}

// This is an ordinary whole-file kernel lock contender, not a reimplementation
// of QEMU's permission-byte protocol or evidence from a running VM.
func writerLock(file *os.File) error {
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	return unix.FcntlFlock(file.Fd(), unix.F_OFD_SETLK, &lock)
}

func TestTransferredGuardSurvivesSourceCloseAndStaysReadOnly(t *testing.T) {
	root, relative, id := fixture(t)
	s := openFixture(t, root, relative, id)
	first, err := s.DupForTransfer()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := s.DupForTransfer()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for _, file := range []*os.File{first, second} {
		flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
			t.Fatal("transferred file not read-only", flags, err)
		}
		flags, err = unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 {
			t.Fatal("transferred file could leak across exec", flags, err)
		}
		if _, err = file.WriteAt([]byte("bad"), 0); err == nil {
			t.Fatal("read authority escalated to write")
		}
		if err = file.Truncate(0); err == nil {
			t.Fatal("read authority escalated to truncate")
		}
	}
	if _, err = first.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if offset, err := second.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatal("duplicates do not share guarded OFD", offset, err)
	}
	writer, err := os.OpenFile(filepath.Join(root, relative), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, closeReference := range []func() error{s.Close, first.Close} {
		if err = closeReference(); err != nil {
			t.Fatal(err)
		}
		if err = writerLock(writer); !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
			t.Fatal("transferred guard lost lock", err)
		}
	}
	buf := make([]byte, 8)
	if n, err := second.ReadAt(buf, 0); err != nil || n != 8 || string(buf) != "original" {
		t.Fatal("transfer cannot read after Source.Close", n, err)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	if err = writerLock(writer); err != nil {
		t.Fatal("guard leaked after last transferred descriptor closed", err)
	}
	current, err := fileidentity.Observe(filepath.Join(root, relative), false)
	if err != nil || current != id {
		t.Fatal("source bytes or metadata mutated", current, err)
	}
}

func TestBusyOpenReleasesItsPartialResources(t *testing.T) {
	root, relative, id := fixture(t)
	writer, err := os.OpenFile(filepath.Join(root, relative), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = writerLock(writer); err != nil {
		writer.Close()
		t.Fatal(err)
	}
	before := heldDescriptors(t, root, filepath.Join(root, relative))
	refuse(t, root, relative, id)
	if after := heldDescriptors(t, root, filepath.Join(root, relative)); after != before {
		t.Fatal("failed Open leaked source/root descriptors", before, after)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	s := openFixture(t, root, relative, id)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if count := heldDescriptors(t, root, filepath.Join(root, relative)); count != 0 {
		t.Fatal("Close leaked source/root descriptors", count)
	}
	writer, err = os.OpenFile(filepath.Join(root, relative), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err = writerLock(writer); err != nil {
		t.Fatal("failed Open leaked a lock", err)
	}
}

func heldDescriptors(t *testing.T, root, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if target == root || target == path {
			count++
		}
	}
	return count
}

func TestCancellationAndConcurrentClose(t *testing.T) {
	root, relative, id := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s, err := Open(ctx, root, relative, id); s != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled Open accepted", s, err)
	}
	s := openFixture(t, root, relative, id)
	if err := s.Recheck(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled recheck accepted", err)
	}
	if err := s.Recheck(context.Background()); err != nil {
		t.Fatal("canceled observation damaged source", err)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			<-start
			for range 20 {
				buf := make([]byte, 1)
				if _, err := s.ReadAt(buf, 0); err != nil && !errors.Is(err, os.ErrClosed) {
					t.Error(err)
				}
				if err := s.Recheck(context.Background()); err != nil && !errors.Is(err, os.ErrClosed) {
					t.Error(err)
				}
				dup, err := s.DupForTransfer()
				if err != nil && !errors.Is(err, os.ErrClosed) {
					t.Error(err)
				}
				if dup != nil {
					if err := dup.Close(); err != nil {
						t.Error(err)
					}
				}
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			}
		})
	}
	close(start)
	workers.Wait()
	if n, err := s.ReadAt(make([]byte, 1), 0); n != 0 || !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed read accepted", n, err)
	}
	if dup, err := s.DupForTransfer(); dup != nil || !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed transfer accepted", dup, err)
	}
	if err := s.Recheck(context.Background()); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed recheck accepted", err)
	}
	if s.Identity() != id {
		t.Fatal("Close changed immutable identity")
	}
}
