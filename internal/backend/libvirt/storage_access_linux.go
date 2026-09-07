//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	native "libvirt.org/go/libvirt"
	"path/filepath"
	"regexp"
	"strings"
	"virmill.local/core/internal/domain"
)

var managedVolumeSuffix = regexp.MustCompile(`^(disk-[0-9]{3}\.qcow2|media-[0-9]{3}\.iso)$`)

func managedDiskSource(xml, target string) (*xmlNode, error) {
	tree, err := xmlTree(xml)
	if err != nil {
		return nil, err
	}
	if tree.name.Local != "domain" || tree.name.Space != "" {
		return nil, domain.Fail("INVALID_INPUT", "ordinary domain XML required")
	}
	var devices *xmlNode
	for _, n := range tree.children {
		if n.name.Local == "devices" && n.name.Space == "" {
			if devices != nil {
				return nil, domain.Fail("INVALID_INPUT", "ambiguous device inventory")
			}
			devices = n
		}
	}
	if devices == nil {
		return nil, domain.Fail("INVALID_INPUT", "domain devices missing")
	}
	var found *xmlNode
	for _, n := range devices.children {
		if n.name.Local != "disk" || n.name.Space != "" {
			continue
		}
		count := 0
		matches := false
		for _, c := range n.children {
			if c.name.Local == "target" && c.name.Space == "" {
				count++
				matches = matches || attr(c, "dev") == target
			}
		}
		if !matches {
			continue
		}
		if found != nil || count != 1 {
			return nil, domain.Fail("INVALID_INPUT", "ambiguous disk target")
		}
		if attr(n, "device") != "disk" && attr(n, "device") != "cdrom" {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "ordinary disk or retained read-only medium required")
		}
		sourceCount := 0
		for _, c := range n.children {
			if c.name.Local == "source" && c.name.Space == "" {
				sourceCount++
			}
		}
		if sourceCount != 1 {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "exactly one nonempty disk source required")
		}
		if attr(n, "device") == "cdrom" && child(n, "readonly") == nil {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "retained medium must be explicitly read-only")
		}
		found = n
	}
	if found == nil {
		return nil, domain.Fail("INVALID_INPUT", "disk target not found")
	}
	return found, nil
}

func (p *Provider) InspectManagedFileVolume(ctx context.Context, uri, id, target string) (domain.ManagedFileVolume, error) {
	var out domain.ManagedFileVolume
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if uri != "qemu:///system" {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "privileged read grants are limited to local system libvirt file volumes")
	}
	if id == "" || target == "" {
		return out, domain.Fail("INVALID_INPUT", "VM UUID and explicit disk target required")
	}
	c, err := connect(uri, false)
	if err != nil {
		return out, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		return out, err
	}
	defer d.Free()
	vm, err := observe(d, uri)
	if err != nil {
		return out, err
	}
	if vm.State != "stopped" || vm.PersistentXML == "" || vm.HasManagedSave || vm.Autostart {
		return out, domain.Fail("RESOURCE_BUSY", "read-access change requires a stopped persistent VM without managed-save state or autostart")
	}
	disk, err := managedDiskSource(vm.PersistentXML, target)
	if err != nil {
		return out, err
	}
	source := child(disk, "source")
	var volume *native.StorageVol
	switch attr(disk, "type") {
	case "file":
		path := attr(source, "file")
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return out, domain.Fail("UNSUPPORTED_CAPABILITY", "canonical local file volume required")
		}
		volume, err = c.LookupStorageVolByPath(path)
	case "volume":
		pool, e := c.LookupStoragePoolByName(attr(source, "pool"))
		if e != nil {
			return out, e
		}
		volume, err = pool.LookupStorageVolByName(attr(source, "volume"))
		pool.Free()
	default:
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "file-backed managed volume required")
	}
	if err != nil {
		return out, err
	}
	defer volume.Free()
	pool, err := volume.LookupPoolByVolume()
	if err != nil {
		return out, err
	}
	defer pool.Free()
	observed, err := observePool(pool, uri)
	if err != nil {
		return out, err
	}
	if !observed.Active || observed.State != "running" || (observed.Type != "dir" && observed.Type != "fs" && observed.Type != "netfs") {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "active local file pool required")
	}
	directory, err := cleanupPoolDirectory(pool)
	if err != nil {
		return out, err
	}
	name, err := volume.GetName()
	if err != nil {
		return out, err
	}
	prefix := "virmill-" + vm.Key.UUID + "-"
	if !strings.HasPrefix(name, prefix) || !managedVolumeSuffix.MatchString(strings.TrimPrefix(name, prefix)) {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "read-access helper requires a Virmill-named volume matching this VM identity")
	}
	path, err := volume.GetPath()
	if err != nil {
		return out, err
	}
	if path != filepath.Join(directory, name) || (attr(disk, "type") == "file" && attr(source, "file") != path) {
		return out, domain.Fail("SOURCE_CHANGED", "native volume path differs from the selected disk and exact pool entry")
	}
	key, err := volume.GetKey()
	if err != nil {
		return out, err
	}
	latest, err := observe(d, uri)
	if err != nil {
		return out, err
	}
	if latest.Fingerprint != vm.Fingerprint {
		return out, domain.Fail("STALE_PLAN", "VM changed during managed volume observation")
	}
	return domain.ManagedFileVolume{VMID: vm.Key.UUID, VMFingerprint: vm.Fingerprint, DiskTarget: target, PoolID: observed.Key.UUID, PoolFingerprint: observed.Fingerprint, VolumeName: name, VolumeKey: key, Path: path}, nil
}

var _ domain.ManagedFileAccessBackend = (*Provider)(nil)
