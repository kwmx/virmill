//go:build !linux || !amd64

package coldstore

import (
	"context"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
)

func Publish(ctx context.Context, _ string, _ protection.CaptureManifest, _ []Source) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	return Receipt{}, domain.Fail("UNSUPPORTED_CAPABILITY", "cold capture publication requires Linux amd64")
}
func Inspect(ctx context.Context, _, _ string) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	return Receipt{}, domain.Fail("UNSUPPORTED_CAPABILITY", "cold capture inspection requires Linux amd64")
}
