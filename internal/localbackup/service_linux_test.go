//go:build linux && amd64

package localbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/backend/restic"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

const captureID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
const resticID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// Real generated coldstore files, SQLite journal and operation engine. Restic
// is a deterministic copy boundary; no cryptographic/native qualification.
type fakeTool struct {
	mu       sync.Mutex
	identity restic.Identity
	calls    []string
	snapshot restic.Snapshot
	archive  map[string][]byte
	journal  *store.Store
	hook     func(string, context.Context) error
}

func (f *fakeTool) Identity(ctx context.Context) (restic.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "identity")
	return f.identity, ctx.Err()
}
func (f *fakeTool) effect(method string, ctx context.Context) error {
	f.mu.Lock()
	f.calls = append(f.calls, method)
	hook := f.hook
	journal := f.journal
	f.mu.Unlock()
	job := operations.OperationID(ctx)
	var state progress
	if job == "" || journal.Get(records, job, &state) != nil || state.OperationID != job || state.Phase == "" {
		return errors.New("effect preceded durable intent")
	}
	observed, err := journal.Job(job)
	if err != nil || observed.State != "running" {
		return errors.New("effect preceded durable running job")
	}
	if hook != nil {
		return hook(method, ctx)
	}
	return ctx.Err()
}
func (f *fakeTool) Init(ctx context.Context, r restic.Repository) error {
	if err := f.effect("init", ctx); err != nil {
		return err
	}
	if err := os.Mkdir(r.Path, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Path, "config"), []byte("generated repository configuration"), 0400); err != nil {
		return err
	}
	return f.after("after-init", ctx)
}
func (f *fakeTool) Check(ctx context.Context, r restic.Repository) error {
	if err := f.effect("check", ctx); err != nil {
		return err
	}
	return f.after("after-check", ctx)
}
func (f *fakeTool) after(method string, ctx context.Context) error {
	f.mu.Lock()
	hook := f.hook
	f.mu.Unlock()
	if hook != nil {
		return hook(method, ctx)
	}
	return ctx.Err()
}
func (f *fakeTool) Backup(ctx context.Context, r restic.Repository, source, job string) (restic.Snapshot, error) {
	if err := f.effect("backup", ctx); err != nil {
		return restic.Snapshot{}, err
	}
	archive := map[string][]byte{}
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		archive[rel] = b
		return err
	})
	if err != nil {
		return restic.Snapshot{}, err
	}
	snapshot := restic.Snapshot{ID: resticID, Tags: []string{"virmill-operation:" + job}, Paths: []string{source}, Time: time.Now().UTC()}
	f.mu.Lock()
	f.archive = archive
	f.snapshot = snapshot
	f.mu.Unlock()
	if err = f.after("after-backup", ctx); err != nil {
		return restic.Snapshot{}, err
	}
	return snapshot, nil
}
func (f *fakeTool) Observe(ctx context.Context, r restic.Repository, job string) ([]restic.Snapshot, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "observe")
	snapshot := f.snapshot
	hook := f.hook
	f.mu.Unlock()
	if hook != nil {
		if err := hook("observe", ctx); err != nil {
			return nil, err
		}
	}
	if snapshot.ID == "" {
		return []restic.Snapshot{}, ctx.Err()
	}
	return []restic.Snapshot{snapshot}, ctx.Err()
}
func (f *fakeTool) Restore(ctx context.Context, r restic.Repository, id, destination string) error {
	if err := f.effect("restore", ctx); err != nil {
		return err
	}
	f.mu.Lock()
	archive := f.archive
	snapshot := f.snapshot
	f.mu.Unlock()
	if id != snapshot.ID || len(archive) == 0 {
		return errors.New("unknown fixture archive")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	names := []string{}
	for name := range archive {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(destination, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, archive[name], 0400); err != nil {
			return err
		}
	}
	if err := filepath.WalkDir(destination, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != destination {
			return os.Chmod(path, 0500)
		}
		return nil
	}); err != nil {
		return err
	}
	return f.after("after-restore", ctx)
}
func (f *fakeTool) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.calls {
		if m == method {
			n++
		}
	}
	return n
}
func (f *fakeTool) setHook(h func(string, context.Context) error) {
	f.mu.Lock()
	f.hook = h
	f.mu.Unlock()
}

