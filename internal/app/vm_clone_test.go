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

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

const cloneSourceID = "5b1f2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
const clonePoolID = "0f1e2d3c-4b5a-4968-8776-655443322110"

// cloneFixture stands in for libvirt: copies appear one by one and the clone is
// defined once every copy exists.
type cloneFixture struct {
	fixtureProvider
	mu         sync.Mutex
	fresh      bool
	available  uint64
	present    []bool
	defined    bool
	copies     int
	defines    int
	failCopyAt int
	failDefine error
	lastUUID   string
}

func (p *cloneFixture) clone(uri, id, name, uuid string) domain.VMClone {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	newKey := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: uuid}
	c := domain.VMClone{Source: key, SourceName: "web", SourceFingerprint: strings.Repeat("a", 64), DefinitionSHA256: strings.Repeat("c", 64),
		UUID: uuid, Name: name, SharedMedia: []string{"sdb"}, FreshNVRAM: p.fresh}
	resources := []string{key.String(), newKey.String()}
	need := uint64(0)
	for i, target := range []string{"sda", "vda"} {
		path := "/var/lib/libvirt/images/web-" + target + ".qcow2"
		copyPath := "/var/lib/libvirt/images/virmill-" + uuid + "-disk-00" + string(rune('0'+i)) + ".qcow2"
		d := domain.CloneDiskCopy{Disk: domain.RemovalDisk{Target: target, Path: path, PoolID: clonePoolID, VolumeName: filepath.Base(path), VolumeKey: path,
			Generation: "linux-statx-v1:8:1:42:1:000000000", Fingerprint: strings.Repeat("b", 64), Format: "qcow2", CapacityBytes: 10 << 30},
			SourcePoolName: "images", PoolID: clonePoolID, PoolName: "images", VolumeName: filepath.Base(copyPath), VolumePath: copyPath, CopyBytes: 12 << 30}
		c.Disks = append(c.Disks, d)
		need += d.CopyBytes
		resources = append(resources, "local-file|"+copyPath, "local-file|"+path)
	}
	resources = append(resources, domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool", UUID: clonePoolID}.String())
	slices.Sort(resources)
	c.ResourceIDs = slices.Compact(resources)
	c.Pools = []domain.ClonePoolSpace{{PoolName: "images", NeedBytes: need, AvailableBytes: p.available}}
	return c
}

func (p *cloneFixture) InspectClone(_ context.Context, uri, id, name, pool, uuid string) (domain.VMClone, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastUUID = uuid
	p.present = []bool{false, false}
	return p.clone(uri, id, name, uuid), nil
}
func (p *cloneFixture) CheckClone(_ context.Context, c domain.VMClone) (string, []bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	present := slices.Clone(p.present)
	count := 0
	for _, v := range present {
		if v {
			count++
		}
	}
	switch {
	case p.defined:
		return "defined", present, nil
	case count == 0:
		return "before", present, nil
	case count == len(present):
		return "copied", present, nil
	}
	return "copying", present, nil
}
func (p *cloneFixture) CopyCloneDisk(_ context.Context, _ domain.VMClone, i int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i == p.failCopyAt {
		p.present[i] = true // a partial copy was left behind
		return domain.Fail("OPERATION_FAILED", "copying the disk failed: reading the disk stopped early")
	}
	p.copies++
	p.present[i] = true
	return nil
}
func (p *cloneFixture) DefineClone(context.Context, domain.VMClone) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defines++
	if p.failDefine != nil {
		return p.failDefine
	}
	p.defined = true
	return nil
}

func cloneService(t *testing.T, available uint64, fresh bool) (*Service, *cloneFixture) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	db, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	t.Cleanup(func() { engine.Close(); db.Close() })
	p := &cloneFixture{available: available, fresh: fresh, failCopyAt: -1}
	return New(p, engine), p
}

func planClone(s *Service, input map[string]any) (domain.Plan, error) {
	response := s.Call(context.Background(), 1000, "vm.plan", Request{Connection: "qemu:///system", ID: cloneSourceID, Action: "clone", Input: input})
	if response.Error != nil {
		return domain.Plan{}, response.Error
	}
	return response.Data.(domain.Plan), nil
}

