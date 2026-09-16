//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	native "libvirt.org/go/libvirt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"virmill.local/core/internal/backend/fileidentity"
	imageTool "virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
)

func cleanupPoolDirectory(pool *native.StoragePool) (string, error) {
	x, err := pool.GetXMLDesc(0)
	if err != nil {
		return "", err
	}
	var v struct {
		Type   string `xml:"type,attr"`
		Target struct {
			Path string `xml:"path"`
		} `xml:"target"`
	}
	if err = xml.Unmarshal([]byte(x), &v); err != nil {
		return "", err
	}
	active, err := pool.IsActive()
	if err != nil {
		return "", err
	}
	if !active || (v.Type != "dir" && v.Type != "fs" && v.Type != "netfs") {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "cleanup needs an active, locally inspectable file pool")
	}
	if _, err = fileidentity.Observe(v.Target.Path, true); err != nil {
		return "", err
	}
	return v.Target.Path, nil
}
func inspectCleanupVolumes(c *native.Connect, candidates []domain.CleanupCandidate) ([]domain.CleanupVolume, error) {
	if len(candidates) < 1 || len(candidates) > 64 {
		return nil, domain.Fail("INVALID_INPUT", "bounded creation volume set required")
	}
	result := []domain.CleanupVolume{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if err := validateVolume(candidate.Intent); err != nil {
			return nil, err
		}
		id := candidate.Intent.PoolID + "|" + candidate.Intent.Name
		if seen[id] {
			return nil, domain.Fail("INVALID_INPUT", "duplicate cleanup volume")
		}
		seen[id] = true
		pool, err := c.LookupStoragePoolByUUIDString(candidate.Intent.PoolID)
		if err != nil {
			return nil, err
		}
		directory, err := cleanupPoolDirectory(pool)
		if err != nil {
			pool.Free()
			return nil, err
		}
		v, err := pool.LookupStorageVolByName(candidate.Intent.Name)
		pool.Free()
		item := domain.CleanupVolume{Candidate: candidate, State: "unknown"}
		if candidate.Allocated != nil && candidate.Allocated.Path != filepath.Join(directory, candidate.Intent.Name) {
			if v != nil {
				v.Free()
			}
			item.Reason = "allocation path differs from the exact pool entry; absence is ambiguous"
			result = append(result, item)
			continue
		}
		if err != nil {
			var ne native.Error
			if !errors.As(err, &ne) || ne.Code != native.ERR_NO_STORAGE_VOL {
				return nil, err
			}
			// Cached absence does not prove absence on disk. No refresh mutation is
			// performed by preview; disagreement requires separate operator attention.
			_, diskErr := os.Lstat(filepath.Join(directory, candidate.Intent.Name))
			if !os.IsNotExist(diskErr) {
				item.Reason = "volume is not listed but its path cannot be proved absent"
			} else {
				item.State = "absent"
			}
		} else {
			key, e := v.GetKey()
			if e != nil {
				v.Free()
				return nil, e
			}
			path, e := v.GetPath()
			v.Free()
			if e != nil {
				return nil, e
			}
			a := candidate.Allocated
			if a == nil || a.BackendKey == "" || a.Path == "" || a.Generation == "" || inventoryDigest(a.Intent) != inventoryDigest(candidate.Intent) {
				item.Reason = "no durable allocation generation; never infer ownership from a reserved name"
			} else if key != a.BackendKey || path != a.Path || path != filepath.Join(directory, candidate.Intent.Name) {
				item.Reason = "native volume identity or pool path differs from allocation"
			} else if identity, e := fileidentity.Observe(path, false); e != nil {
				item.Reason = "filesystem generation unavailable: " + e.Error()
			} else if identity.Generation != a.Generation {
				item.Reason = "file was replaced after allocation"
			} else {
				item.State = "present"
				item.Fingerprint = inventoryDigest(identity)
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (p *Provider) InspectCreationCleanup(ctx context.Context, uri string, spec domain.CreationSpec, candidates []domain.CleanupCandidate, deleting bool) (domain.CreationCleanup, error) {
	var empty domain.CreationCleanup
	c, err := connect(uri, false)
	if err != nil {
		return empty, err
	}
	defer c.Close()
	return inspectCreationCleanup(ctx, c, uri, spec, candidates, deleting)
}
func inspectCreationCleanup(ctx context.Context, c *native.Connect, uri string, spec domain.CreationSpec, candidates []domain.CleanupCandidate, deleting bool) (domain.CreationCleanup, error) {
	var out domain.CreationCleanup
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := absentDomain(c, spec.UUID, spec.Name); err != nil {
		return out, err
	}
	var err error
	out.Volumes, err = inspectCleanupVolumes(c, candidates)
	if err != nil {
		return out, err
	}
	if !deleting {
		return out, nil
	}
	for _, v := range out.Volumes {
		if v.State == "unknown" {
			return out, domain.Fail("RECOVERY_REQUIRED", v.Candidate.Intent.Name+": "+v.Reason)
		}
	}
	out.GraphDigest, err = cleanupGraph(ctx, c, uri, candidates, &out.ResourceIDs)
	return out, err
}
func (p *Provider) DeleteCreationVolume(ctx context.Context, uri string, spec domain.CreationSpec, candidates []domain.CleanupCandidate, reviewed domain.CreationCleanup, index int) error {
	if index < 0 || index >= len(candidates) || len(reviewed.Volumes) != len(candidates) || reviewed.Volumes[index].State != "present" || reviewed.GraphDigest == "" {
		return domain.Fail("INVALID_INPUT", "one reviewed present volume and complete dependency proof required")
	}
	c, err := connect(uri, true)
	if err != nil {
		return err
	}
	defer c.Close()
	current, err := inspectCreationCleanup(ctx, c, uri, spec, candidates, true)
	if err != nil {
		return err
	}
	if inventoryDigest(current) != inventoryDigest(reviewed) {
		return domain.Fail("STALE_PLAN", "volume state or dependency graph changed before deletion")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = absentDomain(c, spec.UUID, spec.Name); err != nil {
		return err
	}
	selected := candidates[index].Allocated
	if selected == nil || selected.Generation == "" {
		return domain.Fail("RECOVERY_REQUIRED", "allocation generation missing")
	}
	v, err := lookupCreated(c, *selected)
	if err != nil {
		return err
	}
	defer v.Free()
	// No wipe, recursive deletion, source removal, VM undefine or firmware action.
	// External writers remain races; the coordinator records intent and observes
	// absence after this single native request instead of blindly replaying it.
	return v.Delete(0)
}

type cleanupGraphVolume struct{ key, path, format, xml, pool, name string }

func cleanupGraph(ctx context.Context, c *native.Connect, uri string, candidates []domain.CleanupCandidate, resources *[]string) (string, error) {
	return cleanupGraphExceptOwner(ctx, c, uri, candidates, resources, "")
}

// Only the separately revalidated definition-removal adapter may omit its exact
// owner. Existing creation cleanup uses the empty owner and retains all checks.
// The omitted owner still must be inactive without saved or snapshot state.
func cleanupGraphExceptOwner(ctx context.Context, c *native.Connect, uri string, candidates []domain.CleanupCandidate, resources *[]string, owner string) (string, error) {
	if owner != "" && (!uuidPattern.MatchString(owner) || owner == "00000000-0000-0000-0000-000000000000") {
		return "", domain.Fail("INVALID_INPUT", "exact owner UUID required for removal graph")
	}
	graph := map[string]any{}
	selected := map[string]bool{}
	for _, v := range candidates {
		if v.Allocated != nil {
			selected[v.Allocated.Path] = true
		}
	}
	pools, err := c.ListAllStoragePools(0)
	if err != nil {
		return "", err
	}
	defer func() {
		for i := range pools {
			pools[i].Free()
		}
	}()
	if len(pools) > 128 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "cleanup graph exceeds 128 storage pools")
	}
	volumes := map[string]cleanupGraphVolume{}
	byName := map[string]string{}
	poolGenerations := map[string]bool{}
	selectedGenerations := map[string]bool{}
	for _, v := range candidates {
		if v.Allocated != nil {
			selectedGenerations[v.Allocated.Generation] = true
		}
	}
	for i := range pools {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		pool := &pools[i]
		directory, e := cleanupPoolDirectory(pool)
		if e != nil {
			return "", e
		}
		info, e := observePool(pool, uri)
		if e != nil {
			return "", e
		}
		held, identity, e := fileidentity.Open(directory, true, true)
		if e != nil {
			return "", e
		}
		entries, e := held.ReadDir(4097)
		held.Close()
		if e != nil && e != io.EOF {
			return "", e
		}
		if len(entries) > 4096 {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "pool exceeds bounded cleanup graph inspection")
		}
		if poolGenerations[identity.Generation] {
			return "", domain.Fail("RECOVERY_REQUIRED", "multiple pools alias the same directory generation")
		}
		poolGenerations[identity.Generation] = true
		*resources = append(*resources, info.Key.String())
		config, e := poolCreationFingerprint(info)
		if e != nil {
			return "", e
		}
		graph["pool:"+info.Key.UUID] = []string{config, identity.Generation}
		all, e := pool.ListAllStorageVolumes(0)
		if e != nil {
			return "", e
		}
		registered := map[string]bool{}
		for j := range all {
			v := &all[j]
			x, e := v.GetXMLDesc(0)
			if e != nil {
				for k := range all {
					all[k].Free()
				}
				return "", e
			}
			key, e := v.GetKey()
			if e != nil {
				for k := range all {
					all[k].Free()
				}
				return "", e
			}
			path, e := v.GetPath()
			if e != nil {
				for k := range all {
					all[k].Free()
				}
				return "", e
			}
			var meta struct {
				Name   string `xml:"name"`
				Target struct {
					Format struct {
						Type string `xml:"type,attr"`
					} `xml:"format"`
				} `xml:"target"`
			}
			if e = xml.Unmarshal([]byte(x), &meta); e != nil {
				for k := range all {
					all[k].Free()
				}
				return "", e
			}
			if path != filepath.Join(directory, meta.Name) || filepath.Base(meta.Name) != meta.Name || volumes[path].path != "" {
				for k := range all {
					all[k].Free()
				}
				return "", domain.Fail("RECOVERY_REQUIRED", "ambiguous or overlapping pool paths prevent deletion")
			}
			registered[meta.Name] = true
			volumes[path] = cleanupGraphVolume{key: key, path: path, format: meta.Target.Format.Type, xml: x, pool: info.Name, name: meta.Name}
			if byName[info.Name+"|"+meta.Name] != "" {
				for k := range all {
					all[k].Free()
				}
				return "", domain.Fail("RECOVERY_REQUIRED", "ambiguous native pool-volume name")
			}
			byName[info.Name+"|"+meta.Name] = path
		}
		for j := range all {
			all[j].Free()
		}
		for _, entry := range entries {
			if !registered[entry.Name()] || !entry.Type().IsRegular() {
				return "", domain.Fail("RECOVERY_REQUIRED", "unlisted or non-regular pool entry prevents a complete dependency graph: "+entry.Name())
			}
		}
	}
	if len(volumes) > 4096 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "cleanup graph exceeds 4096 volumes")
	}
	external := cleanupExternal{}
	// Running image metadata is never inspected offline. Until live block-graph
	// observation is qualified, any active guest makes this deletion proof fail.
	domains, err := c.ListAllDomains(0)
	if err != nil {
		return "", err
	}
	defer func() {
		for i := range domains {
			domains[i].Free()
		}
	}()
	for i := range domains {
		d := &domains[i]
		active, e := d.IsActive()
		if e != nil {
			return "", e
		}
		if active {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "active guest requires live block-graph reconciliation; cleanup deletion is unavailable on this connection")
		}
		id, e := d.GetUUIDString()
		if e != nil {
			return "", e
		}
		x, e := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
		if e != nil {
			return "", e
		}
		if id != owner {
			*resources = append(*resources, domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}.String())
		}
		documents := []string{x}
		save, e := d.HasManagedSaveImage(0)
		if e != nil {
			return "", e
		}
		if save {
			x, e = d.ManagedSaveGetXMLDesc(0)
			if e != nil {
				return "", e
			}
			documents = append(documents, x)
		}
		snapshots, e := d.ListAllSnapshots(0)
		if e != nil {
			return "", e
		}
		for j := range snapshots {
			x, e = snapshots[j].GetXMLDesc(0)
			if e != nil {
				for k := range snapshots {
					snapshots[k].Free()
				}
				return "", e
			}
			documents = append(documents, x)
		}
		for j := range snapshots {
			snapshots[j].Free()
		}
		checkpoints, e := d.ListAllCheckpoints(0)
		if e != nil {
			return "", e
		}
		for j := range checkpoints {
			x, e = checkpoints[j].GetXMLDesc(0)
			if e != nil {
				for k := range checkpoints {
					checkpoints[k].Free()
				}
				return "", e
			}
			documents = append(documents, x)
		}
		for j := range checkpoints {
			checkpoints[j].Free()
		}
		if id == owner {
			if err := cleanupGraphOwnerState(save, len(snapshots), len(checkpoints)); err != nil {
				return "", err
			}
			continue
		}
		sort.Strings(documents)
		for j, x := range documents {
			if e = checkCleanupXML(x, selected, volumes, byName, external); e != nil {
				return "", e
			}
			graph[fmt.Sprintf("domain:%s:%06d", id, j)] = inventoryDigest(x)
		}
	}
	workspace, err := os.MkdirTemp("", "virmill-cleanup-inspect-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workspace)
	edges := map[string]string{}
	for path, v := range volumes {
		selectedFile := selected[path]
		if err = ctx.Err(); err != nil {
			return "", err
		}
		f, before, e := fileidentity.Open(path, false, true)
		if e != nil {
			return "", e
		}
		if !selectedFile && selectedGenerations[before.Generation] {
			f.Close()
			return "", domain.Fail("RESOURCE_BUSY", "another pool path aliases a cleanup file generation")
		}
		// ISO pool metadata still denotes raw CD-ROM bytes for qemu-img.
		format := cleanupImageFormat(v.format)
		// A stale raw/ISO declaration must not hide a qcow2 backing edge.
		var magic [4]byte
		n, _ := f.ReadAt(magic[:], 0)
		if format == "raw" && n == 4 && string(magic[:]) == "QFI\xfb" {
			f.Close()
			return "", domain.Fail("SOURCE_CHANGED", "native raw declaration masks a qcow2 header")
		}
		image, identity, e := (imageTool.Tool{}).InspectFile(ctx, f, workspace, format)
		f.Close()
		if e != nil {
			return "", e
		}
		after, e := fileidentity.Observe(path, false)
		if e != nil {
			return "", e
		}
		if before != identity || identity != after {
			return "", domain.Fail("SOURCE_CHANGED", "pool image changed during graph observation")
		}
		backing, e := cleanupBackingPath(path, image.Backing, image.BackingFormat)
		if e != nil {
			return "", e
		}
		if selected[backing] {
			return "", domain.Fail("RESOURCE_BUSY", "retained image references a cleanup volume: "+path)
		}
		if backing != "" && (volumes[backing].path == "" || cleanupImageFormat(volumes[backing].format) != image.BackingFormat) {
			return "", domain.Fail("RECOVERY_REQUIRED", "missing, external or ambiguous backing dependency: "+path)
		}
		var stored struct {
			Backing struct {
				Path string `xml:"path"`
			} `xml:"backingStore"`
		}
		if e = xml.Unmarshal([]byte(v.xml), &stored); e != nil {
			return "", e
		}
		nativeBacking := ""
		if stored.Backing.Path != "" {
			nativeBacking, e = cleanupBackingPath(path, stored.Backing.Path, image.BackingFormat)
			if e != nil {
				return "", e
			}
		}
		if nativeBacking != backing {
			return "", domain.Fail("SOURCE_CHANGED", "native and image backing metadata disagree")
		}
		edges[path] = backing
		if !selectedFile {
			graph["volume:"+v.key] = struct {
				Path     string
				Identity fileidentity.Identity
				Image    imageTool.Info
			}{path, identity, image}
		}
	}
	outside, err := checkCleanupExternal(ctx, external, candidates, selected, volumes, func(ctx context.Context, f *os.File) (imageTool.Info, error) {
		info, _, e := (imageTool.Tool{}).InspectFile(ctx, f, workspace, "qcow2")
		return info, e
	})
	if err != nil {
		return "", err
	}
	for path, observed := range outside {
		graph["external:"+path] = observed
	}
	for start := range edges {
		seen := map[string]bool{}
		for at := start; at != ""; at = edges[at] {
			if seen[at] {
				return "", domain.Fail("RECOVERY_REQUIRED", "backing graph cycle prevents deletion")
			}
			seen[at] = true
		}
	}
	sort.Strings(*resources)
	return inventoryDigest(graph), nil
}
func cleanupBackingPath(source, backing, format string) (string, error) {
	if backing == "" {
		return "", nil
	}
	if (format != "raw" && format != "qcow2") || strings.ContainsAny(backing, ":\\\x00") {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "backing edge requires an explicit local raw/qcow2 path")
	}
	if !filepath.IsAbs(backing) {
		backing = filepath.Join(filepath.Dir(source), backing)
	}
	return filepath.Clean(backing), nil
}

