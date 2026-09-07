package domain

import "context"

// ManagedFileVolume binds one explicit stopped-VM disk target to an observed
// active file pool. Access metadata remains in the platform adapter.
type ManagedFileVolume struct {
	VMID            string `json:"vmID"`
	VMFingerprint   string `json:"vmFingerprint"`
	DiskTarget      string `json:"diskTarget"`
	PoolID          string `json:"poolID"`
	PoolFingerprint string `json:"poolFingerprint"`
	VolumeName      string `json:"volumeName"`
	VolumeKey       string `json:"volumeKey"`
	Path            string `json:"path"`
}

type ManagedFileAccessBackend interface {
	InspectManagedFileVolume(context.Context, string, string, string) (ManagedFileVolume, error)
}