// ADR 0066: one review copies every writable disk and defines the clone once.
func TestCloneReviewsCopiesEveryDiskAndDefinesTheCloneOnce(t *testing.T) {
	s, p := cloneService(t, 100<<30, false)
	plan, err := planClone(s, map[string]any{"name": "web-2"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.Acknowledgements, []string{"host-mutation", "exclusive-storage-writer", "exclusive-configuration-writer", "copy-managed-volumes", "new-vm-identity"}) {
		t.Fatal(plan.Acknowledgements)
	}
	risks := strings.Join(plan.Risks, " ")
	for _, want := range []string{"SSH host keys", "new MAC addresses", "web is not changed", "Read-only media (sdb) are shared"} {
		if !strings.Contains(risks, want) {
			t.Fatalf("review does not say %q: %s", want, risks)
		}
	}
	if plan.Review["name"] != "web-2" || plan.Review["changesOriginal"] != false || plan.Review["uuid"] != p.lastUUID || p.lastUUID == cloneSourceID {
		t.Fatal(plan.Review)
	}
	if j := applyGrow(t, s, plan, "clone"); j.State != "succeeded" || p.copies != 2 || p.defines != 1 {
		t.Fatal(j.State, j.Error, p.copies, p.defines)
	}
}

func TestCloneAsksForFreshFirmwareAndOvercommit(t *testing.T) {
	s, _ := cloneService(t, 1<<30, true)
	plan, err := planClone(s, map[string]any{"name": "web-2"})
	if err != nil || !slices.Contains(plan.Acknowledgements, "new-firmware-state") || !slices.Contains(plan.Acknowledgements, "pool-overcommit") ||
		!strings.Contains(strings.Join(plan.Risks, " "), "boot entries are reset") {
		t.Fatal(plan.Acknowledgements, err)
	}
	for _, input := range []map[string]any{{}, {"name": ""}, {"name": "web-2", "force": true}, {"pool": "images"}} {
		if _, err := planClone(s, input); err == nil {
			t.Fatal("invalid input accepted", input)
		}
	}
}

// A copy that fails leaves at most unreferenced volumes: the job fails plainly,
// names every copy that may exist and frees the original.
func TestCloneThatFailsWhileCopyingNamesTheCopiesAndFreesTheVM(t *testing.T) {
	s, p := cloneService(t, 100<<30, false)
	p.failCopyAt = 1
	plan, err := planClone(s, map[string]any{"name": "web-2"})
	if err != nil {
		t.Fatal(err)
	}
	j := applyGrow(t, s, plan, "first")
	if j.State != "failed" || p.defines != 0 || j.Error == nil {
		t.Fatal(j.State, j.Error, p.defines)
	}
	next := strings.Join(j.Error.SafeNextActions, " ")
	for _, i := range []int{0, 1} {
		if !strings.Contains(next, plan.Review["disks"].([]map[string]any)[i]["volume"].(string)) {
			t.Fatal("a copy that may exist is not named", next)
		}
	}
	// The locks are released: a fresh review of the same VM is accepted.
	p.mu.Lock()
	p.failCopyAt = -1
	p.mu.Unlock()
	again, err := planClone(s, map[string]any{"name": "web-3"})
	if err != nil {
		t.Fatal(err)
	}
	if j := applyGrow(t, s, again, "second"); j.State != "succeeded" {
		t.Fatal("the failed clone kept the VM locked", j.State, j.Error)
	}
}

// A define whose result is uncertain is observed, never replayed.
func TestCloneRecoveryObservesTheDefinedCloneWithoutCopyingAgain(t *testing.T) {
	s, p := cloneService(t, 100<<30, false)
	p.failDefine = domain.Fail("RECOVERY_REQUIRED", "secure readback unavailable after defining the clone; do not replay")
	plan, err := planClone(s, map[string]any{"name": "web-2"})
	if err != nil {
		t.Fatal(err)
	}
	j := applyGrow(t, s, plan, "clone")
	if j.State != "recovery-required" {
		t.Fatal(j.State, j.Error)
	}
	if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
		t.Fatal("an undefined clone was reconciled as done")
	}
	p.mu.Lock()
	p.defined = true
	p.mu.Unlock()
	if j, err = s.Engine.Reconcile(context.Background(), j.ID); err != nil || j.State != "succeeded" || p.copies != 2 || p.defines != 1 {
		t.Fatal(j.State, err, p.copies, p.defines)
	}
}

func TestCloneRefusesAPlanWhoseCopiesAlreadyExist(t *testing.T) {
	s, p := cloneService(t, 100<<30, false)
	plan, err := planClone(s, map[string]any{"name": "web-2"})
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.present[0] = true
	p.mu.Unlock()
	_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "stale", Acknowledgements: plan.Acknowledgements})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "STALE_PLAN" || p.copies != 0 {
		t.Fatal(err, p.copies)
	}
}
