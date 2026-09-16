//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

var _ domain.DiskRemovalProvider = (*Provider)(nil)

func cleanupGraphOwnerState(saved bool, snapshots, checkpoints int) error {
	if saved || snapshots != 0 || checkpoints != 0 {
		return domain.Fail("RESOURCE_BUSY", "selected definition acquired saved, snapshot or checkpoint state; removal graph cannot omit it")
	}
	return nil
}

type removalDiskSource struct{ target, path, format string }

// sameColdSource compares declared storage identities: a file path, or a pool
// volume as the VMs Virmill creates use.
func sameColdSource(a, b domain.ColdStorageSource) bool {
	return a.Type == "file" && b.Type == "file" && a.File != "" && a.File == b.File ||
		a.Type == "volume" && b.Type == "volume" && a.Pool != "" && a.Pool == b.Pool && a.Volume == b.Volume
}

func removalDiskSources(raw string, targets []string, h removalHandle) ([]removalDiskSource, error) {
	if len(targets) < 1 || len(targets) > 64 {
		return nil, domain.Fail("INVALID_INPUT", "select 1–64 explicit writable disk targets; no implicit all-disks deletion")
	}
	selected := map[string]bool{}
	for _, target := range targets {
		if !coldDiskTarget.MatchString(target) || len(target) > 128 || selected[target] {
			return nil, domain.Fail("INVALID_INPUT", "disk targets must be exact, bounded and unique")
		}
		selected[target] = true
	}
	source, err := InspectColdSourceXML(raw)
	if err != nil {
		return nil, err
	}
	result := []removalDiskSource{}
	for _, disk := range source.Disks {
		if !selected[disk.Target] {
			continue
		}
		s := disk.Source
		declared := s.Type == "file" && s.File != "" || s.Type == "volume" && s.Pool != "" && s.Volume != ""
		if disk.Device != "disk" || disk.ReadOnly || disk.Empty || !declared || (s.Format != "raw" && s.Format != "qcow2") {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "selected target must be a writable raw/qcow2 disk in a registered local file or pool volume; media and read-only sources are retained")
		}
		for _, dep := range source.External {
			if dep.Target == disk.Target || strings.HasPrefix(dep.Target, disk.Target+"/") {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "selected disk has unresolved shared, encrypted or external state; retain it")
			}
		}
		for _, other := range source.Disks {
			if other.Target != disk.Target && sameColdSource(other.Source, s) {
				return nil, domain.Fail("RESOURCE_BUSY", "another disk or source medium references the selected image")
			}
			for _, parent := range other.Backing {
				if sameColdSource(parent, s) {
					return nil, domain.Fail("RESOURCE_BUSY", "selected disk is a declared backing parent and must be retained")
				}
			}
		}
		path := s.File
		if s.Type == "volume" {
			// Deletion needs the exact registered path of the pool volume.
			resolved, resolveErr := h.volumePath(s.Pool, s.Volume)
			if resolveErr != nil {
				return nil, resolveErr
			}
			path = resolved
		}
		result = append(result, removalDiskSource{disk.Target, path, s.Format})
	}
	if len(result) != len(selected) {
		return nil, domain.Fail("INVALID_INPUT", "one or more selected disk targets are absent")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].target < result[j].target })
	return result, nil
}

