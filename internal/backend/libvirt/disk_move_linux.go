//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sort"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
)

var _ domain.DiskMoveProvider = (*Provider)(nil)

// moveChunkBytes bounds one stream hop. The copy never holds a whole disk in
// memory: it reads this much from the source and writes it to the destination
// before reading again.
const moveChunkBytes = 1 << 20

func validDiskMove(m domain.DiskMove) error {
	if err := removalKey(m.VM); err != nil {
		return err
	}
	d := m.Disk
	if !digestPattern.MatchString(m.VMFingerprint) || !digestPattern.MatchString(m.DefinitionSHA256) ||
		!coldDiskTarget.MatchString(d.Target) || coldPath(d.Path) != nil || !uuidPattern.MatchString(d.PoolID) ||
		!diskRemovalName(d.VolumeName) || d.VolumeName != filepath.Base(d.Path) || d.VolumeKey == "" ||
		!digestPattern.MatchString(d.Fingerprint) || d.Format != "qcow2" || d.CapacityBytes == 0 ||
		m.SourcePoolName == "" || m.PoolName == "" || !uuidPattern.MatchString(m.PoolID) ||
		m.PoolID == d.PoolID || !volumePattern.MatchString(m.VolumeName) ||
		coldPath(m.VolumePath) != nil || filepath.Base(m.VolumePath) != m.VolumeName || m.VolumePath == d.Path ||
		m.CopyBytes != d.CapacityBytes || m.CopyBytes < 1 || m.CopyBytes > 512<<30 {
		return domain.Fail("INVALID_INPUT", "complete exact disk move observation required")
	}
	if len(m.ResourceIDs) != 4 || !slices.IsSorted(m.ResourceIDs) || !slices.Contains(m.ResourceIDs, m.VM.String()) {
		return domain.Fail("INVALID_INPUT", "canonical resource lock set required")
	}
	return nil
}

