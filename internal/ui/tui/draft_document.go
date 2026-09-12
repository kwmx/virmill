package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

// SavedSetupDocument is a deliberately closed, secret-free allowlist. Cached
// observations, service requests, plans, approvals and credentials never enter it.
type SavedSetupDocument struct {
	Version       int                  `json:"version"`
	Connection    string               `json:"connection"`
	State         string               `json:"state"`
	OperationID   string               `json:"operationID,omitempty"`
	SourceBinding string               `json:"sourceBinding,omitempty"`
	Import        *SavedImportValues   `json:"import,omitempty"`
	Creation      *SavedCreationValues `json:"creation,omitempty"`
}
type SavedImportValues struct {
	Kind, Source, SelectedSource, DestinationParent, DestinationName string
	SystemID, MediaID, SHA256, VMName, VCPUs, MemoryMiB              string
	Offline                                                          bool
	Disks                                                            []ImportDisk
	Files                                                            []ImportFile
	Page                                                             int
}
type SavedCreationValues struct {
	OperationID         string
	Spec                domain.CreationSpec
	CPUText, MemoryText string
	Page                int
}

func setupBinding(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func saveImportValues(f ImportForm) *SavedImportValues {
	d := f.Draft
	return &SavedImportValues{Kind: d.Kind, Source: d.Source, SelectedSource: d.SelectedSource, DestinationParent: d.DestinationParent, DestinationName: d.DestinationName, SystemID: d.SystemID, MediaID: d.MediaID, SHA256: d.SHA256, VMName: d.VMName, VCPUs: d.VCPUs, MemoryMiB: d.MemoryMiB, Offline: d.Offline, Disks: slices.Clone(d.Disks), Files: slices.Clone(d.Files), Page: f.Page}
}
func (d SavedImportValues) form() ImportForm {
	f := NewImportForm(d.Kind)
	f.Draft = ImportDraft{Kind: d.Kind, Source: d.Source, SelectedSource: d.SelectedSource, DestinationParent: d.DestinationParent, DestinationName: d.DestinationName, SystemID: d.SystemID, MediaID: d.MediaID, SHA256: d.SHA256, VMName: d.VMName, VCPUs: d.VCPUs, MemoryMiB: d.MemoryMiB, Offline: d.Offline, Disks: slices.Clone(d.Disks), Files: slices.Clone(d.Files)}
	f.Page = max(0, min(3, d.Page))
	return f
}
func saveCreationValues(f CreationForm) *SavedCreationValues {
	// Deep copy the typed declaration so later form edits cannot alter a queued save.
	raw, _ := json.Marshal(f.Spec)
	var spec domain.CreationSpec
	_ = json.Unmarshal(raw, &spec)
	return &SavedCreationValues{OperationID: f.OperationID, Spec: spec, CPUText: f.CPUText, MemoryText: f.MemoryText, Page: f.Page}
}
func (d SavedCreationValues) restore(f *CreationForm) {
	raw, _ := json.Marshal(d.Spec)
	_ = json.Unmarshal(raw, &f.Spec)
	f.CPUText, f.MemoryText = d.CPUText, d.MemoryText
	f.Page = max(0, min(3, d.Page))
	f.Focus = 0
}
func decodeSetup(raw []byte, connection string) (SavedSetupDocument, error) {
	var d SavedSetupDocument
	if err := wire.Decode(raw, &d); err != nil {
		return d, err
	}
	if d.Version != 1 || d.Connection != connection || (d.State != "editing" && d.State != "submitting" && d.State != "submitted") {
		return d, fmt.Errorf("Saved setup has an unsupported version, connection or state")
	}
	if d.Import == nil && d.Creation == nil {
		return d, fmt.Errorf("Saved setup contains no editable settings")
	}
	if d.SourceBinding != "" {
		if decoded, err := hex.DecodeString(d.SourceBinding); err != nil || len(decoded) != sha256.Size {
			return d, fmt.Errorf("Saved setup source binding is invalid")
		}
	}
	if d.Import == nil && d.Creation.OperationID == "" {
		return d, fmt.Errorf("Saved VM setup has no prepared-image operation")
	}
	if d.Import != nil && !slices.Contains([]string{"auto", "ova", "iso", "disks"}, d.Import.Kind) {
		return d, fmt.Errorf("Saved setup image type is unsupported")
	}

	return d, nil
}
func (m Workspace) setupDocument() *SavedSetupDocument {
	d := &SavedSetupDocument{Version: 1, Connection: m.Connection, State: "editing"}
	if m.Import != nil {
		d.Import = saveImportValues(*m.Import)
		if m.Import.Draft.Description != nil {
			d.SourceBinding = setupBinding(m.Import.Draft.Description)
		}
		if m.Import.VM != nil && m.Import.VMBinding == creationDraftBinding(m.Import.Draft) {
			d.Creation = saveCreationValues(*m.Import.VM)
		}
	}
	if m.Creation != nil {
		d.Creation = saveCreationValues(*m.Creation)
		if !m.Creation.BeforePreparation {
			d.Import = nil
			d.SourceBinding = setupBinding(m.Creation.Source)
		}
	}
	if d.Import == nil && d.Creation == nil {
		return nil
	}
	return d
}
