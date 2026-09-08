//go:build linux

package networksettings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func sameFile(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode &&
		a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink &&
		a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func readFile(ctx context.Context, configDir string) ([]byte, bool, error) {
	// Parent directory symlinks follow normal config-directory resolution. The
	// leaf never follows a symlink; a held O_PATH inode prevents FIFO/device I/O
	// even if another process replaces the directory entry while we inspect it.
	dir, err := unix.Open(configDir, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer unix.Close(dir)
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	path, err := unix.Openat(dir, filename, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer unix.Close(path)
	var before unix.Stat_t
	if err := unix.Fstat(path, &before); err != nil {
		return nil, true, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, true, errors.New("network allocation settings are not a regular file")
	}
	if before.Size < 0 || before.Size > maxBytes {
		return nil, true, errors.New("network allocation settings exceed 64 KiB")
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	// /proc/self/fd is used only with this live, private descriptor, never a
	// caller-supplied descriptor or path. Refuse if procfs is unavailable.
	fd, err := unix.Open("/proc/self/fd/"+strconv.Itoa(path), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, true, err
	}
	f := os.NewFile(uintptr(fd), filename)
	defer f.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return nil, true, err
	}
	if !sameFile(before, opened) {
		return nil, true, errors.New("network allocation file changed before read")
	}
	raw, err := readBounded(ctx, f)
	if err != nil {
		return nil, true, err
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, true, err
	}
	if err := unix.Fstatat(dir, filename, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, true, err
	}
	if !sameFile(before, after) || !sameFile(after, named) || int64(len(raw)) != after.Size {
		return nil, true, errors.New("network allocation file changed during read")
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	return raw, true, nil
}

// Reads from a regular file remain subject to filesystem syscall latency; no
// goroutine is abandoned to pretend that a blocked filesystem call was canceled.
func readBounded(ctx context.Context, r io.Reader) ([]byte, error) {
	data := make([]byte, 0, maxBytes+1)
	var buf [4096]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := r.Read(buf[:min(len(buf), maxBytes+1-len(data))])
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if len(data) > maxBytes {
			return nil, errors.New("network allocation settings exceed 64 KiB")
		}
		if cause := ctx.Err(); cause != nil {
			return nil, cause
		}
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read settings bytes: %w", err)
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
}
