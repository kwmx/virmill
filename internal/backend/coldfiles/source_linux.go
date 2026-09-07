//go:build linux && amd64

// Package coldfiles pins and guards explicitly selected cold-capture source
// files. It supplies no authorization, VM-state check, or privileged endpoint.
package coldfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
)

// Source retains its root, inode pin, readable file and independent QEMU read
// guard until Close. A transferred duplicate independently retains the guard's
// open file description until the receiver closes its last reference.
type Source struct {
	mu           sync.RWMutex
	root         *os.File
	pin          *os.File
	readable     *os.File
	guard        *os.File
	rootPath     string
	relativePath string
	rootIdentity fileidentity.Identity
	identity     fileidentity.Identity
	closed       bool
	closeErr     error
}

// Open accepts only a complete, previously observed expected identity. The
// caller separately approves the root, member mapping and stopped VM state.
func Open(ctx context.Context, absoluteRoot, canonicalRelativePath string, expected fileidentity.Identity) (_ *Source, err error) {
	if err = checkContext(ctx); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(absoluteRoot) || filepath.Clean(absoluteRoot) != absoluteRoot || strings.ContainsRune(absoluteRoot, 0) ||
		canonicalRelativePath == "." || !filepath.IsLocal(canonicalRelativePath) ||
		filepath.Clean(canonicalRelativePath) != canonicalRelativePath || strings.ContainsRune(canonicalRelativePath, 0) {
		return nil, domain.Fail("INVALID_INPUT", "canonical absolute root and canonical relative source file required")
	}
	if expected.Generation == "" || expected.Modified == "" || expected.Changed == "" || expected.Links != 1 || expected.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, domain.Fail("INVALID_INPUT", "complete expected single-link ordinary-file identity required")
	}
	s := &Source{rootPath: absoluteRoot, relativePath: canonicalRelativePath, identity: expected}
	defer func() {
		if err != nil {
			if closeErr := s.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	s.root, err = openRoot(absoluteRoot)
	if err != nil {
		return nil, err
	}
	s.rootIdentity, err = fileidentity.InspectFile(s.root, true)
	if err != nil {
		return nil, err
	}
	s.pin, err = s.openMember()
	if err != nil {
		return nil, err
	}
	// O_PATH resolves identity without opening a substituted device or FIFO for
	// bytes. Only an exact ordinary inode may reach the deliberate proc reopen.
	if err = s.checkFile(s.pin); err != nil {
		return nil, err
	}
	if err = checkContext(ctx); err != nil {
		return nil, err
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", s.pin.Fd()), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	s.readable = os.NewFile(uintptr(fd), canonicalRelativePath)
	if err = s.checkFile(s.readable); err != nil {
		return nil, err
	}
	s.guard, err = image.AcquireReadGuard(s.readable)
	if err != nil {
		return nil, err
	}
	if err = s.recheck(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func openRoot(path string) (*os.File, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (s *Source) openMember() (*os.File, error) {
	fd, err := unix.Openat2(int(s.root.Fd()), s.relativePath, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), s.relativePath), nil
}

func (s *Source) checkFile(file *os.File) error {
	id, err := fileidentity.InspectFile(file, false)
	if err != nil {
		return err
	}
	if id != s.identity {
		return domain.Fail("SOURCE_CHANGED", "cold source generation or metadata differs from its expected identity")
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return domain.Fail("INVALID_INPUT", "cold source context required")
	}
	return ctx.Err()
}

// Identity returns the immutable expected identity, including after Close.
func (s *Source) Identity() fileidentity.Identity { return s.identity }

// Recheck verifies held identities and current member/root path bindings. Full
// root metadata comparison deliberately also rejects unrelated sibling changes.
func (s *Source) Recheck(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.guard == nil {
		return os.ErrClosed
	}
	return s.recheck(ctx)
}

func (s *Source) recheck(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	for _, file := range []*os.File{s.pin, s.readable, s.guard} {
		if err := s.checkFile(file); err != nil {
			return err
		}
	}
	current, err := s.openMember()
	if err != nil {
		return err
	}
	err = closeObservation(current, s.checkFile(current))
	if err != nil {
		return err
	}
	if err = checkContext(ctx); err != nil {
		return err
	}
	heldRoot, err := fileidentity.InspectFile(s.root, true)
	if err != nil {
		return err
	}
	current, err = openRoot(s.rootPath)
	if err != nil {
		return err
	}
	currentRoot, inspectErr := fileidentity.InspectFile(current, true)
	if err = closeObservation(current, inspectErr); err != nil {
		return err
	}
	if heldRoot != s.rootIdentity || currentRoot != s.rootIdentity {
		return domain.Fail("SOURCE_CHANGED", "cold source root generation, metadata or path binding changed")
	}
	return checkContext(ctx)
}

func closeObservation(file *os.File, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

// ReadAt reads only the retained guarded descriptor. Callers must Recheck before
// and after copying and before committing; advisory locks cannot freeze a
// noncooperative writer. An in-flight regular-file kernel read is not cancelable.
func (s *Source) ReadAt(p []byte, off int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.guard == nil {
		return 0, os.ErrClosed
	}
	return s.guard.ReadAt(p, off)
}

// DupForTransfer rechecks, then atomically duplicates the guarded read-only OFD
// with close-on-exec. It neither reopens the inode nor creates an unguarded OFD.
// The receiver owns the returned file and must close it on every outcome.
func (s *Source) DupForTransfer() (*os.File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.guard == nil {
		return nil, os.ErrClosed
	}
	if err := s.recheck(context.Background()); err != nil {
		return nil, err
	}
	fd, err := unix.FcntlInt(s.guard.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), s.relativePath), nil
}

// Close is concurrent-safe and idempotent. Never explicitly unlock the guard:
// that would also unlock any transferred duplicate of the same OFD.
func (s *Source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	for _, file := range []*os.File{s.guard, s.readable, s.pin, s.root} {
		if file != nil {
			s.closeErr = errors.Join(s.closeErr, file.Close())
		}
	}
	return s.closeErr
}
