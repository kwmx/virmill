//go:build linux

// Package fileidentity pins ordinary local filesystem objects before adapters
// inspect or act on them. Identities are strings to preserve statx precision in
// canonical JSON; a path or a libvirt file-volume key is not a generation ID.
package fileidentity

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"virmill.local/core/internal/domain"
)

type Identity struct {
	Generation string `json:"generation"`
	Size       uint64 `json:"size"`
	Modified   string `json:"modified"`
	Changed    string `json:"changed"`
	Links      uint32 `json:"links"`
	Mode       uint16 `json:"mode"`
}

func InspectFile(f *os.File, directory bool) (Identity, error) {
	var out Identity
	var st unix.Statx_t
	if err := unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_STATX_FORCE_SYNC, unix.STATX_BASIC_STATS|unix.STATX_BTIME, &st); err != nil {
		return out, err
	}
	required := uint32(unix.STATX_TYPE | unix.STATX_MODE | unix.STATX_NLINK | unix.STATX_INO | unix.STATX_SIZE | unix.STATX_MTIME | unix.STATX_CTIME | unix.STATX_BTIME)
	if st.Mask&required != required {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "filesystem omitted fields needed for a complete identity observation")
	}
	want := uint16(unix.S_IFREG)
	if directory {
		want = unix.S_IFDIR
	}
	if st.Mode&unix.S_IFMT != want || (!directory && st.Nlink != 1) {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "single-link ordinary file or exact directory required for managed filesystem identity")
	}
	if st.Mask&unix.STATX_BTIME == 0 || st.Btime.Sec <= 0 {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "filesystem does not expose a stable birth identity; do not infer ownership from its path")
	}
	out.Generation = fmt.Sprintf("linux-statx-v1:%d:%d:%d:%d:%09d", st.Dev_major, st.Dev_minor, st.Ino, st.Btime.Sec, st.Btime.Nsec)
	out.Size, out.Links, out.Mode = st.Size, st.Nlink, st.Mode
	out.Modified = fmt.Sprintf("%d:%09d", st.Mtime.Sec, st.Mtime.Nsec)
	out.Changed = fmt.Sprintf("%d:%09d", st.Ctime.Sec, st.Ctime.Nsec)
	return out, nil
}

func Open(path string, directory, readable bool) (*os.File, Identity, error) {
	var empty Identity
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, empty, domain.Fail("INVALID_INPUT", "canonical absolute filesystem path required")
	}
	flags := uint64(unix.O_PATH | unix.O_CLOEXEC)
	if readable {
		flags = unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC
	}
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, empty, err
	}
	f := os.NewFile(uintptr(fd), path)
	identity, err := InspectFile(f, directory)
	if err != nil {
		f.Close()
		return nil, empty, err
	}
	return f, identity, nil
}

func Observe(path string, directory bool) (Identity, error) {
	f, identity, err := Open(path, directory, false)
	if err != nil {
		return identity, err
	}
	return identity, f.Close()
}

// Object names the inode a path resolves to. Unlike Generation it carries no
// birth time: it answers only whether two names reach the same object now.
type Object struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Inode uint64 `json:"inode"`
}

// GenerationObject extracts the device and inode from a Generation string.
func GenerationObject(generation string) (Object, error) {
	var out Object
	var sec int64
	var nsec uint32
	n, err := fmt.Sscanf(generation, "linux-statx-v1:%d:%d:%d:%d:%d", &out.Major, &out.Minor, &out.Inode, &sec, &nsec)
	if err != nil || n != 5 || fmt.Sprintf("linux-statx-v1:%d:%d:%d:%d:%09d", out.Major, out.Minor, out.Inode, sec, nsec) != generation {
		return Object{}, domain.Fail("INVALID_INPUT", "unrecognized file generation")
	}
	return out, nil
}

// Resolve follows symlinks, "..", and bind mounts the way QEMU's open does, and
// reports the object and file type found. Absence is returned as the raw errno
// so callers can tell ENOENT from an unreadable path.
func Resolve(path string) (Object, uint16, error) {
	var st unix.Statx_t
	if !filepath.IsAbs(path) {
		return Object{}, 0, domain.Fail("INVALID_INPUT", "absolute filesystem path required")
	}
	if err := unix.Statx(unix.AT_FDCWD, path, unix.AT_STATX_FORCE_SYNC, unix.STATX_TYPE|unix.STATX_INO, &st); err != nil {
		return Object{}, 0, err
	}
	if st.Mask&(unix.STATX_TYPE|unix.STATX_INO) != unix.STATX_TYPE|unix.STATX_INO {
		return Object{}, 0, domain.Fail("UNSUPPORTED_CAPABILITY", "filesystem omitted the inode of a referenced path")
	}
	return Object{st.Dev_major, st.Dev_minor, st.Ino}, st.Mode & unix.S_IFMT, nil
}

// FileObject reports the object an already open file refers to.
func FileObject(f *os.File) (Object, uint16, error) {
	var st unix.Statx_t
	if err := unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_STATX_FORCE_SYNC, unix.STATX_TYPE|unix.STATX_INO, &st); err != nil {
		return Object{}, 0, err
	}
	if st.Mask&(unix.STATX_TYPE|unix.STATX_INO) != unix.STATX_TYPE|unix.STATX_INO {
		return Object{}, 0, domain.Fail("UNSUPPORTED_CAPABILITY", "filesystem omitted the inode of a referenced file")
	}
	return Object{st.Dev_major, st.Dev_minor, st.Ino}, st.Mode & unix.S_IFMT, nil
}
