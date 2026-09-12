//go:build linux

package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const importSettingsExportLimit = 1 << 20

// ExportJSONDocument saves an explicitly requested, validated JSON document.
// The caller supplies only the ordinary settings object, excluding credentials.
// Every directory is opened without following links, and the held destination
// directory is used for both staging and exclusive publication.
func ExportJSONDocument(path string, input map[string]any) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, 0) {
		return errors.New("choose a full file path without . or .. components")
	}
	if input == nil {
		return errors.New("settings are empty; complete the form before saving")
	}
	var encoded settingsExportBuffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(input); err != nil {
		return fmt.Errorf("cannot encode settings: %w", err)
	}
	dir, err := openSettingsExportDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	// Anonymous staging avoids a mutable temporary name entirely. Closing this
	// descriptor cleans up an unpublished export; no other file can be removed.
	fd, err := unix.Openat(dir, ".", unix.O_WRONLY|unix.O_TMPFILE|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("this folder cannot safely stage settings; choose another local folder: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("cannot set private settings-file permissions: %w", err)
	}
	if _, err := file.Write(encoded.Bytes()); err != nil {
		return fmt.Errorf("cannot write settings: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("cannot safely save settings: %w", err)
	}
	// /proc/self/fd names the held inode and permits unprivileged publication
	// of O_TMPFILE without CAP_DAC_READ_SEARCH. The destination is never followed
	// or replaced; linkat fails if any directory entry already occupies it.
	if err := unix.Linkat(unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", fd), dir, filepath.Base(path), unix.AT_SYMLINK_FOLLOW); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return errors.New("a file already exists at that path; choose a new filename")
		}
		return fmt.Errorf("cannot publish settings file: %w", err)
	}
	// Once published, never remove the user's file, even when durability cannot
	// be confirmed. The error distinguishes that case from a failed export.
	if err := unix.Fsync(dir); err != nil {
		return fmt.Errorf("settings were saved, but folder durability could not be confirmed: %w", err)
	}
	return nil
}

func openSettingsExportDirectory(path string) (int, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("cannot open settings folder: %w", err)
	}
	if path == "/" {
		return fd, nil
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return -1, fmt.Errorf("choose an existing folder with no symbolic links: %w", err)
		}
		fd = next
	}
	return fd, nil
}

type settingsExportBuffer struct{ bytes.Buffer }

func (b *settingsExportBuffer) Write(p []byte) (int, error) {
	if len(p) > importSettingsExportLimit-b.Len() {
		return 0, errors.New("settings exceed the 1 MiB size limit")
	}
	return b.Buffer.Write(p)
}
