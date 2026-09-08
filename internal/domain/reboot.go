package domain

import (
	"context"
	"time"
)

// RebootObservation records a selected-domain reboot event, not guest readiness.
type RebootObservation struct {
	Resource   ResourceKey `json:"resource"`
	ObservedAt time.Time   `json:"observedAt"`
	Evidence   string      `json:"evidence"`
}

// RebootProvider issues one graceful reboot and requires a subsequent event.
// An uncertain request must never be retried based only on a running state.
type RebootProvider interface {
	Reboot(context.Context, string, string, string) (RebootObservation, error)
}
