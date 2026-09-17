package domain

import "context"

// CloneDiskCopy is one writable disk of the original and the new volume its copy
// takes (ADR 0066). The source is identified exactly as Move identifies it.
type CloneDiskCopy struct {
	Disk           RemovalDisk `json:"disk"`
	SourcePoolName string      `json:"sourcePoolName"`
	PoolID         string      `json:"poolID"`
	PoolName       string      `json:"poolName"`
	VolumeName     string      `json:"volumeName"`
	VolumePath     string      `json:"volumePath"`
	// CopyBytes is the most the copy may hold: the disk's virtual size plus the
	// qcow2 overhead creation allows, as for Move.
	CopyBytes uint64 `json:"copyBytes"`
}

// ClonePoolSpace is what the copies need in one destination pool and what that
// pool reported at review.
type ClonePoolSpace struct {
	PoolName       string `json:"poolName"`
	NeedBytes      uint64 `json:"needBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

// VMClone binds a reviewed full clone: the stopped original, the clone's new
// UUID and name chosen at review, every writable disk's copy, the read-only
// media the clone shares, and whether firmware variables are recreated from
// their template. There is no content digest: bytes are hashed as they are
// copied and each copy is read back against that digest, as Move does.
type VMClone struct {
	Source            ResourceKey      `json:"source"`
	SourceName        string           `json:"sourceName"`
	SourceFingerprint string           `json:"sourceFingerprint"`
	DefinitionSHA256  string           `json:"definitionSHA256"`
	UUID              string           `json:"uuid"`
	Name              string           `json:"name"`
	Disks             []CloneDiskCopy  `json:"disks"`
	SharedMedia       []string         `json:"sharedMedia"`
	FreshNVRAM        bool             `json:"freshNVRAM"`
	Pools             []ClonePoolSpace `json:"pools"`
	ResourceIDs       []string         `json:"resourceIDs"`
}

// CloneProvider copies an original's writable disks and defines the clone. It
// never changes the original, never writes over an existing volume name and
// never defines a second clone.
//
// CheckClone reports how many copies exist and are complete, and "before",
// "copying", "copied" or "defined"; anything else is an error, never a guess.
// A copy is only counted once the receipt records it verified, so the provider
// also reports which copy names exist at all.
type CloneProvider interface {
	InspectClone(ctx context.Context, uri, id, name, pool, uuid string) (VMClone, error)
	CheckClone(context.Context, VMClone) (state string, present []bool, err error)
	CopyCloneDisk(ctx context.Context, clone VMClone, index int) error
	DefineClone(context.Context, VMClone) error
}
