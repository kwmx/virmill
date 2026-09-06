package plugins

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

func managerFixture(t *testing.T) (*Manager, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "state", "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	t.Cleanup(func() { engine.Close(); db.Close() })
	payload := filepath.Join(root, "plugins")
	if err = os.Mkdir(payload, 0700); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Store: db, Engine: engine, Root: payload}
	for _, a := range []string{"install", "update", "enable", "disable", "remove", "rollback", "grant", "revoke"} {
		engine.Handlers["plugin."+a] = m
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return m, pub, key
}

func distribution(t *testing.T, key ed25519.PrivateKey, version string) string {
	t.Helper()
	root := t.TempDir()
	exe := []byte("#!/bin/sh\n# packaging fixture; never launched\nexit 0\n")
	if err := os.WriteFile(filepath.Join(root, "entry"), exe, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"manifestVersion": "1", "id": "example.virmill.fixture", "name": "Signed fixture", "version": version, "protocol": map[string]string{"minVersion": "1.0", "maxVersion": "1.0", "transport": "stdio-jsonrpc"}, "entrypoints": map[string]any{"linux/amd64": map[string]string{"path": "entry", "sha256": hashBytes(exe)}}, "extensionTypes": []string{"action"}, "permissions": []Permission{{"vm.read", "selection"}}, "network": "none"}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := Pack(root, key, "fixture-key", &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "signed.tar")
	if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitJob(t *testing.T, m *Manager, id string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := m.Store.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(job.State) {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return domain.Job{}
}

func applyPlan(t *testing.T, m *Manager, p domain.Plan) domain.Job {
	t.Helper()
	j, err := m.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	j = waitJob(t, m, j.ID)
	if j.State != "succeeded" {
		t.Fatalf("%s: %s %+v", p.Operation, j.State, j.Error)
	}
	return j
}

func installRequest(path string, pub ed25519.PublicKey) app.Request {
	return app.Request{Path: path, Input: map[string]any{"keyID": "fixture-key", "publicKey": hex.EncodeToString(pub), "permissions": []Permission{{"vm.read", "selection"}}}}
}

func TestInstallUpdateRollbackPermissionAndPreservation(t *testing.T) {
	m, pub, key := managerFixture(t)
	p, err := m.Plan(context.Background(), 1000, "install", installRequest(distribution(t, key, "0.1.0"), pub))
	if err != nil {
		t.Fatal(err)
	}
	if p.Review["packageDigest"] == "" || p.Review["version"] != "0.1.0" {
		t.Fatal("approval preview omitted exact package identity", p.Review)
	}
	planJSON, _ := json.Marshal(p)
	if err = validation.Schema("operation-plan", planJSON); err != nil {
		t.Fatal("implementation plan schema drift", err)
	}
	if list, err := m.List(); err != nil || len(list) != 0 {
		t.Fatal("preview installed payload", list, err)
	}
	if _, err = m.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "missing-review"}); err == nil {
		t.Fatal("trust review omitted")
	}
	applyPlan(t, m, p)
	installed, err := m.Show("example.virmill.fixture")
	if err != nil || installed.Enabled {
		t.Fatal("installation must start disabled", err)
	}
	original := installed.Active
	plan := func(action string, input map[string]any) domain.Plan {
		t.Helper()
		p, err := m.Plan(context.Background(), 1000, action, app.Request{ID: installed.ID, Input: input})
		if err != nil {
			t.Fatal(action, err)
		}
		return p
	}
	applyPlan(t, m, plan("enable", nil))
	applyPlan(t, m, plan("revoke", map[string]any{"permissions": []Permission{{"vm.read", "selection"}}}))
	if _, err = m.Plan(context.Background(), 1000, "enable", app.Request{ID: installed.ID}); err == nil {
		t.Fatal("missing scope enabled")
	}
	applyPlan(t, m, plan("grant", map[string]any{"permissions": []Permission{{"vm.read", "selection"}}}))
	p, err = m.Plan(context.Background(), 1000, "update", installRequest(distribution(t, key, "0.2.0"), pub))
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, p)
	updated, _ := m.Show(installed.ID)
	if updated.Active == original || updated.Previous != original || updated.Enabled {
		t.Fatal("version activation contract", updated)
	}
	applyPlan(t, m, plan("rollback", nil))
	rolled, _ := m.Show(installed.ID)
	if rolled.Active != original || len(rolled.Versions) != 2 {
		t.Fatal("rollback lost version")
	}
	preserve := filepath.Join(t.TempDir(), "external-resource")
	os.WriteFile(preserve, []byte("fixture representing external ownership"), 0600)
	applyPlan(t, m, plan("remove", nil))
	list, _ := m.List()
	if len(list) != 0 {
		t.Fatal("removed installation still listed")
	}
	removed, _ := m.Show(installed.ID)
	if !removed.Removed || removed.Enabled || len(removed.Versions) != 2 {
		t.Fatal("removal lost recovery metadata")
	}
	if _, err = os.Stat(preserve); err != nil {
		t.Fatal("external artifact modified")
	}
	for _, v := range removed.Versions {
		if _, err = m.verifyInstalled(v); err != nil {
			t.Fatal("removed retained version corrupted", err)
		}
	}
}

