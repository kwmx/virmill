package operations

import (
	"context"
	"errors"
	"virmill.local/core/internal/store"
)

// ErrCanceledSafely is returned only after a handler proves that no published
// effect remains. Context cancellation alone never carries that guarantee.
var ErrCanceledSafely = errors.New("operation canceled at a verified safe boundary")

// ErrNotDone marks a step whose request did not take effect, with the resource
// observed unchanged and nothing unsafe left in flight, so a retry or another
// request needs no recovery. The engine fails a first step that returns it and
// releases the job's locks. Wrap the user-facing error with NotDone to keep its
// code and message.
var ErrNotDone = errors.New("step did not take effect; nothing unsafe remains in flight")

type notDone struct{ err error }

func (n notDone) Error() string   { return n.err.Error() }
func (n notDone) Unwrap() []error { return []error{n.err, ErrNotDone} }

// NotDone wraps err as ErrNotDone.
func NotDone(err error) error { return notDone{err} }

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
