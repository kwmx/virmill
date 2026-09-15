package app

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

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

const growVMID = "5b1f2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
const growPoolID = "0f1e2d3c-4b5a-4968-8776-655443322110"

// growFixture stands in for libvirt: CheckDiskGrow reports the fixture state
// and GrowDisk moves it from "before" to "grown" unless told to fail.
type growFixture struct {
	fixtureProvider
	mu        sync.Mutex
	grow      domain.DiskGrow
	state     string
	fail      error
	grows     int
	inspected uint64
}

func (p *growFixture) InspectDiskGrow(_ context.Context, uri, id, target string, capacity uint64) (domain.DiskGrow, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inspected = capacity
	g := p.grow
	g.CapacityBytes = capacity
	return g, nil
}
func (p *growFixture) CheckDiskGrow(context.Context, domain.DiskGrow) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, nil
}
func (p *growFixture) GrowDisk(context.Context, domain.DiskGrow) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail != nil {
		return p.fail
	}
	p.grows++
	p.state = "grown"
	return nil
}

func growService(t *testing.T, available uint64) (*Service, *growFixture) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	db, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	t.Cleanup(func() { engine.Close(); db.Close() })
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: growVMID}
	path := "/var/lib/libvirt/images/app.qcow2"
	resources := []string{key.String(), domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "storage-pool", UUID: growPoolID}.String(), "local-file|" + path}
	slices.Sort(resources)
	p := &growFixture{state: "before", grow: domain.DiskGrow{
		VM: key, VMFingerprint: strings.Repeat("a", 64), PoolAvailableBytes: available, ResourceIDs: resources,
		Disk: domain.RemovalDisk{Target: "vda", Path: path, PoolID: growPoolID, VolumeName: "app.qcow2", VolumeKey: path, Generation: "linux-statx-v1:8:1:42:1:000000000", Fingerprint: strings.Repeat("b", 64), Format: "qcow2", CapacityBytes: 20 << 30, AllocatedBytes: 3 << 30},
	}}
	return New(p, engine), p
}

func planGrow(t *testing.T, s *Service, size float64) (domain.Plan, error) {
	t.Helper()
	response := s.Call(context.Background(), 1000, "vm.plan", Request{Connection: "qemu:///system", ID: growVMID, Action: "grow-disk", Input: map[string]any{"target": "vda", "sizeGiB": size}})
	if response.Error != nil {
		return domain.Plan{}, response.Error
	}
	return response.Data.(domain.Plan), nil
}

func applyGrow(t *testing.T, s *Service, plan domain.Plan, key string) domain.Job {
	t.Helper()
	j, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: key, Acknowledgements: plan.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		if j, err = s.Engine.Store.Job(j.ID); err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(j.State) {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("grow job did not finish")
	return j
}

func TestDiskGrowReviewsAndGrowsTheDiskOnce(t *testing.T) {
	s, p := growService(t, 100<<30)
	plan, err := planGrow(t, s, 40)
	if err != nil {
		t.Fatal(err)
	}
	if p.inspected != 40<<30 || !slices.Equal(plan.Acknowledgements, []string{"host-mutation", "exclusive-storage-writer"}) || plan.Review["afterCapacityBytes"] != uint64(40<<30) || plan.Review["beforeCapacityBytes"] != uint64(20<<30) || plan.Review["guestFilesystemsGrown"] != false {
		t.Fatal(p.inspected, plan.Acknowledgements, plan.Review)
	}
	if j := applyGrow(t, s, plan, "grow"); j.State != "succeeded" || p.grows != 1 {
		t.Fatal(j.State, j.Error, p.grows)
	}
}

func TestDiskGrowRefusesShrinkingAndAsksBeforeOvercommit(t *testing.T) {
	s, _ := growService(t, 5<<30)
	for _, size := range []float64{20, 10} {
		if _, err := planGrow(t, s, size); err == nil || !strings.Contains(err.Error(), "already 20 GiB") {
			t.Fatal(size, err)
		}
	}
	for _, input := range []map[string]any{{"target": "vda"}, {"target": "/dev/vda", "sizeGiB": 40}, {"target": "vda", "sizeGiB": 40.5}, {"target": "vda", "sizeGiB": 40, "force": true}} {
		if response := s.Call(context.Background(), 1000, "vm.plan", Request{Connection: "qemu:///system", ID: growVMID, Action: "grow-disk", Input: input}); response.Error == nil {
			t.Fatal("invalid input accepted", input)
		}
	}
	plan, err := planGrow(t, s, 40)
	if err != nil || !slices.Contains(plan.Acknowledgements, "pool-overcommit") {
		t.Fatal(plan.Acknowledgements, err)
	}
}

func TestDiskGrowThatDidNotHappenFailsAndFreesTheDisk(t *testing.T) {
	s, p := growService(t, 100<<30)
	p.fail = errors.New("resize refused")
	plan, err := planGrow(t, s, 40)
	if err != nil {
		t.Fatal(err)
	}
	if j := applyGrow(t, s, plan, "first"); j.State != "failed" || p.grows != 0 {
		t.Fatal(j.State, j.Error)
	}
	p.mu.Lock()
	p.fail = nil
	p.mu.Unlock()
	plan, err = planGrow(t, s, 40)
	if err != nil {
		t.Fatal(err)
	}
	if j := applyGrow(t, s, plan, "second"); j.State != "succeeded" || p.grows != 1 {
		t.Fatal("the failed grow kept the disk locked", j.State, j.Error)
	}
}

func TestDiskGrowRefusesAPlanWhoseDiskChanged(t *testing.T) {
	s, p := growService(t, 100<<30)
	plan, err := planGrow(t, s, 40)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.state = "grown"
	p.mu.Unlock()
	// The engine rechecks before accepting the job, so nothing is started.
	_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "stale", Acknowledgements: plan.Acknowledgements})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "STALE_PLAN" || p.grows != 0 {
		t.Fatal(err, p.grows)
	}
}
