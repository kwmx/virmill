//go:build linux && amd64

// Package creating coordinates native volume allocation and define-last creation.
package creating

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/provision"
	"virmill.local/core/internal/backend/seed"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type SourceLoader func(context.Context, *store.Store, uint32, string) (importing.Artifact, string, error)
type Service struct {
	Engine     *operations.Engine
	Store      *store.Store
	Backend    domain.CreationBackend
	Inventory  domain.ResourceInventory
	LoadSource SourceLoader
	SeedTool   SeedTool
	SeedCache  string
}
type request struct {
	IdentityMode string              `json:"identityMode"`
	Provisioning *provision.Config   `json:"provisioning,omitempty"`
	Hardware     domain.CreationSpec `json:"hardware"`
}
type input struct {
	SourceOperationID string                `json:"sourceOperationID"`
	Seed              *seedRecipe           `json:"seed,omitempty"`
	Artifact          importing.Artifact    `json:"artifact"`
	Directory         string                `json:"directory"`
	Target            domain.CreationTarget `json:"target"`
	Volumes           []domain.VolumeIntent `json:"volumes"`
	RequiredBytes     uint64                `json:"requiredBytes"`
}
type volumeProgress struct {
	Intent    domain.VolumeIntent   `json:"intent"`
	Allocated *domain.CreatedVolume `json:"allocated"`
	Verified  bool                  `json:"verified"`
}
type Receipt struct {
	Version           int              `json:"schemaVersion"`
	PlanID            string           `json:"planID"`
	OperationID       string           `json:"operationID"`
	Binding           string           `json:"binding"`
	VMID              string           `json:"vmID"`
	Connection        string           `json:"connection"`
	Volumes           []volumeProgress `json:"volumes"`
	VolumesVerified   bool             `json:"volumesVerified"`
	Defined           bool             `json:"defined"`
	GuestBootVerified bool             `json:"guestBootVerified"`
}

func Register(service *app.Service, seedCacheDirectory string) {
	backend, ok := service.Provider.(domain.CreationBackend)
	if !ok {
		return
	}
	inventory, ok := service.Provider.(domain.ResourceInventory)
	if !ok {
		return
	}
	s := &Service{Engine: service.Engine, Store: service.Engine.Store, Backend: backend, Inventory: inventory, LoadSource: importing.Approved, SeedTool: seed.Tool{}, SeedCache: seedCacheDirectory}
	service.InventoryVM = s.Ownership
	service.Engine.Handlers["vm.create"] = s
	service.Engine.Handlers["vm.create.devices-v1"] = s
	service.Engine.Handlers["vm.create.resume"] = &resumeHandler{s: s}
	if cleanup, ok := service.Provider.(domain.CreationCleanupBackend); ok {
		h := &cleanupHandler{s: s, backend: cleanup}
		service.Engine.Handlers["vm.create.cleanup"] = h
		service.Extensions["vm.creation.cleanup"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return h.Plan(ctx, uid, r) }
	}
	service.Extensions["vm.create"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.Plan(ctx, uid, r) }
	service.Extensions["vm.creation.result"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.Result(ctx, uid, r.ID) }
	service.Extensions["vm.creation.resume"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.PlanResume(ctx, uid, r) }
}
func newMAC() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[0] = (b[0] | 2) & 0xfe
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5]), nil
}
func same(a, b any) bool {
	x, e := operations.Canonical(a)
	if e != nil {
		return false
	}
	y, e := operations.Canonical(b)
	return e == nil && bytes.Equal(x, y)
}
func bound(size uint64) uint64 { return size + size/4 + (16 << 20) }