// checkCleanupXML refuses exact references to a cleanup volume and records every
// absolute file reference outside the reconciled pools in external, with its
// declared format, for checkCleanupExternal to compare by object identity.
func checkCleanupXML(document string, selected map[string]bool, volumes map[string]cleanupGraphVolume, byName map[string]string, external cleanupExternal) error {
	root, err := xmlTree(document)
	if err != nil {
		return err
	}
	var walk func(n, parent *xmlNode, disk bool) error
	walk = func(n, parent *xmlNode, disk bool) error {
		disk = disk || n.name.Local == "disk" || n.name.Local == "backingStore" || n.name.Local == "dataStore"
		// Any exact path occurrence protects the target, including firmware,
		// snapshot-memory and vendor extension attributes rather than only disks.
		if selected[strings.TrimSpace(n.text)] {
			return domain.Fail("RESOURCE_BUSY", "domain or recovery XML references a cleanup volume")
		}
		for _, a := range n.attrs {
			if selected[a.Value] {
				return domain.Fail("RESOURCE_BUSY", "domain or recovery XML references a cleanup volume")
			}
		}
		if (n.name.Local == "nvram" || n.name.Local == "loader") && filepath.IsAbs(strings.TrimSpace(n.text)) && volumes[strings.TrimSpace(n.text)].path == "" {
			external.add(strings.TrimSpace(n.text), attr(n, "format"))
		}
		if n.name.Local == "memory" && filepath.IsAbs(strings.TrimSpace(n.text)) && volumes[strings.TrimSpace(n.text)].path == "" {
			external.add(strings.TrimSpace(n.text), "")
		}
		if n.name.Local == "source" {
			path := attr(n, "file")
			if directory := attr(n, "dir"); directory != "" {
				for path := range selected {
					if path == directory || strings.HasPrefix(path, filepath.Clean(directory)+string(filepath.Separator)) {
						return domain.Fail("RESOURCE_BUSY", "shared filesystem exposes a cleanup candidate")
					}
				}
				return domain.Fail("UNSUPPORTED_CAPABILITY", "shared-directory aliases require a complete filesystem dependency adapter")
			}
			if attr(n, "dev") != "" || (disk && attr(n, "protocol") != "") {
				return domain.Fail("UNSUPPORTED_CAPABILITY", "block/network disk dependencies need a dedicated live storage graph adapter")
			}
			if name := attr(n, "volume"); name != "" {
				path = byName[attr(n, "pool")+"|"+name]
				if path == "" {
					return domain.Fail("RECOVERY_REQUIRED", "unresolved pool-volume reference")
				}
			}
			if path != "" {
				if selected[path] {
					return domain.Fail("RESOURCE_BUSY", "VM or snapshot references cleanup volume")
				}
				if volumes[path].path == "" {
					if !filepath.IsAbs(path) {
						return domain.Fail("RECOVERY_REQUIRED", "relative disk source outside reconciled storage pools")
					}
					external.add(path, cleanupDeclaredFormat(parent))
				}
			}
		}
		for _, c := range n.children {
			if err := walk(c, n, disk); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root, nil, false)
}

// cleanupDeclaredFormat is the format QEMU is told to open a source with: a
// disk's driver type, or a backing or data store's format. Empty means none
// was declared and the file's own header must be read.
func cleanupDeclaredFormat(parent *xmlNode) string {
	if parent == nil {
		return ""
	}
	switch parent.name.Local {
	case "disk":
		if d := child(parent, "driver"); d != nil {
			return attr(d, "type")
		}
	case "backingStore", "dataStore":
		if f := child(parent, "format"); f != nil {
			return attr(f, "type")
		}
	}
	return ""
}

var _ domain.CreationCleanupBackend = (*Provider)(nil)

// Keep the original native XML/format in the graph proof; normalize only the
// fixed image inspector format and backing-edge equivalence.
func cleanupImageFormat(format string) string {
	if format == "iso" {
		return "raw"
	}
	return format
}
