package domain

import "context"

type CleanupCandidate struct {
	Intent    VolumeIntent   `json:"intent"`
	Allocated *CreatedVolume `json:"allocated"`
}
type CleanupVolume struct {
	Candidate   CleanupCandidate `json:"candidate"`
	State       string           `json:"state"` // present, absent, or unknown; unknown is never deletable
	Fingerprint string           `json:"fingerprint,omitempty"`
	Reason      string           `json:"reason,omitempty"`
}
type CreationCleanup struct {
	Volumes     []CleanupVolume `json:"volumes"`
	ResourceIDs []string        `json:"resourceIDs"`
	GraphDigest string          `json:"graphDigest,omitempty"`
}

// CreationCleanupBackend may delete only the specifically reviewed newly
// allocated generations. Implementations repeat the complete dependency check
// immediately before deletion. They never undefine a VM or remove source media.
type CreationCleanupBackend interface {
	InspectCreationCleanup(context.Context, string, CreationSpec, []CleanupCandidate, bool) (CreationCleanup, error)
	DeleteCreationVolume(context.Context, string, CreationSpec, []CleanupCandidate, CreationCleanup, int) error
}
