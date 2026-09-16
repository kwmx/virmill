//go:build linux && amd64 && cgo

package creating

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const addVMID = "4d9f1b2c-3e4a-4b5c-8d6e-7f8091a2b3c4"

// addFixtureBackend stands in for libvirt: the check walks from "before" to
// "added" once the definition has been written.
type addFixtureBackend struct {
	mu       sync.Mutex
	state    string
	defines  int
	defineAt error
	bus      string
}

func (f *addFixtureBackend) target() domain.DiskAdditionTarget {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: addVMID}
	out := domain.DiskAdditionTarget{VM: key, VMFingerprint: strings.Repeat("a", 64), DefinitionSHA256: strings.Repeat("b", 64),
		AfterXMLSHA256: strings.Repeat("c", 64), PoolID: poolID, PoolName: "fixture-pool",
		VolumeName: "virmill-" + addVMID + "-disk-001.qcow2", Bus: "sata", Target: "sdb", Unit: 1, PoolAvailableBytes: 1 << 30}
	out.ResourceIDs = []string{key.String(), domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "storage-pool", UUID: poolID}.String(), "local-file|/synthetic/" + out.VolumeName}
	slices.Sort(out.ResourceIDs)
	return out
}
func (f *addFixtureBackend) InspectDiskAddition(_ context.Context, uri, id, bus string) (domain.DiskAdditionTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bus = bus
	return f.target(), nil
}
func (f *addFixtureBackend) CheckDiskAddition(context.Context, domain.DiskAdditionPlan) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}
func (f *addFixtureBackend) DefineAddedDisk(context.Context, domain.DiskAdditionPlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.defineAt != nil {
		return f.defineAt
	}
	f.defines++
	f.state = "added"
	return nil
}

// addFixtureTool writes a small stand-in for the sandboxed blank disk.
type addFixtureTool struct{ calls int }

func (f *addFixtureTool) CreateEmpty(_ context.Context, work string, size, bound int64) error {
	f.calls++
	if size < 1<<20 || bound <= size {
		return errors.New("unbounded blank disk request")
	}
	return os.WriteFile(filepath.Join(work, "disk.qcow2"), []byte("synthetic blank disk bytes"), 0600)
}

func addFixture(t *testing.T) (*diskAddHandler, *addFixtureBackend, *addFixtureTool, app.Request) {
	t.Helper()
	s, _, _, _ := creationFixture(t)
	// The private-cache checks require an owner-only directory.
	cache := t.TempDir()
	if err := os.Chmod(cache, 0700); err != nil {
		t.Fatal(err)
	}
	s.SeedCache = cache
	backend := &addFixtureBackend{state: "before"}
	tool := &addFixtureTool{}
	h := &diskAddHandler{s: s, backend: backend, empty: tool}
	s.Engine.Handlers[diskAddOperation] = h
	return h, backend, tool, app.Request{Connection: "qemu:///system", ID: addVMID, Input: map[string]any{"sizeGiB": float64(1)}}
}

