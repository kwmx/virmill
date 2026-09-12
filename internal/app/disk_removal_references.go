package app

import (
	"context"
	"encoding/json"
	"reflect"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

// Only exact allocation history for this VM may be discounted as a live
// reference. All other metadata, including unknown record kinds, remains part
// of the deletion check. Source/preparation/backup records are never exempted.
type removalOwnership struct {
	Version          int                    `json:"schemaVersion"`
	Key              domain.ResourceKey     `json:"key"`
	CreationPlanID   string                 `json:"creationPlanID"`
	OperationID      string                 `json:"operationID"`
	Binding          string                 `json:"binding"`
	Volumes          []domain.CreatedVolume `json:"volumes"`
	AcceptancePlanID string                 `json:"acceptancePlanID,omitempty"`
}
type removalVolumeProgress struct {
	Intent    domain.VolumeIntent   `json:"intent"`
	Allocated *domain.CreatedVolume `json:"allocated"`
	Verified  bool                  `json:"verified"`
}
type removalCreationReceipt struct {
	Version           int                     `json:"schemaVersion"`
	PlanID            string                  `json:"planID"`
	OperationID       string                  `json:"operationID"`
	Binding           string                  `json:"binding"`
	VMID              string                  `json:"vmID"`
	Connection        string                  `json:"connection"`
	Volumes           []removalVolumeProgress `json:"volumes"`
	VolumesVerified   bool                    `json:"volumesVerified"`
	Defined           bool                    `json:"defined"`
	GuestBootVerified bool                    `json:"guestBootVerified"`
}

func removalAllocatedMatches(v domain.CreatedVolume, r domain.DiskRemoval) bool {
	for _, d := range r.Disks {
		if v.Path == d.Path && v.BackendKey == d.VolumeKey && v.Generation == d.Generation && v.Intent.PoolID == d.PoolID && v.Intent.Name == d.VolumeName {
			return true
		}
	}
	return false
}
func removalJSON(v any) (any, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	var out any
	e = wire.Decode(b, &out)
	return out, e
}

func (s *Service) checkDiskRemovalReferences(ctx context.Context, p domain.Plan, r domain.DiskRemoval) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	records, err := s.Engine.Store.MetadataRecords()
	if err != nil {
		return err
	}
	var own *removalOwnership
	for _, record := range records {
		if record.Kind != "created-vm" || record.ID != r.Definition.Resource.String() {
			continue
		}
		var o removalOwnership
		if wire.Decode(record.Body, &o) != nil || (o.Version != 1 && o.Version != 2) || o.Key != r.Definition.Resource || !removalUUID.MatchString(o.CreationPlanID) || !removalUUID.MatchString(o.OperationID) || !removalDigest(o.Binding) || (o.Version == 1 && o.AcceptancePlanID != "") || (o.Version == 2 && !removalUUID.MatchString(o.AcceptancePlanID)) {
			return domain.Fail("RECOVERY_REQUIRED", "VM ownership metadata is not a recognized allocation record; preserve its disks.")
		}
		own = &o
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		var value any
		if err = wire.Decode(record.Body, &value); err != nil {
			return err
		}
		if record.Kind == diskRemovalReceiptKind && record.ID == operations.OperationID(ctx) {
			var receipt diskRemovalReceipt
			sum, e := operations.Digest(r)
			if e != nil || wire.Decode(record.Body, &receipt) != nil || receipt.Version != 1 || receipt.OperationID != record.ID || receipt.PlanID != p.ID || receipt.PlanDigest != p.Digest || receipt.RemovalDigest != sum {
				return domain.Fail("RECOVERY_REQUIRED", "Current removal receipt does not match the accepted plan.")
			}
			continue
		}
		if own != nil && record.Kind == "created-vm" && record.ID == own.Key.String() {
			copy := *own
			copy.Volumes = nil
			for _, v := range own.Volumes {
				if !removalAllocatedMatches(v, r) {
					copy.Volumes = append(copy.Volumes, v)
				}
			}
			value, err = removalJSON(copy)
			if err != nil {
				return err
			}
		}
		if own != nil && record.Kind == "vm-creation" && record.ID == own.CreationPlanID {
			var receipt removalCreationReceipt
			if wire.Decode(record.Body, &receipt) != nil || receipt.Version != 1 || receipt.PlanID != own.CreationPlanID || receipt.OperationID != own.OperationID || receipt.Binding != own.Binding || receipt.VMID != own.Key.UUID || receipt.Connection != own.Key.ConnectionID || !receipt.Defined || !receipt.VolumesVerified {
				return domain.Fail("RECOVERY_REQUIRED", "Creation receipt does not match this VM's verified allocation history.")
			}
			kept := []removalVolumeProgress{}
			for _, v := range receipt.Volumes {
				if v.Allocated == nil || !v.Verified || !reflect.DeepEqual(v.Intent, v.Allocated.Intent) || !removalAllocatedMatches(*v.Allocated, r) {
					kept = append(kept, v)
				}
			}
			receipt.Volumes = kept
			value, err = removalJSON(receipt)
			if err != nil {
				return err
			}
		}
		if referencesRemoval(value, r) {
			return diskReferenceError(record.Kind, record.ID)
		}
	}
	jobs, err := s.Engine.Store.StorageReferenceJobs()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.PlanID == p.ID {
			continue
		}
		switch job.State {
		case "queued", "validating", "running", "verifying", "recovery-required", "interrupted", "partial", "reconciling":
		default:
			continue
		}
		_, input, e := s.Engine.Store.Plan(job.PlanID)
		if e != nil {
			return e
		}
		var value any
		if e = wire.Decode(input, &value); e != nil {
			return e
		}
		if referencesRemoval(value, r) {
			return diskReferenceError("unresolved operation", job.ID)
		}
	}
	return ctx.Err()
}
