//go:build linux

package guestsetup

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func readRecipe(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !safeText(path, 4096, false) {
		return nil, invalid("recipe path must be canonical and absolute")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, invalid("recipe directory unavailable")
	}
	for _, part := range parts[:len(parts)-1] {
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(fd)
		if e != nil {
			return nil, invalid("recipe ancestors must be ordinary directories without symlinks")
		}
		fd = next
	}
	defer syscall.Close(fd)
	fileFD, err := syscall.Openat(fd, parts[len(parts)-1], unix.O_PATH|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, invalid("selected recipe is unavailable or unsafe")
	}
	defer syscall.Close(fileFD)
	var before, after, named syscall.Stat_t
	if syscall.Fstat(fileFD, &before) != nil || before.Mode&syscall.S_IFMT != syscall.S_IFREG || before.Nlink != 1 || before.Size < 1 || before.Size > recipeLimit {
		return nil, invalid("recipe must be a bounded ordinary single-link file")
	}
	readFD, err := syscall.Open(fmt.Sprintf("/proc/self/fd/%d", fileFD), syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, invalid("held recipe file cannot be read")
	}
	f := os.NewFile(uintptr(readFD), "selected-guest-recipe")
	defer f.Close()
	if syscall.Fstat(readFD, &after) != nil || !sameRecipeFile(before, after) {
		return nil, invalid("held recipe changed before reading")
	}
	data, err := io.ReadAll(io.LimitReader(f, recipeLimit+1))
	if err != nil || len(data) > recipeLimit {
		return nil, invalid("recipe read failed or exceeded its bound")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if syscall.Fstat(fileFD, &after) != nil {
		return nil, invalid("recipe final metadata unavailable")
	}
	// Reopen the selected name without following it and compare the held inode.
	namedFD, err := syscall.Openat(fd, parts[len(parts)-1], unix.O_PATH|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, invalid("recipe changed while being frozen")
	}
	defer syscall.Close(namedFD)
	if syscall.Fstat(namedFD, &named) != nil || !sameRecipeFile(before, after) || !sameRecipeFile(before, named) || int64(len(data)) != before.Size {
		return nil, invalid("recipe changed while being frozen")
	}
	return data, nil
}
func sameRecipeFile(a, b syscall.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Nlink == b.Nlink && a.Uid == b.Uid && a.Gid == b.Gid && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