// hashVolumeBytes reads a volume through one libvirt stream and returns its
// physical size and digest. Nothing is written and nothing is kept: the bytes
// are hashed as they arrive, bounded by the size the caller already observed.
func hashVolumeBytes(ctx context.Context, c *native.Connect, volume *native.StorageVol, bound uint64) (uint64, string, error) {
	stream, err := c.NewStream(0)
	if err != nil {
		return 0, "", err
	}
	defer stream.Free()
	join := watchStream(ctx, stream)
	defer join()
	finished := false
	defer func() {
		if !finished {
			_ = stream.Abort()
		}
	}()
	if err = volume.Download(stream, 0, 0, 0); err != nil {
		return 0, "", err
	}
	h := sha256.New()
	var size uint64
	buffer := make([]byte, moveChunkBytes)
	for {
		if err = ctx.Err(); err != nil {
			return 0, "", err
		}
		n, e := stream.Recv(buffer)
		if n > 0 {
			size += uint64(n)
			if size > bound {
				return 0, "", domain.Fail("SOURCE_CHANGED", "the disk grew while it was being read")
			}
			if _, err = h.Write(buffer[:n]); err != nil {
				return 0, "", err
			}
		}
		if e != nil {
			return 0, "", e
		}
		if n == 0 {
			break
		}
	}
	if err = stream.Finish(); err != nil {
		return 0, "", err
	}
	finished = true
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

func (p *Provider) InspectDiskMove(ctx context.Context, uri, id, target, pool string, keepOldCopy bool) (domain.DiskMove, error) {
	var out domain.DiskMove
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	if err := removalKey(key); err != nil {
		return out, err
	}
	if !diskRemovalName(pool) {
		return out, domain.Fail("INVALID_INPUT", "an exact destination storage pool name is required")
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
		return out, domain.Fail("SOURCE_CHANGED", "VM identity changed during disk inspection")
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return out, domain.Fail("PERMISSION_DENIED", "a secure read of the definition is required before moving a disk")
	}
	if err = checkSecureConfiguration(vm.PersistentXML, secure); err != nil {
		return out, err
	}
	source, err := growDiskSource(vm.PersistentXML, target)
	if err != nil {
		return out, err
	}
	if source.pool == "" || source.volume == "" {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "only a disk already stored in a storage pool can be moved")
	}
	if source.format != "qcow2" {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "only a qcow2 disk can be moved")
	}
	if source.pool == pool {
		return out, domain.Fail("INVALID_INPUT", "this disk already lives in that storage pool; choose another destination")
	}
	held, err := lookupGrowVolume(c, source)
	if err != nil {
		return out, err
	}
	defer held.Free()
	disk, err := observeGrowDisk(held, target)
	if err != nil {
		return out, err
	}
	sourcePool, err := held.LookupPoolByVolume()
	if err != nil {
		return out, err
	}
	defer sourcePool.Free()
	sourceName, err := sourcePool.GetName()
	if err != nil {
		return out, err
	}
	if sourceName != source.pool || disk.VolumeName != source.volume || disk.Format != source.format {
		return out, domain.Fail("SOURCE_CHANGED", "the disk's volume differs from the VM's disk declaration")
	}
	if err = diskSharedElsewhere(ctx, c, id, disk, sourcePool); err != nil {
		return out, err
	}
	destination, err := c.LookupStoragePoolByName(pool)
	if err != nil {
		return out, err
	}
	defer destination.Free()
	observed, err := observePool(destination, uri)
	if err != nil {
		return out, err
	}
	if !observed.Active || observed.State != "running" || (observed.Type != "dir" && observed.Type != "fs" && observed.Type != "netfs") {
		return out, domain.Fail("SOURCE_CHANGED", "the destination is not an active file-based storage pool")
	}
	if observed.AvailableBytes == nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "this pool does not report its free space")
	}
	if observed.Key.UUID == disk.PoolID {
		return out, domain.Fail("INVALID_INPUT", "this disk already lives in that storage pool; choose another destination")
	}
	name, err := nextDiskVolumeName(destination, id)
	if err != nil {
		return out, err
	}
	// The retarget must be possible before anything is copied, and it is tried
	// with the name the copy will really take: a placeholder could collide with
	// this disk's own volume and refuse for the wrong reason.
	if _, err = xmlpatch.RetargetDisk(vm.PersistentXML, target, pool, name); err != nil {
		return out, err
	}
	definition, err := xmlpatch.HardwareDigest(vm.PersistentXML)
	if err != nil {
		return out, err
	}
	directory, err := cleanupPoolDirectory(destination)
	if err != nil {
		return out, err
	}
	out = domain.DiskMove{VM: key, VMFingerprint: vm.Fingerprint, DefinitionSHA256: definition, Disk: disk,
		SourcePoolName: sourceName, PoolID: observed.Key.UUID, PoolName: pool,
		VolumeName: name, VolumePath: filepath.Join(directory, name), CopyBytes: disk.CapacityBytes,
		DestinationAvailableBytes: *observed.AvailableBytes, KeepOldCopy: keepOldCopy}
	out.ResourceIDs = append([]string{key.String(), "local-file|" + out.VolumePath}, diskRemovalResources(uri, disk)...)
	sort.Strings(out.ResourceIDs)
	if err = validDiskMove(out); err != nil {
		return domain.DiskMove{}, err
	}
	return out, nil
}

// moveState reports what the host holds now, from the definition and the two
// volumes, without writing anything.
func moveState(definitionNamesCopy, sourcePresent, copyPresent bool) (string, error) {
	switch {
	case !definitionNamesCopy && sourcePresent && !copyPresent:
		return "before", nil
	case !definitionNamesCopy && sourcePresent && copyPresent:
		return "copied", nil
	case definitionNamesCopy && copyPresent && sourcePresent:
		return "retargeted", nil
	case definitionNamesCopy && copyPresent && !sourcePresent:
		return "old-deleted", nil
	case definitionNamesCopy && !copyPresent:
		return "", domain.Fail("RECOVERY_REQUIRED", "the definition names the copy but the copy is missing")
	}
	return "", domain.Fail("SOURCE_CHANGED", "neither the original disk nor its copy is where the review left it")
}

func volumePresent(c *native.Connect, poolID, name string) (bool, error) {
	pool, err := c.LookupStoragePoolByUUIDString(poolID)
	if err != nil {
		return false, err
	}
	defer pool.Free()
	v, err := pool.LookupStorageVolByName(name)
	if err == nil {
		v.Free()
		return true, nil
	}
	var e native.Error
	if errors.As(err, &e) && e.Code == native.ERR_NO_STORAGE_VOL {
		return false, nil
	}
	return false, err
}

