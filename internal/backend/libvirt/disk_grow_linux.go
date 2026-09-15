//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"encoding/xml"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

var _ domain.DiskGrowProvider = (*Provider)(nil)

// InspectDiskGrow observes one writable disk of a stopped VM for growing
// (ADR 0062). Unlike removal, other running VMs do not block it: only a VM or
// an image that uses the same volume does.
func (p *Provider) InspectDiskGrow(ctx context.Context, uri, id, target string, capacity uint64) (domain.DiskGrow, error) {
	var out domain.DiskGrow
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
	vm, err := growableVM(d, uri)
	if err != nil {
		return out, err
	}
	if vm.Key != key {
		return out, domain.Fail("SOURCE_CHANGED", "VM identity changed during disk inspection")
	}
	sources, err := removalDiskSources(vm.PersistentXML, []string{target})
	if err != nil {
		return out, err
	}
	volume, err := c.LookupStorageVolByPath(sources[0].path)
	if err != nil {
		return out, err
	}
	defer volume.Free()
	disk, err := observeGrowDisk(volume, target)
	if err != nil {
		return out, err
	}
	if disk.Path != sources[0].path || disk.Format != sources[0].format {
		return out, domain.Fail("SOURCE_CHANGED", "the disk's volume differs from the VM's disk declaration")
	}
	pool, err := volume.LookupPoolByVolume()
	if err != nil {
		return out, err
	}
	defer pool.Free()
	if err = diskSharedElsewhere(ctx, c, id, disk, pool); err != nil {
		return out, err
	}
	info, err := pool.GetInfo()
	if err != nil {
		return out, err
	}
	out = domain.DiskGrow{VM: key, VMFingerprint: vm.Fingerprint, Disk: disk, CapacityBytes: capacity, PoolAvailableBytes: info.Available}
	out.ResourceIDs = append([]string{key.String()}, diskRemovalResources(uri, disk)...)
	sort.Strings(out.ResourceIDs)
	return out, nil
}

// growableVM requires a stopped persistent VM without saved, snapshot or
// checkpoint state; libvirt snapshots may hold internal qcow2 snapshots.
func growableVM(d *native.Domain, uri string) (domain.VM, error) {
	vm, err := observe(d, uri)
	if err != nil {
		return vm, err
	}
	if vm.State != "stopped" || vm.PersistentXML == "" || vm.LiveXML != "" || vm.HasManagedSave {
		return vm, domain.Fail("RESOURCE_BUSY", "shut down the VM before growing its disk; saved state must be resumed and shut down first")
	}
	h := nativeRemovalHandle{d}
	snapshots, err := h.snapshotCount()
	if err != nil {
		return vm, err
	}
	checkpoints, err := h.checkpointCount()
	if err != nil {
		return vm, err
	}
	if snapshots != 0 || checkpoints != 0 {
		return vm, domain.Fail("UNSUPPORTED_CAPABILITY", "this VM has snapshots or checkpoints; growing a disk under them is not supported")
	}
	return vm, nil
}

// observeGrowDisk is the removal observation plus a refusal of a volume with a
// backing file, whose chain a resize would not cover.
func observeGrowDisk(volume *native.StorageVol, target string) (domain.RemovalDisk, error) {
	disk, err := observeRemovalDisk(volume, target)
	if err != nil {
		return disk, err
	}
	raw, err := volume.GetXMLDesc(0)
	if err != nil {
		return disk, err
	}
	if backing, err := volumeBackingPath(raw); err != nil {
		return disk, err
	} else if backing != "" {
		return disk, domain.Fail("UNSUPPORTED_CAPABILITY", "this disk has a backing file; growing layered disks is not supported")
	}
	return disk, nil
}

func volumeBackingPath(raw string) (string, error) {
	var meta struct {
		Backing *struct {
			Path string `xml:"path"`
		} `xml:"backingStore"`
	}
	if err := xml.Unmarshal([]byte(raw), &meta); err != nil {
		return "", domain.Fail("INVALID_INPUT", "unreadable volume metadata")
	}
	if meta.Backing == nil {
		return "", nil
	}
	return strings.TrimSpace(meta.Backing.Path), nil
}

// definitionUsesVolume reports whether a domain definition names the volume as
// a disk source, including inside a backing chain, by path or pool volume.
func definitionUsesVolume(raw, path, pool, volume string) (bool, error) {
	dec := xml.NewDecoder(strings.NewReader(raw))
	disks := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, domain.Fail("INVALID_INPUT", "unreadable VM definition")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "disk" {
				disks++
			}
			if disks > 0 && t.Name.Local == "source" {
				var file, p, v string
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "file":
						file = a.Value
					case "pool":
						p = a.Value
					case "volume":
						v = a.Value
					}
				}
				if file == path || p != "" && p == pool && v == volume {
					return true, nil
				}
			}
		case xml.EndElement:
			if t.Name.Local == "disk" {
				disks--
			}
		}
	}
}

