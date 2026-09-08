//go:build linux && amd64

// Package coldcapture coordinates complete, stopped recovery sets through the
// shared durable operation engine. It never starts or stops the source guest.
package coldcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/coldfiles"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const operation = "snapshot.capture-cold-v1"
const maximumDisk = int64(16 << 40)

type DiskTool interface {
	Identity(context.Context) (image.Identity, error)
	InspectFile(context.Context, *os.File, string, string) (image.Info, fileidentity.Identity, error)
	InspectFiles(context.Context, []platform.DiskSourceFile, string, string, string, int64) ([]image.Info, error)
	ConvertFiles(context.Context, []platform.DiskSourceFile, string, string, string, int64, int64) error
}
type AuxiliaryClient interface {
	Root(string) (string, error)
	KeyID() (string, error)
	InspectAuxiliary(context.Context, helper.Request) (helper.AuxiliaryResponse, error)
	CaptureAuxiliary(context.Context, helper.Request) (*helper.AuxiliaryTransfer, error)
	ObserveAuxiliary(context.Context, helper.Request) (helper.AuxiliaryResponse, error)
}
type Service struct {
	Engine         *operations.Engine
	Backend        domain.ColdCaptureBackend
	Tool           DiskTool
	Helper         AuxiliaryClient
	Catalog, Cache string
}
type captureRequest struct {
	SourceRoot      string `json:"sourceRoot"`
	AuxiliaryRootID string `json:"auxiliaryRootID,omitempty"`
}
type sourceFile struct {
	Path     string                `json:"path"`
	Identity fileidentity.Identity `json:"identity"`
}
type diskRecipe struct {
	Volume       *domain.ColdResolvedVolume `json:"volume,omitempty"`
	Target       string                     `json:"target"`
	Format       string                     `json:"format"`
	Media        bool                       `json:"media"`
	Files        []sourceFile               `json:"files"`
	VirtualBytes int64                      `json:"virtualBytes"`
}
type recipe struct {
	Version         int                        `json:"version"`
	SnapshotID      string                     `json:"snapshotID"`
	SourceRoot      string                     `json:"sourceRoot"`
	Native          domain.ColdStateInspection `json:"native"`
	PersistentXML   string                     `json:"persistentXML"`
	Versions        map[string]string          `json:"nativeVersions"`
	Tool            image.Identity             `json:"tool"`
	Disks           []diskRecipe               `json:"disks"`
	Firmware        *sourceFile                `json:"firmware,omitempty"`
	AuxiliaryRootID string                     `json:"auxiliaryRootID,omitempty"`
	AuxiliaryKeyID  string                     `json:"auxiliaryKeyID,omitempty"`
	Auxiliary       *helper.AuxiliaryInventory `json:"auxiliary,omitempty"`
}

type publicationIntent struct {
	Version        int    `json:"version"`
	SnapshotID     string `json:"snapshotID"`
	OperationID    string `json:"operationID"`
	ManifestSHA256 string `json:"manifestSHA256"`
}

