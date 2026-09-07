package domain

// ColdSourceLayout enumerates configuration-declared recovery dependencies.
// Files are unresolved authority: only an independently checked native adapter
// may bind them to held source generations for a capture operation. The complete
// original XML remains authoritative and must be captured separately.
type ColdSourceLayout struct {
	State        ColdStateLayout  `json:"state"`
	Architecture string           `json:"architecture"`
	Machine      string           `json:"machine"`
	Disks        []ColdDiskSource `json:"disks"`
	External     []ColdDependency `json:"externalDependencies"`
}

type ColdDiskSource struct {
	Target   string              `json:"target"`
	Device   string              `json:"device"`
	ReadOnly bool                `json:"readOnly"`
	Empty    bool                `json:"empty"`
	Bus      string              `json:"bus"`
	Source   ColdStorageSource   `json:"source"`
	Backing  []ColdStorageSource `json:"backing"`
	// This records an explicit empty backingStore terminator in XML only.
	// It does not prove the disk image's actual backing graph is complete.
	BackingTerminated bool `json:"backingTerminated"`
}

// ColdStorageSource preserves explicit native source identity without following
// paths or claiming a complete backing graph. Backing entries are in XML order;
// image metadata and native consumers still require independent reconciliation.
type ColdStorageSource struct {
	Type   string `json:"type"`
	Format string `json:"format"`
	File   string `json:"file"`
	Pool   string `json:"pool"`
	Volume string `json:"volume"`
}

// ColdDependency is an explicit unresolved requirement, never a silent capture
// exclusion. Target contains only a bounded kind/location identifier, not secret
// values, URI credentials, arbitrary extension content or filesystem bytes.
type ColdDependency struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}
