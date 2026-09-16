//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
)

var _ domain.DiskAdditionBackend = (*Provider)(nil)

// diskAdditionPool finds the storage pool the VM's own disks live in. A VM
// whose disks are plain files has no pool to add a managed disk to.
func diskAdditionPool(raw string) (string, error) {
	source, err := InspectColdSourceXML(raw)
	if err != nil {
		return "", err
	}
	for _, disk := range source.Disks {
		if disk.Device == "disk" && !disk.Empty && disk.Source.Type == "volume" && disk.Source.Pool != "" {
			return disk.Source.Pool, nil
		}
	}
	return "", domain.Fail("UNSUPPORTED_CAPABILITY", "adding a disk needs a VM whose disks live in a storage pool")
}

// nextDiskVolumeName continues this VM's own disk numbering and refuses to
// reuse a name that already exists in the pool.
func nextDiskVolumeName(pool *native.StoragePool, id string) (string, error) {
	names, err := pool.ListAllStorageVolumes(0)
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for i := range names {
		name, e := names[i].GetName()
		names[i].Free()
		if e != nil {
			err = e
			continue
		}
		taken[name] = true
	}
	if err != nil {
		return "", err
	}
	for index := 0; index < 1000; index++ {
		name := fmt.Sprintf("virmill-%s-disk-%03d.qcow2", id, index)
		if !taken[name] {
			return name, nil
		}
	}
	return "", domain.Fail("UNSUPPORTED_CAPABILITY", "this VM has no free managed disk name left in its pool")
}

func (p *Provider) InspectDiskAddition(ctx context.Context, uri, id, bus string) (domain.DiskAdditionTarget, error) {
	var out domain.DiskAdditionTarget
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	if err := removalKey(key); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	c, err := connect(uri, true)
	if err != nil {
		return out, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		return out, err
	}
	defer d.Free()
	vm, err := growableVM(c, d, uri)
	if err != nil {
		return out, err
	}
	if vm.Key != key {
		return out, domain.Fail("SOURCE_CHANGED", "VM identity changed during inspection")
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return out, domain.Fail("PERMISSION_DENIED", "a secure read of the definition is required before adding a disk")
	}
	if err = checkSecureConfiguration(vm.PersistentXML, secure); err != nil {
		return out, err
	}
	poolName, err := diskAdditionPool(vm.PersistentXML)
	if err != nil {
		return out, err
	}
	pool, err := c.LookupStoragePoolByName(poolName)
	if err != nil {
		return out, err
	}
	defer pool.Free()
	observed, err := observePool(pool, uri)
	if err != nil {
		return out, err
	}
	if !observed.Active || observed.State != "running" || (observed.Type != "dir" && observed.Type != "fs" && observed.Type != "netfs") {
		return out, domain.Fail("SOURCE_CHANGED", "this VM's storage pool is not an active file-based pool")
	}
	if observed.AvailableBytes == nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "this pool does not report its free space")
	}
	add, err := xmlpatch.InspectDiskAddition(vm.PersistentXML, bus)
	if err != nil {
		return out, err
	}
	name, err := nextDiskVolumeName(pool, id)
	if err != nil {
		return out, err
	}
	add.Pool, add.Volume = poolName, name
	// The insert is built here only to refuse at review what could not be
	// applied. Its result is not digested: libvirt files a new disk among the
	// other disks, so no digest of the whole definition can confirm an insert.
	if _, err = xmlpatch.AddDisk(vm.PersistentXML, add); err != nil {
		return out, err
	}
	// Libvirt reformats the definition it stores, so the digest of the
	// definition before the insert tolerates attribute order and indentation
	// and nothing else, as cold restore does.
	before, err := xmlpatch.HardwareDigest(vm.PersistentXML)
	if err != nil {
		return out, err
	}
	directory, err := cleanupPoolDirectory(pool)
	if err != nil {
		return out, err
	}
	out = domain.DiskAdditionTarget{VM: key, VMFingerprint: vm.Fingerprint, DefinitionSHA256: before,
		PoolID: observed.Key.UUID, PoolName: poolName, VolumeName: name,
		Bus: add.Bus, Target: add.Target, Unit: add.Unit, PoolAvailableBytes: *observed.AvailableBytes}
	out.ResourceIDs = []string{key.String(), observed.Key.String(), "local-file|" + filepath.Join(directory, name)}
	sort.Strings(out.ResourceIDs)
	return out, nil
}

