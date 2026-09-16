//go:build linux && cgo

package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
)

var _ domain.DefinitionRemovalProvider = (*Provider)(nil)

// A UEFI VM's NVRAM file and an emulated TPM's state are kept through the KEEP
// flags and listed in the review; every other auxiliary state profile is still
// refused (ADR 0063). Retaining disk paths is not a disk capture or a claim
// that their backing graph has been verified.
type removalHandle interface {
	observation(string) (domain.VM, error)
	active() (bool, error)
	persistent() (bool, error)
	snapshotCount() (int, error)
	checkpointCount() (int, error)
	secureXML() (string, error)
	volumePath(pool, volume string) (string, error)
	undefine(native.DomainUndefineFlagsValues) error
}
type nativeRemovalHandle struct {
	d *native.Domain
	c *native.Connect
}

// volumePath resolves a disk declared as a pool volume, as the VMs Virmill
// creates are, to its exact registered pool entry.
func (h nativeRemovalHandle) volumePath(pool, volume string) (string, error) {
	if h.c == nil {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "pool-volume disks require a connected inspection")
	}
	if coldSourceIdentifier(pool, 255) != nil || !diskRemovalName(volume) {
		return "", domain.Fail("INVALID_INPUT", "exact pool and volume names required")
	}
	p, err := h.c.LookupStoragePoolByName(pool)
	if err != nil {
		return "", err
	}
	defer p.Free()
	v, err := p.LookupStorageVolByName(volume)
	if err != nil {
		return "", err
	}
	defer v.Free()
	path, err := v.GetPath()
	if err != nil {
		return "", err
	}
	directory, err := cleanupPoolDirectory(p)
	if err != nil {
		return "", err
	}
	if coldPath(path) != nil || path != filepath.Join(directory, volume) {
		return "", domain.Fail("SOURCE_CHANGED", "the volume is not the exact registered pool entry")
	}
	return path, nil
}

func (h nativeRemovalHandle) observation(uri string) (domain.VM, error) { return observe(h.d, uri) }
func (h nativeRemovalHandle) active() (bool, error)                     { return h.d.IsActive() }
func (h nativeRemovalHandle) persistent() (bool, error)                 { return h.d.IsPersistent() }
func (h nativeRemovalHandle) snapshotCount() (int, error)               { return h.d.SnapshotNum(0) }
func (h nativeRemovalHandle) checkpointCount() (int, error) {
	items, err := h.d.ListAllCheckpoints(0)
	if err != nil {
		return 0, err
	}
	count := len(items)
	var releaseErr error
	for i := range items {
		if err := items[i].Free(); err != nil && releaseErr == nil {
			releaseErr = err
		}
	}
	return count, releaseErr
}
func (h nativeRemovalHandle) secureXML() (string, error) {
	return h.d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
}
func (h nativeRemovalHandle) undefine(flags native.DomainUndefineFlagsValues) error {
	return h.d.UndefineFlags(flags)
}

