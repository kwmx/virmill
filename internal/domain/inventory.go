package domain

import "context"

// StoragePool is an observed backend resource, not permission to use its storage.
type StoragePool struct {
	Key            ResourceKey `json:"key"`
	Name           string      `json:"name"`
	Type           string      `json:"type"`
	State          string      `json:"state"`
	Active         bool        `json:"active"`
	Persistent     bool        `json:"persistent"`
	Autostart      bool        `json:"autostart"`
	Ownership      string      `json:"ownership"`
	XML            string      `json:"xml"`
	Fingerprint    string      `json:"fingerprint"`
	CapacityBytes  *uint64     `json:"capacityBytes"`
	AllocatedBytes *uint64     `json:"allocatedBytes"`
	AvailableBytes *uint64     `json:"availableBytes"`
}

// VirtualNetwork keeps live and persistent XML separate. Packet enforcement
// requires independent verification of the running host and guests.
type VirtualNetwork struct {
	Key                   ResourceKey `json:"key"`
	Name                  string      `json:"name"`
	Active                bool        `json:"active"`
	Persistent            bool        `json:"persistent"`
	Autostart             bool        `json:"autostart"`
	Ownership             string      `json:"ownership"`
	LiveXML               string      `json:"liveXML"`
	PersistentXML         string      `json:"persistentXML"`
	Fingerprint           string      `json:"fingerprint"`
	IsolationVerification string      `json:"isolationVerification"`
}

type ResourceInventory interface {
	ListStoragePools(context.Context, string) ([]StoragePool, error)
	GetStoragePool(context.Context, string, string) (StoragePool, error)
	ListNetworks(context.Context, string) ([]VirtualNetwork, error)
	GetNetwork(context.Context, string, string) (VirtualNetwork, error)
}
