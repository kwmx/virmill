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