func validDiskAddition(in domain.DiskAdditionPlan) error {
	t := in.Target
	if err := removalKey(t.VM); err != nil {
		return err
	}
	if !digestPattern.MatchString(t.VMFingerprint) || !digestPattern.MatchString(t.DefinitionSHA256) ||
		!uuidPattern.MatchString(t.PoolID) || t.PoolName == "" || t.VolumeName != in.Volume.Name || t.PoolID != in.Volume.PoolID ||
		in.Volume.ContentType != "" {
		return domain.Fail("INVALID_INPUT", "complete exact disk addition required")
	}
	if err := validateVolume(in.Volume); err != nil {
		return err
	}
	if len(t.ResourceIDs) != 3 || !slices.IsSorted(t.ResourceIDs) || !slices.Contains(t.ResourceIDs, t.VM.String()) {
		return domain.Fail("INVALID_INPUT", "canonical resource lock set required")
	}
	return nil
}

// addedDiskXML recomputes the reviewed definition from what is on the host now.
// The caller has already bound the definition it read to the review, and the
// result is confirmed afterwards by removing the disk again, so there is no
// digest of the insert to compare here.
func addedDiskXML(raw string, in domain.DiskAdditionPlan) (string, error) {
	t := in.Target
	return xmlpatch.AddDisk(raw, xmlpatch.DiskAddition{Pool: t.PoolName, Volume: t.VolumeName, Bus: t.Bus, Target: t.Target, Unit: t.Unit})
}

// addedDiskState reports what the saved definition holds now: "before" while it
// is still the reviewed definition, "added" once it is that definition plus
// exactly the reviewed disk, and "" for anything else, with a reason the caller
// can record. No digest of the whole definition can confirm the insert, because
// libvirt files a new disk among the other disks while the hardware digest
// treats child ordering as significant. Removing the disk at the reviewed target
// must instead give back the definition the review bound, which also proves no
// other device changed, and that disk must name the reviewed image.
func addedDiskState(raw string, t domain.DiskAdditionTarget) (string, string, error) {
	stored, err := xmlpatch.HardwareDigest(raw)
	if err != nil {
		return "", "", err
	}
	if stored == t.DefinitionSHA256 {
		return "before", "the definition is still the reviewed one", nil
	}
	targets, err := xmlpatch.DiskTargets(raw)
	if err != nil {
		return "", "", err
	}
	if !slices.Contains(targets, t.Target) {
		return "", "the definition is not the reviewed one and has no disk at " + t.Target, nil
	}
	without, err := xmlpatch.WithoutDisk(raw, t.Target)
	if err != nil {
		return "", "", err
	}
	digest, err := xmlpatch.HardwareDigest(without)
	if err != nil {
		return "", "", err
	}
	if digest != t.DefinitionSHA256 {
		return "", "the definition differs from the reviewed one beyond the disk at " + t.Target, nil
	}
	source, err := growDiskSource(raw, t.Target)
	if err != nil {
		return "", "", err
	}
	if source.pool != t.PoolName || source.volume != t.VolumeName || source.format != "qcow2" {
		return "", "the disk at " + t.Target + " names another image", nil
	}
	return "added", "", nil
}

