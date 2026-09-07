package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

type fixtureProvider struct {
	VM    domain.VM
	calls int
}

func (p *fixtureProvider) List(context.Context, string) ([]domain.VM, error) {
	return []domain.VM{p.VM}, nil
}
func (p *fixtureProvider) Get(context.Context, string, string) (domain.VM, error) { return p.VM, nil }
func (p *fixtureProvider) Capabilities(context.Context, string) ([]domain.Capability, error) {
	return nil, nil
}
func (p *fixtureProvider) Execute(context.Context, string, string, string, map[string]any) error {
	p.calls++
	return nil
}
func TestMissingManagedSaveAndUnknownInputFailBeforeEffect(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	db, e := store.Open(filepath.Join(dir, "journal.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	engine := operations.New(db)
	defer engine.Close()
	provider := &fixtureProvider{VM: domain.VM{Key: domain.ResourceKey{ProviderID: "fixture", ConnectionID: "fixture", Kind: "vm", UUID: "fixture-vm"}, State: "stopped", Fingerprint: "before"}}
	s := New(provider, engine)
	for _, request := range []Request{{Connection: "fixture", ID: "fixture-vm", Action: "restore-saved"}, {Connection: "fixture", ID: "fixture-vm", Action: "start", Input: map[string]any{"password": "must-not-be-stored"}}} {
		response := s.Call(context.Background(), 1000, "vm.plan", request)
		if response.Error == nil || provider.calls != 0 {
			t.Fatal("unsafe plan accepted", response)
		}
	}
	var count int
	if e = db.DB.QueryRow("SELECT COUNT(*) FROM plans").Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid input journaled", count, e)
	}
}

type resourceFixture struct {
	fixtureProvider
	connection, id string
}

func (p *resourceFixture) ListStoragePools(ctx context.Context, uri string) ([]domain.StoragePool, error) {
	p.connection = uri
	return []domain.StoragePool{}, nil
}
func (p *resourceFixture) GetStoragePool(ctx context.Context, uri, id string) (domain.StoragePool, error) {
	p.connection = uri
	p.id = id
	return domain.StoragePool{}, nil
}
func (p *resourceFixture) ListNetworks(ctx context.Context, uri string) ([]domain.VirtualNetwork, error) {
	p.connection = uri
	return []domain.VirtualNetwork{}, nil
}
func (p *resourceFixture) GetNetwork(ctx context.Context, uri, id string) (domain.VirtualNetwork, error) {
	p.connection = uri
	p.id = id
	return domain.VirtualNetwork{}, nil
}
func TestResourceInventoryDoesNotPlanAdoptOrMutate(t *testing.T) {
	p := &resourceFixture{}
	s := &Service{Provider: p}
	for _, method := range []string{"storage.pool.list", "storage.pool.get", "network.list", "network.get"} {
		r := s.Call(context.Background(), 1000, method, Request{Connection: "qemu:///session", ID: "resource-uuid"})
		if r.Error != nil || p.connection != "qemu:///session" || p.calls != 0 {
			t.Fatal("wrong connection or side effect", method, r, p)
		}
	}
	for _, method := range []string{"storage.pool.get", "network.get"} {
		if r := s.Call(context.Background(), 1000, method, Request{}); r.Error == nil || r.Error.Code != "INVALID_INPUT" {
			t.Fatal("missing UUID accepted", r)
		}
	}
	s.Provider = &fixtureProvider{}
	if r := s.Call(context.Background(), 1000, "storage.pool.list", Request{}); r.Error == nil || r.Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatal("unsupported inventory fabricated", r)
	}
}