func (s *Service) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	b, err := json.Marshal(r.Input)
	if err != nil {
		return empty, err
	}
	if err = validation.Schema("vm-creation-input", b); err != nil {
		// A schema error may quote rejected input (including a pasted private key).
		// Never forward that formatter across the credential input boundary.
		return empty, domain.Fail("INVALID_INPUT", "creation input does not match the bundled vm-creation-input schema; check the documented hardware and provisioning fields")
	}
	var req request
	if err = wire.Decode(b, &req); err != nil {
		return empty, err
	}
	if req.IdentityMode != "clone" || req.Hardware.UUID != "" {
		return empty, domain.Fail("INVALID_INPUT", "this creation path requires clone identity; recovery restores use a separate workflow")
	}
	spec := req.Hardware
	if spec.DevicePolicy == nil {
		spec.DevicePolicy, err = domain.DefaultCreationDevices(spec.Machine)
		if err != nil {
			return empty, err
		}
	}
	if err = spec.DevicePolicy.Validate(spec.Machine); err != nil {
		return empty, err
	}
	spec.UUID = domain.ID()
	name, err := validation.DisplayName(spec.Name)
	if err != nil {
		return empty, err
	}
	spec.Name = name
	for i := range spec.NICs {
		if spec.NICs[i].MAC != "" {
			return empty, domain.Fail("INVALID_INPUT", "new creation generates MAC identities; source MAC reuse is not implicit")
		}
		spec.NICs[i].MAC, err = newMAC()
		if err != nil {
			return empty, err
		}
	}
	artifact, directory, err := s.LoadSource(ctx, s.Store, uid, r.ID)
	if err != nil {
		return empty, err
	}
	if len(spec.Disks) != len(artifact.Disks) {
		return empty, domain.Fail("INVALID_INPUT", "map every prepared source disk exactly once")
	}
	byID := map[string]importing.PreparedDisk{}
	for _, d := range artifact.Disks {
		byID[d.SourceID] = d
	}
	seen := map[string]bool{}
	in := input{SourceOperationID: r.ID, Artifact: artifact, Directory: directory, RequiredBytes: 64 << 20, Volumes: []domain.VolumeIntent{}}
	for i, d := range spec.Disks {
		source, ok := byID[d.SourceID]
		if !ok || seen[d.SourceID] {
			return empty, domain.Fail("INVALID_INPUT", "source disk selection is incomplete or duplicated")
		}
		seen[d.SourceID] = true
		in.RequiredBytes += bound(uint64(source.VirtualBytes))
		in.Volumes = append(in.Volumes, domain.VolumeIntent{PoolID: spec.PoolID, Name: fmt.Sprintf("virmill-%s-disk-%03d.qcow2", spec.UUID, i), SourceID: d.SourceID, VirtualBytes: uint64(source.VirtualBytes), FileBytes: uint64(source.FileBytes), SHA256: source.SHA256})
	}
	preparedSeed, err := s.planSeed(ctx, req.Provisioning, spec, artifact)
	if err != nil {
		return empty, err
	}
	in.Seed = preparedSeed
	expectedMedia := len(artifact.Media)
	if preparedSeed != nil {
		expectedMedia++
	}
	if len(spec.Media) != expectedMedia {
		return empty, domain.Fail("INVALID_INPUT", "map every prepared read-only medium exactly once")
	}
	media := map[string]importing.PreparedMedia{}
	for _, m := range artifact.Media {
		media[m.SourceID] = m
	}
	if preparedSeed != nil {
		id := preparedSeed.Config.MediaID
		if seen[id] || media[id].SourceID != "" {
			return empty, domain.Fail("INVALID_INPUT", "NoCloud medium must have a unique source ID")
		}
		media[id] = importing.PreparedMedia{SourceID: id, Path: "seed.iso", Format: "raw", FileBytes: preparedSeed.Artifact.FileBytes, SHA256: preparedSeed.Artifact.SHA256}
	}
	for i, m := range spec.Media {
		source, ok := media[m.SourceID]
		if !ok || seen[m.SourceID] || source.Format != "raw" {
			return empty, domain.Fail("INVALID_INPUT", "media selection is incomplete, duplicated or not an independent ISO")
		}
		seen[m.SourceID] = true
		in.RequiredBytes += uint64(source.FileBytes)
		in.Volumes = append(in.Volumes, domain.VolumeIntent{ContentType: "cdrom-iso", PoolID: spec.PoolID, Name: fmt.Sprintf("virmill-%s-media-%03d.iso", spec.UUID, i), SourceID: m.SourceID, VirtualBytes: uint64(source.FileBytes), FileBytes: uint64(source.FileBytes), SHA256: source.SHA256})
	}
	nics := 0
	for _, item := range artifact.System.Items {
		if item.ResourceType == "10" {
			nics++
		}
	}
	mapped := map[int]bool{}
	for _, nic := range spec.NICs {
		if nic.SourceIndex == -1 {
			continue
		}
		if nic.SourceIndex < 0 || nic.SourceIndex >= nics || mapped[nic.SourceIndex] {
			return empty, domain.Fail("INVALID_INPUT", "source NIC indices must map each original adapter exactly once")
		}
		mapped[nic.SourceIndex] = true
	}
	if len(mapped) != nics {
		return empty, domain.Fail("INVALID_INPUT", "map all source NICs explicitly, using link down for an initially disconnected adapter")
	}
	in.Target, err = s.Backend.PreflightCreation(ctx, r.Connection, spec)
	if err != nil {
		return empty, err
	}
	spec.Machine = in.Target.Spec.Machine
	if !same(spec, in.Target.Spec) {
		return empty, domain.Fail("SOURCE_CHANGED", "backend silently changed requested hardware")
	}
	resources := []string{domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "vm", UUID: spec.UUID}.String(), domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "storage-pool", UUID: spec.PoolID}.String(), "vm-name|" + r.Connection + "|" + spec.Name}
	before := map[string]string{"prepared-artifact": artifact.InputDigest, "pool": in.Target.PoolFingerprint, "capabilities": in.Target.CapabilitiesDigest}
	for _, n := range in.Target.Networks {
		resources = append(resources, n.Key.String())
		before[n.Key.String()] = n.Fingerprint
	}
	step := domain.Step{ID: "create", Action: "vm.create", Preconditions: []string{"unchanged successful local preparation receipt", "complete disk/NIC/boot mapping", "pinned hardware, firmware and active pool/network settings", "all target volume names and VM identity absent"}, Idempotency: "reconcile-before-retry", Compensation: "Retain original artifacts and clearly identified partial new volumes; no automatic disk deletion", Reconciliation: "Match created definition and durable verified-volume receipt; never replay allocation or upload", CompletionPredicate: "All independent new volumes verified before an exact persistent VM definition is observed"}
	acks := []string{"host-mutation", "copy-managed-volumes", "new-vm-identity"}
	risks := []string{"Creates new independent managed volumes and defines a powered-off VM; original source remains unchanged", "Guest drivers, boot, provisioning, routes and isolation are not verified by definition", "Failed allocation/upload/definition retains partial resources and the journal; no automatic deletion", "New UUID and MAC identities; guest OS identities/credentials remain in copied disks and need explicit guest adaptation"}
	if spec.Firmware.Mode == "uefi" {
		acks = append(acks, "new-firmware-state")
		risks = append(risks, "Fresh NVRAM/TPM state is for clone creation, not recovery of an encrypted guest; old keys are not restored")
	}
	if len(spec.NICs) > 0 {
		acks = append(acks, "network-attachment")
		risks = append(risks, "Selected existing networks may expose the guest or bridge segments; guest route/policy configuration is a separate workflow")
	}
	if len(spec.Media) > 0 {
		acks = append(acks, "attach-readonly-media")
		risks = append(risks, "Copies all selected media into independent managed read-only CD-ROM volumes; boot order is explicit and no unattended installation is inferred")
	}
	if preparedSeed != nil {
		acks = append(acks, "guest-root-provisioning", "rotate-guest-host-keys")
		risks = append(risks, "First boot asks the explicitly declared cloud-init/Netplan image to create the selected user, install public SSH keys, set hostname and apply the reviewed per-NIC network intent", "A fresh per-VM NoCloud instance ID and guest SSH host-key rotation are requested; existing image credentials and machine identity are not proven generalized", "Source digest matches the selected original file; the declared HTTPS provenance is not fetched or signature-verified", "Seed content and public keys persist in the managed media and may persist inside the guest; guest completion, routes and isolation remain unverified")
		if preparedSeed.Config.PasswordlessSudo {
			acks = append(acks, "guest-passwordless-sudo")
			risks = append(risks, "The selected guest account receives passwordless sudo root authority")
		}
	}
	step.Action = "vm.create.devices-v1"
	acks = append(acks, "creation-device-policy")
	risks = append(risks, "The reviewed chipset policy fixes USB, balloon, watchdog, PS/2 input, disabled host audio and ISA serial settings; libvirt assigns bounded PCI addresses and bridge topology")
	if spec.DevicePolicy.WatchdogAction == "reset" {
		acks = append(acks, "watchdog-reset")
		risks = append(risks, "The Q35 watchdog can forcefully reset the guest if its guest driver arms it and the timer expires")
	}
	return s.Engine.Plan(ctx, uid, r.Connection, "vm.create.devices-v1", resources, before, in, []domain.Step{step}, acks, risks)
}
func (s *Service) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in input
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	if err := creationRecipeVersion(p, in); err != nil {
		return nil, err
	}
	staging := ""
	if in.Seed != nil {
		staging = filepath.Join(in.Seed.CacheDirectory, seedStage(p))
	}
	return map[string]any{"seedStagingDirectory": staging, "provisioning": in.Seed, "sourceOperationID": in.SourceOperationID, "sourceDirectory": in.Directory, "sourceSHA256": in.Artifact.SourceSHA256, "sourceHardware": in.Artifact.System, "target": in.Target, "volumes": in.Volumes, "requiredFreeBytes": in.RequiredBytes, "identityMode": "clone", "startsVM": false, "guestBootVerified": false, "serialConsole": true, "diskCache": "writethrough", "guestAdaptation": "not-run"}, nil
}
func (s *Service) checkSource(ctx context.Context, p domain.Plan, in input) error {
	artifact, directory, err := s.LoadSource(ctx, s.Store, p.ActorUID, in.SourceOperationID)
	if err != nil {
		return err
	}
	if directory != in.Directory || !same(artifact, in.Artifact) {
		return domain.Fail("SOURCE_CHANGED", "prepared source changed after review")
	}
	return nil
}
func (s *Service) checkTarget(ctx context.Context, p domain.Plan, in input) error {
	if err := creationRecipeVersion(p, in); err != nil {
		return err
	}
	target, err := s.Backend.PreflightCreation(ctx, p.ConnectionID, in.Target.Spec)
	if err != nil {
		return err
	}
	if !same(target, in.Target) {
		return domain.Fail("STALE_PLAN", "hardware, firmware, pool or network configuration changed")
	}
	return nil
}
func (s *Service) checkSpace(ctx context.Context, p domain.Plan, in input, alreadyWritten uint64) error {
	pool, err := s.Inventory.GetStoragePool(ctx, p.ConnectionID, in.Target.Spec.PoolID)
	if err != nil {
		return err
	}
	required := in.RequiredBytes
	if alreadyWritten < required {
		required -= alreadyWritten
	} else {
		required = 64 << 20
	}
	if !pool.Active || pool.State != "running" || pool.AvailableBytes == nil || *pool.AvailableBytes < required {
		return domain.Fail("INSUFFICIENT_SPACE", "active target pool does not have the reviewed worst-case space budget")
	}
	return nil
}
func (s *Service) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	var in input
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	if err := s.checkSeed(ctx, in); err != nil {
		return err
	}
	if err := s.checkSource(ctx, p, in); err != nil {
		return err
	}
	if err := s.checkTarget(ctx, p, in); err != nil {
		return err
	}
	if err := s.checkSpace(ctx, p, in, 0); err != nil {
		return err
	}
	for _, v := range in.Volumes {
		if err := s.Backend.VolumeAbsent(ctx, p.ConnectionID, v); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) save(receipt Receipt, previous *[]byte) error {
	if err := s.Store.ComparePut("vm-creation", receipt.PlanID, *previous, receipt); err != nil {
		return err
	}
	b, err := json.Marshal(receipt)
	*previous = b
	return err
}
func (s *Service) load(planID string) (Receipt, error) {
	out, present, err := s.loadOptional(planID)
	if err != nil {
		return out, err
	}
	if !present {
		return out, domain.Fail("RECOVERY_REQUIRED", "creation receipt is missing")
	}
	return out, nil
}

