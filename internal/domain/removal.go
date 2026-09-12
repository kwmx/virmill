package domain

import "context"

// DefinitionRemoval records the exact stopped definition selected for removal.
// RetainedSources are informational: no storage deletion is part of this API.
type DefinitionRemoval struct {
	Resource         ResourceKey `json:"resource"`
	Name             string      `json:"name"`
	Fingerprint      string      `json:"fingerprint"`
	DefinitionSHA256 string      `json:"definitionSHA256"`
	RetainedSources  []string    `json:"retainedSources"`
}

// DefinitionRemovalProvider checks saved-state and backend-metadata dependencies,
// removes only a revalidated persistent definition, and observes exact absence.
// It must never stop a guest, discard auxiliary state or delete storage.
type DefinitionRemovalProvider interface {
	InspectDefinitionRemoval(context.Context, string, string) (DefinitionRemoval, error)
	RemoveDefinition(context.Context, DefinitionRemoval) error
	DefinitionAbsent(context.Context, ResourceKey) (bool, error)
}
