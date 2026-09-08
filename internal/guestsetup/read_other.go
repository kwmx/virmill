//go:build !linux

package guestsetup

import (
	"context"
	"virmill.local/core/internal/domain"
)

func readRecipe(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "safe guest recipe file observation requires Linux")
}
