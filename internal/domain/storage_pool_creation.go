package domain

import "context"

// StoragePoolDefinition describes a new directory-backed pool. Its UUID is
// reserved by a durable plan before definition. Existing pools are never
// rewritten, adopted or reconstructed through this type.
type StoragePoolDefinition struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Autostart bool   `json:"autostart"`
}

// StoragePoolCreationProvider cannot replace, undefine, stop or delete a pool
// or its folder. Methods repeat collision checks immediately before a native
// mutation. InspectCreatedStoragePool is read-only and verifies the exact
// reviewed definition; missing or different state is an error.
type StoragePoolCreationProvider interface {
	CheckStoragePoolCreation(context.Context, string, StoragePoolDefinition) error
	DefineStoragePool(context.Context, string, StoragePoolDefinition) error
	StartStoragePool(context.Context, string, StoragePoolDefinition) error
	SetStoragePoolAutostart(context.Context, string, StoragePoolDefinition) error
	InspectCreatedStoragePool(context.Context, string, StoragePoolDefinition) (StoragePool, error)
}

// StoragePoolStartProvider starts an existing persistent pool exactly as it is
// defined. It never builds, redefines, stops or deletes a pool or its folder.
// Start refuses a pool whose fingerprint differs from the reviewed one.
type StoragePoolStartProvider interface {
	StartExistingStoragePool(ctx context.Context, uri, id, fingerprint string) error
	EnableStoragePoolAutostart(ctx context.Context, uri, id string) error
}