// diskSharedElsewhere refuses a volume that another VM's saved or running
// definition uses, or that another volume in an active pool is layered on.
func diskSharedElsewhere(ctx context.Context, c *native.Connect, owner string, disk domain.RemovalDisk, pool *native.StoragePool) error {
	poolName, err := pool.GetName()
	if err != nil {
		return err
	}
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
		if err := ctx.Err(); err != nil {
			return err
		}
		d := &domains[i]
		id, err := d.GetUUIDString()
		if err != nil {
			return err
		}
		if id == owner {
			continue
		}
		definitions := []string{}
		if persistent, err := d.IsPersistent(); err != nil {
			return err
		} else if persistent {
			x, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
			if err != nil {
				return err
			}
			definitions = append(definitions, x)
		}
		if active, err := d.IsActive(); err != nil {
			return err
		} else if active {
			x, err := d.GetXMLDesc(0)
			if err != nil {
				return err
			}
			definitions = append(definitions, x)
		}
		for _, x := range definitions {
			used, err := definitionUsesVolume(x, disk.Path, poolName, disk.VolumeName)
			if err != nil {
				return err
			}
			if used {
				name, _ := d.GetName()
				return domain.Fail("RESOURCE_BUSY", "another VM ("+validation.SafeText(name)+") uses this disk; it cannot be grown")
			}
		}
	}
	pools, err := c.ListAllStoragePools(native.CONNECT_LIST_STORAGE_POOLS_ACTIVE)
	if err != nil {
		return err
	}
	defer func() {
		for i := range pools {
			pools[i].Free()
		}
	}()
	for i := range pools {
		volumes, err := pools[i].ListAllStorageVolumes(0)
		if err != nil {
			return err
		}
		layered := false
		for j := range volumes {
			if err == nil && !layered {
				var raw, backing string
				if raw, err = volumes[j].GetXMLDesc(0); err == nil {
					backing, err = volumeBackingPath(raw)
					layered = backing == disk.Path
				}
			}
			volumes[j].Free()
		}
		if err != nil {
			return err
		}
		if layered {
			return domain.Fail("RESOURCE_BUSY", "another disk image is layered on this disk; it cannot be grown")
		}
	}
	return nil
}

func sameGrowVolume(a, b domain.RemovalDisk) bool {
	return a.Target == b.Target && a.Path == b.Path && a.PoolID == b.PoolID && a.VolumeName == b.VolumeName && a.VolumeKey == b.VolumeKey && a.Generation == b.Generation && a.Format == b.Format
}

func validDiskGrow(g domain.DiskGrow) error {
	if err := removalKey(g.VM); err != nil {
		return err
	}
	d := g.Disk
	if !digestPattern.MatchString(g.VMFingerprint) || !coldDiskTarget.MatchString(d.Target) || coldPath(d.Path) != nil || !uuidPattern.MatchString(d.PoolID) || !diskRemovalName(d.VolumeName) || d.VolumeName != filepath.Base(d.Path) || d.VolumeKey == "" || !strings.HasPrefix(d.Generation, "linux-statx-v1:") || !digestPattern.MatchString(d.Fingerprint) || (d.Format != "raw" && d.Format != "qcow2") || d.CapacityBytes == 0 || g.CapacityBytes <= d.CapacityBytes {
		return domain.Fail("INVALID_INPUT", "complete exact disk grow observation required")
	}
	want := append([]string{g.VM.String()}, diskRemovalResources(g.VM.ConnectionID, d)...)
	sort.Strings(want)
	if !slices.Equal(want, g.ResourceIDs) {
		return domain.Fail("INVALID_INPUT", "canonical resource lock set required")
	}
	return nil
}

func checkDiskGrow(ctx context.Context, c *native.Connect, g domain.DiskGrow) (string, *native.StorageVol, error) {
	d, err := c.LookupDomainByUUIDString(g.VM.UUID)
	if err != nil {
		return "", nil, err
	}
	defer d.Free()
	vm, err := growableVM(d, g.VM.ConnectionID)
	if err != nil {
		return "", nil, err
	}
	if vm.Fingerprint != g.VMFingerprint {
		return "", nil, domain.Fail("STALE_PLAN", "the VM changed since the review")
	}
	pool, err := c.LookupStoragePoolByUUIDString(g.Disk.PoolID)
	if err != nil {
		return "", nil, err
	}
	defer pool.Free()
	volume, err := pool.LookupStorageVolByName(g.Disk.VolumeName)
	if err != nil {
		return "", nil, err
	}
	disk, err := observeGrowDisk(volume, g.Disk.Target)
	if err == nil {
		err = diskSharedElsewhere(ctx, c, g.VM.UUID, disk, pool)
	}
	if err != nil {
		volume.Free()
		return "", nil, err
	}
	switch {
	case reflect.DeepEqual(disk, g.Disk):
		return "before", volume, nil
	case sameGrowVolume(disk, g.Disk) && disk.CapacityBytes == g.CapacityBytes:
		return "grown", volume, nil
	}
	volume.Free()
	return "", nil, domain.Fail("SOURCE_CHANGED", "the disk changed since the review")
}

func (p *Provider) CheckDiskGrow(ctx context.Context, g domain.DiskGrow) (string, error) {
	if err := validDiskGrow(g); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c, err := connect(g.VM.ConnectionID, true)
	if err != nil {
		return "", err
	}
	defer c.Close()
	state, volume, err := checkDiskGrow(ctx, c, g)
	if volume != nil {
		volume.Free()
	}
	return state, err
}

// GrowDisk makes exactly one non-shrinking, non-allocating resize of the held,
// rechecked volume. The caller journals the intent and reconciles a lost
// acknowledgement by observation, never by resizing again.
func (p *Provider) GrowDisk(ctx context.Context, g domain.DiskGrow) error {
	if err := validDiskGrow(g); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(g.VM.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	state, volume, err := checkDiskGrow(ctx, c, g)
	if err != nil {
		return err
	}
	defer volume.Free()
	if state != "before" {
		return domain.Fail("RECOVERY_REQUIRED", "the disk already has the requested size; it was not resized again")
	}
	return volume.Resize(g.CapacityBytes, 0)
}
