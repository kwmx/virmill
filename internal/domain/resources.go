package domain

// ResourceValues separates configured boot resources from an active domain's
// resource configuration. Memory is bytes, never rounded into a different value.
// These values describe configuration, not current CPU load or guest RSS.
type ResourceValues struct {
	VCPUs              *uint64 `json:"vcpus"`
	MaximumVCPUs       *uint64 `json:"maximumVcpus"`
	MemoryBytes        *uint64 `json:"memoryBytes"`
	MaximumMemoryBytes *uint64 `json:"maximumMemoryBytes"`
	CPUError           string  `json:"cpuError,omitempty"`
	MemoryError        string  `json:"memoryError,omitempty"`
}

// VMResourceView is read-only. It is not embedded in VM: adding observations to
// VM would change existing durable provider fingerprints and old plan semantics.
type VMResourceView struct {
	Resource         ResourceKey     `json:"resource"`
	Name             string          `json:"name"`
	State            string          `json:"state"`
	Fingerprint      string          `json:"fingerprint"`
	HasManagedSave   bool            `json:"hasManagedSave"`
	Persistent       ResourceValues  `json:"persistent"`
	Live             *ResourceValues `json:"live"`
	CanEditCPU       bool            `json:"canEditCPU"`
	CanEditMemory    bool            `json:"canEditMemory"`
	CPUReason        string          `json:"cpuReason,omitempty"`
	MemoryReason     string          `json:"memoryReason,omitempty"`
	RequiresShutdown bool            `json:"requiresShutdown"`
	ApplyModes       []string        `json:"applyModes"`
	// Live changes alter only what the VM is running with (ADR 0068). A reason
	// says why one is not offered, in the words the user needs.
	CanChangeLiveCPU    bool   `json:"canChangeLiveCPU"`
	CanChangeLiveMemory bool   `json:"canChangeLiveMemory"`
	LiveCPUReason       string `json:"liveCPUReason,omitempty"`
	LiveMemoryReason    string `json:"liveMemoryReason,omitempty"`
	MemoryBalloon       string `json:"memoryBalloon,omitempty"`
}