func applyAdd(t *testing.T, h *diskAddHandler, plan domain.Plan, key string) domain.Job {
	t.Helper()
	j, err := h.s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: key, Acknowledgements: plan.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		if j, err = h.s.Engine.Store.Job(j.ID); err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(j.State) {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("disk addition did not finish")
	return j
}

func TestDiskAddBuildsVerifiesAndNamesOneNewDisk(t *testing.T) {
	h, backend, tool, request := addFixture(t)
	plan, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls != 1 || backend.bus != "" || plan.Estimates.AdditionalBytes == 0 {
		t.Fatal(tool.calls, backend.bus, plan.Estimates)
	}
	if !slices.Equal(plan.Acknowledgements, []string{"host-mutation", "exclusive-storage-writer", "exclusive-configuration-writer"}) {
		t.Fatal(plan.Acknowledgements)
	}
	if plan.Review["volume"] != backend.target().VolumeName || plan.Review["target"] != "sdb" || plan.Review["bus"] != "sata" ||
		plan.Review["sizeBytes"] != uint64(1<<30) || plan.Review["emptyDisk"] != true {
		t.Fatal(plan.Review)
	}
	_, raw, err := h.s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var in diskAddInput
	if err = wire.Decode(raw, &in); err != nil {
		t.Fatal(err)
	}
	// The recipe pins the blank disk that was actually built.
	workspace := filepath.Join(in.Cache, in.Workspace, "disk.qcow2")
	sealed, size, err := sealDiskFile(workspace)
	if err != nil || sealed != in.Plan.Volume.SHA256 || uint64(size) != in.Plan.Volume.FileBytes {
		t.Fatal(sealed, size, err)
	}
	job := applyAdd(t, h, plan, "add")
	if job.State != "succeeded" || backend.defines != 1 {
		t.Fatal(job.State, job.Error, backend.defines)
	}
	if _, err := os.Stat(filepath.Join(in.Cache, in.Workspace)); !os.IsNotExist(err) {
		t.Fatal("the private workspace was left behind", err)
	}
	var receipt diskAddReceipt
	stored, err := h.s.Store.MetadataBytes(diskAddReceiptKind, job.ID)
	if err != nil || wire.Decode(stored, &receipt) != nil || receipt.Allocated == nil || !receipt.Verified || !receipt.Defined {
		t.Fatal(receipt, err)
	}
}

func TestDiskAddRefusesStaleReviewsAndFullPools(t *testing.T) {
	h, backend, _, request := addFixture(t)
	plan, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.state = "volume-present"
	backend.mu.Unlock()
	// The engine rechecks before accepting the job, so nothing is started.
	_, err = h.s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "stale", Acknowledgements: plan.Acknowledgements})
	var refusal *domain.Error
	if !errors.As(err, &refusal) || refusal.Code != "STALE_PLAN" || backend.defines != 0 {
		t.Fatal(err, backend.defines)
	}

	// The fixture pool reports one GiB free. A bigger disk is allowed, because
	// its file grows only as the guest writes, but it must be acknowledged.
	h, _, _, request = addFixture(t)
	request.Input = map[string]any{"sizeGiB": float64(8)}
	plan, err = h.Plan(context.Background(), 1000, request)
	if err != nil || !slices.Contains(plan.Acknowledgements, "pool-overcommit") {
		t.Fatal(plan.Acknowledgements, err)
	}

	// Invalid input is refused before anything is built, so nothing is left in
	// the private cache.
	h, _, _, request = addFixture(t)
	for _, bad := range []map[string]any{{}, {"sizeGiB": float64(0)}, {"sizeGiB": float64(1), "bus": "ide"}, {"sizeGiB": float64(1), "extra": true}} {
		request.Input = bad
		if _, err = h.Plan(context.Background(), 1000, request); err == nil {
			t.Fatal("invalid input accepted", bad)
		}
	}
	entries, err := os.ReadDir(h.s.SeedCache)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

// Until the definition changes, a failure must not strand this VM's locks.
func TestDiskAddFailureBeforeTheDefinitionFreesTheLocks(t *testing.T) {
	h, backend, _, request := addFixture(t)
	plan, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := h.s.Engine.Store.Plan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var in diskAddInput
	if err = wire.Decode(raw, &in); err != nil {
		t.Fatal(err)
	}
	// The prepared blank disk no longer matches its review, which is refused
	// before any volume exists.
	if err = os.WriteFile(filepath.Join(in.Cache, in.Workspace, "disk.qcow2"), []byte("different bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	job := applyAdd(t, h, plan, "first")
	if job.State != "failed" || job.Error == nil || job.Error.Code != "SOURCE_CHANGED" || backend.defines != 0 {
		t.Fatal(job.State, job.Error, backend.defines)
	}
	// The released locks let a fresh review apply on the same VM.
	plan, err = h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	if job = applyAdd(t, h, plan, "second"); job.State != "succeeded" || backend.defines != 1 {
		t.Fatal("the failed addition kept this VM locked", job.State, job.Error)
	}
}

func TestDiskAddRecoveryObservesInsteadOfWritingAgain(t *testing.T) {
	h, backend, _, request := addFixture(t)
	backend.defineAt = errors.New("synthetic define failure")
	plan, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	j := applyAdd(t, h, plan, "recover")
	if j.State != "recovery-required" || backend.defines != 0 {
		t.Fatal(j.State, j.Error, backend.defines)
	}
	// The volume was written and verified, so recovery only has to observe the
	// definition; it never uploads or defines again.
	backend.mu.Lock()
	backend.defineAt, backend.state = nil, "added"
	backend.mu.Unlock()
	if _, err = h.s.Engine.Reconcile(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := h.s.Engine.Store.Job(j.ID)
	if err != nil || recovered.State != "succeeded" || backend.defines != 0 {
		t.Fatal(recovered.State, backend.defines, err)
	}
}
