//go:build !linux

package app

import (
	"os"
	"virmill.local/core/internal/domain"
)

func openDeclaration(path string) (*os.File, error) {
	s, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !s.Mode().IsRegular() {
		return nil, domain.Fail("INVALID_INPUT", "declaration must be a regular file")
	}
	return os.Open(path)
}
