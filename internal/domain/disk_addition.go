package domain

import "context"

// DiskAdditionTarget is what inspection found for adding one disk to a stopped
// VM (ADR 0062): where the disk goes, the name its new volume must take, and
// the VM's saved definition before and after the reviewed insert.
type DiskAdditionTarget struct {
	VM                 ResourceKey `json:"vm"`
	VMFingerprint      string      `json:"vmFingerprint"`
	DefinitionSHA256   string      `json:"definitionSHA256"`
	AfterXMLSHA256     string      `json:"afterXMLSHA256"`
	PoolID             string      `json:"poolID"`
	PoolName           string      `json:"poolName"`
	VolumeName         string      `json:"volumeName"`
	Bus                string      `json:"bus"`
	Target             string      `json:"target"`
	Unit               int         `json:"unit"`
	PoolAvailableBytes uint64      `json:"poolAvailableBytes"`
	ResourceIDs        []string    `json:"resourceIDs"`
}

// DiskAdditionPlan adds the blank volume's exact identity to that target. Its
// bytes are written through CreationBackend, which verifies them by read-back.
type DiskAdditionPlan struct {
	Target DiskAdditionTarget `json:"target"`
	Volume VolumeIntent       `json:"volume"`
}

// DiskAdditionBackend never writes volume bytes and never changes anything but
// the reviewed disk. CheckDiskAddition reports "before" while nothing exists
// yet, "volume-present" once the new volume is there, and "added" once the
// saved definition is the reviewed result; anything else is an error.
type DiskAdditionBackend interface {
	InspectDiskAddition(ctx context.Context, uri, id, bus string) (DiskAdditionTarget, error)
	CheckDiskAddition(context.Context, DiskAdditionPlan) (string, error)
	DefineAddedDisk(context.Context, DiskAdditionPlan) error
}