func Register(a *app.Service, data, cache, config string) {
	backend, ok := a.Provider.(domain.ColdCaptureBackend)
	if !ok {
		return
	}
	s := &Service{Engine: a.Engine, Backend: backend, Tool: image.Tool{}, Helper: helper.Client{KeyPath: filepath.Join(config, "helper-key.pem")}, Catalog: filepath.Join(data, "captures"), Cache: filepath.Join(cache, "cold-capture")}
	s.Register(a)
	if backend, ok := a.Provider.(domain.ColdRestoreBackend); ok {
		h := &restorer{s: s, backend: backend}
		a.Engine.Handlers[restoreOperation] = h
		a.Extensions["snapshot.restore"] = h.Plan
	}
}
func (s *Service) Register(a *app.Service) {
	a.Engine.Handlers[operation] = s
	a.Extensions["snapshot.create"] = s.Plan
	a.Extensions["snapshot.show"] = s.Show
	a.Extensions["snapshot.list"] = s.List
}
func exact(a, b any) bool { return reflect.DeepEqual(a, b) }
func stopped(n domain.ColdStateInspection) error {
	if n.State != "stopped" || !n.Persistent || n.HasManagedSave || n.Autostart || n.Source == nil || !exact(n.Layout, n.Source.State) {
		return domain.Fail("INVALID_STATE", "cold capture requires a persistent stopped VM without managed save or autostart")
	}
	if len(n.Source.External) != 0 || len(n.Layout.SecretReferences) != 0 || n.Layout.TPM != nil && n.Layout.TPM.EncryptionSecret != "" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "unresolved devices or secret dependencies need a qualified capture adapter; no partial set will be published")
	}
	return nil
}
func relative(root, path string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", domain.Fail("INVALID_INPUT", "canonical absolute source root and paths required")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return "", domain.Fail("PERMISSION_DENIED", "every disk and backing source must remain inside the selected source root")
	}
	return rel, nil
}
func (s *Service) Plan(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if uid == 0 || r.ID == "" || r.Path != "" || r.After != 0 || r.Apply != nil || r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return nil, domain.Fail("INVALID_INPUT", "cold capture requires an ordinary actor, local connection and stable VM UUID")
	}
	b, err := operations.Canonical(r.Input)
	if err != nil {
		return nil, err
	}
	if err = validation.Schema("cold-capture-input", b); err != nil {
		return nil, domain.Fail("INVALID_INPUT", "capture input requires sourceRoot and optional auxiliaryRootID")
	}
	var args captureRequest
	if err = wire.Decode(b, &args); err != nil {
		return nil, err
	}
	idBytes, _ := json.Marshal(map[string]string{"id": r.ID})
	if err = validation.Schema("auxiliary-vm-identity", idBytes); err != nil {
		return nil, err
	}
	n, err := s.Backend.InspectColdState(ctx, r.Connection, r.ID)
	if err != nil {
		return nil, err
	}
	if err = stopped(n); err != nil {
		return nil, err
	}
	vm, err := s.Backend.Get(ctx, r.Connection, r.ID)
	if err != nil {
		return nil, err
	}
	if vm.Fingerprint != n.Fingerprint || vm.PersistentXML == "" {
		return nil, domain.Fail("SOURCE_CHANGED", "native definition changed during capture planning")
	}
	in := recipe{Version: 1, SnapshotID: domain.ID(), SourceRoot: args.SourceRoot, Native: n, PersistentXML: vm.PersistentXML, Disks: []diskRecipe{}}
	in.Versions, err = s.Backend.ColdRuntimeVersions(ctx, r.Connection, n.Layout.TPM != nil)
	if err != nil {
		return nil, err
	}
	in.Tool, err = s.Tool.Identity(ctx)
	if err != nil {
		return nil, err
	}
	if err = platform.PrivateDir(s.Cache); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(s.Cache, "inspect-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	for _, d := range n.Source.Disks {
		if d.Empty {
			continue
		}
		var volume *domain.ColdResolvedVolume
		if d.Source.Type == "volume" {
			resolver, ok := s.Backend.(domain.ColdVolumeResolver)
			if !ok {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native file-volume resolver unavailable")
			}
			resolved, err := resolver.ResolveColdVolume(ctx, r.Connection, d.Source)
			if err != nil {
				return nil, err
			}
			volume = &resolved
			d.Source = domain.ColdStorageSource{Type: "file", Format: d.Source.Format, File: resolved.Path}
		}
		if d.Source.Type != "file" || d.Source.Format != "raw" && d.Source.Format != "qcow2" {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "cold capture currently requires explicit raw/qcow2 file sources")
		}
		disk, err := s.planDisk(ctx, args.SourceRoot, work, d)
		if err != nil {
			return nil, err
		}
		disk.Volume = volume
		in.Disks = append(in.Disks, disk)
	}
	if n.Layout.Firmware.Loader != "" {
		id, err := fileidentity.Observe(n.Layout.Firmware.Loader, false)
		if err != nil {
			return nil, err
		}
		if id.Size == 0 || id.Size > 64<<20 {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "firmware code exceeds capture bounds")
		}
		in.Firmware = &sourceFile{Path: n.Layout.Firmware.Loader, Identity: id}
	}
	hasAux := n.Layout.Firmware.NVRAM != nil || n.Layout.TPM != nil
	if hasAux {
		if args.AuxiliaryRootID == "" || r.Connection != "qemu:///system" {
			return nil, domain.Fail("PERMISSION_REQUIRED", "firmware/TPM bytes require VM-specific administrator helper policy and auxiliaryRootID")
		}
		in.AuxiliaryRootID = args.AuxiliaryRootID
		in.AuxiliaryKeyID, err = s.Helper.KeyID()
		if err != nil {
			return nil, err
		}
		req := s.auxiliaryRequest(in, uid, domain.ID(), "", "inspect")
		req.PlanDigest, err = operations.Digest(req)
		if err != nil {
			return nil, err
		}
		observed, err := s.Helper.InspectAuxiliary(ctx, req)
		if err != nil {
			return nil, err
		}
		if err = helper.ValidateAuxiliaryInspection(req, &observed); err != nil {
			return nil, err
		}
		root, err := s.Helper.Root(args.AuxiliaryRootID)
		if err != nil {
			return nil, err
		}
		if observed.Inventory.Root.Path != root || !exact(observed.Inventory.Layout, n.Layout) {
			return nil, domain.Fail("SOURCE_CHANGED", "helper observed a different firmware/TPM mapping")
		}
		in.Auxiliary = observed.Inventory
	} else if args.AuxiliaryRootID != "" {
		return nil, domain.Fail("INVALID_INPUT", "auxiliaryRootID supplied for a VM without auxiliary state")
	}
	resources := []string{n.Resource.String(), "cold-capture|" + in.SnapshotID}
	before := map[string]string{n.Resource.String(): n.Fingerprint}
	for _, d := range in.Disks {
		if d.Volume != nil {
			key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "storage-pool", UUID: d.Volume.PoolID}.String()
			resources = append(resources, key)
			before[key] = d.Volume.PoolFingerprint
		}
		for _, f := range d.Files {
			key := "file|" + f.Identity.Generation
			resources = append(resources, key)
			before[key], _ = operations.Digest(f)
		}
	}
	// Shared backing nodes need one lease, never duplicate lock identifiers.
	sort.Strings(resources)
	resources = unique(resources)
	return s.Engine.Plan(ctx, uid, r.Connection, operation, resources, before, in, []domain.Step{{ID: "capture", Action: "copy and publish one complete stopped recovery set", Preconditions: []string{"source stopped and unchanged"}, Idempotency: "exclusive snapshot UUID; never replay copy", Compensation: "retain partial staging", Reconciliation: "inspect immutable complete catalog set", CompletionPredicate: "all members, source mapping and operation identity verified"}}, []string{"offline-source-read", "private-recovery-state"}, []string{"Capture includes guest data and firmware/TPM identity in a private local catalog; it does not certify restore or guest boot."})
}
func unique(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
func (s *Service) planDisk(ctx context.Context, root, work string, d domain.ColdDiskSource) (diskRecipe, error) {
	out := diskRecipe{Target: d.Target, Format: d.Source.Format, Media: d.Device == "cdrom" || d.Device == "floppy", Files: []sourceFile{}}
	current, format := d.Source.File, d.Source.Format
	held := []*coldfiles.Source{}
	defer func() {
		for _, f := range held {
			_ = f.Close()
		}
	}()
	selected := []platform.DiskSourceFile{}
	defer func() {
		for _, f := range selected {
			_ = f.File.Close()
		}
	}()
	seen := map[string]bool{}
	for {
		if len(out.Files) >= 16 || seen[current] {
			return out, domain.Fail("UNSUPPORTED_CAPABILITY", "backing graph exceeds the depth limit or repeats a source")
		}
		seen[current] = true
		rel, err := relative(root, current)
		if err != nil {
			return out, err
		}
		id, err := fileidentity.Observe(current, false)
		if err != nil {
			return out, err
		}
		if id.Size == 0 || id.Size > uint64(maximumDisk) {
			return out, domain.Fail("UNSUPPORTED_CAPABILITY", "disk source size exceeds capture bounds")
		}
		source, err := coldfiles.Open(ctx, root, rel, id)
		if err != nil {
			return out, err
		}
		held = append(held, source)
		f, err := source.DupForTransfer()
		if err != nil {
			return out, err
		}
		selected = append(selected, platform.DiskSourceFile{Path: filepath.ToSlash(rel), File: f})
		info, observed, err := s.Tool.InspectFile(ctx, f, work, format)
		if err != nil {
			return out, err
		}
		if observed != id {
			return out, domain.Fail("SOURCE_CHANGED", "source changed during confined header inspection")
		}
		out.Files = append(out.Files, sourceFile{Path: current, Identity: id})
		if info.Backing == "" {
			break
		}
		if filepath.IsAbs(info.Backing) || strings.ContainsAny(info.Backing, ":\\") || info.BackingFormat != "raw" && info.BackingFormat != "qcow2" {
			return out, domain.Fail("UNSUPPORTED_CAPABILITY", "backing files require a contained relative path and explicit raw/qcow2 format")
		}
		current = filepath.Clean(filepath.Join(filepath.Dir(current), info.Backing))
		format = info.BackingFormat
	}
	name, _ := relative(root, d.Source.File)
	chain, err := s.Tool.InspectFiles(ctx, selected, work, filepath.ToSlash(name), d.Source.Format, maximumDisk)
	if err != nil {
		return out, err
	}
	if len(chain) != len(out.Files) {
		return out, domain.Fail("SOURCE_CHANGED", "confined backing graph differs from selected source set")
	}
	if len(d.Backing) > 0 || d.BackingTerminated {
		if len(d.Backing) != len(out.Files)-1 {
			return out, domain.Fail("SOURCE_CHANGED", "native backing inventory and image header graph differ")
		}
		for i, backing := range d.Backing {
			if backing.Type != "file" || backing.File != out.Files[i+1].Path || backing.Format != "" && backing.Format != chain[i+1].Format {
				return out, domain.Fail("UNSUPPORTED_CAPABILITY", "native backing override does not match the confined file graph")
			}
		}
	}
	out.VirtualBytes = chain[0].VirtualSize
	if out.Media && (len(chain) != 1 || out.Format != "raw") {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "removable media require an independent raw source")
	}
	for _, source := range held {
		if err = source.Recheck(ctx); err != nil {
			return out, err
		}
	}
	return out, nil
}
func (s *Service) auxiliaryRequest(in recipe, uid uint32, job, digest, mode string) helper.Request {
	req := helper.Request{APIVersion: domain.APIVersion, ActorUID: uid, Operation: "state.auxiliary", ResourceID: in.Native.Resource.UUID, RootID: in.AuxiliaryRootID, JobID: job, PlanDigest: digest, KeyID: in.AuxiliaryKeyID, Mode: mode, Auxiliary: &helper.AuxiliaryRequest{Version: 1, Fingerprint: in.Native.Fingerprint}}
	if mode != "inspect" {
		req.Auxiliary.Expected = in.Auxiliary
	}
	return req
}
func decode(b []byte) (recipe, error) {
	var in recipe
	err := wire.Decode(b, &in)
	if err == nil && (in.Version != 1 || in.SnapshotID == "" || in.Native.Source == nil) {
		err = domain.Fail("INVALID_INPUT", "invalid cold capture recipe")
	}
	return in, err
}
func (s *Service) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := decode(b)
	if err != nil {
		return err
	}
	if p.Operation != operation || p.ActorUID == 0 || p.ConnectionID != in.Native.Resource.ConnectionID {
		return domain.Fail("INVALID_INPUT", "cold capture plan identity differs")
	}
	n, err := s.Backend.InspectColdState(ctx, p.ConnectionID, in.Native.Resource.UUID)
	if err != nil {
		return err
	}
	if err = stopped(n); err != nil {
		return err
	}
	if !exact(n, in.Native) {
		return domain.Fail("STALE_PLAN", "native source state or definition changed; generate a new capture plan")
	}
	if err = s.Backend.CheckColdConfiguration(ctx, p.ConnectionID, n.Resource.UUID, n.Fingerprint); err != nil {
		return err
	}
	versions, err := s.Backend.ColdRuntimeVersions(ctx, p.ConnectionID, n.Layout.TPM != nil)
	if err != nil {
		return err
	}
	tool, err := s.Tool.Identity(ctx)
	if err != nil {
		return err
	}
	if !exact(versions, in.Versions) || tool != in.Tool {
		return domain.Fail("STALE_PLAN", "native runtime or image tool changed")
	}
	for _, d := range in.Disks {
		if d.Volume != nil {
			resolver, ok := s.Backend.(domain.ColdVolumeResolver)
			if !ok {
				return domain.Fail("UNSUPPORTED_CAPABILITY", "native volume resolver unavailable")
			}
			fresh, err := resolver.ResolveColdVolume(ctx, p.ConnectionID, d.Volume.Source)
			if err != nil {
				return err
			}
			if !exact(fresh, *d.Volume) {
				return domain.Fail("SOURCE_CHANGED", "native pool/volume mapping changed")
			}
		}
		for _, f := range d.Files {
			id, err := fileidentity.Observe(f.Path, false)
			if err != nil {
				return err
			}
			if id != f.Identity {
				return domain.Fail("SOURCE_CHANGED", "source disk generation changed")
			}
		}
	}
	if in.Firmware != nil {
		id, err := fileidentity.Observe(in.Firmware.Path, false)
		if err != nil {
			return err
		}
		if id != in.Firmware.Identity {
			return domain.Fail("SOURCE_CHANGED", "firmware code changed")
		}
	}
	return ctx.Err()
}
func (s *Service) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := decode(b)
	if err != nil {
		return nil, err
	}
	return map[string]any{"snapshotID": in.SnapshotID, "sourceVM": in.Native.Resource, "catalog": s.Catalog, "disks": in.Disks, "firmwareIncluded": in.Firmware != nil, "auxiliaryIncluded": in.Auxiliary != nil, "guestBootVerified": false, "sourceMutation": "none"}, ctx.Err()
}
func (s *Service) Show(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if r.ID == "" || len(r.Input) != 0 || r.Path != "" || r.Apply != nil {
		return nil, domain.Fail("INVALID_INPUT", "snapshot show accepts one snapshot UUID")
	}
	return coldstore.Inspect(ctx, s.Catalog, r.ID)
}
func (s *Service) List(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if r.ID != "" || r.Path != "" || len(r.Input) != 0 || r.Apply != nil {
		return nil, domain.Fail("INVALID_INPUT", "snapshot list accepts no parameters")
	}
	out := []string{}
	entries, err := os.ReadDir(s.Catalog)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}
func (s *Service) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := decode(b)
	if err != nil {
		return false, err
	}
	receipt, err := coldstore.Inspect(ctx, s.Catalog, in.SnapshotID)
	if err != nil {
		return false, err
	}
	var intent publicationIntent
	if err = s.Engine.Store.Get("cold-capture-publication", p.ID, &intent); err != nil {
		return false, err
	}
	if intent.Version != 1 || intent.SnapshotID != receipt.SnapshotID || intent.OperationID != receipt.OperationID || intent.ManifestSHA256 != receipt.ManifestSHA256 {
		return false, domain.Fail("RECOVERY_REQUIRED", "published set differs from the durably recorded capture digest")
	}
	m := receipt.Manifest
	if receipt.OperationID != operations.OperationID(ctx) || m.SourceFingerprint != in.Native.Fingerprint || !exact(m.Source, *in.Native.Source) || !bytes.Equal([]byte(in.PersistentXML), mustXML(ctx, s.Catalog, receipt)) {
		return false, domain.Fail("RECOVERY_REQUIRED", "published recovery set differs from its immutable operation")
	}
	return true, nil
}