// Missing metadata is normal before an active operation persists its first
// receipt. Invalid or unreadable metadata is never treated as missing progress.
func (s *Service) loadOptional(planID string) (Receipt, bool, error) {
	var out Receipt
	b, err := s.Store.MetadataBytes("vm-creation", planID)
	if err != nil {
		return out, false, err
	}
	if b == nil {
		return out, false, nil
	}
	if err = wire.Decode(b, &out); err != nil {
		return out, true, err
	}
	if out.Version != 1 {
		return out, true, domain.Fail("UNSUPPORTED_CAPABILITY", "newer creation receipt schema refused")
	}
	return out, true, nil
}
func (s *Service) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (result error) {
	var in input
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	if err := s.Validate(ctx, p, b); err != nil {
		return err
	}
	binding, err := operations.Digest([]string{p.ID, p.InputDigest})
	if err != nil {
		return err
	}
	r := Receipt{Version: 1, PlanID: p.ID, OperationID: operations.OperationID(ctx), Binding: binding, VMID: in.Target.Spec.UUID, Connection: p.ConnectionID, Volumes: []volumeProgress{}}
	for _, v := range in.Volumes {
		r.Volumes = append(r.Volumes, volumeProgress{Intent: v})
	}
	var previous []byte
	if err = s.save(r, &previous); err != nil {
		return err
	}
	root, err := os.OpenRoot(in.Directory)
	if err != nil {
		return err
	}
	defer root.Close()
	sources := map[string]string{}
	for _, d := range in.Artifact.Disks {
		sources[d.SourceID] = d.Path
	}
	for _, m := range in.Artifact.Media {
		sources[m.SourceID] = m.Path
	}
	seedRoot, removeSeed, err := s.executeSeed(ctx, p, in)
	if err != nil {
		return err
	}
	defer func() {
		if err := removeSeed(); err != nil {
			result = fmt.Errorf("creation temporary seed cleanup requires attention: %w", err)
		}
	}()
	var written uint64
	for i, v := range in.Volumes {
		cancel, err := operations.CancellationRequested(ctx, s.Store)
		if err != nil {
			return err
		}
		if cancel {
			if i == 0 {
				return operations.ErrCanceledSafely
			}
			return domain.Fail("RECOVERY_REQUIRED", "creation canceled after allocation; new volumes retained, no VM defined")
		}
		if err = s.checkSpace(ctx, p, in, written); err != nil {
			return err
		}
		if err = s.checkTarget(ctx, p, in); err != nil {
			return err
		}
		if err = operations.Note(ctx, s.Store, "Intent persisted: allocate new managed volume "+v.Name); err != nil {
			return err
		}
		allocated, allocationErr := s.Backend.AllocateVolume(ctx, p.ConnectionID, v)
		// An allocation may have happened even if a later identity observation
		// failed. Preserve any returned exact key/path without claiming that its
		// generation or contents were verified. Recovery must not delete it by name.
		if !same(allocated.Intent, v) || allocated.BackendKey == "" || allocated.Path == "" {
			if allocationErr != nil {
				return allocationErr
			}
			return domain.Fail("RECOVERY_REQUIRED", "allocation identity differs; no upload attempted")
		}
		r.Volumes[i].Allocated = &allocated
		if err = s.save(r, &previous); err != nil {
			return err
		}
		if allocationErr != nil {
			return allocationErr
		}
		if err = operations.Note(ctx, s.Store, "Intent persisted: upload verified source artifact "+v.SourceID+" into its new volume"); err != nil {
			return err
		}
		sourceRoot, sourcePath := root, sources[v.SourceID]
		if in.Seed != nil && v.SourceID == in.Seed.Config.MediaID {
			sourceRoot, sourcePath = seedRoot, in.Seed.Artifact.Path
		}
		f, err := sourceRoot.OpenFile(sourcePath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if !st.Mode().IsRegular() || uint64(st.Size()) != v.FileBytes {
			f.Close()
			return domain.Fail("SOURCE_CHANGED", "prepared disk is no longer the approved ordinary file")
		}
		err = s.cancellable(ctx, func(worker context.Context) error {
			return s.Backend.PopulateVolume(worker, p.ConnectionID, allocated, f)
		})
		f.Close()
		if err != nil {
			return err
		}
		if err = operations.Note(ctx, s.Store, "Readback verification of new volume "+v.Name); err != nil {
			return err
		}
		if err = s.cancellable(ctx, func(worker context.Context) error {
			return s.Backend.VerifyCreatedVolume(worker, p.ConnectionID, allocated)
		}); err != nil {
			return err
		}
		r.Volumes[i].Verified = true
		written += v.FileBytes
		if err = s.save(r, &previous); err != nil {
			return err
		}
	}
	if err = s.checkSource(ctx, p, in); err != nil {
		return err
	}
	if err = s.checkTarget(ctx, p, in); err != nil {
		return err
	}
	cancel, err := operations.CancellationRequested(ctx, s.Store)
	if err != nil {
		return err
	}
	if cancel {
		return domain.Fail("RECOVERY_REQUIRED", "creation canceled with complete independent volumes retained; no VM defined")
	}
	r.VolumesVerified = true
	if err = s.save(r, &previous); err != nil {
		return err
	}
	volumes, err := readyVolumes(r, in)
	if err != nil {
		return err
	}
	if err = operations.Note(ctx, s.Store, "Intent persisted: define the new VM after all managed volumes verified"); err != nil {
		return err
	}
	if _, err = s.Backend.DefineCreatedVM(ctx, p.ConnectionID, in.Target, volumes, r.Binding); err != nil {
		return err
	}
	_, ok, err := s.Backend.ObserveCreatedVM(ctx, p.ConnectionID, in.Target, volumes, r.Binding)
	if err != nil {
		return err
	}
	if !ok {
		return domain.Fail("RECOVERY_REQUIRED", "new definition was not observed")
	}
	r.Defined = true
	return s.save(r, &previous)
}

func (s *Service) cancellable(ctx context.Context, effect func(context.Context) error) error {
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var requested atomic.Bool
	var watchErr error
	go func() {
		defer close(done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-worker.Done():
				return
			case <-tick.C:
				canceled, err := operations.CancellationRequested(ctx, s.Store)
				if err != nil {
					watchErr = err
					cancel()
					return
				}
				if canceled {
					requested.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	err := effect(worker)
	cancel()
	<-done
	if requested.Load() {
		return domain.Fail("RECOVERY_REQUIRED", "volume transfer canceled; allocated volume and source retained; no VM defined")
	}
	if err == nil && watchErr != nil {
		return watchErr
	}
	return err
}
func readyVolumes(r Receipt, in input) ([]domain.CreatedVolume, error) {
	if !r.VolumesVerified || len(r.Volumes) != len(in.Volumes) || r.GuestBootVerified {
		return nil, domain.Fail("RECOVERY_REQUIRED", "complete verified-volume receipt required")
	}
	out := []domain.CreatedVolume{}
	for i, v := range r.Volumes {
		if v.Allocated == nil || !v.Verified || !same(v.Intent, in.Volumes[i]) || !same(v.Allocated.Intent, v.Intent) {
			return nil, domain.Fail("RECOVERY_REQUIRED", "allocated volume receipt differs")
		}
		out = append(out, *v.Allocated)
	}
	return out, nil
}
func (s *Service) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	if err := s.requireUndisposed(p.ID); err != nil {
		return false, err
	}
	var in input
	if err := wire.Decode(b, &in); err != nil {
		return false, err
	}
	if err := creationRecipeVersion(p, in); err != nil {
		return false, err
	}
	r, err := s.load(p.ID)
	if err != nil {
		return false, err
	}
	binding, err := operations.Digest([]string{p.ID, p.InputDigest})
	if err != nil {
		return false, err
	}
	if r.PlanID != p.ID || r.Binding != binding || r.VMID != in.Target.Spec.UUID || r.Connection != p.ConnectionID {
		return false, domain.Fail("RECOVERY_REQUIRED", "creation receipt binding differs")
	}
	volumes, err := readyVolumes(r, in)
	if err != nil {
		return false, err
	}
	_, ok, err := s.Backend.ObserveCreatedVM(ctx, p.ConnectionID, in.Target, volumes, r.Binding)
	if err != nil || !ok {
		return false, err
	}
	if !r.Defined {
		previous, e := s.Store.MetadataBytes("vm-creation", p.ID)
		if e != nil {
			return false, e
		}
		r.Defined = true
		if e = s.save(r, &previous); e != nil {
			return false, e
		}
	}
	if err = s.recordOwnership(p, in, r); err != nil {
		return false, err
	}
	return true, nil
}
func (s *Service) Result(ctx context.Context, uid uint32, id string) (any, error) {
	j, err := s.Store.Job(id)
	if err != nil {
		return nil, err
	}
	p, encoded, err := s.Store.Plan(j.PlanID)
	if err != nil {
		return nil, err
	}
	if p.ActorUID != uid || (!creationOperation(p.Operation) && p.Operation != "vm.create.resume" && p.Operation != "vm.create.cleanup") {
		return nil, domain.Fail("INVALID_INPUT", "not your VM creation operation")
	}
	if p.Operation == "vm.create.cleanup" {
		return s.cleanupResult(j, p, encoded)
	}
	if p.Operation == "vm.create.resume" {
		var recovery resumeInput
		if err = wire.Decode(encoded, &recovery); err != nil {
			return nil, err
		}
		p, _, err = s.Store.Plan(recovery.CreationPlanID)
		if err != nil {
			return nil, err
		}
	}
	receipt, present, err := s.loadOptional(p.ID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"operation": j, "receipt": nil, "receiptAvailable": present, "complete": false, "guestBootVerified": false, "setupVerified": false, "connectivityVerified": false}
	if present {
		result["receipt"] = receipt
	}
	if creationInProgress(j.State) {
		// A successful observation of progress does not mean the operation has
		// completed. The operation's state and receipt flags remain authoritative.
		result["nextActions"] = []string{"operation watch " + j.ID, "operation show " + j.ID}
		return result, nil
	}
	if !present {
		return result, domain.Fail("RECOVERY_REQUIRED", "creation receipt is missing")
	}
	if j.State != "succeeded" {
		return result, domain.Fail("RECOVERY_REQUIRED", "creation is not confirmed complete; retain listed volumes and inspect/reconcile the operation")
	}
	if !receipt.Defined || !receipt.VolumesVerified {
		return result, domain.Fail("RECOVERY_REQUIRED", "successful creation lacks a complete verified definition receipt; inspect without replay")
	}
	result["complete"] = true
	return result, nil
}

func creationInProgress(state string) bool {
	switch state {
	case "queued", "validating", "running", "verifying":
		return true
	default:
		return false
	}
}

var _ operations.Handler = (*Service)(nil)
