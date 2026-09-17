//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

var _ domain.CloneProvider = (*Provider)(nil)

var cloneVolumePattern = regexp.MustCompile(`^virmill-[0-9a-f-]{36}-disk-[0-9]{3}\.(qcow2|raw)$`)

// cloneDependencies are the only external dependencies the cold-source
// inspection may report for a VM that can be cloned: its network adapters,
// whose MAC addresses the clone replaces, and the fixed system emulator.
var cloneDependencies = map[string]bool{"interface": true, "emulator": true}

func validClone(c domain.VMClone) error {
	if err := removalKey(c.Source); err != nil {
		return err
	}
	if _, err := validation.DisplayName(c.Name); err != nil || c.Name == c.SourceName || !uuidPattern.MatchString(c.UUID) || c.UUID == c.Source.UUID ||
		!digestPattern.MatchString(c.SourceFingerprint) || !digestPattern.MatchString(c.DefinitionSHA256) || len(c.Disks) == 0 || len(c.Disks) > 64 {
		return domain.Fail("INVALID_INPUT", "complete exact clone observation required")
	}
	names := map[string]bool{}
	for _, d := range c.Disks {
		s := d.Disk
		if !coldDiskTarget.MatchString(s.Target) || coldPath(s.Path) != nil || !uuidPattern.MatchString(s.PoolID) || !diskRemovalName(s.VolumeName) ||
			s.VolumeName != filepath.Base(s.Path) || s.VolumeKey == "" || !digestPattern.MatchString(s.Fingerprint) ||
			(s.Format != "qcow2" && s.Format != "raw") || s.CapacityBytes == 0 || s.CapacityBytes > 512<<30 ||
			d.SourcePoolName == "" || d.PoolName == "" || !uuidPattern.MatchString(d.PoolID) || !cloneVolumePattern.MatchString(d.VolumeName) ||
			coldPath(d.VolumePath) != nil || filepath.Base(d.VolumePath) != d.VolumeName || d.VolumePath == s.Path ||
			d.CopyBytes != cloneBound(s) || names[d.PoolID+"/"+d.VolumeName] {
			return domain.Fail("INVALID_INPUT", "complete exact clone disk observation required")
		}
		names[d.PoolID+"/"+d.VolumeName] = true
	}
	if len(c.ResourceIDs) < 3 || !slices.IsSorted(c.ResourceIDs) || !slices.Contains(c.ResourceIDs, c.Source.String()) {
		return domain.Fail("INVALID_INPUT", "canonical resource lock set required")
	}
	return nil
}

// cloneBound is the most a copy may hold: a qcow2 image may exceed its virtual
// size by creation's allowance, while raw bytes are exactly the virtual size.
func cloneBound(d domain.RemovalDisk) uint64 {
	if d.Format == "raw" {
		return d.CapacityBytes
	}
	return moveBound(d.CapacityBytes)
}

func cloneKey(uri, id string) domain.ResourceKey {
	return domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
}

func domainAbsent(err error) bool {
	var e native.Error
	return errors.As(err, &e) && e.Code == native.ERR_NO_DOMAIN
}