type harness struct {
	app                      *app.Service
	s                        *Service
	tool                     *fakeTool
	db                       *store.Store
	root, data, cache, state string
	repo                     restic.Repository
	receipt                  coldstore.Receipt
}

func privateDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
}
func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	privateDir(t, root)
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				return os.Chmod(p, 0700)
			}
			return err
		})
	})
	h := &harness{root: root, data: filepath.Join(root, "data"), cache: filepath.Join(root, "cache"), state: filepath.Join(root, "state")}
	for _, p := range []string{h.data, h.cache, h.state} {
		privateDir(t, p)
	}
	db, err := store.Open(filepath.Join(h.state, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	h.db = db
	h.tool = &fakeTool{identity: restic.Identity{Path: "/usr/bin/restic", SHA256: strings.Repeat("c", 64), Version: "restic fixture-only"}, journal: db}
	h.app = &app.Service{Engine: operations.New(db), Extensions: map[string]func(context.Context, uint32, app.Request) (any, error){}}
	h.s = &Service{Engine: h.app.Engine, Tool: h.tool, Catalog: filepath.Join(h.data, "captures"), Cache: filepath.Join(h.cache, "local-backup")}
	h.s.Register(h.app)
	t.Cleanup(func() { h.app.Engine.Close(); h.db.Close() })
	h.repo = restic.Repository{Path: filepath.Join(root, "repository"), PasswordFile: filepath.Join(root, "password")}
	privateDir(t, h.repo.Path)
	if err = os.WriteFile(h.repo.PasswordFile, []byte("generated-only-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(h.repo.Path, "config"), []byte("generated repository configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	privateDir(t, h.s.Catalog)
	h.receipt = publish(t, h.s.Catalog)
	return h
}
func publish(t *testing.T, catalog string) coldstore.Receipt {
	t.Helper()
	vm := "11111111-2222-4333-8444-555555555555"
	m := protection.CaptureManifest{APIVersion: domain.APIVersion, Kind: "ColdRecoveryPoint", Version: 1, ID: captureID, OperationID: "aaaaaaaa-bbbb-4ccc-8ddd-ffffffffffff", SourceVM: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: vm}, StartedAt: time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 9, 8, 1, 0, 1, 0, time.UTC), SourceFingerprint: strings.Repeat("a", 64), StateBefore: "stopped", StateAfter: "stopped", NativeVersions: map[string]string{"libvirt": "synthetic", "qemu": "synthetic"}, Source: domain.ColdSourceLayout{Architecture: "x86_64", Machine: "q35", State: domain.ColdStateLayout{VMID: vm, SecretReferences: []string{}}, External: []domain.ColdDependency{}, Disks: []domain.ColdDiskSource{{Target: "vda", Device: "disk", Bus: "virtio", Source: domain.ColdStorageSource{Type: "file", File: "/generated/source/disk.raw", Format: "raw"}, Backing: []domain.ColdStorageSource{}}}}, Disks: []protection.CapturedDisk{{Target: "vda", MemberID: "disk", Format: "raw", Independent: true}}, PersistentXMLMember: "xml", EffectiveXMLMember: "xml", Secrets: []protection.CapturedSecret{}, IndependentlyRecoverable: true}
	m.TPMMembers = []protection.CapturedTPMFile{}
	sources := []coldstore.Source{}
	for _, x := range []struct {
		id, kind, path string
		data           []byte
	}{{"disk", "disk", "disks/vda.raw", bytes.Repeat([]byte("generated disk"), 1000)}, {"xml", "persistent-xml", "config/persistent.xml", []byte("<domain/>")}} {
		sum := sha256.Sum256(x.data)
		member := protection.CaptureMember{ID: x.id, Kind: x.kind, Path: x.path, Size: int64(len(x.data)), SHA256: hex.EncodeToString(sum[:])}
		m.Members = append(m.Members, member)
		sources = append(sources, coldstore.Source{Member: member, Reader: bytes.NewReader(x.data)})
	}
	receipt, err := coldstore.Publish(context.Background(), catalog, m, sources)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
func (h *harness) request(kind string) app.Request {
	r := app.Request{Connection: "qemu:///system", Input: map[string]any{"passwordFile": h.repo.PasswordFile}}
	if kind == "repository-init" || kind == "repository-check" {
		r.Path = h.repo.Path
	} else {
		r.ID = captureID
		r.Input["repository"] = h.repo.Path
		if kind == "restore" {
			r.ID = resticID
			r.Input["captureID"] = captureID
			r.Input["manifestSHA256"] = h.receipt.ManifestSHA256
		}
	}
	return r
}
func (h *harness) plan(t *testing.T, kind string) domain.Plan {
	t.Helper()
	r := h.request(kind)
	method := "backup." + strings.ReplaceAll(kind, "-", ".")
	before := h.tool.count("identity")
	response := h.app.Call(context.Background(), uint32(os.Geteuid()), method, r)
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	p, ok := response.Data.(domain.Plan)
	if !ok || p.Digest == "" {
		t.Fatal("no immutable plan", response)
	}
	if h.tool.count("identity") != before+1 {
		t.Fatal("planning repeated executable version observation")
	}
	return p
}
func (h *harness) apply(t *testing.T, p domain.Plan) domain.Job {
	t.Helper()
	j, err := h.app.Engine.Apply(context.Background(), uint32(os.Geteuid()), operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err = h.db.Job(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == "succeeded" || j.State == "recovery-required" || j.State == "failed" || j.State == "canceled" {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job failed to reach terminal boundary", j)
	return j
}
func (h *harness) proof(t *testing.T, j domain.Job) Proof {
	t.Helper()
	r := h.app.Call(context.Background(), uint32(os.Geteuid()), "backup.result", app.Request{Connection: "qemu:///system", ID: j.ID})
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	proof, ok := r.Data.(*Proof)
	if !ok || proof.OperationID != j.ID || proof.GuestBootVerified {
		t.Fatal("incorrect proof", r)
	}
	return *proof
}

func TestLocalBackupRoundtripAndFreshProfileRecovery(t *testing.T) {
	h := newHarness(t)
	p := h.plan(t, "create")
	if h.tool.count("backup")+h.tool.count("restore")+h.tool.count("check")+h.tool.count("observe") != 0 {
		t.Fatal("plan touched restic repository")
	}
	if !reflect.DeepEqual(p.Acknowledgements, []string{"local-storage-mutation", "encrypted-backup"}) {
		t.Fatal("missing acknowledgement", p.Acknowledgements)
	}
	j := h.apply(t, p)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	proof := h.proof(t, j)
	if !proof.RoundtripVerified || proof.RecoveredSetVerified || proof.RepositoryDataChecked || proof.ManifestSHA256 != h.receipt.ManifestSHA256 || proof.SnapshotID != resticID {
		t.Fatal("proof scope wrong", proof)
	}
	if h.tool.count("backup") != 1 || h.tool.count("restore") != 1 || h.tool.count("observe") != 1 {
		t.Fatal("operation repeated repository calls")
	}
	if _, err := coldstore.Inspect(context.Background(), h.s.Catalog, captureID); err != nil {
		t.Fatal("source capture changed", err)
	}
	if _, err := coldstore.Inspect(context.Background(), filepath.Dir(proof.Destination), captureID); err != nil {
		t.Fatal("roundtrip set invalid", err)
	}
	// A second independent store/profile restores from the retained fake archive
	// and supplied manifest hash without reading the original application DB.
	fresh := newHarness(t)
	fresh.app.Engine.Close()
	fresh.db.Close()
	if err := os.RemoveAll(fresh.state); err != nil {
		t.Fatal(err)
	}
	privateDir(t, fresh.state)
	db, err := store.Open(filepath.Join(fresh.state, "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	fresh.db = db
	fresh.app.Engine = operations.New(db)
	fresh.s.Engine = fresh.app.Engine
	fresh.s.Catalog = filepath.Join(fresh.data, "fresh-captures")
	fresh.s.Tool = h.tool
	fresh.tool = h.tool
	fresh.repo = h.repo
	fresh.receipt = h.receipt
	h.tool.mu.Lock()
	h.tool.journal = db
	h.tool.mu.Unlock()
	fresh.s.Register(fresh.app)
	q := fresh.plan(t, "restore")
	job := fresh.apply(t, q)
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	restored := fresh.proof(t, job)
	if !restored.RecoveredSetVerified || restored.RoundtripVerified || restored.SnapshotID != resticID || restored.ManifestSHA256 != proof.ManifestSHA256 || restored.Destination != filepath.Join(fresh.s.Catalog, captureID) {
		t.Fatal("fresh-profile recovery proof wrong", restored)
	}
	if _, err = coldstore.Inspect(context.Background(), fresh.s.Catalog, captureID); err != nil {
		t.Fatal(err)
	}
	r := fresh.app.Call(context.Background(), uint32(os.Geteuid()), "backup.restore", fresh.request("restore"))
	if r.Error == nil {
		t.Fatal("existing capture overwrite offered")
	}
}

func TestLocalBackupRepositoryInitCheckAndProof(t *testing.T) {
	for _, kind := range []string{"repository-init", "repository-check"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			if kind == "repository-init" {
				h.repo.Path = filepath.Join(h.root, "new-repository")
			}
			p := h.plan(t, kind)
			if h.tool.count("init")+h.tool.count("check") != 0 {
				t.Fatal("plan invoked repository operation")
			}
			j := h.apply(t, p)
			if j.State != "succeeded" {
				t.Fatal(j)
			}
			proof := h.proof(t, j)
			if proof.RepositoryConfig.Generation == "" || proof.RepositoryDataChecked != (kind == "repository-check") || proof.RoundtripVerified || proof.RecoveredSetVerified {
				t.Fatal("repository proof scope wrong", proof)
			}
		})
	}
}

func TestLocalBackupUncertainWritesNeverReplay(t *testing.T) {
	for _, fault := range []string{"backup-lost-ack", "roundtrip-lost-ack", "canceled-after-backup", "init-lost-ack", "check-failure"} {
		t.Run(fault, func(t *testing.T) {
			h := newHarness(t)
			kind := "create"
			if fault == "init-lost-ack" {
				kind = "repository-init"
				h.repo.Path = filepath.Join(h.root, "new-repository")
			}
			if fault == "check-failure" {
				kind = "repository-check"
			}
			h.tool.setHook(func(stage string, ctx context.Context) error {
				if fault == "canceled-after-backup" && stage == "after-backup" {
					_, err := h.app.Engine.Cancel(operations.OperationID(ctx))
					return err
				}
				if fault == "backup-lost-ack" && stage == "after-backup" || fault == "roundtrip-lost-ack" && stage == "after-restore" || fault == "init-lost-ack" && stage == "after-init" || fault == "check-failure" && stage == "check" {
					return errors.New("fixture lost acknowledgement")
				}
				return nil
			})
			p := h.plan(t, kind)
			j := h.apply(t, p)
			if j.State != "recovery-required" {
				t.Fatal("uncertain effect not retained", j)
			}
			before := []int{h.tool.count("init"), h.tool.count("backup"), h.tool.count("restore"), h.tool.count("check")}
			for i := 0; i < 2; i++ {
				if _, err := h.app.Engine.Reconcile(context.Background(), j.ID); err == nil {
					t.Fatal("missing completion proof became success")
				}
			}
			after := []int{h.tool.count("init"), h.tool.count("backup"), h.tool.count("restore"), h.tool.count("check")}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("reconcile replayed writes", before, after)
			}
			r := h.app.Call(context.Background(), uint32(os.Geteuid()), "backup.result", app.Request{Connection: "qemu:///system", ID: j.ID})
			if r.Error == nil || r.Data != nil {
				t.Fatal("uncertain job exposed completion")
			}
		})
	}
}

func TestLocalBackupRestoreLostAcknowledgementRecoversByInspection(t *testing.T) {
	h := newHarness(t)
	j := h.apply(t, h.plan(t, "create"))
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	h.s.Catalog = filepath.Join(h.data, "new-catalog")
	h.tool.setHook(func(stage string, _ context.Context) error {
		if stage == "after-restore" {
			return errors.New("lost restore acknowledgement")
		}
		return nil
	})
	p := h.plan(t, "restore")
	j = h.apply(t, p)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	before := h.tool.count("restore")
	job, err := h.app.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || job.State != "succeeded" {
		t.Fatal("exact recovered set was not reconciled", job, err)
	}
	if h.tool.count("restore") != before {
		t.Fatal("restore replayed")
	}
	if !h.proof(t, job).RecoveredSetVerified {
		t.Fatal("missing recovered-set proof")
	}
}

func TestLocalBackupInputStalenessAndRecordConfusion(t *testing.T) {
	for _, fault := range []string{"extra-key", "root-overlap", "credential-symlink", "invalid-capture", "canceled-plan", "changed-tool", "changed-credential", "changed-capture"} {
		t.Run(fault, func(t *testing.T) {
			h := newHarness(t)
			r := h.request("create")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "extra-key":
				r.Input["command"] = "forbidden"
			case "root-overlap":
				r.Input["repository"] = h.s.Catalog
			case "credential-symlink":
				alias := filepath.Join(h.root, "alias")
				if err := os.Symlink(h.repo.PasswordFile, alias); err != nil {
					t.Fatal(err)
				}
				r.Input["passwordFile"] = alias
			case "invalid-capture":
				r.ID = "../capture"
			case "canceled-plan":
				cancel()
			}
			response := h.app.Call(ctx, uint32(os.Geteuid()), "backup.create", r)
			if !strings.HasPrefix(fault, "changed-") {
				if response.Error == nil {
					t.Fatal("invalid input planned", response)
				}
				return
			}
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			p := response.Data.(domain.Plan)
			switch fault {
			case "changed-tool":
				h.tool.mu.Lock()
				h.tool.identity.Version = "changed"
				h.tool.mu.Unlock()
			case "changed-credential":
				if err := os.WriteFile(h.repo.PasswordFile, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-capture":
				path := filepath.Join(h.s.Catalog, captureID, "disks/vda.raw")
				_ = os.Chmod(path, 0600)
				_ = os.WriteFile(path, []byte("changed"), 0600)
			}
			j, err := h.app.Engine.Apply(context.Background(), uint32(os.Geteuid()), operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
			if err == nil || j.ID != "" || h.tool.count("backup") != 0 {
				t.Fatal("stale plan started repository job", j, err)
			}
		})
	}
	for _, fault := range []string{"missing-version", "duplicate-version", "wrong-operation", "wrong-proof-digest"} {
		t.Run(fault, func(t *testing.T) {
			h := newHarness(t)
			j := h.apply(t, h.plan(t, "create"))
			if j.State != "succeeded" {
				t.Fatal(j)
			}
			var raw json.RawMessage
			if err := h.db.Get(records, j.ID, &raw); err != nil {
				t.Fatal(err)
			}
			var state progress
			_ = json.Unmarshal(raw, &state)
			switch fault {
			case "missing-version":
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				delete(m, "version")
				raw, _ = json.Marshal(m)
			case "duplicate-version":
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1))
			case "wrong-operation":
				state.OperationID = domain.ID()
				raw, _ = json.Marshal(state)
			case "wrong-proof-digest":
				state.Proof.ManifestSHA256 = strings.Repeat("b", 64)
				raw, _ = json.Marshal(state)
			}
			if err := h.db.Put(records, j.ID, raw); err != nil {
				t.Fatal(err)
			}
			r := h.app.Call(context.Background(), uint32(os.Geteuid()), "backup.result", app.Request{Connection: "qemu:///system", ID: j.ID})
			if r.Error == nil || r.Data != nil {
				t.Fatal("corrupt completion record became proof")
			}
		})
	}
}
