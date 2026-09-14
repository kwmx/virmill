package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

type poolCreationProvider struct {
	resourceFixture
	mu                          sync.Mutex
	defined                     *domain.StoragePoolDefinition
	active, autostart           bool
	defines, starts, autostarts int
	fault                       string
}

func (p *poolCreationProvider) CheckStoragePoolCreation(ctx context.Context, _ string, d domain.StoragePoolDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.defined != nil {
		return domain.Fail("INVALID_STATE", "a storage pool named "+p.defined.Name+" already exists")
	}
	return ctx.Err()
}
func (p *poolCreationProvider) DefineStoragePool(ctx context.Context, _ string, d domain.StoragePoolDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defines++
	if p.defined != nil {
		return errors.New("overwrite")
	}
	p.defined = &d
	if p.fault == "lost-define" {
		return errors.New("lost define acknowledgement")
	}
	return ctx.Err()
}
func (p *poolCreationProvider) StartStoragePool(ctx context.Context, _ string, d domain.StoragePoolDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts++
	if p.defined == nil || *p.defined != d || p.active {
		return errors.New("different definition or replay")
	}
	p.active = true
	return ctx.Err()
}
func (p *poolCreationProvider) SetStoragePoolAutostart(ctx context.Context, _ string, d domain.StoragePoolDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.autostarts++
	if p.defined == nil || *p.defined != d || !p.active {
		return errors.New("autostart before start")
	}
	p.autostart = true
	return ctx.Err()
}
func (p *poolCreationProvider) InspectCreatedStoragePool(ctx context.Context, uri string, d domain.StoragePoolDefinition) (domain.StoragePool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.defined == nil || *p.defined != d {
		return domain.StoragePool{}, errors.New("absent or drifted pool")
	}
	return domain.StoragePool{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool", UUID: d.UUID}, Name: d.Name, Type: "dir", Active: p.active, Persistent: true, Autostart: p.autostart}, ctx.Err()
}

func poolService(t *testing.T) (*Service, *poolCreationProvider) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &poolCreationProvider{}
	s := New(provider, operations.New(db))
	t.Cleanup(func() { s.Engine.Close(); s.Engine.Store.Close() })
	return s, provider
}
func poolPlanDefinition(t *testing.T, p domain.Plan) domain.StoragePoolDefinition {
	t.Helper()
	var d domain.StoragePoolDefinition
	raw, _ := json.Marshal(p.Review["definition"])
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func poolStepIDs(p domain.Plan) []string {
	out := []string{}
	for _, s := range p.Steps {
		out = append(out, s.ID)
	}
	return out
}

// With no input the plan is libvirt's standard default pool: one reviewed
// acknowledgement and no other tool needed.
func TestStoragePoolCreationPlansLibvirtDefaultsAndCustomChoices(t *testing.T) {
	s, _ := poolService(t)
	ctx := context.Background()
	response := s.Call(ctx, 1000, "storage.pool.create", Request{Action: "create"})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	p := response.Data.(domain.Plan)
	raw, _ := json.Marshal(p)
	if err := validation.Schema("operation-plan", raw); err != nil {
		t.Fatal(err)
	}
	d := poolPlanDefinition(t, p)
	if p.Operation != "storage.pool.create" || p.ConnectionID != "qemu:///system" || d.Name != "default" || d.Path != "/var/lib/libvirt/images" || !d.Autostart || !slices.Equal(p.Acknowledgements, []string{"host-mutation"}) || !slices.Equal(poolStepIDs(p), []string{"define", "start", "autostart"}) {
		t.Fatal("default pool plan", p, d)
	}
	if p.Review["existingFilesChanged"] != false || !strings.Contains(p.Review["poolXML"].(string), "<path>/var/lib/libvirt/images</path>") || !strings.Contains(strings.Join(p.Risks, " "), "keeps its owner, permissions, security label and files") {
		t.Fatal("review does not state folder preservation", p.Review, p.Risks)
	}
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	p, err := s.planStoragePoolCreation(ctx, 1000, Request{Connection: "qemu:///session", Action: "create", Input: map[string]any{}})
	if err != nil || poolPlanDefinition(t, p).Path != filepath.Join(data, "libvirt", "images") {
		t.Fatal("session default folder", p, err)
	}
	p, err = s.planStoragePoolCreation(ctx, 1000, Request{Connection: "qemu:///system", Action: "create", Input: map[string]any{"name": "vms", "path": "/srv/vms", "autostart": false}})
	d = poolPlanDefinition(t, p)
	if err != nil || d.Name != "vms" || d.Path != "/srv/vms" || d.Autostart || !slices.Equal(poolStepIDs(p), []string{"define", "start"}) || !strings.Contains(strings.Join(p.Risks, " "), "not automatically") {
		t.Fatal("custom pool plan", p, err)
	}
}

func TestStoragePoolCreationRefusesUnsafeOrUnknownInput(t *testing.T) {
	s, _ := poolService(t)
	for _, r := range []Request{
		{Connection: "qemu:///system", Action: "create", Input: map[string]any{"folder": "/srv/vms"}},
		{Connection: "qemu:///system", Action: "create", Input: map[string]any{"path": "/etc/libvirt/images"}},
		{Connection: "qemu:///system", Action: "create", Input: map[string]any{"path": "relative"}},
		{Connection: "qemu:///system", Action: "create", Input: map[string]any{"name": "bad name"}},
		{Connection: "qemu:///system", Action: "create", Input: map[string]any{"name": 5}},
		{Connection: "qemu:///system", Action: "create", Input: map[string]any{"autostart": "yes"}},
		{Connection: "qemu:///system", Action: "create", ID: "existing-pool"},
		{Connection: "qemu:///system", Action: "adopt"},
		{Connection: "qemu+ssh://host/system", Action: "create"},
	} {
		if _, err := s.planStoragePoolCreation(context.Background(), 1000, r); err == nil {
			t.Fatal("unsafe pool request planned", r)
		}
	}
}

func TestStoragePoolCreationAppliesOnceAndRefusesASecondPool(t *testing.T) {
	s, provider := poolService(t)
	ctx := context.Background()
	p, err := s.planStoragePoolCreation(ctx, 1000, Request{Connection: "qemu:///system", Action: "create"})
	if err != nil {
		t.Fatal(err)
	}
	req := operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "pool-once", Acknowledgements: p.Acknowledgements}
	j, err := s.Engine.Apply(ctx, 1000, req)
	if err != nil {
		t.Fatal(err)
	}
	if j = awaitConfig(t, s, j.ID); j.State != "succeeded" {
		t.Fatal(j)
	}
	again, err := s.Engine.Apply(ctx, 1000, req)
	if err != nil || again.ID != j.ID {
		t.Fatal(again, err)
	}
	provider.mu.Lock()
	if provider.defines != 1 || provider.starts != 1 || provider.autostarts != 1 || !provider.active || !provider.autostart {
		t.Fatal("pool effects replayed or incomplete", provider.defines, provider.starts, provider.autostarts)
	}
	provider.mu.Unlock()
	if _, err = s.planStoragePoolCreation(ctx, 1000, Request{Connection: "qemu:///system", Action: "create"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal("second default pool planned", err)
	}
}

func TestStoragePoolCreationLostDefinitionNeverStarts(t *testing.T) {
	s, provider := poolService(t)
	provider.fault = "lost-define"
	ctx := context.Background()
	p, err := s.planStoragePoolCreation(ctx, 1000, Request{Connection: "qemu:///system", Action: "create"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Engine.Apply(ctx, 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "pool-lost", Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	if j = awaitConfig(t, s, j.ID); j.State != "recovery-required" {
		t.Fatal(j)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.defines != 1 || provider.starts != 0 || provider.autostarts != 0 {
		t.Fatal("uncertain definition was followed by start")
	}
}