func TestChangedPackageStalePlanAndImmutableVersion(t *testing.T) {
	m, pub, key := managerFixture(t)
	path := distribution(t, key, "0.1.0")
	r := installRequest(path, pub)
	p, err := m.Plan(context.Background(), 1000, "install", r)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("changed after preview"), 0600)
	if _, err = m.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "changed", Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("changed source accepted")
	}
	r.Path = distribution(t, key, "0.1.0")
	p, err = m.Plan(context.Background(), 1000, "install", r)
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, p)
	first, err := m.Plan(context.Background(), 1000, "enable", app.Request{ID: "example.virmill.fixture"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Plan(context.Background(), 1000, "remove", app.Request{ID: "example.virmill.fixture"})
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, first)
	if _, err = m.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: second.ID, PlanDigest: second.Digest, IdempotencyKey: "stale", Acknowledgements: second.Acknowledgements}); err == nil {
		t.Fatal("stale installation applied")
	}
	_, different, _ := ed25519.GenerateKey(rand.Reader)
	r = installRequest(distribution(t, different, "0.1.0"), different.Public().(ed25519.PublicKey))
	if _, err = m.Plan(context.Background(), 1000, "update", r); err == nil {
		t.Fatal("same semantic version overwritten")
	}
}

type lostAcknowledgement struct{ *Manager }

func (h lostAcknowledgement) Execute(ctx context.Context, p domain.Plan, b []byte, s domain.Step) error {
	if err := h.Manager.Execute(ctx, p, b, s); err != nil {
		return err
	}
	return errors.New("fault fixture: commit completed, acknowledgement lost")
}

func TestLifecycleRecoveryObservesCommitWithoutRelaunch(t *testing.T) {
	m, pub, key := managerFixture(t)
	m.Engine.Handlers["plugin.install"] = lostAcknowledgement{m}
	p, err := m.Plan(context.Background(), 1000, "install", installRequest(distribution(t, key, "0.1.0"), pub))
	if err != nil {
		t.Fatal(err)
	}
	j, err := m.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "recover", Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	if job := waitJob(t, m, j.ID); job.State != "recovery-required" {
		t.Fatal("uncertain effect declared complete")
	}
	// The source package is no longer required to reconcile the committed version.
	_, input, _ := m.Store.Plan(p.ID)
	var in lifecycleInput
	json.Unmarshal(input, &in)
	os.Remove(in.Source)
	if err = m.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	recovered, err := m.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || recovered.State != "succeeded" {
		t.Fatal("cannot observe installed original effect", recovered, err)
	}
}

func TestDistributionRejectsTrailingArchiveAndParentCollisions(t *testing.T) {
	b, pub := packageFixture(t)
	if _, err := Verify(bytes.NewReader(append(b, []byte("hidden unsigned payload")...)), map[string]ed25519.PublicKey{"test-key": pub}); err == nil {
		t.Fatal("trailing unsigned bytes accepted")
	}
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	for _, name := range []string{"a/b", "a"} {
		tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: 1})
		tw.Write([]byte{0})
	}
	tw.Close()
	if _, err := Verify(bytes.NewReader(out.Bytes()), nil); err == nil {
		t.Fatal("file/parent collision accepted")
	}
}

func TestLifecycleRequiresActiveInvocationDispositionAndSchemaGuard(t *testing.T) {
	m, pub, key := managerFixture(t)
	p, err := m.Plan(context.Background(), 1000, "install", installRequest(distribution(t, key, "0.1.0"), pub))
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, p)
	r, err := m.Show("example.virmill.fixture")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a durably accepted invocation lock without launching any program.
	active := p
	active.ID = domain.ID()
	active.Operation = "plugin.call"
	active.ResourceIDs = []string{"plugin-version:local:" + r.ID + ":" + r.Active}
	if err = m.Store.SavePlan(active, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	job, err := m.Store.Accept(active, "active-invocation", "active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Plan(context.Background(), 1000, "disable", app.Request{ID: r.ID}); err == nil {
		t.Fatal("active invocation disposition silently selected")
	}
	p, err = m.Plan(context.Background(), 1000, "disable", app.Request{ID: r.ID, Input: map[string]any{"activeJobs": "finish"}})
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, p)
	still, err := m.Store.Job(job.ID)
	if err != nil || still.State != "queued" {
		t.Fatal("lifecycle operation killed or changed existing invocation", still, err)
	}
	r, err = m.Show(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.SchemaVersion = 2
	if err = m.Store.Put("plugin-installation", r.ID, r); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Show(r.ID); err == nil {
		t.Fatal("newer installation schema silently interpreted")
	}
}