func (p *Provider) InspectClone(ctx context.Context, uri, id, name, pool, uuid string) (domain.VMClone, error) {
	var out domain.VMClone
	key := cloneKey(uri, id)
	if err := removalKey(key); err != nil {
		return out, err
	}
	if _, err := validation.DisplayName(name); err != nil {
		return out, domain.Fail("INVALID_INPUT", "the clone needs a valid name")
	}
	if pool != "" && !diskRemovalName(pool) {
		return out, domain.Fail("INVALID_INPUT", "an exact storage pool name is required")
	}
	if !uuidPattern.MatchString(uuid) || uuid == id {
		return out, domain.Fail("INVALID_INPUT", "a new UUID is required")
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
		return out, domain.Fail(refusalCodeOf(err), "shut down the VM, with no saved state, snapshots or checkpoints, before cloning it")
	}
	if vm.Key != key || vm.Name == name {
		return out, domain.Fail("INVALID_INPUT", "the clone needs a name different from the original's")
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return out, domain.Fail("PERMISSION_DENIED", "a secure read of the definition is required before cloning")
	}
	if err = checkSecureConfiguration(vm.PersistentXML, secure); err != nil {
		return out, err
	}
	if other, e := c.LookupDomainByName(name); e == nil {
		other.Free()
		return out, domain.Fail("INVALID_INPUT", "a VM named "+name+" already exists; choose another name")
	} else if !domainAbsent(e) {
		return out, e
	}
	if other, e := c.LookupDomainByUUIDString(uuid); e == nil {
		other.Free()
		return out, domain.Fail("STALE_PLAN", "the chosen UUID is already in use; review again")
	} else if !domainAbsent(e) {
		return out, e
	}
	layout, err := InspectColdSourceXML(vm.PersistentXML)
	if err != nil {
		return out, err
	}
	for _, dep := range layout.External {
		if !cloneDependencies[dep.Kind] {
			return out, domain.Fail("UNSUPPORTED_CAPABILITY", "this VM has a "+dep.Kind+" dependency that a clone cannot copy or share safely")
		}
	}
	if layout.State.TPM != nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "this VM has an emulated TPM, whose state cannot be copied")
	}
	out = domain.VMClone{Source: key, SourceName: vm.Name, SourceFingerprint: vm.Fingerprint, UUID: uuid, Name: name,
		SharedMedia: []string{}, FreshNVRAM: layout.State.Firmware.NVRAM != nil}
	resources := []string{key.String(), cloneKey(uri, uuid).String()}
	space := map[string]*domain.ClonePoolSpace{}
	index := 0
	for _, disk := range layout.Disks {
		if disk.Device != "disk" || disk.ReadOnly || disk.Empty {
			if !disk.Empty {
				out.SharedMedia = append(out.SharedMedia, disk.Target)
			}
			continue
		}
		source, err := growDiskSource(vm.PersistentXML, disk.Target)
		if err != nil {
			return domain.VMClone{}, err
		}
		if source.pool == "" || source.volume == "" {
			return domain.VMClone{}, domain.Fail("UNSUPPORTED_CAPABILITY", "disk "+disk.Target+" is not in a storage pool; move it into one before cloning")
		}
		held, err := lookupGrowVolume(c, source)
		if err != nil {
			return domain.VMClone{}, err
		}
		observed, err := observeGrowDisk(held, disk.Target)
		if err != nil {
			held.Free()
			return domain.VMClone{}, err
		}
		sourcePool, err := held.LookupPoolByVolume()
		held.Free()
		if err != nil {
			return domain.VMClone{}, err
		}
		sourceName, err := sourcePool.GetName()
		if err == nil && (sourceName != source.pool || observed.VolumeName != source.volume || observed.Format != source.format) {
			err = domain.Fail("SOURCE_CHANGED", "disk "+disk.Target+"'s volume differs from the VM's disk declaration")
		}
		if err == nil {
			err = diskSharedElsewhere(ctx, c, id, observed, sourcePool)
		}
		sourcePool.Free()
		if err != nil {
			return domain.VMClone{}, err
		}
		destinationName := source.pool
		if pool != "" {
			destinationName = pool
		}
		destination, err := c.LookupStoragePoolByName(destinationName)
		if err != nil {
			return domain.VMClone{}, err
		}
		poolState, err := observePool(destination, uri)
		if err == nil && (!poolState.Active || poolState.State != "running" || (poolState.Type != "dir" && poolState.Type != "fs" && poolState.Type != "netfs")) {
			err = domain.Fail("SOURCE_CHANGED", "storage pool "+destinationName+" is not an active file-based pool")
		}
		if err == nil && poolState.AvailableBytes == nil {
			err = domain.Fail("UNSUPPORTED_CAPABILITY", "storage pool "+destinationName+" does not report its free space")
		}
		var directory, volumeName string
		if err == nil {
			volumeName = fmt.Sprintf("virmill-%s-disk-%03d.%s", uuid, index, observed.Format)
			err = absentVolume(destination, volumeName)
		}
		if err == nil {
			directory, err = cleanupPoolDirectory(destination)
		}
		destination.Free()
		if err != nil {
			return domain.VMClone{}, err
		}
		index++
		copy := domain.CloneDiskCopy{Disk: observed, SourcePoolName: sourceName, PoolID: poolState.Key.UUID, PoolName: destinationName,
			VolumeName: volumeName, VolumePath: filepath.Join(directory, volumeName), CopyBytes: cloneBound(observed)}
		out.Disks = append(out.Disks, copy)
		if space[destinationName] == nil {
			space[destinationName] = &domain.ClonePoolSpace{PoolName: destinationName, AvailableBytes: *poolState.AvailableBytes}
		}
		space[destinationName].NeedBytes += copy.CopyBytes
		resources = append(resources, "local-file|"+copy.VolumePath)
		resources = append(resources, diskRemovalResources(uri, observed)...)
	}
	if len(out.Disks) == 0 {
		return domain.VMClone{}, domain.Fail("UNSUPPORTED_CAPABILITY", "this VM has no writable disk to copy")
	}
	for _, s := range space {
		out.Pools = append(out.Pools, *s)
	}
	sort.Slice(out.Pools, func(i, j int) bool { return out.Pools[i].PoolName < out.Pools[j].PoolName })
	if _, err = xmlpatch.CloneDefinition(vm.PersistentXML, clonePatch(out)); err != nil {
		return domain.VMClone{}, err
	}
	if out.DefinitionSHA256, err = xmlpatch.HardwareDigest(vm.PersistentXML); err != nil {
		return domain.VMClone{}, err
	}
	sort.Strings(resources)
	out.ResourceIDs = slices.Compact(resources)
	if err = validClone(out); err != nil {
		return domain.VMClone{}, err
	}
	return out, nil
}

