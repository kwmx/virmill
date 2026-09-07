//go:build linux && amd64

package storageaccess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/fileaccess"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

// Deterministic service/engine contract fixture; no native or ACL behavior is
// certified by these simulated helper observations.
type fixture struct {
	mu         sync.Mutex
	mapping    domain.ManagedFileVolume
	state      fileaccess.State
	calls      []helper.Request
	applied    int
	deny, lost bool
	result     helper.AccessResult
}

func (f *fixture) InspectManagedFileVolume(context.Context, string, string, string) (domain.ManagedFileVolume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mapping, nil
}
func (f *fixture) Root(string) (string, error) { return "/fixture", nil }
func (f *fixture) KeyID() (string, error)      { return strings.Repeat("d", 64), nil }
func (f *fixture) Identity() (any, error)      { return map[string]any{"publicKey": "synthetic"}, nil }
func (f *fixture) Call(ctx context.Context, r helper.Request) (helper.AccessResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r)
	if f.deny {
		return helper.AccessResult{}, errors.New("synthetic current policy denial")
	}
	if r.Mode == "apply" {
		f.applied++
		f.state.File.Changed = "after"
		f.result = helper.AccessResult{Version: 1, JobID: r.JobID, Binding: "synthetic-binding", Complete: true, State: f.state, DesiredACL: "synthetic-acl"}
		if f.lost {
			return helper.AccessResult{}, errors.New("synthetic lost helper acknowledgement")
		}
		return f.result, nil
	}
	if r.Mode == "observe" {
		if f.result.JobID != r.JobID {
			return helper.AccessResult{}, errors.New("synthetic missing helper intent")
		}
		return f.result, nil
	}
	return helper.AccessResult{Version: 1, JobID: r.JobID, State: f.state, DesiredACL: "synthetic-acl"}, nil
}
func serviceFixture(t *testing.T) (*Service, *fixture, app.Request) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	t.Cleanup(func() { engine.Close(); db.Close() })
	vm := domain.ID()
	name := "virmill-" + vm + "-disk-000.qcow2"
	f := &fixture{mapping: domain.ManagedFileVolume{VMID: vm, VMFingerprint: strings.Repeat("a", 64), PoolID: domain.ID(), PoolFingerprint: strings.Repeat("b", 64), DiskTarget: "vda", VolumeName: name, VolumeKey: "key", Path: "/fixture/" + name}, state: fileaccess.State{UID: 0, GID: 0, File: fileidentity.Identity{Generation: "synthetic-file", Mode: 0100600, Links: 1, Size: 512, Modified: "before", Changed: "before"}}}
	s := &Service{Engine: engine, Backend: f, Helper: f, groups: func() ([]uint32, error) { return []uint32{1000}, nil }, snapshot: func(string) (fileaccess.State, error) { f.mu.Lock(); defer f.mu.Unlock(); return f.state, nil }}
	engine.Handlers["storage.grant-read"] = s
	engine.Handlers["storage.revoke-read"] = s
	return s, f, app.Request{Connection: "qemu:///system", ID: vm, Input: map[string]any{"target": "vda", "rootID": "pool"}}
}
func awaitJob(t *testing.T, s *Service, id string) domain.Job {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		j, err := s.Engine.Store.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		switch j.State {
		case "succeeded", "failed", "canceled", "partial", "recovery-required":
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not reach a terminal observation")
	return domain.Job{}
}
func apply(t *testing.T, s *Service, p domain.Plan) domain.Job {
	t.Helper()
	j, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	return awaitJob(t, s, j.ID)
}
func TestReadAccessPlansAndRevocationUseDurableEngine(t *testing.T) {
	s, f, r := serviceFixture(t)
	p, err := s.Plan(context.Background(), 1000, r, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := operations.Canonical(p)
	if err = validation.Schema("operation-plan", b); err != nil {
		t.Fatal("plan schema", err)
	}
	if f.applied != 0 || len(p.ResourceIDs) != 3 || !p.Estimates.RequiresDowntime || p.Review["desiredAccessACL"] != "synthetic-acl" {
		t.Fatal("preview effects or incomplete review", p)
	}
	_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID()})
	if err == nil {
		t.Fatal("missing acknowledgements accepted")
	}
	j := apply(t, s, p)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	value, err := s.Result(1000, j.ID)
	if err != nil || value.(map[string]any)["complete"] != true {
		t.Fatal("result", err, value)
	}
	revoke, err := s.Plan(context.Background(), 1000, app.Request{Connection: r.Connection, ID: j.ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	if revoke.Operation != "storage.revoke-read" || revoke.Review["originalGrantOperationID"] != j.ID {
		t.Fatal("revocation reference differs")
	}
	restored := apply(t, s, revoke)
	if restored.State != "succeeded" {
		t.Fatal(restored)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applied != 2 {
		t.Fatal("effect count", f.applied)
	}
	for _, call := range f.calls {
		if call.Mode == "apply" && (call.JobID == p.ID || call.PlanDigest == p.InputDigest) {
			t.Fatal("helper effect not bound to accepted final plan/job")
		}
	}
}
func TestLostAcknowledgementReconcilesWithoutReplay(t *testing.T) {
	s, f, r := serviceFixture(t)
	f.lost = true
	p, err := s.Plan(context.Background(), 1000, r, false)
	if err != nil {
		t.Fatal(err)
	}
	j := apply(t, s, p)
	if j.State != "recovery-required" {
		t.Fatal("lost acknowledgement claimed complete", j)
	}
	reconciled, err := s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || reconciled.State != "succeeded" {
		t.Fatal(reconciled, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applied != 1 {
		t.Fatal("recovery repeated effect")
	}
}
func TestChangedMappingGroupsAndPolicyFailBeforeEffect(t *testing.T) {
	for _, which := range []string{"mapping", "groups", "policy"} {
		t.Run(which, func(t *testing.T) {
			s, f, r := serviceFixture(t)
			p, err := s.Plan(context.Background(), 1000, r, false)
			if err != nil {
				t.Fatal(err)
			}
			switch which {
			case "mapping":
				f.mapping.VMFingerprint = strings.Repeat("e", 64)
			case "groups":
				s.groups = func() ([]uint32, error) { return []uint32{10, 1000}, nil }
			case "policy":
				f.deny = true
			}
			_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
			if err == nil || f.applied != 0 {
				t.Fatal("changed authority accepted")
			}
		})
	}
}
func TestAccessSchemaAndRecipeFailClosed(t *testing.T) {
	s, _, r := serviceFixture(t)
	for _, in := range []map[string]any{{"target": "vda"}, {"target": "vda", "rootID": "pool", "path": "/etc/shadow"}, {"target": "../vda", "rootID": "pool"}} {
		r.Input = in
		if _, err := s.Plan(context.Background(), 1000, r, false); err == nil {
			t.Fatal("invalid input accepted", in)
		}
	}
	if _, err := decode(domain.Plan{Operation: "storage.grant-read", ConnectionID: "qemu:///system"}, []byte(`{"version":2}`)); err == nil {
		t.Fatal("newer recipe executed")
	}
}
