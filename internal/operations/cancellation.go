package operations

import (
	"context"
	"errors"
	"virmill.local/core/internal/store"
)

// ErrCanceledSafely is returned only after a handler proves that no published
// effect remains. Context cancellation alone never carries that guarantee.
var ErrCanceledSafely = errors.New("operation canceled at a verified safe boundary")

func CancellationRequested(ctx context.Context, s *store.Store) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	id := OperationID(ctx)
	if id == "" {
		return false, nil
	}
	j, err := s.Job(id)
	return j.CancelRequested, err
}

func Note(ctx context.Context, s *store.Store, message string) error {
	id := OperationID(ctx)
	if id == "" {
		return errors.New("operation context missing")
	}
	j, err := s.Job(id)
	if err != nil {
		return err
	}
	return s.Update(j, message)
}
