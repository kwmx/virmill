package domain

import "context"

// DiskMove binds one reviewed disk of a stopped VM to the copy it will be
// pointed at in another pool (ADR 0062). The source disk is identified exactly
// as selected-disk removal identifies it, and the destination is a new volume
// name that must be absent when the plan is applied. Capacities are bytes.
//
// KeepOldCopy records the reviewed decision about the original volume: when it
// is false the old volume is deleted after the definition names the copy, under
// the same reference checks selected-disk removal uses, and the plan carries the
// data-loss acknowledgement for it. Peak space is two copies either way.
type DiskMove struct {
	VM               ResourceKey `json:"vm"`
	VMFingerprint    string      `json:"vmFingerprint"`
	DefinitionSHA256 string      `json:"definitionSHA256"`
	Disk             RemovalDisk `json:"disk"`
	SourcePoolName   string      `json:"sourcePoolName"`
	PoolID           string      `json:"poolID"`
	PoolName         string      `json:"poolName"`
	VolumeName       string      `json:"volumeName"`
	// VolumePath is where the copy will be registered in the destination pool.
	// It is bound at review because a definition names a disk either by pool
	// volume or by file path, and asking whether a definition uses the copy
	// needs the exact path: an empty one matches every pool-volume disk.
	VolumePath string `json:"volumePath"`
	// CopyBytes is the most the copy can hold, which is the disk's own virtual
	// size. There is deliberately no content digest here: reviewing a move
	// never reads the disk. The bytes are hashed as they are copied and the
	// copy is read back against that digest in the same operation, so a disk
	// that changed since the review is caught when it is copied rather than
	// making every review as slow as reading the whole disk.
	CopyBytes                 uint64   `json:"copyBytes"`
	DestinationAvailableBytes uint64   `json:"destinationAvailableBytes"`
	KeepOldCopy               bool     `json:"keepOldCopy"`
	ResourceIDs               []string `json:"resourceIDs"`
}

// DiskMoveProvider copies one volume between pools through libvirt streams and
// points the disk at the copy. It never converts as root, never writes over an
// existing name and never deletes a volume the definition still names.
//
// CheckDiskMove reports "before" while the definition still names the original
// and no copy exists, "copied" once the verified copy is there, "retargeted"
// once the saved definition names the copy, and "old-deleted" once the original
// is gone; anything else is an error, never a guess. Deletion is never replayed
// during recovery: a move whose copy is verified and named is complete whether
// or not the original was removed.
type DiskMoveProvider interface {
	InspectDiskMove(ctx context.Context, uri, id, target, pool string, keepOldCopy bool) (DiskMove, error)
	CheckDiskMove(context.Context, DiskMove) (string, error)
	CopyDiskVolume(context.Context, DiskMove) error
	DefineMovedDisk(context.Context, DiskMove) error
	DeleteOldVolume(context.Context, DiskMove) error
}