func refusalCodeOf(err error) string {
	var refusal *domain.Error
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return "OPERATION_FAILED"
}

func clonePatch(c domain.VMClone) xmlpatch.ClonePatch {
	patch := xmlpatch.ClonePatch{UUID: c.UUID, Name: c.Name, FreshNVRAM: c.FreshNVRAM}
	for _, d := range c.Disks {
		patch.Disks = append(patch.Disks, xmlpatch.CloneDisk{Target: d.Disk.Target, Pool: d.PoolName, Volume: d.VolumeName})
	}
	return patch
}

// cloneState reports what the host holds now, without writing anything.
func cloneState(ctx context.Context, c *native.Connect, clone domain.VMClone) (string, []bool, error) {
	source, err := c.LookupDomainByUUIDString(clone.Source.UUID)
	if err != nil {
		return "", nil, err
	}
	defer source.Free()
	vm, err := observe(source, clone.Source.ConnectionID)
	if err != nil {
		return "", nil, err
	}
	// The original is never changed, so any change is someone else's.
	if vm.Fingerprint != clone.SourceFingerprint {
		return "", nil, domain.Fail("STALE_PLAN", "the original VM changed since the review; review again")
	}
	present := make([]bool, len(clone.Disks))
	count := 0
	for i, d := range clone.Disks {
		if err = ctx.Err(); err != nil {
			return "", nil, err
		}
		if present[i], err = volumePresent(c, d.PoolID, d.VolumeName); err != nil {
			return "", nil, err
		}
		if present[i] {
			count++
		}
	}
	defined, err := c.LookupDomainByUUIDString(clone.UUID)
	if err == nil {
		defer defined.Free()
		got, e := observe(defined, clone.Source.ConnectionID)
		if e != nil {
			return "", nil, e
		}
		if got.Name != clone.Name || count != len(clone.Disks) {
			return "", nil, domain.Fail("SOURCE_CHANGED", "a VM with the clone's identity exists but is not the reviewed clone")
		}
		for _, d := range clone.Disks {
			used, e := definitionUsesVolume(got.PersistentXML, d.VolumePath, d.PoolName, d.VolumeName)
			if e != nil {
				return "", nil, e
			}
			if !used {
				return "", nil, domain.Fail("SOURCE_CHANGED", "a VM with the clone's identity does not use the reviewed copies")
			}
		}
		return "defined", present, nil
	}
	if !domainAbsent(err) {
		return "", nil, err
	}
	if other, e := c.LookupDomainByName(clone.Name); e == nil {
		other.Free()
		return "", nil, domain.Fail("STALE_PLAN", "another VM now uses the clone's name; review again")
	} else if !domainAbsent(e) {
		return "", nil, e
	}
	switch count {
	case 0:
		return "before", present, nil
	case len(clone.Disks):
		return "copied", present, nil
	}
	return "copying", present, nil
}

