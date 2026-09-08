//go:build !linux || !amd64

package restic

import (
	"context"
	"virmill.local/core/internal/domain"
)

func unsupported() error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", "local restic adapter requires Linux amd64 descriptor binding")
}
func (Tool) Identity(context.Context) (Identity, error) { return Identity{}, unsupported() }
func (Tool) Init(context.Context, Repository) error     { return unsupported() }
func (Tool) Backup(context.Context, Repository, string, string) (Snapshot, error) {
	return Snapshot{}, unsupported()
}
func (Tool) Observe(context.Context, Repository, string) ([]Snapshot, error) {
	return nil, unsupported()
}
func (Tool) Restore(context.Context, Repository, string, string) error { return unsupported() }
func (Tool) Check(context.Context, Repository) error                   { return unsupported() }