func removalKey(key domain.ResourceKey) error {
	if err := Connection(key.ConnectionID); err != nil {
		return err
	}
	if key.ProviderID != "libvirt" || key.Kind != "vm" || !uuidPattern.MatchString(key.UUID) || key.UUID == "00000000-0000-0000-0000-000000000000" {
		return domain.Fail("INVALID_INPUT", "definition removal requires an exact local libvirt VM UUID")
	}
	return nil
}
func (p *Provider) InspectDefinitionRemoval(ctx context.Context, uri, id string) (domain.DefinitionRemoval, error) {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	if err := removalKey(key); err != nil {
		return domain.DefinitionRemoval{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.DefinitionRemoval{}, err
	}
	// Libvirt requires a writable connection for DOMAIN_XML_SECURE, even
	// though this inspection only reads state. No mutation occurs here; the
	// separately authorized RemoveDefinition method owns the undefine call.
	c, err := connect(uri, true)
	if err != nil {
		return domain.DefinitionRemoval{}, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		return domain.DefinitionRemoval{}, err
	}
	defer d.Free()
	return inspectRemovalHandle(ctx, nativeRemovalHandle{d, c}, key)
}
func inspectRemovalHandle(ctx context.Context, h removalHandle, key domain.ResourceKey) (domain.DefinitionRemoval, error) {
	var out domain.DefinitionRemoval
	if err := ctx.Err(); err != nil {
		return out, err
	}
	vm, err := h.observation(key.ConnectionID)
	if err != nil {
		return out, err
	}
	if vm.Key != key {
		return out, domain.Fail("SOURCE_CHANGED", "VM identity changed during removal inspection")
	}
	if vm.State != "stopped" || vm.PersistentXML == "" || vm.LiveXML != "" || vm.Autostart || vm.HasManagedSave {
		return out, domain.Fail("RESOURCE_BUSY", "removal requires a stopped persistent VM with autostart disabled and no managed-save state")
	}
	persistent, err := h.persistent()
	if err != nil {
		return out, err
	}
	active, err := h.active()
	if err != nil {
		return out, err
	}
	if !persistent || active {
		return out, domain.Fail("RESOURCE_BUSY", "VM must remain stopped and persistent")
	}
	snapshots, err := h.snapshotCount()
	if err != nil {
		return out, err
	}
	checkpoints, err := h.checkpointCount()
	if err != nil {
		return out, err
	}
	if snapshots != 0 || checkpoints != 0 {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "snapshot or checkpoint metadata requires a separate retention adapter before removal")
	}
	secure, err := h.secureXML()
	if err != nil {
		return out, err
	}
	if secure != vm.PersistentXML {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "definition contains protected settings requiring a secret-preserving retention adapter")
	}
	sources, err := removalSources(vm.PersistentXML, key.UUID, vm.Name, h)
	if err != nil {
		return out, err
	}
	firmware, tpm, err := removalAuxiliaryState(vm.PersistentXML)
	if err != nil {
		return out, err
	}
	if firmware != "" {
		sources = append(sources, firmware)
		sort.Strings(sources)
		sources = slices.Compact(sources)
	}
	sum := sha256.Sum256([]byte(vm.PersistentXML))
	out = domain.DefinitionRemoval{Resource: key, Name: vm.Name, Fingerprint: vm.Fingerprint, DefinitionSHA256: hex.EncodeToString(sum[:]), RetainedSources: sources, Firmware: firmware, EmulatedTPM: tpm}
	// Metadata inventory and secure observation must not silently straddle a
	// concurrent definition change. There is no cross-client atomic libvirt CAS.
	again, err := h.observation(key.ConnectionID)
	if err != nil {
		return domain.DefinitionRemoval{}, err
	}
	if !reflect.DeepEqual(vm, again) {
		return domain.DefinitionRemoval{}, domain.Fail("STALE_PLAN", "VM changed during removal inspection")
	}
	if err := ctx.Err(); err != nil {
		return domain.DefinitionRemoval{}, err
	}
	return out, nil
}
func (p *Provider) RemoveDefinition(ctx context.Context, expected domain.DefinitionRemoval) error {
	if err := removalKey(expected.Resource); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(expected.Resource.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(expected.Resource.UUID)
	if err != nil {
		return err
	}
	defer d.Free()
	return removeHeldDefinition(ctx, nativeRemovalHandle{d, c}, expected)
}
func removeHeldDefinition(ctx context.Context, h removalHandle, expected domain.DefinitionRemoval) error {
	current, err := inspectRemovalHandle(ctx, h, expected.Resource)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, expected) {
		return domain.Fail("STALE_PLAN", "VM definition or retained source list changed after review")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Keep a UEFI VM's firmware settings file and an emulated TPM's state. Both
	// are listed in the review; deleting them needs a separate request.
	var flags native.DomainUndefineFlagsValues
	if expected.Firmware != "" {
		flags |= native.DOMAIN_UNDEFINE_KEEP_NVRAM
	}
	if expected.EmulatedTPM {
		flags |= native.DOMAIN_UNDEFINE_KEEP_TPM
	}
	return h.undefine(flags)
}

// removalAuxiliaryState reports a VM's NVRAM file and whether it has an
// emulated TPM. Removal keeps both; nothing here is deleted or rewritten.
func removalAuxiliaryState(raw string) (string, bool, error) {
	root, err := coldStateTree(raw)
	if err != nil {
		return "", false, err
	}
	system, err := coldChild(root, "os", true)
	if err != nil {
		return "", false, err
	}
	firmware := ""
	nvram, err := coldChild(system, "nvram", false)
	if err != nil {
		return "", false, err
	}
	if nvram != nil {
		if err = coldAttrs(nvram, nil, []string{"template", "templateFormat", "format", "type"}); err != nil {
			return "", false, err
		}
		firmware = strings.TrimSpace(nvram.text)
		if len(nvram.children) != 0 || attr(nvram, "type") != "" && attr(nvram, "type") != "file" || coldPath(firmware) != nil {
			return "", false, domain.Fail("UNSUPPORTED_CAPABILITY", "this VM's firmware settings file needs a retention adapter; the definition was kept")
		}
	}
	devices, err := coldChild(root, "devices", true)
	if err != nil {
		return "", false, err
	}
	tpm := false
	for _, n := range devices.children {
		if n.name.Local != "tpm" {
			continue
		}
		if tpm {
			return "", false, domain.Fail("UNSUPPORTED_CAPABILITY", "more than one TPM needs a retention adapter; the definition was kept")
		}
		backend, err := coldChild(n, "backend", true)
		if err != nil {
			return "", false, err
		}
		if attr(backend, "type") != "emulator" {
			return "", false, domain.Fail("UNSUPPORTED_CAPABILITY", "only an emulated TPM's state is kept during removal; this TPM needs a retention adapter")
		}
		tpm = true
	}
	return firmware, tpm, nil
}
func (p *Provider) DefinitionAbsent(ctx context.Context, key domain.ResourceKey) (bool, error) {
	if err := removalKey(key); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c, err := connect(key.ConnectionID, false)
	if err != nil {
		return false, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(key.UUID)
	if err != nil {
		return removalLookupAbsent(err)
	}
	defer d.Free()
	return false, nil
}
func removalLookupAbsent(err error) (bool, error) {
	var e native.Error
	if errors.As(err, &e) && e.Code == native.ERR_NO_DOMAIN {
		return true, nil
	}
	return false, err
}

func removalSources(raw, id, name string, h removalHandle) ([]string, error) {
	root, err := coldStateTree(raw)
	if err != nil {
		return nil, err
	}
	refuse := func() error {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "removal currently supports BIOS x86 file-backed disks only; this auxiliary state, device or storage layout needs a retention adapter")
	}
	uuid, err := coldChild(root, "uuid", true)
	if err != nil {
		return nil, err
	}
	named, err := coldChild(root, "name", true)
	if err != nil {
		return nil, err
	}
	if uuid.text != id || named.text != name || len(uuid.children) != 0 || len(named.children) != 0 || len(uuid.attrs) != 0 || len(named.attrs) != 0 {
		return nil, domain.Fail("SOURCE_CHANGED", "persistent definition identity differs from native VM")
	}
	os, err := coldChild(root, "os", true)
	if err != nil {
		return nil, err
	}
	if err := coldAttrs(os, nil, []string{"firmware"}); err != nil {
		return nil, err
	}
	// UEFI is supported: its NVRAM file is kept and listed, never deleted.
	if attr(os, "firmware") != "" && attr(os, "firmware") != "bios" && attr(os, "firmware") != "efi" {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "this firmware setting needs a retention adapter; the definition was kept")
	}
	ostype, err := coldChild(os, "type", true)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ostype.text) != "hvm" || (attr(ostype, "arch") != "x86_64" && attr(ostype, "arch") != "i686") {
		return nil, refuse()
	}
	for _, n := range os.children {
		if n.name.Space != "" || !coldEnum(n.name.Local, "type", "boot", "bootmenu", "bios", "smbios", "firmware", "loader", "nvram") {
			return nil, refuse()
		}
	}
	devices, err := coldChild(root, "devices", true)
	if err != nil {
		return nil, err
	}
	for _, n := range root.children {
		if _, err := coldChild(root, n.name.Local, false); err != nil {
			return nil, err
		}
		if n.name.Space != "" || !coldEnum(n.name.Local, "name", "uuid", "metadata", "os", "devices", "hwuuid", "genid", "title", "description", "memory", "currentMemory", "maxMemory", "memoryBacking", "vcpu", "vcpus", "cpu", "cputune", "numatune", "blkiotune", "memtune", "resource", "sysinfo", "features", "clock", "on_poweroff", "on_reboot", "on_crash", "on_lockfailure", "pm", "seclabel", "idmap", "keywrap", "perf", "iothreadids", "iothreads", "defaultiothread", "throttlegroups") {
			return nil, refuse()
		}
	}
	sources := map[string]bool{}
	targets := map[string]bool{}
	diskCount := 0
	for _, n := range devices.children {
		if n.name.Space != "" {
			return nil, refuse()
		}
		if n.name.Local != "disk" {
			// An emulated TPM's state is kept; device NVRAM and pstore files
			// still need their own retention adapter.
			if coldEnum(n.name.Local, "nvram", "pstore") {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "NVRAM or pstore retention during removal requires a dedicated adapter; no state was discarded")
			}
			if !coldEnum(n.name.Local, "tpm", "emulator", "controller", "interface", "serial", "parallel", "console", "channel", "input", "graphics", "video", "sound", "audio", "watchdog", "memballoon", "rng", "panic", "iommu", "hub", "redirdev", "redirfilter") {
				return nil, refuse()
			}
			continue
		}
		diskCount++
		if diskCount > coldSourceDiskLimit {
			return nil, refuse()
		}
		disk, err := coldSourceDisk(n, func(string, string) error { return refuse() })
		if err != nil {
			return nil, err
		}
		if targets[disk.Target] {
			return nil, refuse()
		}
		targets[disk.Target] = true
		entries := append([]domain.ColdStorageSource{}, disk.Backing...)
		if !disk.Empty {
			entries = append(entries, disk.Source)
		}
		for _, entry := range entries {
			switch {
			case entry.Type == "file" && entry.File != "":
				sources[entry.File] = true
			case entry.Type == "volume" && entry.Pool != "" && entry.Volume != "":
				// The VMs Virmill creates declare pool volumes. Resolve each to
				// its exact registered path so the review lists real files.
				path, resolveErr := h.volumePath(entry.Pool, entry.Volume)
				if resolveErr != nil {
					return nil, resolveErr
				}
				sources[path] = true
			default:
				return nil, refuse()
			}
		}
	}
	out := make([]string, 0, len(sources))
	for source := range sources {
		out = append(out, source)
	}
	sort.Strings(out)
	return out, nil
}