func validDiskRemoval(r domain.DiskRemoval) error {
	if err := removalKey(r.Definition.Resource); err != nil {
		return err
	}
	if !digestPattern.MatchString(r.Definition.Fingerprint) || !digestPattern.MatchString(r.Definition.DefinitionSHA256) || !digestPattern.MatchString(r.GraphDigest) || len(r.Disks) < 1 || len(r.Disks) > 64 || len(r.ResourceIDs) < 1 || len(r.ResourceIDs) > 8192 {
		return domain.Fail("INVALID_INPUT", "complete exact disk-removal observation required")
	}
	paths, keys, generations := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, d := range r.Disks {
		if !coldDiskTarget.MatchString(d.Target) || (i > 0 && r.Disks[i-1].Target >= d.Target) || coldPath(d.Path) != nil || !uuidPattern.MatchString(d.PoolID) || d.PoolID == "00000000-0000-0000-0000-000000000000" || !diskRemovalName(d.VolumeName) || d.VolumeName != filepath.Base(d.Path) || d.VolumeKey == "" || len(d.VolumeKey) > 4096 || coldScalar(d.VolumeKey, 4096) != nil || !strings.HasPrefix(d.Generation, "linux-statx-v1:") || len(d.Generation) > 256 || !digestPattern.MatchString(d.Fingerprint) || (d.Format != "raw" && d.Format != "qcow2") || d.CapacityBytes == 0 || paths[d.Path] || keys[d.VolumeKey] || generations[d.Generation] || !slices.Contains(r.Definition.RetainedSources, d.Path) {
			return domain.Fail("INVALID_INPUT", "selected disk identities, paths or generations are incomplete or ambiguous")
		}
		paths[d.Path], keys[d.VolumeKey], generations[d.Generation] = true, true, true
	}
	for i, id := range r.ResourceIDs {
		if id == "" || len(id) > 8192 || (i > 0 && r.ResourceIDs[i-1] >= id) {
			return domain.Fail("INVALID_INPUT", "canonical resource lock set required")
		}
	}
	if !slices.Contains(r.ResourceIDs, r.Definition.Resource.String()) {
		return domain.Fail("INVALID_INPUT", "VM resource lock missing")
	}
	for _, d := range r.Disks {
		for _, key := range diskRemovalResources(r.Definition.Resource.ConnectionID, d) {
			if !slices.Contains(r.ResourceIDs, key) {
				return domain.Fail("INVALID_INPUT", "selected disk resource lock missing")
			}
		}
	}
	return nil
}
func diskRemovalName(s string) bool {
	return s != "" && s != "." && s != ".." && len(s) <= 255 && filepath.Base(s) == s && coldScalar(s, 255) == nil
}
func diskRemovalResources(uri string, d domain.RemovalDisk) []string {
	return []string{domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool", UUID: d.PoolID}.String(), "local-file|" + d.Path}
}
func diskRemovalCandidates(disks []domain.RemovalDisk) []domain.CleanupCandidate {
	out := make([]domain.CleanupCandidate, 0, len(disks))
	// The graph uses path/generation only. These are not creation allocation
	// receipts and must never be passed to the creation deletion adapter.
	for _, d := range disks {
		out = append(out, domain.CleanupCandidate{Allocated: &domain.CreatedVolume{Path: d.Path, Generation: d.Generation}})
	}
	return out
}
func diskRemovalGraph(ctx context.Context, c *native.Connect, r domain.DiskRemoval) (string, []string, error) {
	resources := []string{r.Definition.Resource.String()}
	for _, d := range r.Disks {
		resources = append(resources, diskRemovalResources(r.Definition.Resource.ConnectionID, d)...)
	}
	digest, err := cleanupGraphExceptOwner(ctx, c, r.Definition.Resource.ConnectionID, diskRemovalCandidates(r.Disks), &resources, r.Definition.Resource.UUID)
	sort.Strings(resources)
	resources = slices.Compact(resources)
	return digest, resources, err
}

func (p *Provider) InspectDiskRemoval(ctx context.Context, uri, id string, targets []string) (domain.DiskRemoval, error) {
	var out domain.DiskRemoval
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	if err := removalKey(key); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if len(targets) < 1 || len(targets) > 64 {
		return out, domain.Fail("INVALID_INPUT", "explicit bounded disk target list required")
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
	out.Definition, err = inspectRemovalHandle(ctx, nativeRemovalHandle{d, c}, key)
	if err != nil {
		return out, err
	}
	vm, err := observe(d, uri)
	if err != nil {
		return out, err
	}
	if vm.Fingerprint != out.Definition.Fingerprint {
		return out, domain.Fail("STALE_PLAN", "definition changed before selected disk inspection")
	}
	sources, err := removalDiskSources(vm.PersistentXML, targets, nativeRemovalHandle{d, c})
	if err != nil {
		return out, err
	}
	for _, source := range sources {
		volume, e := c.LookupStorageVolByPath(source.path)
		if e != nil {
			return out, e
		}
		observed, e := observeRemovalDisk(volume, source.target)
		volume.Free()
		if e != nil {
			return out, e
		}
		if observed.Path != source.path || observed.Format != source.format {
			return out, domain.Fail("SOURCE_CHANGED", "selected native volume differs from disk declaration")
		}
		out.Disks = append(out.Disks, observed)
	}
	out.GraphDigest, out.ResourceIDs, err = diskRemovalGraph(ctx, c, out)
	if err != nil {
		return out, err
	}
	if err = validDiskRemoval(out); err != nil {
		return out, err
	}
	_, err = checkRemovalDisks(ctx, nativeDiskRemovalSession{c}, out, false)
	return out, err
}

// Volume metadata is parsed with the existing strict, bounded tree decoder.
// Its wrapper prevents native volume XML from being treated as a domain.
func removalVolumeFormat(raw, name, path string) (string, error) {
	if len(raw) > 1<<20 {
		return "", domain.Fail("INVALID_INPUT", "volume XML exceeds removal inspection bound")
	}
	wrapper, err := coldStateTree("<domain>" + raw + "</domain>")
	if err != nil {
		return "", err
	}
	if len(wrapper.children) != 1 || strings.TrimSpace(wrapper.text) != "" || wrapper.children[0].name != (xml.Name{Local: "volume"}) {
		return "", domain.Fail("INVALID_INPUT", "exact native volume root required")
	}
	root := wrapper.children[0]
	if err := coldAttrs(root, nil, []string{"type"}); err != nil {
		return "", err
	}
	if attr(root, "type") != "" && attr(root, "type") != "file" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "only native file volumes can be selected")
	}
	named, err := coldChild(root, "name", true)
	if err != nil {
		return "", err
	}
	if named.text != name || len(named.attrs) != 0 || len(named.children) != 0 {
		return "", domain.Fail("SOURCE_CHANGED", "native volume name differs")
	}
	target, err := coldChild(root, "target", true)
	if err != nil {
		return "", err
	}
	format, err := coldChild(target, "format", true)
	if err != nil {
		return "", err
	}
	if err = coldAttrs(format, []string{"type"}, nil); err != nil {
		return "", err
	}
	if len(format.children) != 0 || strings.TrimSpace(format.text) != "" || (attr(format, "type") != "raw" && attr(format, "type") != "qcow2") {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "selected native volume must declare raw or qcow2")
	}
	node, err := coldChild(target, "path", true)
	if err != nil {
		return "", err
	}
	if node.text != path || len(node.attrs) != 0 || len(node.children) != 0 {
		return "", domain.Fail("SOURCE_CHANGED", "native volume path differs")
	}
	for _, n := range root.children {
		if _, err := coldChild(root, n.name.Local, false); err != nil {
			return "", err
		}
		if !coldEnum(n.name.Local, "name", "key", "source", "capacity", "allocation", "physical", "target", "backingStore") {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "unknown volume metadata requires retention")
		}
	}
	for _, n := range target.children {
		if _, err := coldChild(target, n.name.Local, false); err != nil {
			return "", err
		}
		if !coldEnum(n.name.Local, "path", "format", "permissions", "timestamps", "compat", "features", "clusterSize") {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "unknown or encrypted volume target requires retention")
		}
	}
	if cluster, e := coldChild(target, "clusterSize", false); e != nil {
		return "", e
	} else if cluster != nil {
		if e = coldAttrs(cluster, nil, []string{"unit"}); e != nil {
			return "", e
		}
		size, parseErr := strconv.ParseUint(strings.TrimSpace(cluster.text), 10, 64)
		if len(cluster.children) != 0 || parseErr != nil || size == 0 || !coldEnum(attr(cluster, "unit"), "", "B", "bytes") {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "invalid native volume cluster size requires retention")
		}
	}
	return attr(format, "type"), nil
}
func observeRemovalDisk(volume *native.StorageVol, target string) (domain.RemovalDisk, error) {
	var out domain.RemovalDisk
	out.Target = target
	name, err := volume.GetName()
	if err != nil {
		return out, err
	}
	if !diskRemovalName(name) {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "ordinary bounded native volume name required")
	}
	out.VolumeName = name
	out.VolumeKey, err = volume.GetKey()
	if err != nil {
		return out, err
	}
	out.Path, err = volume.GetPath()
	if err != nil {
		return out, err
	}
	if err = coldPath(out.Path); err != nil {
		return out, err
	}
	pool, err := volume.LookupPoolByVolume()
	if err != nil {
		return out, err
	}
	defer pool.Free()
	out.PoolID, err = pool.GetUUIDString()
	if err != nil {
		return out, err
	}
	directory, err := cleanupPoolDirectory(pool)
	if err != nil {
		return out, err
	}
	if out.Path != filepath.Join(directory, name) {
		return out, domain.Fail("SOURCE_CHANGED", "volume is not the exact registered pool entry")
	}
	raw, err := volume.GetXMLDesc(0)
	if err != nil {
		return out, err
	}
	out.Format, err = removalVolumeFormat(raw, name, out.Path)
	if err != nil {
		return out, err
	}
	info, err := volume.GetInfo()
	if err != nil {
		return out, err
	}
	if info.Type != native.STORAGE_VOL_FILE || info.Capacity == 0 {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "positive-capacity native file volume required")
	}
	out.CapacityBytes, out.AllocatedBytes = info.Capacity, info.Allocation
	identity, err := fileidentity.Observe(out.Path, false)
	if err != nil {
		return out, err
	}
	out.Generation = identity.Generation
	// statx includes generation, size, mode, mtime and ctime. Atime deliberately
	// does not identify content: the dependency inspector reads each selected file.
	out.Fingerprint = inventoryDigest(struct {
		Disk domain.RemovalDisk
		File fileidentity.Identity
	}{out, identity})
	return out, nil
}

