//go:build !linux

package networksettings

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"virmill.local/core/internal/domain"
)

func readFile(ctx context.Context, configDir string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	_, err := os.Lstat(filepath.Join(configDir, filename))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return nil, true, domain.Fail("UNSUPPORTED_CAPABILITY", "safe allocation settings reads are implemented only on Linux")
}
