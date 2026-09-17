//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
)

var _ domain.DiskAdditionDisposalBackend = (*Provider)(nil)

// ADR 0062: an unresolved disk addition is closed by observation. A disk the
// saved definition still names can only be accepted; only an unreferenced
// volume may be deleted, under the same guards as creation cleanup.
func (p *Provider) InspectAddedDiskDisposal(ctx context.Context, in domain.DiskAdditionPlan, forDeletion bool) (domain.AddedDiskDisposal, error) {
	var out domain.AddedDiskDisposal
	if err := validDiskAddition(in); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	c, err := connect(in.Target.VM.ConnectionID, true)
	if err != nil {
		return out, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(in.Target.VM.UUID)
	if err != nil {
		return out, err
	}
	defer d.Free()
	vm, err := growableVM(c, d, in.Target.VM.ConnectionID)
	if err != nil {
		return out, err
	}
	// The definition names the reviewed disk only when that exact target
	// carries the reviewed pool volume.
	if source, e := growDiskSource(vm.PersistentXML, in.Target.Target); e == nil {
		out.Referenced = source.pool == in.Target.PoolName && source.volume == in.Target.VolumeName
	}
	// Presence is probed by pool and name: the allocation identity is not known
	// until the volume has been observed, so it cannot be a lookup precondition.
	pool, err := c.LookupStoragePoolByUUIDString(in.Target.PoolID)
	if err != nil {
		return out, err
	}
	defer pool.Free()
	out.VolumeState = "present"
	volume, err := pool.LookupStorageVolByName(in.Target.VolumeName)
	if err != nil {
		var missing native.Error
		if !errors.As(err, &missing) || missing.Code != native.ERR_NO_STORAGE_VOL {
			return out, err
		}
		out.VolumeState = "absent"
	} else {
		observed, e := observeRemovalDisk(volume, in.Target.Target)
		volume.Free()
		if e != nil {
			return out, e
		}
		if observed.PoolID != in.Target.PoolID || observed.VolumeName != in.Target.VolumeName {
			return out, domain.Fail("SOURCE_CHANGED", "the new disk's volume identity differs from the review")
		}
		out.Disk = &observed
	}
	resources := append([]string{}, in.Target.ResourceIDs...)
	// The host-wide dependency graph is the precondition for deleting a file,
	// so it is built only when deletion is possible. Accepting a disk the
	// definition already names deletes nothing, and must not be blocked by
	// unrelated firmware state elsewhere on the connection that the deletion
	// graph cannot account for.
	if forDeletion && !out.Referenced && out.Disk != nil {
		candidates := []domain.CleanupCandidate{{Intent: in.Volume,
			Allocated: &domain.CreatedVolume{Intent: in.Volume, BackendKey: out.Disk.VolumeKey, Path: out.Disk.Path, Generation: out.Disk.Generation}}}
		out.GraphDigest, err = cleanupGraphExceptOwner(ctx, c, in.Target.VM.ConnectionID, candidates, &resources, in.Target.VM.UUID)
		if err != nil {
			return out, err
		}
	}
	sort.Strings(resources)
	out.ResourceIDs = slices.Compact(resources)
	return out, nil
}

// DeleteUnreferencedDisk makes exactly one native deletion of the reviewed
// volume, and only while no definition names it.
func (p *Provider) DeleteUnreferencedDisk(ctx context.Context, in domain.DiskAdditionPlan, reviewed domain.AddedDiskDisposal) error {
	if err := validDiskAddition(in); err != nil {
		return err
	}
	if reviewed.Referenced || reviewed.VolumeState != "present" || reviewed.Disk == nil || reviewed.Disk.Generation == "" || reviewed.GraphDigest == "" {
		return domain.Fail("INVALID_INPUT", "only an unreferenced present volume with a complete dependency proof can be deleted")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := p.InspectAddedDiskDisposal(ctx, in, true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, reviewed) {
		return domain.Fail("STALE_PLAN", "the disk, its volume or the dependency graph changed before deletion")
	}
	c, err := connect(in.Target.VM.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	volume, err := lookupCreated(c, domain.CreatedVolume{Intent: in.Volume, BackendKey: reviewed.Disk.VolumeKey, Path: reviewed.Disk.Path, Generation: reviewed.Disk.Generation})
	if err != nil {
		return err
	}
	defer volume.Free()
	observed, err := observeRemovalDisk(volume, in.Target.Target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(&observed, reviewed.Disk) {
		return domain.Fail("SOURCE_CHANGED", "the held volume differs immediately before deletion")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// One non-wiping delete of the reviewed generation. The caller journals the
	// intent and reconciles a lost acknowledgement by observing absence.
	return volume.Delete(0)
}
