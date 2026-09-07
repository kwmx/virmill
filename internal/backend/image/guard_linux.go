//go:build linux && amd64

package image

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
)

// SourceLockProtocol identifies QEMU's file-posix permission-byte protocol.
// Qualified against QEMU 10.2.2 block/file-posix.c and block/block-common.h.
// QEMU metadata readers alone can share raw images with writers. We additionally
// refuse WRITE, WRITE_UNCHANGED and RESIZE for the whole source-reading lifetime.
// This is cooperative locking; locking=off and non-QEMU writers remain outside
// this contract and require the owner's explicit offline-source assertion.
const SourceLockProtocol = "qemu-file-ofd-permissions-v1"

// AcquireReadGuard returns an independent, read-only open file description that
// must remain open until all readers exit. Closing it releases only its locks.
// No source bytes are written and no lock is changed on the caller's descriptor.
func AcquireReadGuard(source *os.File) (*os.File, error) {
	if source == nil {
		return nil, domain.Fail("INVALID_INPUT", "held source file required")
	}
	st, err := source.Stat()
	flags, flagErr := unix.FcntlInt(source.Fd(), unix.F_GETFL, 0)
	if err != nil || flagErr != nil || !st.Mode().IsRegular() || flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_PATH != 0 {
		return nil, domain.Fail("INVALID_INPUT", "read guard needs a held read-only ordinary file")
	}
	// This proc magic link deliberately reopens the held inode, never a user path.
	f, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", source.Fd()), os.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	good := false
	defer func() {
		if !good {
			f.Close()
		}
	}()
	current, err := f.Stat()
	if err != nil || !os.SameFile(st, current) {
		return nil, domain.Fail("SOURCE_CHANGED", "held source changed while taking its read guard")
	}
	// Apply before checking, as QEMU does: a concurrent writer either observes
	// our nonsharing locks, or we observe its requested-permission locks.
	for _, region := range []struct{ start, length int64 }{{100, 1}, {201, 3}} {
		lock := unix.Flock_t{Type: unix.F_RDLCK, Whence: 0, Start: region.start, Len: region.length}
		if err = unix.FcntlFlock(f.Fd(), unix.F_OFD_SETLK, &lock); err != nil {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", fmt.Sprintf("source OFD read guard unavailable: %v", err))
		}
	}
	for _, region := range []struct{ start, length int64 }{{200, 1}, {101, 3}} {
		lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: region.start, Len: region.length}
		if err = unix.FcntlFlock(f.Fd(), unix.F_OFD_GETLK, &lock); err != nil {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", fmt.Sprintf("source OFD permission check unavailable: %v", err))
		}
		if lock.Type != unix.F_UNLCK {
			return nil, domain.Fail("RESOURCE_BUSY", "source has a conflicting QEMU reader/writer permission; shut down its owner before preparing it")
		}
	}
	good = true
	return f, nil
}

func guardedSources(sources []platform.DiskSourceFile) ([]platform.DiskSourceFile, func(), error) {
	if len(sources) < 1 || len(sources) > 10000 {
		return nil, nil, domain.Fail("INVALID_INPUT", "bounded selected source files required")
	}
	guarded := []platform.DiskSourceFile{}
	closeAll := func() {
		for _, source := range guarded {
			source.File.Close()
		}
	}
	for _, source := range sources {
		f, err := AcquireReadGuard(source.File)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		guarded = append(guarded, platform.DiskSourceFile{Path: source.Path, File: f})
	}
	return guarded, closeAll, nil
}