func (p *Provider) CheckDiskAddition(ctx context.Context, in domain.DiskAdditionPlan) (string, error) {
	if err := validDiskAddition(in); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c, err := connect(in.Target.VM.ConnectionID, true)
	if err != nil {
		return "", err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(in.Target.VM.UUID)
	if err != nil {
		return "", err
	}
	defer d.Free()
	vm, err := growableVM(c, d, in.Target.VM.ConnectionID)
	if err != nil {
		return "", err
	}
	// VolumeAbsent refuses with STALE_PLAN when the volume is already there;
	// any other error is a real failure, never read as presence.
	present := true
	if err = p.VolumeAbsent(ctx, in.Target.VM.ConnectionID, in.Volume); err == nil {
		present = false
	} else {
		var refusal *domain.Error
		if !errors.As(err, &refusal) || refusal.Code != "STALE_PLAN" {
			return "", err
		}
	}
	state, detail, err := addedDiskState(vm.PersistentXML, in.Target)
	if err != nil {
		return "", err
	}
	switch state {
	case "before":
		if _, err = addedDiskXML(vm.PersistentXML, in); err != nil {
			return "", err
		}
		if present {
			return "volume-present", nil
		}
		return "before", nil
	case "added":
		if !present {
			return "", domain.Fail("RECOVERY_REQUIRED", "the definition names the new disk but its volume is missing")
		}
		return "added", nil
	}
	refusal := domain.Fail("STALE_PLAN", "the definition changed since the review; review again")
	refusal.Details = map[string]string{"observed": detail}
	return "", refusal
}

// DefineAddedDisk defines the reviewed definition once, then reads it back.
func (p *Provider) DefineAddedDisk(ctx context.Context, in domain.DiskAdditionPlan) error {
	if err := validDiskAddition(in); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(in.Target.VM.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(in.Target.VM.UUID)
	if err != nil {
		return err
	}
	defer d.Free()
	vm, err := growableVM(c, d, in.Target.VM.ConnectionID)
	if err != nil {
		return err
	}
	stored, err := xmlpatch.HardwareDigest(vm.PersistentXML)
	if err != nil {
		return err
	}
	if stored != in.Target.DefinitionSHA256 || vm.Fingerprint != in.Target.VMFingerprint {
		return domain.Fail("STALE_PLAN", "the VM changed since the review; review again")
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("PERMISSION_DENIED", "a secure read of the definition is required before adding a disk")
	}
	if err = checkSecureConfiguration(vm.PersistentXML, secure); err != nil {
		return err
	}
	after, err := addedDiskXML(vm.PersistentXML, in)
	if err != nil {
		return err
	}
	defined, err := c.DomainDefineXMLFlags(after, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		// Native validation errors can quote opaque XML values, which never
		// reach the journal. The operation keeps its uncertainty instead.
		return domain.Fail("RECOVERY_REQUIRED", "native definition of the added disk failed; XML values withheld; inspect and reconcile without replay")
	}
	defer defined.Free()
	read, err := observe(defined, in.Target.VM.ConnectionID)
	if err != nil {
		return err
	}
	securely, err := defined.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "secure readback unavailable after adding the disk; do not replay")
	}
	// Libvirt reformats what it stores and files a new disk among the other
	// disks, so the readback removes the reviewed disk again and expects the
	// definition the review bound. Each check reports separately: one shared
	// message cannot be diagnosed afterwards.
	if read.State != "stopped" {
		return domain.Fail("RECOVERY_REQUIRED", "the VM is no longer stopped after adding the disk; inspect the operation without replaying it")
	}
	if read.HasManagedSave {
		return domain.Fail("RECOVERY_REQUIRED", "the VM acquired saved state while adding the disk; inspect the operation without replaying it")
	}
	state, detail, err := addedDiskState(read.PersistentXML, in.Target)
	if err != nil {
		return err
	}
	if state != "added" {
		failure := domain.Fail("RECOVERY_REQUIRED", "the stored definition is not the reviewed definition plus the reviewed disk; inspect the operation without replaying it")
		failure.Details = map[string]string{"observed": detail}
		return failure
	}
	if securely != read.PersistentXML {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "the stored definition omits sensitive settings, so the added disk cannot be confirmed; inspect the operation without replaying it")
	}
	return nil
}
