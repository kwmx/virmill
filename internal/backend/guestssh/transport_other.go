//go:build !linux

package guestssh

import "context"

func unsupported(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return failure("UNSUPPORTED_CAPABILITY", "fixed OpenSSH transport requires Linux descriptor binding and sealed memory files")
}
func (Tool) Identity(ctx context.Context) (Identity, error) { return Identity{}, unsupported(ctx) }
func (Tool) InspectTarget(ctx context.Context, _ Target) (TargetIdentity, error) {
	return TargetIdentity{}, unsupported(ctx)
}
func (Tool) Run(ctx context.Context, _ Target, _ Script) (Result, error) {
	return Result{}, unsupported(ctx)
}
