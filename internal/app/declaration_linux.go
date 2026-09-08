//go:build linux

package app

import (
	"os"
	"syscall"
)

// Never block opening a FIFO or follow a substituted final symlink.
func openDeclaration(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