func checkDiskMove(ctx context.Context, c *native.Connect, m domain.DiskMove) (string, error) {
	d, err := c.LookupDomainByUUIDString(m.VM.UUID)
	if err != nil {
		return "", err
	}
	defer d.Free()
	vm, err := growableVM(c, d, m.VM.ConnectionID)
	if err != nil {
		return "", err
	}
	if vm.Fingerprint != m.VMFingerprint {
		// The fingerprint covers the definition, which the retarget changes.
		namesCopy, e := definitionUsesVolume(vm.PersistentXML, m.VolumePath, m.PoolName, m.VolumeName)
		if e != nil {
			return "", e
		}
		if !namesCopy {
			return "", domain.Fail("STALE_PLAN", "the VM changed since the review; review again")
		}
	}
	namesCopy, err := definitionUsesVolume(vm.PersistentXML, m.VolumePath, m.PoolName, m.VolumeName)
	if err != nil {
		return "", err
	}
	namesSource, err := definitionUsesVolume(vm.PersistentXML, m.Disk.Path, m.SourcePoolName, m.Disk.VolumeName)
	if err != nil {
		return "", err
	}
	if namesCopy == namesSource {
		return "", domain.Fail("SOURCE_CHANGED", "this VM's definition does not name exactly one of the disk and its copy")
	}
	sourcePresent, err := volumePresent(c, m.Disk.PoolID, m.Disk.VolumeName)
	if err != nil {
		return "", err
	}
	copyPresent, err := volumePresent(c, m.PoolID, m.VolumeName)
	if err != nil {
		return "", err
	}
	if !namesCopy {
		digest, e := xmlpatch.HardwareDigest(vm.PersistentXML)
		if e != nil {
			return "", e
		}
		if digest != m.DefinitionSHA256 {
			return "", domain.Fail("STALE_PLAN", "the definition changed since the review; review again")
		}
	}
	return moveState(namesCopy, sourcePresent, copyPresent)
}

func (p *Provider) CheckDiskMove(ctx context.Context, m domain.DiskMove) (string, error) {
	if err := validDiskMove(m); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c, err := connect(m.VM.ConnectionID, true)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return checkDiskMove(ctx, c, m)
}

