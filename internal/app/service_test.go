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
