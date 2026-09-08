//go:build linux && amd64

package coldcapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/store"
)

// Native observations and image conversion are deterministic test boundaries.
// Source files, QEMU-compatible read guards, operation journal, copy and catalog
// publication are real generated-file operations. Converted bytes are synthetic
// and deliberately make no QCOW2, confinement, native VM or boot claim.
type nativeBackend struct {
	domain.ComputeProvider
	mu                  sync.Mutex
	native              domain.ColdStateInspection
	vm                  domain.VM
	versions            map[string]string
	configurationError  error
	configurationChecks int
}

func clone[T any](value T) T {
	raw, _ := json.Marshal(value)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}
func (b *nativeBackend) InspectColdState(ctx context.Context, uri, id string) (domain.ColdStateInspection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if uri != b.native.Resource.ConnectionID || id != b.native.Resource.UUID {
		return domain.ColdStateInspection{}, errors.New("unexpected fixture native identity")
	}
	return clone(b.native), ctx.Err()
}
func (b *nativeBackend) Get(ctx context.Context, uri, id string) (domain.VM, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if uri != b.vm.Key.ConnectionID || id != b.vm.Key.UUID {
		return domain.VM{}, errors.New("unexpected fixture VM identity")
	}
	return clone(b.vm), ctx.Err()
}
func (b *nativeBackend) ColdRuntimeVersions(ctx context.Context, _ string, aux bool) (map[string]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if aux {
		return nil, errors.New("BIOS fixture has no auxiliary runtime")
	}
	return clone(b.versions), ctx.Err()
}
func (b *nativeBackend) CheckColdConfiguration(ctx context.Context, uri, id, fingerprint string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configurationChecks++
	if b.configurationError != nil {
		return b.configurationError
	}
	if uri != b.vm.Key.ConnectionID || id != b.vm.Key.UUID || fingerprint != captureDigest([]byte(b.vm.PersistentXML)) {
		return domain.Fail("SOURCE_CHANGED", "exact unredacted native XML differs")
	}
	return ctx.Err()
}
func (b *nativeBackend) edit(f func(*nativeBackend)) { b.mu.Lock(); defer b.mu.Unlock(); f(b) }

type diskToolFixture struct {
	mu          sync.Mutex
	identity    image.Identity
	infos       map[string]image.Info
	conversions [][]string
	outputs     map[string][]byte
	onConvert   func(context.Context, int) error
	journal     *store.Store
}

func (d *diskToolFixture) Identity(ctx context.Context) (image.Identity, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.identity, ctx.Err()
}
func (d *diskToolFixture) InspectFile(ctx context.Context, f *os.File, _ string, format string) (image.Info, fileidentity.Identity, error) {
	id, err := fileidentity.InspectFile(f, false)
	if err != nil {
		return image.Info{}, id, err
	}
	d.mu.Lock()
	info, ok := d.infos[filepath.ToSlash(f.Name())]
	d.mu.Unlock()
	if !ok || info.Format != format {
		return image.Info{}, id, fmt.Errorf("unreviewed fixture image %q/%s", f.Name(), format)
	}
	return info, id, ctx.Err()
}
func (d *diskToolFixture) InspectFiles(ctx context.Context, files []platform.DiskSourceFile, _ string, name, format string, _ int64) ([]image.Info, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(files) == 0 || files[0].Path != name {
		return nil, errors.New("fixture chain does not start at the exact reviewed relative name")
	}
	out := make([]image.Info, len(files))
	for i, f := range files {
		info, ok := d.infos[f.Path]
		if !ok || i == 0 && info.Format != format {
			return nil, errors.New("fixture chain contains an unreviewed file")
		}
		out[i] = info
	}
	return out, ctx.Err()
}
func (d *diskToolFixture) ConvertFiles(ctx context.Context, files []platform.DiskSourceFile, work, name, format string, virtual, maximum int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	job, err := d.journal.Job(id)
	if err != nil || job.State != "running" {
		return errors.New("conversion preceded durable running intent")
	}
	events, err := d.journal.Events(id, 0)
	if err != nil {
		return err
	}
	noted := false
	for _, e := range events {
		noted = noted || strings.Contains(e.Message, "flatten and compare captured disk")
	}
	if !noted {
		return errors.New("conversion preceded its durable intent note")
	}
	if len(files) == 0 || files[0].Path != name || virtual <= 0 || maximum < virtual {
		return errors.New("unbounded or unbound fixture conversion")
	}
	paths := []string{}
	raw := []byte("synthetic independent capture\n")
	for _, f := range files {
		info, err := f.File.Stat()
		if err != nil {
			return err
		}
		if info.Size() > 1<<20 {
			return errors.New("fixture input exceeded generated-data bound")
		}
		part := make([]byte, int(info.Size()))
		n, err := f.File.ReadAt(part, 0)
		if err != nil || n != len(part) {
			return errors.New("held fixture source could not be read positionally")
		}
		paths = append(paths, f.Path)
		raw = append(raw, part...)
	}
	d.mu.Lock()
	d.conversions = append(d.conversions, paths)
	number := len(d.conversions)
	hook := d.onConvert
	d.outputs[name] = append([]byte{}, raw...)
	d.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, number); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(work, "disk.qcow2"), raw, 0600)
}
func (d *diskToolFixture) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.conversions) }

