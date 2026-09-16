//go:build linux

package tui

import (
	"os"
	"path/filepath"
	"syscall"
)

// freeBytes is the space available to this user at path, or at its nearest
// existing parent when the folder is not made yet.
func freeBytes(path string) (uint64, bool) {
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			var st syscall.Statfs_t
			if syscall.Statfs(dir, &st) != nil || st.Bsize <= 0 {
				return 0, false
			}
			return st.Bavail * uint64(st.Bsize), true
		}
		if dir == filepath.Dir(dir) {
			return 0, false
		}
	}
}
