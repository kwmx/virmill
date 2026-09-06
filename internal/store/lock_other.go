//go:build !linux

package store

import (
	"errors"
	"os"
)

func lockDatabase(path string) (*os.File, error) {
	return nil, errors.New("local coordinator is supported on Linux only")
}
