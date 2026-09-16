package domain

import "context"

// DiskAdditionTarget is what inspection found for adding one disk to a stopped
// VM (ADR 0062): where the disk goes, the name its new volume must take, and
// the VM's saved definition before the reviewed insert. That digest tolerates
// the reformatting libvirt applies to a definition it stores, and nothing else.
// There is deliberately no digest of the result: libvirt files a new disk among
// the other disks, so the addition is confirmed by removing the disk at Target
// from the stored definition and finding DefinitionSHA256 again, which proves
// nothing else changed either.
type DiskAdditionTarget struct {
	VM                 ResourceKey `json:"vm"`
	VMFingerprint      string      `json:"vmFingerprint"`
	DefinitionSHA256   string      `json:"definitionSHA256"`
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

// AddedDiskDisposal is what an unresolved disk addition looks like now: whether
// the VM's saved definition names the reviewed disk, whether its volume exists,
// and the dependency digest that deletion is bound to. GraphDigest is empty
// when the disk is referenced or its volume is gone, because nothing can be
// deleted then and a deletion-grade graph is not required.
type AddedDiskDisposal struct {
	Referenced  bool     `json:"referenced"`
	VolumeState string   `json:"volumeState"` // present, absent; unknown is an error
	Disk        *Disk    `json:"disk,omitempty"`
	GraphDigest string   `json:"graphDigest,omitempty"`
	ResourceIDs []string `json:"resourceIDs"`
}

// Disk is one observed volume identity, as selected-disk removal records it.
type Disk = RemovalDisk

// DiskAdditionDisposalBackend closes an unresolved disk addition. It never
// uploads, never defines and never deletes a volume the definition still
// names: a referenced disk can only be accepted, never removed here.
type DiskAdditionDisposalBackend interface {
	InspectAddedDiskDisposal(context.Context, DiskAdditionPlan) (AddedDiskDisposal, error)
	DeleteUnreferencedDisk(context.Context, DiskAdditionPlan, AddedDiskDisposal) error
}