// CopyDiskVolume allocates the reviewed name in the destination pool and copies
// the disk into it over one connection, hashing as it goes, then verifies the
// stored bytes by reading them back. It never converts, never writes over an
// existing name and never touches the original.
func (p *Provider) CopyDiskVolume(ctx context.Context, m domain.DiskMove) error {
	if err := validDiskMove(m); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(m.VM.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	state, err := checkDiskMove(ctx, c, m)
	if err != nil {
		return err
	}
	if state != "before" {
		return domain.Fail("RECOVERY_REQUIRED", "the copy already exists or the disk moved; it was not copied again")
	}
	destination, err := c.LookupStoragePoolByUUIDString(m.PoolID)
	if err != nil {
		return err
	}
	defer destination.Free()
	if err = absentVolume(destination, m.VolumeName); err != nil {
		return err
	}
	sourcePool, err := c.LookupStoragePoolByUUIDString(m.Disk.PoolID)
	if err != nil {
		return err
	}
	defer sourcePool.Free()
	held, err := sourcePool.LookupStorageVolByName(m.Disk.VolumeName)
	if err != nil {
		return err
	}
	defer held.Free()
	current, err := observeGrowDisk(held, m.Disk.Target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, m.Disk) {
		return domain.Fail("SOURCE_CHANGED", "the held disk differs immediately before the copy")
	}
	x := fmt.Sprintf(`<volume><name>%s</name><capacity unit="bytes">%d</capacity><allocation unit="bytes">0</allocation><target><format type="raw"/></target></volume>`,
		xmlText(m.VolumeName), m.CopyBytes)
	created, err := destination.StorageVolCreateXML(x, 0)
	if err != nil {
		return err
	}
	defer created.Free()
	written, digest, err := copyVolumeBytes(ctx, c, held, created, m.CopyBytes)
	if err != nil {
		return err
	}
	// The copy is read back here and only here, against the digest taken while
	// it was written. Nothing may verify it after the define: verification
	// refuses a volume any definition names, so the retargeted disk would be
	// refused as busy.
	size, stored, err := hashVolumeBytes(ctx, c, created, m.CopyBytes)
	if err != nil {
		return err
	}
	if size != written || stored != digest {
		return domain.Fail("RECOVERY_REQUIRED", "the stored copy differs from the disk it was made from; inspect the operation without replaying it")
	}
	return nil
}

// copyVolumeBytes drives both streams on one connection: a bounded read from
// the source, the same bytes written to the destination, and one digest over
// what passed through. It returns what it wrote so the caller can read the
// copy back against the same digest. Nothing is retried here.
func copyVolumeBytes(ctx context.Context, c *native.Connect, from, to *native.StorageVol, bound uint64) (uint64, string, error) {
	reader, err := c.NewStream(0)
	if err != nil {
		return 0, "", err
	}
	defer reader.Free()
	writer, err := c.NewStream(0)
	if err != nil {
		return 0, "", err
	}
	defer writer.Free()
	joinReader, joinWriter := watchStream(ctx, reader), watchStream(ctx, writer)
	defer joinReader()
	defer joinWriter()
	done := false
	defer func() {
		if !done {
			_ = reader.Abort()
			_ = writer.Abort()
		}
	}()
	if err = from.Download(reader, 0, 0, 0); err != nil {
		return 0, "", err
	}
	// Length zero uploads whatever the stream carries, so the copy is exactly
	// as long as the disk turns out to be, bounded below.
	if err = to.Upload(writer, 0, 0, 0); err != nil {
		return 0, "", err
	}
	h := sha256.New()
	var copied uint64
	buffer := make([]byte, moveChunkBytes)
	for {
		if err = ctx.Err(); err != nil {
			return 0, "", err
		}
		n, e := reader.Recv(buffer)
		if n > 0 {
			copied += uint64(n)
			if copied > bound {
				return 0, "", domain.Fail("SOURCE_CHANGED", "the disk is larger than the reviewed copy allows")
			}
			if _, err = h.Write(buffer[:n]); err != nil {
				return 0, "", err
			}
			for written := 0; written < n; {
				w, we := writer.Send(buffer[written:n])
				if we != nil {
					return 0, "", we
				}
				if w <= 0 {
					return 0, "", domain.Fail("OPERATION_FAILED", "the destination stopped accepting the copy")
				}
				written += w
			}
		}
		if e != nil {
			return 0, "", e
		}
		if n == 0 {
			break
		}
	}
	if copied == 0 {
		return 0, "", domain.Fail("SOURCE_CHANGED", "the disk read as empty; nothing was copied")
	}
	if err = reader.Finish(); err != nil {
		return 0, "", err
	}
	if err = writer.Finish(); err != nil {
		return 0, "", err
	}
	done = true
	return copied, hex.EncodeToString(h.Sum(nil)), nil
}

// DefineMovedDisk points the reviewed disk at the verified copy and reads the
// stored definition back. The edit replaces the disk's source in place, so the
// result is confirmed by the same normalisation-tolerant digest cold restore
// uses, plus the definition naming the copy and no longer the original.
func (p *Provider) DefineMovedDisk(ctx context.Context, m domain.DiskMove) error {
	if err := validDiskMove(m); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(m.VM.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	state, err := checkDiskMove(ctx, c, m)
	if err != nil {
		return err
	}
	if state != "copied" {
		return domain.Fail("RECOVERY_REQUIRED", "the verified copy is not the only thing waiting; the definition was not changed")
	}
	d, err := c.LookupDomainByUUIDString(m.VM.UUID)
	if err != nil {
		return err
	}
	defer d.Free()
	vm, err := growableVM(c, d, m.VM.ConnectionID)
	if err != nil {
		return err
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("PERMISSION_DENIED", "a secure read of the definition is required before moving a disk")
	}
	if err = checkSecureConfiguration(vm.PersistentXML, secure); err != nil {
		return err
	}
	after, err := xmlpatch.RetargetDisk(vm.PersistentXML, m.Disk.Target, m.PoolName, m.VolumeName)
	if err != nil {
		return err
	}
	expected, err := xmlpatch.HardwareDigest(after)
	if err != nil {
		return err
	}
	defined, err := c.DomainDefineXMLFlags(after, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "native definition of the moved disk failed; XML values withheld; inspect and reconcile without replay")
	}
	defer defined.Free()
	read, err := observe(defined, m.VM.ConnectionID)
	if err != nil {
		return err
	}
	securely, err := defined.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "secure readback unavailable after moving the disk; do not replay")
	}
	if read.State != "stopped" {
		return domain.Fail("RECOVERY_REQUIRED", "the VM is no longer stopped after moving the disk; inspect the operation without replaying it")
	}
	if read.HasManagedSave {
		return domain.Fail("RECOVERY_REQUIRED", "the VM acquired saved state while moving the disk; inspect the operation without replaying it")
	}
	stored, err := xmlpatch.HardwareDigest(read.PersistentXML)
	if err != nil {
		return err
	}
	if stored != expected {
		failure := domain.Fail("RECOVERY_REQUIRED", "the stored definition differs from the reviewed result beyond libvirt's own formatting; inspect the operation without replaying it")
		failure.Details = map[string]string{"expected": expected, "stored": stored}
		return failure
	}
	if securely != read.PersistentXML {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "the stored definition omits sensitive settings, so the moved disk cannot be confirmed; inspect the operation without replaying it")
	}
	namesCopy, err := definitionUsesVolume(read.PersistentXML, m.VolumePath, m.PoolName, m.VolumeName)
	if err != nil {
		return err
	}
	namesSource, err := definitionUsesVolume(read.PersistentXML, m.Disk.Path, m.SourcePoolName, m.Disk.VolumeName)
	if err != nil {
		return err
	}
	if !namesCopy || namesSource {
		return domain.Fail("RECOVERY_REQUIRED", "the stored definition does not name the copy alone at "+m.Disk.Target)
	}
	return nil
}

