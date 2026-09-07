//go:build linux && amd64

// Package storageaccess exposes reviewed per-volume read access through the same
// durable coordinator service used by CLI and TUI.
package storageaccess

import (
	"context"
	"path/filepath"
	"reflect"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/fileaccess"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type client interface {
	Root(string) (string, error)
	KeyID() (string, error)
	Identity() (any, error)
	Call(context.Context, helper.Request) (helper.AccessResult, error)
}
type Service struct {
	Engine   *operations.Engine
	Backend  domain.ManagedFileAccessBackend
	Helper   client
	groups   func() ([]uint32, error)
	snapshot func(string) (fileaccess.State, error)
}
type input struct {
	Version int                  `json:"version"`
	RootID  string               `json:"rootID"`
	KeyID   string               `json:"keyID"`
	Access  helper.AccessRequest `json:"access"`
}

func snapshot(path string) (fileaccess.State, error) {
	f, _, err := fileidentity.Open(path, false, false)
	if err != nil {
		return fileaccess.State{}, err
	}
	defer f.Close()
	return fileaccess.Snapshot(f)
}
func Register(appService *app.Service, configDirectory string) {
	backend, ok := appService.Provider.(domain.ManagedFileAccessBackend)
	if !ok {
		return
	}
	s := &Service{Engine: appService.Engine, Backend: backend, Helper: helper.Client{KeyPath: filepath.Join(configDirectory, "helper-key.pem")}, groups: helper.CurrentGroups, snapshot: snapshot}
	for _, op := range []string{"storage.grant-read", "storage.revoke-read"} {
		s.Engine.Handlers[op] = s
	}
	appService.Extensions["storage.access.grant"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.Plan(ctx, uid, r, false) }
	appService.Extensions["storage.access.revoke"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.Plan(ctx, uid, r, true) }
	appService.Extensions["storage.access.result"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.Result(uid, r.ID) }
	appService.Extensions["host.helper.identity"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.Helper.Identity() }
}
func (s *Service) Plan(ctx context.Context, uid uint32, r app.Request, revoke bool) (domain.Plan, error) {
	var empty domain.Plan
	if uid == 0 || r.Connection != "qemu:///system" {
		return empty, domain.Fail("PERMISSION_DENIED", "read grants require an ordinary actor on the local system connection")
	}
	in := input{Version: 1}
	op := "storage.grant-read"
	if revoke {
		if len(r.Input) != 0 {
			return empty, domain.Fail("INVALID_INPUT", "revoke accepts only the original successful grant operation ID")
		}
		job, err := s.Engine.Store.Job(r.ID)
		if err != nil {
			return empty, err
		}
		p, b, err := s.Engine.Store.Plan(job.PlanID)
		if err != nil {
			return empty, err
		}
		if p.ActorUID != uid || job.State != "succeeded" || p.Operation != "storage.grant-read" {
			return empty, domain.Fail("INVALID_INPUT", "your successful original read grant is required")
		}
		if err = wire.Decode(b, &in); err != nil {
			return empty, err
		}
		in.Access.OriginalGrantJobID = job.ID
		op = "storage.revoke-read"
	} else {
		var args struct {
			Target string `json:"target"`
			RootID string `json:"rootID"`
		}
		b, err := operations.Canonical(r.Input)
		if err != nil {
			return empty, err
		}
		if err = validation.Schema("storage-access-grant-input", b); err != nil {
			return empty, err
		}
		if err = wire.Decode(b, &args); err != nil {
			return empty, err
		}
		if args.Target == "" || args.RootID == "" {
			return empty, domain.Fail("INVALID_INPUT", "explicit disk target and administrator root ID required")
		}
		m, err := s.Backend.InspectManagedFileVolume(ctx, r.Connection, r.ID, args.Target)
		if err != nil {
			return empty, err
		}
		in.Access.Mapping = m
		in.RootID = args.RootID
	}
	root, err := s.Helper.Root(in.RootID)
	if err != nil {
		return empty, err
	}
	relative, err := filepath.Rel(root, in.Access.Mapping.Path)
	if err != nil || !filepath.IsLocal(relative) {
		return empty, domain.Fail("PERMISSION_DENIED", "volume lies outside the approved root")
	}
	in.Access.RelativePath = relative
	before, err := s.snapshot(in.Access.Mapping.Path)
	if err != nil {
		return empty, err
	}
	in.Access.Before, err = operations.Canonical(before)
	if err != nil {
		return empty, err
	}
	in.Access.ActorGroups, err = s.groups()
	if err != nil {
		return empty, err
	}
	in.KeyID, err = s.Helper.KeyID()
	if err != nil {
		return empty, err
	}
	vmKey := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "vm", UUID: in.Access.Mapping.VMID}.String()
	poolKey := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "storage-pool", UUID: in.Access.Mapping.PoolID}.String()
	accessDigest, err := operations.Digest(before)
	if err != nil {
		return empty, err
	}
	acks := []string{"host-permission-change", "exclusive-offline-volume"}
	risks := []string{"Changes only this managed volume's POSIX access ACL through the root-authenticated helper; MAC policy can still deny access", "Keep the VM stopped and exclude non-cooperative external writers through completion; QEMU locking does not constrain arbitrary privileged writers"}
	if !revoke {
		acks = append(acks, "persistent-disk-read-access")
		risks = append(risks, "The actor can read and retain all disk content until explicit revocation; restore requires unchanged granted file identity and metadata")
	} else {
		risks = append(risks, "Restores exactly the original grant's recorded access ACL; existing reader descriptors and copies cannot be recalled")
	}
	return s.Engine.Plan(ctx, uid, r.Connection, op, []string{vmKey, poolKey, "file-access|" + before.File.Generation}, map[string]string{vmKey: in.Access.Mapping.VMFingerprint, poolKey: in.Access.Mapping.PoolFingerprint, "access": accessDigest}, in, []domain.Step{{ID: "access", Action: op, Preconditions: []string{"stopped VM, exact native volume and access metadata, current administrator policy and peer credentials"}, Idempotency: "reconcile-before-retry", Compensation: "Explicit reviewed revoke referring to the original successful grant", Reconciliation: "Observe the exact root-owned helper intent and current metadata without replay", CompletionPredicate: "Root-owned helper intent, native mapping and exact observed access metadata agree"}}, acks, risks)
}
func decode(p domain.Plan, b []byte) (input, error) {
	var in input
	if err := wire.Decode(b, &in); err != nil {
		return in, err
	}
	if in.Version != 1 || (p.Operation != "storage.grant-read" && p.Operation != "storage.revoke-read") || p.ConnectionID != "qemu:///system" {
		return in, domain.Fail("UNSUPPORTED_CAPABILITY", "unsupported storage-access recipe")
	}
	return in, nil
}
func (s *Service) request(ctx context.Context, p domain.Plan, in input, mode string) helper.Request {
	id := operations.OperationID(ctx)
	if id == "" {
		id = p.ID
	}
	digest := p.Digest
	if digest == "" {
		digest = p.InputDigest
	}
	expiry := time.Now().UTC().Add(5 * time.Minute)
	if mode == "apply" && p.ExpiresAt.Before(expiry) {
		expiry = p.ExpiresAt
	}
	return helper.Request{APIVersion: domain.APIVersion, ActorUID: p.ActorUID, Operation: p.Operation, ResourceID: in.Access.Mapping.VMID, RootID: in.RootID, PlanDigest: digest, JobID: id, ExpiresAt: expiry, KeyID: in.KeyID, Mode: mode, Access: &in.Access}
}
func (s *Service) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	in, err := decode(p, b)
	if err != nil {
		return err
	}
	groups, err := s.groups()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(groups, in.Access.ActorGroups) {
		return domain.Fail("STALE_PLAN", "coordinator group credentials changed")
	}
	m, err := s.Backend.InspectManagedFileVolume(ctx, p.ConnectionID, in.Access.Mapping.VMID, in.Access.Mapping.DiskTarget)
	if err != nil {
		return err
	}
	if m != in.Access.Mapping {
		return domain.Fail("STALE_PLAN", "managed volume mapping changed")
	}
	_, err = s.Helper.Call(ctx, s.request(ctx, p, in, "check"))
	return err
}
func (s *Service) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	in, err := decode(p, b)
	if err != nil {
		return nil, err
	}
	var before fileaccess.State
	if err = wire.Decode(in.Access.Before, &before); err != nil {
		return nil, err
	}
	review := map[string]any{"volume": in.Access.Mapping, "rootID": in.RootID, "actorUID": p.ActorUID, "actorGroups": in.Access.ActorGroups, "signingKeyID": in.KeyID, "before": before, "originalGrantOperationID": in.Access.OriginalGrantJobID, "changesDiskBytes": false, "startsVM": false}
	observed, err := s.Helper.Call(ctx, s.request(ctx, p, in, "check"))
	if err != nil {
		return nil, err
	}
	review["desiredAccessACL"] = observed.DesiredACL
	if p.Operation == "storage.revoke-read" {
		review["restores"] = "exact original access ACL/mode from the root-owned successful grant receipt"
	}
	return review, nil
}
func (s *Service) Estimate(ctx context.Context, p domain.Plan, b []byte) (domain.Estimates, error) {
	if _, err := decode(p, b); err != nil {
		return domain.Estimates{}, err
	}
	return domain.Estimates{AdditionalBytes: 1 << 20, RequiresDowntime: true, Notes: "Budget for bounded ACL intent/receipt records; filesystem journal/WAL overhead is not exact. No disk copy. Keep the VM stopped through grant and revocation."}, nil
}
func (s *Service) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := decode(p, b)
	if err != nil {
		return err
	}
	if operations.OperationID(ctx) == "" {
		return domain.Fail("INVALID_INPUT", "durable operation context required")
	}
	canceled, err := operations.CancellationRequested(ctx, s.Engine.Store)
	if err != nil {
		return err
	}
	if canceled {
		return operations.ErrCanceledSafely
	}
	if err = operations.Note(ctx, s.Engine.Store, "Intent persisted: request the exact signed managed-volume access change"); err != nil {
		return err
	}
	_, err = s.Helper.Call(ctx, s.request(ctx, p, in, "apply"))
	return err
}
func (s *Service) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	in, err := decode(p, b)
	if err != nil {
		return false, err
	}
	if operations.OperationID(ctx) == "" {
		return false, domain.Fail("INVALID_INPUT", "durable operation context required")
	}
	result, err := s.Helper.Call(ctx, s.request(ctx, p, in, "observe"))
	if err != nil {
		return false, err
	}
	if !result.Complete {
		return false, domain.Fail("RECOVERY_REQUIRED", "helper completion was not observed")
	}
	previous, err := s.Engine.Store.MetadataBytes("storage-access-v1", p.ID)
	if err != nil {
		return false, err
	}
	if previous != nil {
		var saved helper.AccessResult
		if err = wire.Decode(previous, &saved); err != nil {
			return false, err
		}
		if saved != result {
			return false, domain.Fail("RECOVERY_REQUIRED", "access completion receipt differs")
		}
		return true, nil
	}
	if err = s.Engine.Store.ComparePut("storage-access-v1", p.ID, nil, result); err != nil {
		return false, err
	}
	return true, nil
}
func (s *Service) Result(uid uint32, id string) (any, error) {
	job, err := s.Engine.Store.Job(id)
	if err != nil {
		return nil, err
	}
	p, _, err := s.Engine.Store.Plan(job.PlanID)
	if err != nil {
		return nil, err
	}
	if p.ActorUID != uid || (p.Operation != "storage.grant-read" && p.Operation != "storage.revoke-read") {
		return nil, domain.Fail("INVALID_INPUT", "your access operation required")
	}
	b, err := s.Engine.Store.MetadataBytes("storage-access-v1", p.ID)
	if err != nil {
		return nil, err
	}
	var receipt *helper.AccessResult
	if b != nil {
		receipt = &helper.AccessResult{}
		if err = wire.Decode(b, receipt); err != nil {
			return nil, err
		}
	}
	return map[string]any{"operation": job, "receipt": receipt, "complete": job.State == "succeeded" && receipt != nil && receipt.Complete, "currentAccessRechecked": false}, nil
}

var _ operations.Handler = (*Service)(nil)
