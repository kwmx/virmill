package domain

import "context"

// RemovalDisk binds a selected writable disk to its exact native volume and
// filesystem generation. Targets are guest disk names (for example vda).
type RemovalDisk struct {
	Target         string `json:"target"`
	Path           string `json:"path"`
	PoolID         string `json:"poolID"`
	VolumeName     string `json:"volumeName"`
	VolumeKey      string `json:"volumeKey"`
	Generation     string `json:"generation"`
	Fingerprint    string `json:"fingerprint"`
	Format         string `json:"format"`
	CapacityBytes  uint64 `json:"capacityBytes"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
}

type DiskRemoval struct {
	Definition  DefinitionRemoval `json:"definition"`
	Disks       []RemovalDisk     `json:"disks"`
	ResourceIDs []string          `json:"resourceIDs"`
	GraphDigest string            `json:"graphDigest"`
}

// DiskRemovalProvider never deletes source media, backing parents or backups.
// Check repeats native dependency and generation checks, returning present/absent
// for each selected volume. Unknown state is an error, never inferred absence.
type DiskRemovalProvider interface {
	InspectDiskRemoval(context.Context, string, string, []string) (DiskRemoval, error)
	CheckDiskRemoval(context.Context, DiskRemoval, bool) ([]string, error)
	DeleteRemovalDisk(context.Context, DiskRemoval, int) error
}