// DeleteOldVolume removes the original after the definition names the copy.
// Exactly one non-wiping delete, only while no definition on this connection
// names the original, and never during recovery.
func (p *Provider) DeleteOldVolume(ctx context.Context, m domain.DiskMove) error {
	if err := validDiskMove(m); err != nil {
		return err
	}
	if m.KeepOldCopy {
		return domain.Fail("INVALID_INPUT", "this move keeps the original disk")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(m.VM.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	state, err := checkDiskMove(ctx, c, m)
	if err != nil {
		return err
	}
	if state == "old-deleted" {
		return domain.Fail("RECOVERY_REQUIRED", "the original is already gone; it was not deleted again")
	}
	if state != "retargeted" {
		return domain.Fail("RECOVERY_REQUIRED", "the definition does not name the copy yet; the original was kept")
	}
	pool, err := c.LookupStoragePoolByUUIDString(m.Disk.PoolID)
	if err != nil {
		return err
	}
	defer pool.Free()
	volume, err := pool.LookupStorageVolByName(m.Disk.VolumeName)
	if err != nil {
		return err
	}
	defer volume.Free()
	current, err := observeRemovalDisk(volume, m.Disk.Target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, m.Disk) {
		return domain.Fail("SOURCE_CHANGED", "the held original differs immediately before deletion")
	}
	// No definition anywhere on this connection may still name it.
	if err = originalUnreferenced(ctx, c, m); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return volume.Delete(0)
}

// originalUnreferenced refuses deletion while any saved or running definition
// names the original volume, by path or by pool volume.
func originalUnreferenced(ctx context.Context, c *native.Connect, m domain.DiskMove) error {
	domains, err := c.ListAllDomains(0)
	if err != nil {
		return err
	}
	defer func() {
		for i := range domains {
			domains[i].Free()
		}
	}()
	for i := range domains {
		if err = ctx.Err(); err != nil {
			return err
		}
		d := &domains[i]
		definitions := []string{}
		if persistent, e := d.IsPersistent(); e != nil {
			return e
		} else if persistent {
			x, e := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
			if e != nil {
				return e
			}
			definitions = append(definitions, x)
		}
		if active, e := d.IsActive(); e != nil {
			return e
		} else if active {
			x, e := d.GetXMLDesc(0)
			if e != nil {
				return e
			}
			definitions = append(definitions, x)
		}
		for _, x := range definitions {
			used, e := definitionUsesVolume(x, m.Disk.Path, m.SourcePoolName, m.Disk.VolumeName)
			if e != nil {
				return e
			}
			if used {
				return domain.Fail("RESOURCE_BUSY", "a VM definition still uses the original disk; it was kept")
			}
		}
	}
	return nil
}
