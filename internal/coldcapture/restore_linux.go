//go:build linux && amd64

package coldcapture

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const restoreOperation = "snapshot.restore-new-v1"

type restorer struct {
	s       *Service
	backend domain.ColdRestoreBackend
}
type restoreRequest struct {
	Name   string `json:"name"`
	PoolID string `json:"poolID"`
}
type restoreDisk struct {
	Target string                   `json:"target"`
	Member protection.CaptureMember `json:"member"`
	Intent domain.VolumeIntent      `json:"intent"`
	Format string                   `json:"format"`
}
type restoreRecipe struct {
	Version       int               `json:"version"`
	Capture       coldstore.Receipt `json:"capture"`
	UUID          string            `json:"uuid"`
	Name          string            `json:"name"`
	PoolID        string            `json:"poolID"`
	PoolDigest    string            `json:"poolDigest"`
	SourceXML     string            `json:"sourceXML"`
	Disks         []restoreDisk     `json:"disks"`
	RequiredBytes uint64            `json:"requiredBytes"`
}
type restoreVolumeProgress struct {
	Volume   domain.CreatedVolume `json:"volume"`
	Verified bool                 `json:"verified"`
}
type restoreProgress struct {
	Version     int                     `json:"version"`
	PlanID      string                  `json:"planID"`
	OperationID string                  `json:"operationID"`
	UUID        string                  `json:"uuid"`
	Volumes     []restoreVolumeProgress `json:"volumes"`
	Defined     bool                    `json:"defined"`
}