func (p *Provider) CheckClone(ctx context.Context, clone domain.VMClone) (string, []bool, error) {
	if err := validClone(clone); err != nil {
		return "", nil, err
	}
	c, err := connect(clone.Source.ConnectionID, true)
	if err != nil {
		return "", nil, err
	}
	defer c.Close()
	return cloneState(ctx, c, clone)
}

// CopyCloneDisk allocates one reviewed copy name and copies that disk into it
// with the streaming digest, then reads the copy back, exactly as Move does. It
// never writes over an existing name and never touches the original.
func (p *Provider) CopyCloneDisk(ctx context.Context, clone domain.VMClone, index int) error {
	if err := validClone(clone); err != nil {
		return err
	}
	if index < 0 || index >= len(clone.Disks) {
		return domain.Fail("INVALID_INPUT", "no such clone disk")
	}
	c, err := connect(clone.Source.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	state, present, err := cloneState(ctx, c, clone)
	if err != nil {
		return err
	}
	if state == "defined" || present[index] {
		return domain.Fail("RECOVERY_REQUIRED", "this copy already exists; it was not copied again")
	}
	d := clone.Disks[index]
	destination, err := c.LookupStoragePoolByUUIDString(d.PoolID)
	if err != nil {
		return err
	}
	defer destination.Free()
	if err = absentVolume(destination, d.VolumeName); err != nil {
		return err
	}
	sourcePool, err := c.LookupStoragePoolByUUIDString(d.Disk.PoolID)
	if err != nil {
		return err
	}
	defer sourcePool.Free()
	held, err := sourcePool.LookupStorageVolByName(d.Disk.VolumeName)
	if err != nil {
		return err
	}
	defer held.Free()
	current, err := observeGrowDisk(held, d.Disk.Target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, d.Disk) {
		return domain.Fail("SOURCE_CHANGED", "disk "+d.Disk.Target+" differs immediately before the copy")
	}
	x := fmt.Sprintf(`<volume><name>%s</name><capacity unit="bytes">%d</capacity><allocation unit="bytes">0</allocation><target><format type="raw"/></target></volume>`,
		xmlText(d.VolumeName), d.CopyBytes)
	created, err := destination.StorageVolCreateXML(x, 0)
	if err != nil {
		return err
	}
	defer created.Free()
	written, digest, err := copyVolumeBytes(ctx, c, held, created, d.CopyBytes)
	if err != nil {
		return err
	}
	size, stored, err := hashVolumeBytes(ctx, c, created, written)
	if err != nil {
		return err
	}
	if size != written || stored != digest {
		return domain.Fail("RECOVERY_REQUIRED", "the stored copy of "+d.Disk.Target+" differs from the disk it was made from")
	}
	return nil
}

// DefineClone defines the clone from the original's current definition and
// reads the stored definition back. Only libvirt's formatting, the MAC
// addresses it assigns and the firmware variables path may differ from the
// reviewed result.
func (p *Provider) DefineClone(ctx context.Context, clone domain.VMClone) error {
	if err := validClone(clone); err != nil {
		return err
	}
	c, err := connect(clone.Source.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	state, _, err := cloneState(ctx, c, clone)
	if err != nil {
		return err
	}
	if state != "copied" {
		return domain.Fail("RECOVERY_REQUIRED", "not every verified copy is waiting, or the clone already exists; nothing was defined")
	}
	source, err := c.LookupDomainByUUIDString(clone.Source.UUID)
	if err != nil {
		return err
	}
	defer source.Free()
	vm, err := growableVM(c, source, clone.Source.ConnectionID)
	if err != nil {
		return err
	}
	secure, err := source.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("PERMISSION_DENIED", "a secure read of the definition is required before cloning")
	}
	if err = checkSecureConfiguration(vm.PersistentXML, secure); err != nil {
		return err
	}
	if digest, e := xmlpatch.HardwareDigest(vm.PersistentXML); e != nil || digest != clone.DefinitionSHA256 {
		return domain.Fail("STALE_PLAN", "the original's definition changed since the review; review again")
	}
	patched, err := xmlpatch.CloneDefinition(vm.PersistentXML, clonePatch(clone))
	if err != nil {
		return err
	}
	expectedComparable, err := xmlpatch.CloneComparable(patched)
	if err != nil {
		return err
	}
	expected, err := xmlpatch.HardwareDigest(expectedComparable)
	if err != nil {
		return err
	}
	originalMACs, err := xmlpatch.MACAddresses(vm.PersistentXML)
	if err != nil {
		return err
	}
	defined, err := c.DomainDefineXMLFlags(patched, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "native definition of the clone failed; XML values withheld; inspect and reconcile without replay")
	}
	defer defined.Free()
	read, err := observe(defined, clone.Source.ConnectionID)
	if err != nil {
		return err
	}
	securely, err := defined.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "secure readback unavailable after defining the clone; do not replay")
	}
	if read.Key.UUID != clone.UUID || read.Name != clone.Name || read.State != "stopped" || read.HasManagedSave {
		return domain.Fail("RECOVERY_REQUIRED", "the stored clone is not the reviewed stopped VM; inspect the operation without replaying it")
	}
	if securely != read.PersistentXML {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "the stored clone omits sensitive settings, so it cannot be confirmed; inspect the operation without replaying it")
	}
	storedComparable, err := xmlpatch.CloneComparable(read.PersistentXML)
	if err != nil {
		return err
	}
	stored, err := xmlpatch.HardwareDigest(storedComparable)
	if err != nil {
		return err
	}
	if stored != expected {
		failure := domain.Fail("RECOVERY_REQUIRED", "the stored clone differs from the reviewed definition beyond libvirt's own formatting; inspect the operation without replaying it")
		failure.Details = map[string]string{"expected": expected, "stored": stored}
		return failure
	}
	macs, err := xmlpatch.MACAddresses(read.PersistentXML)
	if err != nil {
		return err
	}
	if len(macs) != len(originalMACs) {
		return domain.Fail("RECOVERY_REQUIRED", "the stored clone's network adapters differ from the original's")
	}
	for _, mac := range macs {
		if slices.Contains(originalMACs, mac) {
			return domain.Fail("RECOVERY_REQUIRED", "the stored clone reuses one of the original's MAC addresses")
		}
	}
	for _, d := range clone.Disks {
		usesCopy, e := definitionUsesVolume(read.PersistentXML, d.VolumePath, d.PoolName, d.VolumeName)
		if e != nil {
			return e
		}
		usesOriginal, e := definitionUsesVolume(read.PersistentXML, d.Disk.Path, d.SourcePoolName, d.Disk.VolumeName)
		if e != nil {
			return e
		}
		if !usesCopy || usesOriginal {
			return domain.Fail("RECOVERY_REQUIRED", "the stored clone does not name the copy alone at "+d.Disk.Target)
		}
	}
	return nil
}
