package domain

import "context"

type ColdRestoredDisk struct {
	Target string        `json:"target"`
	Format string        `json:"format"`
	Volume CreatedVolume `json:"volume"`
}

// ColdRestoreDefinition retains the original opaque configuration and changes
// only explicitly mapped independent sources and the new, disconnected identity.
type ColdRestoreDefinition struct {
	UUID           string             `json:"uuid"`
	Name           string             `json:"name"`
	SourceXML      string             `json:"sourceXML"`
	Disks          []ColdRestoredDisk `json:"disks"`
	NVRAMPath      string             `json:"nvramPath"`
	TPMPath        string             `json:"tpmPath"`
	DisconnectNICs bool               `json:"disconnectNICs"`
}
type ColdRestoreBackend interface {
	CreationBackend
	ResourceInventory
	DefineRestoredVM(context.Context, string, ColdRestoreDefinition) (VM, error)
	ObserveRestoredVM(context.Context, string, ColdRestoreDefinition) (VM, bool, error)
}