func (h *restorer) Plan(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if uid == 0 || r.ID == "" || r.Path != "" || r.Apply != nil || r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return nil, domain.Fail("INVALID_INPUT", "restore requires a snapshot UUID, ordinary actor and local connection")
	}
	raw, err := operations.Canonical(r.Input)
	if err != nil {
		return nil, err
	}
	if err = validation.Schema("cold-restore-input", raw); err != nil {
		return nil, domain.Fail("INVALID_INPUT", "restore input requires a new name and target poolID")
	}
	var req restoreRequest
	if err = wire.Decode(raw, &req); err != nil {
		return nil, err
	}
	capture, err := coldstore.Inspect(ctx, h.s.Catalog, r.ID)
	if err != nil {
		return nil, err
	}
	m := capture.Manifest
	if !m.IndependentlyRecoverable || len(m.Source.External) != 0 || len(m.Secrets) != 0 {
		return nil, domain.Fail("INCOMPLETE_BACKUP", "restore requires a complete independent recovery set without unresolved dependencies")
	}
	if m.Source.State.Firmware.NVRAM != nil || m.Source.State.TPM != nil {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "auxiliary restore requires a qualified firmware/TPM staging adapter; captured identity must never be reset or omitted")
	}
	sourceXML := mustXML(ctx, h.s.Catalog, capture)
	if len(sourceXML) == 0 {
		return nil, domain.Fail("INCOMPLETE_BACKUP", "captured persistent configuration is unavailable")
	}
	name, err := validation.DisplayName(req.Name)
	if err != nil {
		return nil, err
	}
	in := restoreRecipe{Version: 1, Capture: capture, UUID: domain.ID(), Name: name, PoolID: req.PoolID, SourceXML: string(sourceXML), Disks: []restoreDisk{}, RequiredBytes: 64 << 20}
	pool, err := h.backend.GetStoragePool(ctx, r.Connection, req.PoolID)
	if err != nil {
		return nil, err
	}
	in.PoolDigest, err = restorePoolDigest(pool)
	if err != nil {
		return nil, err
	}
	if err = platform.PrivateDir(h.s.Cache); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(h.s.Cache, "restore-inspect-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	members := map[string]protection.CaptureMember{}
	for _, member := range m.Members {
		members[member.ID] = member
	}
	changes := xmlpatch.ColdRestorePatch{UUID: in.UUID, Name: in.Name, DisconnectNICs: true}
	for i, d := range m.Disks {
		member := members[d.MemberID]
		f, err := openCaptureMember(ctx, h.s.Catalog, capture.SnapshotID, member)
		if err != nil {
			return nil, err
		}
		chain, inspectErr := h.s.Tool.InspectFiles(ctx, []platform.DiskSourceFile{{Path: "disk", File: f}}, work, "disk", d.Format, 512<<30)
		closeErr := f.Close()
		if inspectErr != nil {
			return nil, inspectErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(chain) != 1 {
			return nil, domain.Fail("INCOMPLETE_BACKUP", "captured disk is not independent")
		}
		intent := domain.VolumeIntent{PoolID: req.PoolID, Name: fmt.Sprintf("virmill-%s-disk-%03d.qcow2", in.UUID, i), SourceID: member.ID, VirtualBytes: uint64(chain[0].VirtualSize), FileBytes: uint64(member.Size), SHA256: member.SHA256}
		if member.Kind == "media" {
			if d.Format != "raw" || member.Size < 32768 || member.Size > 64<<30 || member.Size%2048 != 0 {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "restored removable media must be a supported raw ISO")
			}
			intent.ContentType = "cdrom-iso"
			intent.Name = fmt.Sprintf("virmill-%s-media-%03d.iso", in.UUID, i)
			intent.VirtualBytes = uint64(member.Size)
		} else if d.Format != "qcow2" {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "this native restore adapter requires captured data disks flattened to qcow2")
		}
		in.Disks = append(in.Disks, restoreDisk{Target: d.Target, Member: member, Intent: intent, Format: d.Format})
		in.RequiredBytes += intent.FileBytes
		// This preview checks the complete opaque rewrite before any allocation.
		// Native execution replaces these disjoint placeholders with verified paths.
		changes.Disks = append(changes.Disks, xmlpatch.ColdRestoreDisk{Target: d.Target, Path: "/virmill-restore-preview/" + in.UUID + "/" + intent.Name, Format: d.Format})
	}
	if _, err = xmlpatch.ColdRestore(in.SourceXML, changes); err != nil {
		return nil, err
	}
	resource := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "vm", UUID: in.UUID}.String()
	return h.s.Engine.Plan(ctx, uid, r.Connection, restoreOperation, []string{resource, pool.Key.String(), "cold-capture|" + m.ID}, map[string]string{pool.Key.String(): in.PoolDigest}, in, []domain.Step{{ID: "restore", Action: "restore independent volumes and define a new disconnected stopped VM", Preconditions: []string{"complete capture verified", "new identity absent", "target pool unchanged"}, Idempotency: "exclusive new volume names; never replay allocations", Compensation: "retain staged volumes and any observed definition", Reconciliation: "verify retained volumes and exact native definition without replay", CompletionPredicate: "all uploaded volumes and preserved native configuration verified"}}, []string{"new-restored-identity", "all-network-interfaces-disconnected"}, []string{"The restored guest has a new UUID/name and no network interfaces; guest application identity and credentials remain as captured. Guest boot requires a separate start operation."})
}
func restorePoolDigest(p domain.StoragePool) (string, error) {
	if !p.Active || p.State != "running" || p.Type != "dir" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "restore requires an active directory storage pool")
	}
	var stats struct {
		Capacity   *string `xml:"capacity"`
		Allocation *string `xml:"allocation"`
		Available  *string `xml:"available"`
	}
	if err := xml.Unmarshal([]byte(p.XML), &stats); err != nil {
		return "", err
	}
	changes := map[string]string{}
	if stats.Capacity != nil {
		changes["pool/capacity"] = "0"
	}
	if stats.Allocation != nil {
		changes["pool/allocation"] = "0"
	}
	if stats.Available != nil {
		changes["pool/available"] = "0"
	}
	normalized, err := xmlpatch.Patch(p.XML, changes)
	if err != nil {
		return "", err
	}
	return operations.Digest(struct {
		Key             domain.ResourceKey
		Name, Type, XML string
		Persistent      bool
	}{p.Key, p.Name, p.Type, normalized, p.Persistent})
}
func decodeRestore(b []byte) (restoreRecipe, error) {
	var in restoreRecipe
	err := wire.Decode(b, &in)
	if err == nil && (in.Version != 1 || in.UUID == "" || in.Capture.Version != 1 || in.SourceXML == "") {
		err = domain.Fail("INVALID_INPUT", "invalid restore recipe")
	}
	return in, err
}
func (h *restorer) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := decodeRestore(b)
	if err != nil {
		return err
	}
	if p.Operation != restoreOperation || p.ActorUID == 0 {
		return domain.Fail("INVALID_INPUT", "invalid restore operation")
	}
	capture, err := coldstore.Inspect(ctx, h.s.Catalog, in.Capture.SnapshotID)
	if err != nil {
		return err
	}
	if !exact(capture, in.Capture) {
		return domain.Fail("SOURCE_CHANGED", "captured recovery set changed")
	}
	if err = h.backend.CheckCreationIdentity(ctx, p.ConnectionID, in.UUID, in.Name); err != nil {
		return err
	}
	pool, err := h.backend.GetStoragePool(ctx, p.ConnectionID, in.PoolID)
	if err != nil {
		return err
	}
	digest, err := restorePoolDigest(pool)
	if err != nil {
		return err
	}
	if digest != in.PoolDigest {
		return domain.Fail("STALE_PLAN", "restore target pool configuration changed")
	}
	if pool.AvailableBytes == nil || *pool.AvailableBytes < in.RequiredBytes {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "target pool has insufficient observed free capacity")
	}
	for _, d := range in.Disks {
		if err = h.backend.VolumeAbsent(ctx, p.ConnectionID, d.Intent); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (h *restorer) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := decodeRestore(b)
	if err != nil {
		return nil, err
	}
	return map[string]any{"snapshotID": in.Capture.SnapshotID, "newVMID": in.UUID, "newName": in.Name, "poolID": in.PoolID, "additionalBytes": in.RequiredBytes, "disks": in.Disks, "disconnectAllNICs": true, "guestBootVerified": false}, ctx.Err()
}
func (h *restorer) Estimate(ctx context.Context, p domain.Plan, b []byte) (domain.Estimates, error) {
	in, err := decodeRestore(b)
	return domain.Estimates{AdditionalBytes: in.RequiredBytes, Notes: "Independent new volumes; source guest and capture remain untouched."}, err
}
func (h *restorer) save(r restoreProgress, previous *[]byte) error {
	if err := h.s.Engine.Store.ComparePut("cold-restore", r.PlanID, *previous, r); err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err == nil {
		*previous = raw
	}
	return err
}
func (h *restorer) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := decodeRestore(b)
	if err != nil {
		return err
	}
	if step.ID != "restore" || operations.OperationID(ctx) == "" {
		return domain.Fail("INVALID_INPUT", "bound restore step required")
	}
	progress := restoreProgress{Version: 1, PlanID: p.ID, OperationID: operations.OperationID(ctx), UUID: in.UUID, Volumes: []restoreVolumeProgress{}}
	var previous []byte
	if err = h.save(progress, &previous); err != nil {
		return err
	}
	for _, d := range in.Disks {
		if err = h.s.canceled(ctx); err != nil {
			return err
		}
		if err = h.backend.CheckCreationIdentity(ctx, p.ConnectionID, in.UUID, in.Name); err != nil {
			return err
		}
		pool, err := h.backend.GetStoragePool(ctx, p.ConnectionID, in.PoolID)
		if err != nil {
			return err
		}
		digest, err := restorePoolDigest(pool)
		if err != nil {
			return err
		}
		if digest != in.PoolDigest {
			return domain.Fail("STALE_PLAN", "restore target pool changed")
		}
		if pool.AvailableBytes == nil || *pool.AvailableBytes < d.Intent.FileBytes+(64<<20) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "insufficient free space for next restore volume")
		}
		source, err := openCaptureMember(ctx, h.s.Catalog, in.Capture.SnapshotID, d.Member)
		if err != nil {
			return err
		}
		if err = operations.Note(ctx, h.s.Engine.Store, "Intent persisted: allocate new restore volume "+d.Intent.Name); err != nil {
			source.Close()
			return err
		}
		volume, allocationErr := h.backend.AllocateVolume(ctx, p.ConnectionID, d.Intent)
		progress.Volumes = append(progress.Volumes, restoreVolumeProgress{Volume: volume})
		if err = h.save(progress, &previous); err != nil {
			source.Close()
			return err
		}
		if allocationErr != nil {
			source.Close()
			return allocationErr
		}
		if !exact(volume.Intent, d.Intent) || volume.Generation == "" || volume.BackendKey == "" || volume.Path == "" {
			source.Close()
			return domain.Fail("RECOVERY_REQUIRED", "new restore volume identity is incomplete")
		}
		if err = operations.Note(ctx, h.s.Engine.Store, "Intent persisted: populate new restore volume "+d.Intent.Name); err != nil {
			source.Close()
			return err
		}
		err = h.backend.PopulateVolume(ctx, p.ConnectionID, volume, io.NewSectionReader(source, 0, d.Member.Size))
		closeErr := source.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = h.backend.VerifyCreatedVolume(ctx, p.ConnectionID, volume); err != nil {
			return err
		}
		progress.Volumes[len(progress.Volumes)-1].Verified = true
		if err = h.save(progress, &previous); err != nil {
			return err
		}
	}
	definition, err := restoreDefinition(in, progress)
	if err != nil {
		return err
	}
	if err = h.s.canceled(ctx); err != nil {
		return err
	}
	if err = operations.Note(ctx, h.s.Engine.Store, "Intent persisted: define the new stopped VM after every independent volume was verified"); err != nil {
		return err
	}
	if _, err = h.backend.DefineRestoredVM(ctx, p.ConnectionID, definition); err != nil {
		return err
	}
	progress.Defined = true
	return h.save(progress, &previous)
}
func restoreDefinition(in restoreRecipe, r restoreProgress) (domain.ColdRestoreDefinition, error) {
	d := domain.ColdRestoreDefinition{UUID: in.UUID, Name: in.Name, SourceXML: in.SourceXML, Disks: []domain.ColdRestoredDisk{}, DisconnectNICs: true}
	if r.Version != 1 || r.UUID != in.UUID || len(r.Volumes) != len(in.Disks) {
		return d, domain.Fail("RECOVERY_REQUIRED", "restore volume set is incomplete")
	}
	for i, v := range r.Volumes {
		if !v.Verified || !exact(v.Volume.Intent, in.Disks[i].Intent) {
			return d, domain.Fail("RECOVERY_REQUIRED", "restore volume has no exact verification receipt")
		}
		d.Disks = append(d.Disks, domain.ColdRestoredDisk{Target: in.Disks[i].Target, Format: in.Disks[i].Format, Volume: v.Volume})
	}
	return d, nil
}
func (h *restorer) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := decodeRestore(b)
	if err != nil {
		return false, err
	}
	var progress restoreProgress
	if err = h.s.Engine.Store.Get("cold-restore", p.ID, &progress); err != nil {
		return false, err
	}
	if progress.PlanID != p.ID || progress.OperationID != operations.OperationID(ctx) {
		return false, domain.Fail("RECOVERY_REQUIRED", "restore receipt operation differs")
	}
	definition, err := restoreDefinition(in, progress)
	if err != nil {
		return false, err
	}
	// A lost native define acknowledgement is observed here, never replayed.
	_, complete, err := h.backend.ObserveRestoredVM(ctx, p.ConnectionID, definition)
	return complete, err
}
func openCaptureMember(ctx context.Context, catalog, id string, m protection.CaptureMember) (*os.File, error) {
	path := filepath.Join(catalog, id, filepath.FromSlash(m.Path))
	f, identity, err := fileidentity.Open(path, false, true)
	if err != nil {
		return nil, err
	}
	if identity.Size != uint64(m.Size) || identity.Mode&0777 != 0400 {
		f.Close()
		return nil, domain.Fail("SOURCE_CHANGED", "capture member metadata changed")
	}
	hash, err := hashReader(ctx, f, m.Size)
	if err != nil || hash != m.SHA256 {
		f.Close()
		if err == nil {
			err = domain.Fail("SOURCE_CHANGED", "capture member digest changed")
		}
		return nil, err
	}
	return f, nil
}
