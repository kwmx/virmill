package domain

import "context"

// DiskGrow binds one reviewed disk of a stopped VM to its exact volume and the
// larger capacity requested (ADR 0062). Capacities are bytes.
type DiskGrow struct {
	VM                 ResourceKey `json:"vm"`
	VMFingerprint      string      `json:"vmFingerprint"`
	Disk               RemovalDisk `json:"disk"`
	CapacityBytes      uint64      `json:"capacityBytes"`
	PoolAvailableBytes uint64      `json:"poolAvailableBytes"`
	ResourceIDs        []string    `json:"resourceIDs"`
}

// DiskGrowProvider grows a VM's own volume. CheckDiskGrow reports "before"
// while the VM and volume match the review and "grown" once the same volume
// has the requested capacity; anything else is an error, never a guess.
type DiskGrowProvider interface {
	InspectDiskGrow(ctx context.Context, uri, id, target string, capacityBytes uint64) (DiskGrow, error)
	CheckDiskGrow(context.Context, DiskGrow) (string, error)
	GrowDisk(context.Context, DiskGrow) error
}