type captureHarness struct {
	s              *Service
	app            *app.Service
	backend        *nativeBackend
	tool           *diskToolFixture
	db             *store.Store
	database, root string
	originals      map[string][]byte
}

func newCaptureHarness(t *testing.T) *captureHarness {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				return os.Chmod(path, 0700)
			}
			return err
		})
	})
	sourceRoot := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{"images/leaf.qcow2": []byte("generated leaf header and data"), "base.raw": []byte("generated immutable backing"), "second.raw": []byte("generated second disk"), "installer.iso": []byte("generated read-only installer media")}
	for name, raw := range originals {
		if err := os.WriteFile(filepath.Join(sourceRoot, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	vmID := "11111111-2222-4333-8444-555555555555"
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: vmID}
	layout := domain.ColdStateLayout{VMID: vmID, SecretReferences: []string{}}
	source := &domain.ColdSourceLayout{State: layout, Architecture: "x86_64", Machine: "pc-q35-synthetic", External: []domain.ColdDependency{}, Disks: []domain.ColdDiskSource{
		{Target: "vda", Device: "disk", Bus: "virtio", Source: domain.ColdStorageSource{Type: "file", File: filepath.Join(sourceRoot, "images/leaf.qcow2"), Format: "qcow2"}, Backing: []domain.ColdStorageSource{{Type: "file", File: filepath.Join(sourceRoot, "base.raw"), Format: "raw"}}},
		{Target: "vdb", Device: "disk", Bus: "virtio", Source: domain.ColdStorageSource{Type: "file", File: filepath.Join(sourceRoot, "second.raw"), Format: "raw"}, Backing: []domain.ColdStorageSource{}},
		{Target: "sda", Device: "cdrom", Bus: "sata", ReadOnly: true, Source: domain.ColdStorageSource{Type: "file", File: filepath.Join(sourceRoot, "installer.iso"), Format: "raw"}, Backing: []domain.ColdStorageSource{}},
		{Target: "sdb", Device: "cdrom", Bus: "sata", ReadOnly: true, Empty: true, Source: domain.ColdStorageSource{Type: "file", Format: "raw"}, Backing: []domain.ColdStorageSource{}},
	}}
	xml := `<domain type="kvm"><name>generated original</name><uuid>` + vmID + `</uuid><!-- retain exactly --><metadata><original:opaque xmlns:original="urn:test:original" flag="retain">opaque value</original:opaque></metadata></domain>`
	fingerprint := captureDigest([]byte(xml))
	backend := &nativeBackend{native: domain.ColdStateInspection{Resource: key, State: "stopped", Persistent: true, Fingerprint: fingerprint, Layout: layout, Source: source, Warnings: []string{}}, vm: domain.VM{Key: key, Name: "generated original", State: "stopped", PersistentXML: xml, Fingerprint: fingerprint, Tags: []string{}}, versions: map[string]string{"libvirt": "synthetic", "qemu": "synthetic"}}
	database := filepath.Join(root, "state", "journal.db")
	db, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	tool := &diskToolFixture{identity: image.Identity{Path: "/generated/test-only/qemu-img", Version: "synthetic", SHA256: strings.Repeat("b", 64)}, infos: map[string]image.Info{
		"images/leaf.qcow2": {Filename: "images/leaf.qcow2", Format: "qcow2", VirtualSize: 4096, Backing: "../base.raw", BackingFormat: "raw"},
		"base.raw":          {Filename: "base.raw", Format: "raw", VirtualSize: 4096},
		"second.raw":        {Filename: "second.raw", Format: "raw", VirtualSize: 8192},
		"installer.iso":     {Filename: "installer.iso", Format: "raw", VirtualSize: int64(len(originals["installer.iso"]))},
	}, outputs: map[string][]byte{}, journal: db}
	s := &Service{Engine: engine, Backend: backend, Tool: tool, Catalog: filepath.Join(root, "data", "captures"), Cache: filepath.Join(root, "cache")}
	a := app.New(backend, engine)
	s.Register(a)
	h := &captureHarness{s: s, app: a, backend: backend, tool: tool, db: db, database: database, root: sourceRoot, originals: originals}
	t.Cleanup(func() { h.s.Engine.Close(); _ = h.db.Close() })
	return h
}
func captureDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func (h *captureHarness) request() app.Request {
	return app.Request{ID: h.backend.native.Resource.UUID, Connection: "qemu:///system", Action: "create", Input: map[string]any{"sourceRoot": h.root}}
}
func (h *captureHarness) plan(t *testing.T) domain.Plan {
	t.Helper()
	r := h.app.Call(context.Background(), uint32(os.Getuid()), "snapshot.create", h.request())
	p, ok := r.Data.(domain.Plan)
	if r.Error != nil || !ok || p.ID == "" || p.Digest == "" {
		t.Fatal("capture preview failed", r)
	}
	return p
}
func (h *captureHarness) apply(t *testing.T, p domain.Plan) domain.Job {
	t.Helper()
	r := h.app.Call(context.Background(), uint32(os.Getuid()), "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "capture-" + p.ID, Acknowledgements: append([]string{}, p.Acknowledgements...)}})
	j, ok := r.Data.(domain.Job)
	if r.Error != nil || !ok || j.ID == "" {
		t.Fatal("capture apply failed", r)
	}
	return j
}
func (h *captureHarness) await(t *testing.T, id string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := h.db.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(j.State) || j.State == "recovery-required" {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("capture did not reach bounded terminal/uncertain state")
	return domain.Job{}
}
func (h *captureHarness) unchanged(t *testing.T) {
	t.Helper()
	for name, want := range h.originals {
		got, err := os.ReadFile(filepath.Join(h.root, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("original source changed", name, err)
		}
	}
}
func (h *captureHarness) noJobs(t *testing.T) {
	t.Helper()
	jobs, err := h.db.Jobs()
	if err != nil || len(jobs) != 0 || h.tool.count() != 0 {
		t.Fatal("rejected plan/apply executed work", jobs, err)
	}
}

func TestColdCapturePublicMultiDiskMediaPublicationAndReopenReconcile(t *testing.T) {
	h := newCaptureHarness(t)
	p := h.plan(t)
	if p.Review["guestBootVerified"] != false || p.Review["sourceMutation"] != "none" || len(p.Steps) != 1 {
		t.Fatal("review overstated capture proof", p.Review)
	}
	h.noJobs(t)
	if _, err := os.Stat(h.s.Catalog); !os.IsNotExist(err) {
		t.Fatal("preview created capture catalog", err)
	}
	var in recipe
	_, raw, err := h.db.Plan(p.ID)
	if err != nil || json.Unmarshal(raw, &in) != nil {
		t.Fatal(err)
	}
	if len(in.Disks) != 3 || len(in.Disks[0].Files) != 2 || in.Disks[0].Files[1].Path != filepath.Join(h.root, "base.raw") {
		t.Fatal("relative backing graph was not retained", in.Disks)
	}
	j := h.await(t, h.apply(t, p).ID)
	if j.State != "succeeded" {
		t.Fatal("complete BIOS set did not publish", j)
	}
	if h.tool.count() != 2 {
		t.Fatal("media was converted or a disk was omitted", h.tool.count())
	}
	receipt, err := coldstore.Inspect(context.Background(), h.s.Catalog, in.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.OperationID != j.ID || len(receipt.Manifest.Disks) != 3 || len(receipt.Manifest.Members) != 4 || receipt.Manifest.SourceFingerprint != h.backend.native.Fingerprint || !reflect.DeepEqual(receipt.Manifest.Source, *h.backend.native.Source) {
		t.Fatal("published set lost source inventory", receipt)
	}
	byID := map[string]string{}
	for _, member := range receipt.Manifest.Members {
		byID[member.ID] = member.Path
	}
	xml, err := os.ReadFile(filepath.Join(h.s.Catalog, in.SnapshotID, byID[receipt.Manifest.PersistentXMLMember]))
	if err != nil || string(xml) != h.backend.vm.PersistentXML {
		t.Fatal("original opaque XML was reconstructed or lost", err)
	}
	for _, disk := range receipt.Manifest.Disks {
		memberPath := filepath.Join(h.s.Catalog, in.SnapshotID, byID[disk.MemberID])
		got, err := os.ReadFile(memberPath)
		if err != nil {
			t.Fatal(err)
		}
		name := "images/leaf.qcow2"
		if disk.Target == "vdb" {
			name = "second.raw"
		}
		want := h.tool.outputs[name]
		if disk.Target == "sda" {
			want = h.originals["installer.iso"]
			if disk.Format != "raw" {
				t.Fatal("media format changed")
			}
		}
		if !bytes.Equal(got, want) {
			t.Fatal("wrong captured bytes", disk.Target)
		}
	}
	shown := h.app.Call(context.Background(), uint32(os.Getuid()), "snapshot.show", app.Request{ID: in.SnapshotID})
	if shown.Error != nil || !reflect.DeepEqual(shown.Data, receipt) {
		t.Fatal("public snapshot show changed receipt", shown)
	}
	h.unchanged(t)
	// Simulate a durable lost completion acknowledgement, then reopen the real
	// journal. Recovery must inspect the existing set without invoking the tool.
	j.State = "recovery-required"
	j.Error = domain.Fail("RECOVERY_REQUIRED", "generated acknowledgement-loss test")
	if err = h.db.Update(j, "generated lost completion acknowledgement"); err != nil {
		t.Fatal(err)
	}
	h.s.Engine.Close()
	if err = h.db.Close(); err != nil {
		t.Fatal(err)
	}
	h.db, err = store.Open(h.database)
	if err != nil {
		t.Fatal(err)
	}
	h.s.Engine = operations.New(h.db)
	h.tool.journal = h.db
	h.app = app.New(h.backend, h.s.Engine)
	h.s.Register(h.app)
	if err = h.s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	response := h.app.Call(context.Background(), uint32(os.Getuid()), "operation.reconcile", app.Request{ID: j.ID})
	recovered, ok := response.Data.(domain.Job)
	if response.Error != nil || !ok || recovered.State != "succeeded" || recovered.ID != j.ID || h.tool.count() != 2 {
		t.Fatal("recovery replayed or failed exact inspection", response)
	}
	h.unchanged(t)
}

func TestColdCapturePlanningRefusesIncompleteOrUnsafeIntent(t *testing.T) {
	for _, fault := range []string{"external", "secret", "redacted", "running", "managed-save", "autostart", "outside-root", "absolute-backing", "escaping-backing", "missing-backing-format", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			h := newCaptureHarness(t)
			ctx := context.Background()
			r := h.request()
			switch fault {
			case "external":
				h.backend.native.Source.External = []domain.ColdDependency{{Kind: "hostdev", Target: "device"}}
			case "secret":
				h.backend.native.Layout.SecretReferences = []string{"99999999-aaaa-4bbb-8ccc-dddddddddddd"}
				h.backend.native.Source.State = h.backend.native.Layout
			case "redacted":
				h.backend.configurationError = domain.Fail("UNSUPPORTED_CAPABILITY", "unredacted configuration was not retained")
			case "running":
				h.backend.native.State = "running"
			case "managed-save":
				h.backend.native.HasManagedSave = true
			case "autostart":
				h.backend.native.Autostart = true
			case "outside-root":
				r.Input["sourceRoot"] = filepath.Join(h.root, "images")
			case "absolute-backing":
				info := h.tool.infos["images/leaf.qcow2"]
				info.Backing = filepath.Join(h.root, "base.raw")
				h.tool.infos["images/leaf.qcow2"] = info
			case "escaping-backing":
				info := h.tool.infos["images/leaf.qcow2"]
				info.Backing = "../../unapproved.raw"
				h.tool.infos["images/leaf.qcow2"] = info
			case "missing-backing-format":
				info := h.tool.infos["images/leaf.qcow2"]
				info.BackingFormat = ""
				h.tool.infos["images/leaf.qcow2"] = info
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			response := h.app.Call(ctx, uint32(os.Getuid()), "snapshot.create", r)
			if response.Error == nil {
				t.Fatal("unsafe capture intent produced a plan", response)
			}
			h.noJobs(t)
			h.unchanged(t)
			if _, err := os.Stat(h.s.Catalog); !os.IsNotExist(err) {
				t.Fatal("refusal published catalog state", err)
			}
		})
	}
}

func TestColdCaptureApplyRechecksReviewedGenerationsAndNativeState(t *testing.T) {
	for _, fault := range []string{"source-bytes", "source-replacement", "native-state", "native-layout", "native-xml", "native-version", "tool-version"} {
		t.Run(fault, func(t *testing.T) {
			h := newCaptureHarness(t)
			p := h.plan(t)
			switch fault {
			case "source-bytes":
				if err := os.WriteFile(filepath.Join(h.root, "base.raw"), []byte("changed generated source"), 0600); err != nil {
					t.Fatal(err)
				}
			case "source-replacement":
				name := filepath.Join(h.root, "second.raw")
				if err := os.Rename(name, name+".retained"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, h.originals["second.raw"], 0600); err != nil {
					t.Fatal(err)
				}
			case "native-state":
				h.backend.native.State = "running"
			case "native-layout":
				h.backend.native.Source.Machine = "pc-other"
			case "native-xml":
				h.backend.vm.PersistentXML += "<!-- changed -->"
			case "native-version":
				h.backend.versions["qemu"] = "changed"
			case "tool-version":
				h.tool.identity.Version = "changed"
			}
			response := h.app.Call(context.Background(), uint32(os.Getuid()), "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "refused-" + p.ID, Acknowledgements: p.Acknowledgements}})
			if response.Error == nil {
				t.Fatal("stale capture plan was accepted", response)
			}
			h.noJobs(t)
		})
	}
}

func TestColdCaptureMidStepFailureCancellationAndReconcileNeverReplay(t *testing.T) {
	for _, fault := range []string{"conversion", "cancellation", "native-start", "source-write", "existing-publication-stage"} {
		t.Run(fault, func(t *testing.T) {
			h := newCaptureHarness(t)
			p := h.plan(t)
			var in recipe
			_, raw, err := h.db.Plan(p.ID)
			if err != nil || json.Unmarshal(raw, &in) != nil {
				t.Fatal(err)
			}
			h.tool.onConvert = func(ctx context.Context, n int) error {
				if n != 1 {
					return nil
				}
				switch fault {
				case "conversion":
					return errors.New("generated conversion I/O failure")
				case "cancellation":
					_, err := h.s.Engine.Cancel(operations.OperationID(ctx))
					return err
				case "native-start":
					h.backend.edit(func(b *nativeBackend) { b.native.State = "running" })
				case "source-write":
					return os.WriteFile(filepath.Join(h.root, "base.raw"), []byte("uncooperative generated writer"), 0600)
				case "existing-publication-stage":
					if err := os.MkdirAll(h.s.Catalog, 0700); err != nil {
						return err
					}
					return os.Mkdir(filepath.Join(h.s.Catalog, "."+in.SnapshotID+".partial"), 0700)
				}
				return nil
			}
			j := h.await(t, h.apply(t, p).ID)
			if j.State != "recovery-required" {
				t.Fatal("uncertain capture was labeled complete or safe to replay", j)
			}
			count := h.tool.count()
			if count < 1 || count > 2 {
				t.Fatal("unexpected conversion count", count)
			}
			if _, err := os.Stat(filepath.Join(h.s.Cache, in.SnapshotID)); err != nil {
				t.Fatal("failed capture erased private conversion staging", err)
			}
			got, err := coldstore.Inspect(context.Background(), h.s.Catalog, in.SnapshotID)
			if err == nil || got.SnapshotID != "" {
				t.Fatal("partial capture has a complete receipt", got, err)
			}
			reconciled := h.app.Call(context.Background(), uint32(os.Getuid()), "operation.reconcile", app.Request{ID: j.ID})
			if reconciled.Error == nil || h.tool.count() != count {
				t.Fatal("reconciliation replayed a conversion", reconciled)
			}
			resources, err := h.db.ResourceJobs(p.ResourceIDs[0])
			if err != nil || len(resources) != 1 || resources[0] != j.ID {
				t.Fatal("uncertain capture released its resource lease", resources, err)
			}
			if fault != "source-write" {
				h.unchanged(t)
			}
		})
	}
}

func TestColdCaptureSharedBackingHasOneDurableLease(t *testing.T) {
	h := newCaptureHarness(t)
	other := h.backend.native.Source.Disks[1]
	other.Source = domain.ColdStorageSource{Type: "file", File: filepath.Join(h.root, "images/leaf.qcow2"), Format: "qcow2"}
	other.Backing = clone(h.backend.native.Source.Disks[0].Backing)
	h.backend.native.Source.Disks[1] = other
	p := h.plan(t)
	seen := map[string]bool{}
	for _, key := range p.ResourceIDs {
		if seen[key] {
			t.Fatal("duplicate lease for shared backing", key)
		}
		seen[key] = true
	}
	if !sort.StringsAreSorted(p.ResourceIDs) {
		t.Fatal("resource binding is not deterministic")
	}
	h.noJobs(t)
}

func TestColdCaptureReconcileRequiresExactDurablePublicationIntent(t *testing.T) {
	for _, fault := range []string{"missing", "digest", "operation", "snapshot"} {
		t.Run(fault, func(t *testing.T) {
			h := newCaptureHarness(t)
			p := h.plan(t)
			j := h.await(t, h.apply(t, p).ID)
			if j.State != "succeeded" {
				t.Fatal(j)
			}
			previous, err := h.db.MetadataBytes("cold-capture-publication", p.ID)
			if err != nil || len(previous) == 0 {
				t.Fatal("publication lacked durable digest intent", err)
			}
			var intent publicationIntent
			if err = json.Unmarshal(previous, &intent); err != nil {
				t.Fatal(err)
			}
			originalID := intent.SnapshotID
			switch fault {
			case "missing":
				if _, err = h.db.DB.Exec("DELETE FROM metadata WHERE kind=? AND id=?", "cold-capture-publication", p.ID); err != nil {
					t.Fatal(err)
				}
			case "digest":
				intent.ManifestSHA256 = strings.Repeat("0", 64)
			case "operation":
				intent.OperationID = domain.ID()
			case "snapshot":
				intent.SnapshotID = domain.ID()
			}
			if fault != "missing" {
				if err = h.db.ComparePut("cold-capture-publication", p.ID, previous, intent); err != nil {
					t.Fatal(err)
				}
			}
			j.State = "recovery-required"
			j.Error = domain.Fail("RECOVERY_REQUIRED", "generated uncertain completion")
			if err = h.db.Update(j, "generated journal-binding fault"); err != nil {
				t.Fatal(err)
			}
			// Member integrity remains valid. Only its durable operation binding
			// was removed or changed; that must prevent orchestration success.
			if receipt, err := coldstore.Inspect(context.Background(), h.s.Catalog, originalID); err != nil || receipt.OperationID != j.ID {
				t.Fatal("fixture unexpectedly damaged catalog bytes", err)
			}
			response := h.app.Call(context.Background(), uint32(os.Getuid()), "operation.reconcile", app.Request{ID: j.ID})
			if response.Error == nil || h.tool.count() != 2 {
				t.Fatal("unbound catalog was promoted or recaptured", response)
			}
			current, err := h.db.Job(j.ID)
			if err != nil || current.State != "recovery-required" {
				t.Fatal("binding failure resolved the job", current, err)
			}
			h.unchanged(t)
		})
	}
}
