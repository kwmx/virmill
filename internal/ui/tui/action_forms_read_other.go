//go:build !linux

package tui

import "virmill.local/core/internal/domain"

func readActionParameters(string) ([]byte, error) {
	return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "safe catalog parameter-file reading requires Linux")
}
