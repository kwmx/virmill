//go:build linux && amd64

package creating

import (
	"context"
	"encoding/xml"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

type ownershipRecord struct {
	Version        int                    `json:"schemaVersion"`
	Key            domain.ResourceKey     `json:"key"`
	CreationPlanID string                 `json:"creationPlanID"`
	OperationID    string                 `json:"operationID"`
	Binding        string                 `json:"binding"`
	Volumes        []domain.CreatedVolume `json:"volumes"`
}

func (s *Service) recordOwnership(p domain.Plan, in input, receipt Receipt) error {
	volumes, err := readyVolumes(receipt, in)
	if err != nil {
		return err
	}
	if !receipt.Defined {
		return domain.Fail("RECOVERY_REQUIRED", "definition has not been observed for ownership registration")
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: receipt.VMID}
	record := ownershipRecord{Version: 1, Key: key, CreationPlanID: p.ID, OperationID: receipt.OperationID, Binding: receipt.Binding, Volumes: volumes}
	old, err := s.Store.MetadataBytes("created-vm", key.String())
	if err != nil {
		return err
	}
	if old != nil {
		var existing ownershipRecord
		if err = wire.Decode(old, &existing); err != nil {
			return err
		}
		if !same(existing, record) {
			return domain.Fail("RECOVERY_REQUIRED", "VM ownership catalog conflicts with verified creation receipt")
		}
		return nil
	}
	return s.Store.ComparePut("created-vm", key.String(), nil, record)
}

// Ownership is an application catalog fact. Metadata or a Virmill-looking name
// alone never adopts a domain. Later lossless edits may change hardware while
// preserving this identity binding; inventory must not compare original hardware
// to certify present guest behavior or rehash disks used by a running VM.
func (s *Service) Ownership(ctx context.Context, vm domain.VM) (domain.VM, error) {
	if err := ctx.Err(); err != nil {
		return vm, err
	}
	b, err := s.Store.MetadataBytes("created-vm", vm.Key.String())
	if err != nil || b == nil {
		return vm, err
	}
	var record ownershipRecord
	if err = wire.Decode(b, &record); err != nil {
		return vm, err
	}
	if record.Version != 1 || record.Key != vm.Key {
		return vm, domain.Fail("RECOVERY_REQUIRED", "unsupported or mismatched VM ownership record")
	}
	if err = xmlpatch.Validate(vm.PersistentXML); err != nil {
		return vm, err
	}
	var doc struct {
		XMLName  xml.Name `xml:"domain"`
		UUID     string   `xml:"uuid"`
		Metadata struct {
			Creation []struct {
				APIVersion string `xml:"apiVersion,attr"`
				Binding    string `xml:"binding,attr"`
			} `xml:"urn:virmill:v1 creation"`
		} `xml:"metadata"`
	}
	if err = xml.Unmarshal([]byte(vm.PersistentXML), &doc); err != nil {
		return vm, err
	}
	if doc.UUID != vm.Key.UUID || len(doc.Metadata.Creation) != 1 || doc.Metadata.Creation[0].APIVersion != domain.APIVersion || doc.Metadata.Creation[0].Binding != record.Binding {
		return vm, domain.Fail("SOURCE_CHANGED", "domain identity conflicts with its Virmill ownership record; explicit reconciliation required")
	}
	vm.Ownership = "managed"
	return vm, nil
}
