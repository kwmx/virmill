//go:build linux && amd64

package creating

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

type fixtureBackend struct {
	mu                                sync.Mutex
	volumes                           map[string][]byte
	verified                          map[string]bool
	fail, caps                        string
	allocated, populated, definitions int
	defined                           bool
	target                            domain.CreationTarget
	binding                           string
	started, release                  chan struct{}
	poolPath                          string // the pool's folder in its XML, for hand-over tests
}

const poolID = "11111111-1111-4111-8111-111111111111"
const networkID = "22222222-2222-4222-8222-222222222222"

func (f *fixtureBackend) PreflightCreation(ctx context.Context, uri string, s domain.CreationSpec) (domain.CreationTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.defined {
		return domain.CreationTarget{}, domain.Fail("STALE_PLAN", "fixture VM exists")
	}
	return domain.CreationTarget{Spec: s, PoolName: "fixture-pool", PoolFingerprint: "fixture-config", Emulator: "fixture-only", CapabilitiesDigest: f.caps, Networks: []domain.VirtualNetwork{{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "network", UUID: networkID}, Name: "fixture-network", Fingerprint: "network-before", IsolationVerification: "not-run"}}}, nil
}
func (f *fixtureBackend) CheckCreationIdentity(ctx context.Context, uri, id, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.defined {
		return errors.New("fixture exists")
	}
	return nil
}
func (f *fixtureBackend) VolumeAbsent(ctx context.Context, uri string, in domain.VolumeIntent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.volumes[in.Name]; ok {
		return errors.New("fixture volume exists")
	}
	return nil
}
func (f *fixtureBackend) AllocateVolume(ctx context.Context, uri string, in domain.VolumeIntent) (domain.CreatedVolume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allocated++
	if f.fail == "allocate" && f.allocated == 2 {
		return domain.CreatedVolume{}, errors.New("synthetic allocation failure")
	}
	f.volumes[in.Name] = nil
	out := domain.CreatedVolume{Intent: in, BackendKey: "fixture:" + in.Name, Path: "/synthetic/" + in.Name, Generation: "synthetic-generation:" + in.Name}
	if f.fail == "allocation-identity" {
		out.Generation = ""
		return out, errors.New("synthetic generation observation failed after allocation")
	}
	return out, nil
}
func (f *fixtureBackend) PopulateVolume(ctx context.Context, uri string, v domain.CreatedVolume, r io.Reader) error {
	f.mu.Lock()
	f.populated++
	count := f.populated
	wait, started := f.release, f.started
	fail := f.fail
	f.mu.Unlock()
	if wait != nil {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
		}
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumes[v.Intent.Name] = append([]byte(nil), b...)
	if (fail == "populate" && count == 2) || (fail == "populate-media" && v.Intent.ContentType == "cdrom-iso") {
		return errors.New("synthetic upload failure after write")
	}
	return nil
}
func (f *fixtureBackend) VerifyCreatedVolume(ctx context.Context, uri string, v domain.CreatedVolume) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(f.volumes[v.Intent.Name])
	if f.fail == "verify" && len(f.verified) == 1 {
		return errors.New("synthetic readback failure")
	}
	if hex.EncodeToString(sum[:]) != v.Intent.SHA256 {
		return errors.New("fixture data hash differs")
	}
	f.verified[v.Intent.Name] = true
	return nil
}
func (f *fixtureBackend) DefineCreatedVM(ctx context.Context, uri string, target domain.CreationTarget, volumes []domain.CreatedVolume, binding string) (domain.VM, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.definitions++
	if len(f.verified) != len(volumes) {
		return domain.VM{}, errors.New("defined before all disks verified")
	}
	if f.fail == "define" {
		return domain.VM{}, errors.New("synthetic definition failure")
	}
	f.defined = true
	f.target = target
	f.binding = binding
	if f.fail == "ack" {
		return domain.VM{}, errors.New("synthetic lost definition acknowledgement")
	}
	return domain.VM{Key: domain.ResourceKey{UUID: target.Spec.UUID}, State: "stopped"}, nil
}
func (f *fixtureBackend) ObserveCreatedVM(ctx context.Context, uri string, target domain.CreationTarget, volumes []domain.CreatedVolume, binding string) (domain.VM, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return domain.VM{Key: domain.ResourceKey{UUID: target.Spec.UUID}, State: "stopped"}, f.defined && same(target, f.target) && binding == f.binding, nil
}
func (f *fixtureBackend) GetStoragePool(ctx context.Context, uri, id string) (domain.StoragePool, error) {
	free := uint64(1 << 30)
	pool := domain.StoragePool{Key: domain.ResourceKey{UUID: id}, Active: true, State: "running", AvailableBytes: &free}
	if f.poolPath != "" {
		pool.XML = "<pool type='dir'><target><path>" + f.poolPath + "</path></target></pool>"
	}
	return pool, nil
}
func (f *fixtureBackend) ListStoragePools(context.Context, string) ([]domain.StoragePool, error) {
	return nil, errors.New("unused fixture path")
}
func (f *fixtureBackend) ListNetworks(context.Context, string) ([]domain.VirtualNetwork, error) {
	return nil, errors.New("unused fixture path")
}
func (f *fixtureBackend) GetNetwork(context.Context, string, string) (domain.VirtualNetwork, error) {
	return domain.VirtualNetwork{}, errors.New("unused fixture path")
}