type removalDiskSession interface {
	definition(context.Context, domain.DefinitionRemoval, bool) error
	disk(domain.RemovalDisk) (string, error)
	graph(context.Context, domain.DiskRemoval) (string, []string, error)
}
type nativeDiskRemovalSession struct{ c *native.Connect }

func (s nativeDiskRemovalSession) definition(ctx context.Context, want domain.DefinitionRemoval, absent bool) error {
	d, err := s.c.LookupDomainByUUIDString(want.Resource.UUID)
	if err != nil {
		gone, e := removalLookupAbsent(err)
		if absent && gone {
			return nil
		}
		return eOrMissing(e)
	}
	defer d.Free()
	if absent {
		return domain.Fail("RESOURCE_BUSY", "VM UUID is present; disk deletion requires confirmed definition absence")
	}
	actual, err := inspectRemovalHandle(ctx, nativeRemovalHandle{d, s.c}, want.Resource)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, want) {
		return domain.Fail("STALE_PLAN", "VM definition changed after selected-disk review")
	}
	return nil
}
func eOrMissing(err error) error {
	if err != nil {
		return err
	}
	return domain.Fail("SOURCE_CHANGED", "reviewed VM definition is missing")
}
func (s nativeDiskRemovalSession) disk(want domain.RemovalDisk) (string, error) {
	pool, err := s.c.LookupStoragePoolByUUIDString(want.PoolID)
	if err != nil {
		return "", err
	}
	defer pool.Free()
	directory, err := cleanupPoolDirectory(pool)
	if err != nil {
		return "", err
	}
	if want.Path != filepath.Join(directory, want.VolumeName) {
		return "", domain.Fail("SOURCE_CHANGED", "selected pool entry path changed")
	}
	volume, err := pool.LookupStorageVolByName(want.VolumeName)
	if err != nil {
		var ne native.Error
		if !errors.As(err, &ne) || ne.Code != native.ERR_NO_STORAGE_VOL {
			return "", err
		}
		_, fileErr := os.Lstat(want.Path)
		if !os.IsNotExist(fileErr) {
			return "", domain.Fail("RECOVERY_REQUIRED", "native volume absence disagrees with its filesystem path")
		}
		return "absent", nil
	}
	defer volume.Free()
	got, err := observeRemovalDisk(volume, want.Target)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(got, want) {
		return "", domain.Fail("SOURCE_CHANGED", "selected disk generation, native identity or metadata changed")
	}
	return "present", nil
}
func (s nativeDiskRemovalSession) graph(ctx context.Context, r domain.DiskRemoval) (string, []string, error) {
	return diskRemovalGraph(ctx, s.c, r)
}
func checkRemovalDisks(ctx context.Context, s removalDiskSession, r domain.DiskRemoval, absent bool) ([]string, error) {
	if err := validDiskRemoval(r); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.definition(ctx, r.Definition, absent); err != nil {
		return nil, err
	}
	states := make([]string, len(r.Disks))
	for i, d := range r.Disks {
		state, err := s.disk(d)
		if err != nil {
			return nil, err
		}
		if state != "present" && (state != "absent" || !absent) {
			return nil, domain.Fail("SOURCE_CHANGED", "selected disk is not in the required exact state")
		}
		states[i] = state
	}
	digest, resources, err := s.graph(ctx, r)
	if err != nil {
		return nil, err
	}
	if digest != r.GraphDigest || !reflect.DeepEqual(resources, r.ResourceIDs) {
		return nil, domain.Fail("STALE_PLAN", "storage references, pool identities or lock resources changed")
	}
	for i, d := range r.Disks {
		state, err := s.disk(d)
		if err != nil {
			return nil, err
		}
		if state != states[i] {
			return nil, domain.Fail("SOURCE_CHANGED", "selected disk changed during graph observation")
		}
	}
	if err := s.definition(ctx, r.Definition, absent); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return states, nil
}
func (p *Provider) CheckDiskRemoval(ctx context.Context, r domain.DiskRemoval, absent bool) ([]string, error) {
	if err := validDiskRemoval(r); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := connect(r.Definition.Resource.ConnectionID, true)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return checkRemovalDisks(ctx, nativeDiskRemovalSession{c}, r, absent)
}
func (p *Provider) DeleteRemovalDisk(ctx context.Context, r domain.DiskRemoval, index int) error {
	if err := validDiskRemoval(r); err != nil {
		return err
	}
	if index < 0 || index >= len(r.Disks) {
		return domain.Fail("INVALID_INPUT", "one explicit reviewed disk index required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(r.Definition.Resource.ConnectionID, true)
	if err != nil {
		return err
	}
	defer c.Close()
	wanted := r.Disks[index]
	pool, err := c.LookupStoragePoolByUUIDString(wanted.PoolID)
	if err != nil {
		return err
	}
	defer pool.Free()
	volume, err := pool.LookupStorageVolByName(wanted.VolumeName)
	if err != nil {
		return err
	}
	defer volume.Free()
	states, err := checkRemovalDisks(ctx, nativeDiskRemovalSession{c}, r, true)
	if err != nil {
		return err
	}
	if states[index] != "present" {
		return domain.Fail("RECOVERY_REQUIRED", "selected volume is absent; deletion was not repeated")
	}
	current, err := observeRemovalDisk(volume, wanted.Target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, wanted) {
		return domain.Fail("SOURCE_CHANGED", "held native volume differs immediately before deletion")
	}
	if err = (nativeDiskRemovalSession{c}).definition(ctx, r.Definition, true); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Exactly one non-wiping native delete. No undefine, source mutation, retry,
	// directory removal or fallback path deletion. The caller journals the intent
	// and must reconcile a lost acknowledgement by observation rather than replay.
	return volume.Delete(0)
}
