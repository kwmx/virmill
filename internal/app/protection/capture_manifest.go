package protection

import (
	"time"
	"virmill.local/core/internal/domain"
)

// CaptureManifest is a versioned recovery-set declaration. Validation and file
// integrity alone never authenticate capture provenance or prove a guest boot.
// The coordinator's durable source observations and receipt establish those
// separate claims; independent repository recovery must retain this manifest.
type CaptureManifest struct {
	// Required when NVRAM or TPM state exists. This member records the complete
	// independently resolved helper inventory/proof, not confidential bytes.
	AuxiliaryInventoryMember string                  `json:"auxiliaryInventoryMember"`
	APIVersion               string                  `json:"apiVersion"`
	Kind                     string                  `json:"kind"`
	Version                  int                     `json:"version"`
	ID                       string                  `json:"snapshotID"`
	OperationID              string                  `json:"operationID"`
	SourceVM                 domain.ResourceKey      `json:"sourceVM"`
	StartedAt                time.Time               `json:"startedAt"`
	FinishedAt               time.Time               `json:"finishedAt"`
	SourceFingerprint        string                  `json:"sourceFingerprint"`
	Source                   domain.ColdSourceLayout `json:"source"`
	StateBefore              string                  `json:"stateBefore"`
	StateAfter               string                  `json:"stateAfter"`
	HasManagedSave           bool                    `json:"hasManagedSave"`
	NativeVersions           map[string]string       `json:"nativeVersions"`
	Members                  []CaptureMember         `json:"members"`
	Disks                    []CapturedDisk          `json:"disks"`
	PersistentXMLMember      string                  `json:"persistentXMLMember"`
	EffectiveXMLMember       string                  `json:"effectiveXMLMember"`
	FirmwareCodeMember       string                  `json:"firmwareCodeMember"`
	NVRAMMember              string                  `json:"nvramMember"`
	TPMMembers               []CapturedTPMFile       `json:"tpmMembers"`
	Secrets                  []CapturedSecret        `json:"secrets"`
	IndependentlyRecoverable bool                    `json:"independentlyRecoverable"`
}

// Relative paths identify immutable recovery members. Source ownership/labels
// belong to separately verified capture/restore receipts and are never blindly
// applied from a manifest supplied by an untrusted caller.
type CaptureMember struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Each nonempty source disk/media target maps to one independent captured file.
// The initial complete-capture executor must flatten backing chains; this field
// is a checked declaration, not permission to skip backing-graph verification.
type CapturedDisk struct {
	Target      string `json:"target"`
	MemberID    string `json:"memberID"`
	Format      string `json:"format"`
	Independent bool   `json:"independent"`
}

type CapturedTPMFile struct {
	Name     string `json:"name"`
	MemberID string `json:"memberID"`
}

// Exactly one disposition per referenced secret is required. An external
// reference keeps the set explicitly dependent on that secret; no value is
// serialized in metadata or a plan. Included values are separate private members.
type CapturedSecret struct {
	UUID     string `json:"uuid"`
	MemberID string `json:"memberID"`
	External bool   `json:"external"`
}
