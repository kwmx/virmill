//go:build !linux

package protection

import (
	"context"
	"virmill.local/core/internal/domain"
)

func verifyManifestMembers(ctx context.Context, _ string, _ []Member) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return domain.Fail("UNSUPPORTED_CAPABILITY", "recovery member verification requires the Linux no-symlink file identity adapter")
}

func readManifestDocument(ctx context.Context, _ string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "recovery manifest reading requires the Linux no-symlink file identity adapter")
}
