package plugins

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

type selectedFixture struct{ fingerprint string }

func (f *selectedFixture) List(context.Context, string) ([]domain.VM, error) { return nil, nil }
func (f *selectedFixture) Get(ctx context.Context, connection, id string) (domain.VM, error) {
	return domain.VM{Key: domain.ResourceKey{ProviderID: "fixture", ConnectionID: connection, Kind: "vm", UUID: id}, Name: "Synthetic selection", State: "stopped", Fingerprint: f.fingerprint}, nil
}
func (f *selectedFixture) Capabilities(context.Context, string) ([]domain.Capability, error) {
	return nil, nil
}
func (f *selectedFixture) Execute(context.Context, string, string, string, map[string]any) error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", "fixture must never receive a VM mutation")
}

func signedPythonFixture(t *testing.T, key ed25519.PrivateKey) string {
	t.Helper()
	source := t.TempDir()
	for _, name := range []string{"main.py", "manifest.json"} {
		b, err := os.ReadFile(filepath.Join("../../tests/fixtures/plugins/python-summary", name))
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0600)
		if name == "main.py" {
			mode = 0700
		}
		if err = os.WriteFile(filepath.Join(source, name), b, mode); err != nil {
			t.Fatal(err)
		}
	}
	m, err := WorkspaceManifest(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(source, "main.py"))
	if err != nil {
		t.Fatal(err)
	}
	entry := m.Entrypoints["linux/amd64"]
	entry.SHA256 = hashBytes(b)
	m.Entrypoints["linux/amd64"] = entry
	os.WriteFile(filepath.Join(source, "manifest.json"), EncodeManifest(m), 0600)
	var archive bytes.Buffer
	if _, err = Pack(source, key, "fixture-key", &archive); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "python.tar")
	os.WriteFile(path, archive.Bytes(), 0600)
	return path
}

func TestInstalledActionPinnedPlanAndDurableResult(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_CONFORMANCE") != "1" {
		t.Skip("requires explicit confined process fixture execution; no real VMs")
	}
	m, pub, key := managerFixture(t)
	provider := &selectedFixture{fingerprint: "before"}
	a := &Actions{Manager: m, Provider: provider, Cache: t.TempDir()}
	m.Engine.Handlers["plugin.call"] = a
	p, err := m.Plan(context.Background(), 1000, "install", installRequest(signedPythonFixture(t, key), pub))
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, p)
	pluginID := "example.virmill.python-summary"
	p, err = m.Plan(context.Background(), 1000, "enable", app.Request{ID: pluginID})
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, p)
	request := app.Request{Connection: "fixture-local", ID: pluginID, Input: map[string]any{"action": "summary", "parameters": map[string]any{}, "vmIDs": []string{"fixture-vm-1"}}}
	p, err = a.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	provider.fingerprint = "changed"
	if _, err = m.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "changed-vm", Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("stale selected inventory accepted")
	}
	p, err = a.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	job := applyPlan(t, m, p)
	result, err := a.Result(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), "fixture-vm-1") || !strings.Contains(string(b), job.ID) {
		t.Fatal("result not durably bound to selected context", string(b))
	}
	p, err = a.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	revoke, err := m.Plan(context.Background(), 1000, "revoke", app.Request{ID: pluginID, Input: map[string]any{"permissions": []Permission{{"vm.read", "selection"}}}})
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, revoke)
	if _, err = m.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "revoked", Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("revoked invocation plan applied")
	}
	t.Log("Confined Python action executed on synthetic selected records; exact result persisted, stale VM and revoked-grant plans refused. No VM/hardware effects.")
}
