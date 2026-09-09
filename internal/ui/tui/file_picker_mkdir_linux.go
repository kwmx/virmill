//go:build linux

package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// pickerMakeFolder binds the write to the directory observed by the explorer.
// Each ancestor is opened relative to a held descriptor without following links;
// replacing the displayed path cannot redirect mkdirat to a different folder.
// Once mkdir succeeds, failures never remove or replace the created entry.
func pickerMakeFolder(path, name string, expected os.FileInfo) (bool, error) {
	if !pickerFolderName(name) || !filepath.IsAbs(path) || filepath.Clean(path) != path || !pickerName(path) || expected == nil || !expected.IsDir() {
		return false, fmt.Errorf("Choose a folder and enter a single new folder name.")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, fmt.Errorf("Cannot open the parent folder: %w", err)
	}
	if path != "/" {
		for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
			next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			unix.Close(fd)
			if err != nil {
				return false, fmt.Errorf("Choose an accessible folder without symbolic links: %w", err)
			}
			fd = next
		}
	}
	parent := os.NewFile(uintptr(fd), path)
	defer parent.Close()
	info, err := parent.Stat()
	if err != nil || !os.SameFile(expected, info) {
		return false, fmt.Errorf("The parent folder changed. Reopen it before creating a folder.")
	}
	if err := unix.Mkdirat(fd, name, 0700); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return false, fmt.Errorf("That name already exists. Choose a different folder name.")
		}
		return false, fmt.Errorf("Cannot create a folder here. Check permissions and free space: %w", err)
	}
	if err := unix.Fsync(fd); err != nil {
		return true, fmt.Errorf("Folder created, but its saved state could not be confirmed: %w", err)
	}
	return true, nil
}
