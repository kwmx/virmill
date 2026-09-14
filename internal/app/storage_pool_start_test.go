package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

const startPoolID = "44444444-4444-4444-8444-444444444444"

type poolStartProvider struct {
	resourceFixture
	mu                 sync.Mutex
	pool               domain.StoragePool
	starts, autostarts int
}

func (p *poolStartProvider) fingerprint() string {
	return fmt.Sprintf("fixture-%v-%v-%s", p.pool.Active, p.pool.Autostart, p.pool.XML)
}
func (p *poolStartProvider) GetStoragePool(ctx context.Context, uri, id string) (domain.StoragePool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != p.pool.Key.UUID {
		return domain.StoragePool{}, errors.New("pool absent")
	}
	out := p.pool
	out.Key.ConnectionID = uri
	out.Fingerprint = p.fingerprint()
	return out, ctx.Err()
}
func (p *poolStartProvider) StartExistingStoragePool(ctx context.Context, _, id, fingerprint string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts++
	if id != p.pool.Key.UUID || p.pool.Active || fingerprint != p.fingerprint() {
		return errors.New("stale plan or replayed start")
	}
	p.pool.Active = true
	return ctx.Err()
}
func (p *poolStartProvider) EnableStoragePoolAutostart(ctx context.Context, _, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.autostarts++
	if id != p.pool.Key.UUID || !p.pool.Active {
		return errors.New("autostart before start")
	}
	p.pool.Autostart = true
	return ctx.Err()
}

func poolStartService(t *testing.T) (*Service, *poolStartProvider) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &poolStartProvider{pool: domain.StoragePool{Key: domain.ResourceKey{ProviderID: "libvirt", Kind: "storage-pool", UUID: startPoolID}, Name: "images", Type: "dir", Persistent: true, XML: "<pool type='dir'><name>images</name><target><path>/srv/vms</path></target></pool>"}}
	s := New(provider, operations.New(db))
	t.Cleanup(func() { s.Engine.Close(); s.Engine.Store.Close() })
	return s, provider
}
func startRequest(input map[string]any) Request {
	return Request{Connection: "qemu:///system", ID: startPoolID, Action: "start", Input: input}
}

func TestStoragePoolStartPlansAStoppedPoolWithAutostart(t *testing.T) {
	s, provider := poolStartService(t)
	ctx := context.Background()
	response := s.Call(ctx, 1000, "storage.pool.start", Request{ID: startPoolID, Action: "start"})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	p := response.Data.(domain.Plan)
	key := storagePoolKey("qemu:///system", startPoolID)
	pool, _ := p.Review["pool"].(map[string]any)
	if p.Operation != "storage.pool.start" || !slices.Equal(p.Acknowledgements, []string{"host-mutation"}) || !slices.Equal(poolStepIDs(p), []string{"start", "autostart"}) || p.Before[key] == "" || pool["name"] != "images" || pool["path"] != "/srv/vms" || p.Review["enableAutostart"] != true {
		t.Fatal("start plan", p)
	}
	if !strings.Contains(strings.Join(p.Risks, " "), "not created or changed") {
		t.Fatal("review does not say the folder is untouched", p.Risks)
	}
	if p, err := s.planStoragePoolStart(ctx, 1000, startRequest(map[string]any{"autostart": false})); err != nil || !slices.Equal(poolStepIDs(p), []string{"start"}) {
		t.Fatal("start without autostart", p, err)
	}
	provider.pool.Autostart = true
	if p, err := s.planStoragePoolStart(ctx, 1000, startRequest(nil)); err != nil || !slices.Equal(poolStepIDs(p), []string{"start"}) || !strings.Contains(strings.Join(p.Risks, " "), "already on") {
		t.Fatal("pool that already starts with the host", p, err)
	}
}

func TestStoragePoolStartRefusesRunningTransientAndBlockPools(t *testing.T) {
	for name, change := range map[string]func(*domain.StoragePool){
		"running":   func(p *domain.StoragePool) { p.Active = true },
		"transient": func(p *domain.StoragePool) { p.Persistent = false },
		"block":     func(p *domain.StoragePool) { p.Type = "logical" },
	} {
		s, provider := poolStartService(t)
		change(&provider.pool)
		if _, err := s.planStoragePoolStart(context.Background(), 1000, startRequest(nil)); err == nil {
			t.Fatal(name, "pool start planned")
		}
	}
	s, _ := poolStartService(t)
	for _, r := range []Request{
		startRequest(map[string]any{"path": "/srv"}),
		startRequest(map[string]any{"autostart": "yes"}),
		{Connection: "qemu:///system", ID: "images", Action: "start"},
		{Connection: "qemu:///system", ID: startPoolID, Action: "create"},
		{Connection: "qemu+ssh://host/system", ID: startPoolID, Action: "start"},
	} {
		if _, err := s.planStoragePoolStart(context.Background(), 1000, r); err == nil {
			t.Fatal("invalid pool start planned", r)
		}
	}
}

func TestStoragePoolStartAppliesOnceAndRefusesAChangedPool(t *testing.T) {
	s, provider := poolStartService(t)
	ctx := context.Background()
	p, err := s.planStoragePoolStart(ctx, 1000, startRequest(nil))
	if err != nil {
		t.Fatal(err)
	}
	req := operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "pool-start-once", Acknowledgements: p.Acknowledgements}
	j, err := s.Engine.Apply(ctx, 1000, req)
	if err != nil {
		t.Fatal(err)
	}
	if j = awaitConfig(t, s, j.ID); j.State != "succeeded" {
		t.Fatal(j)
	}
	if again, err := s.Engine.Apply(ctx, 1000, req); err != nil || again.ID != j.ID {
		t.Fatal(again, err)
	}
	if provider.starts != 1 || provider.autostarts != 1 || !provider.pool.Active || !provider.pool.Autostart {
		t.Fatal("pool start replayed or incomplete", provider.starts, provider.autostarts)
	}

	s, provider = poolStartService(t)
	p, err = s.planStoragePoolStart(ctx, 1000, startRequest(nil))
	if err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	provider.pool.XML = strings.Replace(provider.pool.XML, "/srv/vms", "/srv/other", 1)
	provider.mu.Unlock()
	j, err = s.Engine.Apply(ctx, 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "pool-start-stale", Acknowledgements: p.Acknowledgements})
	if err == nil {
		j = awaitConfig(t, s, j.ID)
	}
	if err == nil && j.State == "succeeded" || provider.starts != 0 {
		t.Fatal("changed pool was started", j, err)
	}
}