func creationFixture(t *testing.T) (*Service, *fixtureBackend, app.Request, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state", "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	backend := &fixtureBackend{volumes: map[string][]byte{}, verified: map[string]bool{}, caps: "fixture-before"}
	s := &Service{Engine: engine, Store: db, Backend: backend, Inventory: backend}
	engine.Handlers["vm.create"] = s
	engine.Handlers["vm.create.devices-v1"] = s
	engine.Handlers["vm.create.resume"] = &resumeHandler{s: s}
	t.Cleanup(func() { engine.Close(); db.Close() })
	directory := t.TempDir()
	artifact := importing.Artifact{APIVersion: domain.APIVersion, Kind: "PreparedImport", SourceSHA256: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), Disks: []importing.PreparedDisk{}, System: importer.System{ID: "fixture", DiskIDs: []string{"boot", "data"}, Items: []importer.Item{{ResourceType: "10"}, {ResourceType: "10"}}}}
	for _, name := range []string{"boot", "data"} {
		b := []byte("synthetic coordinator phase bytes: " + name)
		if err = os.WriteFile(filepath.Join(directory, name), b, 0400); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		artifact.Disks = append(artifact.Disks, importing.PreparedDisk{SourceID: name, Path: name, VirtualBytes: 1 << 20, FileBytes: int64(len(b)), SHA256: hex.EncodeToString(h[:])})
	}
	s.LoadSource = func(ctx context.Context, _ *store.Store, uid uint32, id string) (importing.Artifact, string, error) {
		for _, d := range artifact.Disks {
			b, err := os.ReadFile(filepath.Join(directory, d.Path))
			if err != nil {
				return artifact, "", err
			}
			h := sha256.Sum256(b)
			if hex.EncodeToString(h[:]) != d.SHA256 {
				return artifact, "", domain.Fail("SOURCE_CHANGED", "fixture source changed")
			}
		}
		return artifact, directory, nil
	}
	raw := `{"identityMode":"clone","hardware":{"name":"Creation fixture","poolID":"` + poolID + `","architecture":"x86_64","machine":"pc-q35-fixture","vcpus":2,"memoryMiB":512,"cpu":{"mode":"host-passthrough"},"firmware":{"mode":"bios","secureBoot":false,"tpm":false},"clock":"utc","graphics":"none","disks":[{"sourceID":"boot","bus":"sata","bootOrder":1},{"sourceID":"data","bus":"scsi","bootOrder":2}],"nics":[{"id":"internet","sourceIndex":0,"networkID":"` + networkID + `","model":"virtio","link":"up"},{"id":"lab","sourceIndex":1,"networkID":"` + networkID + `","model":"e1000e","link":"down"}]}}`
	var params map[string]any
	if err = json.Unmarshal([]byte(raw), &params); err != nil {
		t.Fatal(err)
	}
	return s, backend, app.Request{Connection: "fixture", ID: "fixture-import-operation", Input: params}, directory
}
func applyCreation(t *testing.T, s *Service, p domain.Plan) domain.Job {
	t.Helper()
	j, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func awaitCreation(t *testing.T, s *Service, id string) domain.Job {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		j, err := s.Store.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(j.State) {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fixture operation did not settle")
	return domain.Job{}
}
func TestDefinitionFollowsCompleteIndependentVolumeVerification(t *testing.T) {
	s, backend, r, dir := creationFixture(t)
	original, err := os.ReadFile(filepath.Join(dir, "boot"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	if backend.allocated != 0 {
		t.Fatal("preview allocated resources")
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	receipt, err := s.load(p.ID)
	if err != nil || !receipt.Defined || !receipt.VolumesVerified || receipt.GuestBootVerified {
		t.Fatal("incorrect creation stages", receipt, err)
	}
	if backend.definitions != 1 || backend.allocated != 2 || backend.populated != 2 {
		t.Fatal("disk loss or repeated effects", backend)
	}
	backend.mu.Lock()
	backend.volumes[receipt.Volumes[0].Intent.Name][0] = 'X'
	backend.mu.Unlock()
	after, _ := os.ReadFile(filepath.Join(dir, "boot"))
	if !bytes.Equal(original, after) {
		t.Fatal("managed copy changed original")
	}
	result, err := s.Result(context.Background(), 1000, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(result)
	if !bytes.Contains(b, []byte(`"guestBootVerified":false`)) {
		t.Fatal("boot claim not explicit")
	}
	t.Log("Synthetic coordinator adapter proves ordering and independent copied fixture bytes only; no native volume upload, VM definition or guest boot executed")
}
func TestCreationFailuresRetainOriginalsAndNeverDefinePartialSets(t *testing.T) {
	for _, failure := range []string{"allocate", "populate", "verify", "define"} {
		t.Run(failure, func(t *testing.T) {
			s, backend, r, dir := creationFixture(t)
			backend.fail = failure
			original, _ := os.ReadFile(filepath.Join(dir, "boot"))
			p, err := s.Plan(context.Background(), 1000, r)
			if err != nil {
				t.Fatal(err)
			}
			j := awaitCreation(t, s, applyCreation(t, s, p).ID)
			if j.State != "recovery-required" || backend.defined {
				t.Fatal("partial set represented as VM", j, backend)
			}
			after, _ := os.ReadFile(filepath.Join(dir, "boot"))
			if !bytes.Equal(original, after) {
				t.Fatal("source mutated on failure")
			}
			receipt, err := s.load(p.ID)
			if err != nil || len(receipt.Volumes) != 2 {
				t.Fatal("partial receipt missing", receipt, err)
			}
			if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
				t.Fatal("absent definition accepted as complete")
			}
		})
	}
}
func TestLostDefinitionAcknowledgementReconcilesWithoutReplay(t *testing.T) {
	s, backend, r, _ := creationFixture(t)
	backend.fail = "ack"
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	recovered, err := s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || recovered.State != "succeeded" || backend.definitions != 1 || backend.allocated != 2 {
		t.Fatal("definition replayed or unconfirmed", recovered, err)
	}
	receipt, _ := s.load(p.ID)
	if !receipt.Defined {
		t.Fatal("receipt not reconciled")
	}
}
func TestCancelUploadRetainsAllocatedVolumeWithoutDefining(t *testing.T) {
	s, backend, r, dir := creationFixture(t)
	backend.started = make(chan struct{})
	backend.release = make(chan struct{})
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j := applyCreation(t, s, p)
	select {
	case <-backend.started:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	if _, err = s.Engine.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	j = awaitCreation(t, s, j.ID)
	if j.State != "recovery-required" || backend.defined || backend.allocated != 1 {
		t.Fatal("cancellation lost retained resource state", j)
	}
	if _, err = os.Stat(filepath.Join(dir, "boot")); err != nil {
		t.Fatal("source removed")
	}
}
func TestIncompleteMappingsAndStaleTargetFailBeforeAllocation(t *testing.T) {
	s, backend, r, _ := creationFixture(t)
	hardware := r.Input["hardware"].(map[string]any)
	original := hardware["disks"]
	hardware["disks"] = hardware["disks"].([]any)[:1]
	if _, err := s.Plan(context.Background(), 1000, r); err == nil {
		t.Fatal("missing source disk accepted")
	}
	hardware["disks"] = original
	nics := hardware["nics"]
	hardware["nics"] = hardware["nics"].([]any)[:1]
	if _, err := s.Plan(context.Background(), 1000, r); err == nil {
		t.Fatal("missing source NIC accepted")
	}
	hardware["nics"] = nics
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	backend.caps = "fixture-after"
	if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "stale", Acknowledgements: p.Acknowledgements}); err == nil || backend.allocated != 0 {
		t.Fatal("stale capabilities permitted allocation")
	}
}
