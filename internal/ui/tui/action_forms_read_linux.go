//go:build linux

package tui

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

func readActionParameters(path string) ([]byte, error) {
	// Pin type and generation before a readable open. Reopening the held inode
	// prevents a path replacement FIFO/device from blocking or being opened.
	pin, before, err := fileidentity.Open(path, false, false)
	if err != nil {
		return nil, err
	}
	defer pin.Close()
	if before.Size == 0 || before.Size > actionParametersLimit {
		return nil, domain.Fail("INVALID_INPUT", "parameter file exceeds the size bound or is empty")
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", pin.Fd()), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	opened, err := fileidentity.InspectFile(file, false)
	if err != nil || opened != before {
		return nil, domain.Fail("SOURCE_CHANGED", "parameter file changed before reading")
	}
	data, err := io.ReadAll(io.LimitReader(file, actionParametersLimit+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) != before.Size {
		return nil, domain.Fail("SOURCE_CHANGED", "parameter file size changed while reading")
	}
	after, err := fileidentity.InspectFile(file, false)
	if err != nil || after != before {
		return nil, domain.Fail("SOURCE_CHANGED", "parameter file changed while reading")
	}
	current, err := fileidentity.Observe(path, false)
	if err != nil || current != before {
		return nil, domain.Fail("SOURCE_CHANGED", "parameter file path changed while reading")
	}
	return data, nil
}
