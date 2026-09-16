//go:build linux && amd64 && cgo

package creating

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// disposeFixtureBackend reports what the host would show for an unresolved
// addition and deletes only an unreferenced volume.
type disposeFixtureBackend struct {
	mu         sync.Mutex
	referenced bool
	state      string
	deletes    int
}

func (f *disposeFixtureBackend) InspectAddedDiskDisposal(_ context.Context, in domain.DiskAdditionPlan) (domain.AddedDiskDisposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := domain.AddedDiskDisposal{Referenced: f.referenced, VolumeState: f.state, GraphDigest: strings.Repeat("e", 64), ResourceIDs: in.Target.ResourceIDs}
	if f.state == "present" {
		out.Disk = &domain.RemovalDisk{Target: in.Target.Target, Path: "/synthetic/" + in.Target.VolumeName, PoolID: in.Target.PoolID,
			VolumeName: in.Target.VolumeName, VolumeKey: "fixture:" + in.Target.VolumeName, Generation: "linux-statx-v1:1:1:1:1:000000000",
			Fingerprint: strings.Repeat("d", 64), Format: "qcow2", CapacityBytes: in.Volume.VirtualBytes}
	}
	return out, nil
}
func (f *disposeFixtureBackend) DeleteUnreferencedDisk(_ context.Context, _ domain.DiskAdditionPlan, reviewed domain.AddedDiskDisposal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if reviewed.Referenced {
		return errors.New("fixture refused to delete a referenced disk")
	}
	f.deletes++
	f.state = "absent"
	return nil
}

// stuckAddition leaves one addition needing recovery, as a define failure does.
func stuckAddition(t *testing.T, referenced bool) (*diskAddHandler, *addFixtureBackend, *diskAddDisposeHandler, *disposeFixtureBackend, domain.Job, app.Request) {
	t.Helper()
	h, backend, _, request := addFixture(t)
	backend.defineAt = errors.New("synthetic define failure")
	plan, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	job := applyAdd(t, h, plan, "stuck")
	if job.State != "recovery-required" {
		t.Fatal("expected an unresolved addition, got", job.State, job.Error)
	}
	backend.mu.Lock()
	backend.defineAt = nil
	if referenced {
		backend.state = "added"
	}
	backend.mu.Unlock()
	disposal := &disposeFixtureBackend{referenced: referenced, state: "present"}
	d := &diskAddDisposeHandler{s: h.s, backend: disposal}
	h.s.Engine.Handlers[diskAddDisposeOperation] = d
	return h, backend, d, disposal, job, request
}

func disposeRequest(job domain.Job, disposition string) app.Request {
	return app.Request{Connection: "qemu:///system", ID: job.ID, Input: map[string]any{"disposition": disposition}}
}

// ADR 0062: accepting closes the addition and releases this VM's locks.
func TestDiskAddDispositionAcceptsAndFreesTheVM(t *testing.T) {
	h, backend, d, disposal, job, request := stuckAddition(t, true)
	plan, err := d.Plan(context.Background(), 1000, disposeRequest(job, "accept"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ack := range []string{"inherit-recovery-resources", "close-disk-addition"} {
		if !strings.Contains(strings.Join(plan.Acknowledgements, " "), ack) {
			t.Fatal(plan.Acknowledgements)
		}
	}
	if strings.Contains(strings.Join(plan.Acknowledgements, " "), "data-loss") || plan.Review["diskDeletion"] != false {
		t.Fatal("accepting must not ask to delete anything", plan.Acknowledgements, plan.Review)
	}
	if closed := applyAdd(t, h, plan, "accept"); closed.State != "succeeded" || disposal.deletes != 0 {
		t.Fatal(closed.State, closed.Error, disposal.deletes)
	}
	parent, err := h.s.Engine.Store.Job(job.ID)
	if err != nil || parent.State != "partial" {
		t.Fatal("the closed addition is not recorded as partial", parent.State, err)
	}
	// The released locks let a fresh addition apply on the same VM.
	backend.mu.Lock()
	backend.state = "before"
	backend.mu.Unlock()
	fresh, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	if done := applyAdd(t, h, fresh, "fresh"); done.State != "succeeded" {
		t.Fatal("the closed addition kept this VM locked", done.State, done.Error)
	}
	// One addition cannot be closed twice.
	if _, err = d.Plan(context.Background(), 1000, disposeRequest(job, "accept")); err == nil {
		t.Fatal("a second disposition was accepted")
	}
}

// Deleting is offered only for a volume no definition names.
func TestDiskAddDispositionDeletesOnlyAnUnreferencedVolume(t *testing.T) {
	h, _, d, disposal, job, _ := stuckAddition(t, false)
	plan, err := d.Plan(context.Background(), 1000, disposeRequest(job, "delete"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Acknowledgements, " "), "data-loss-delete-disks") || plan.Review["diskDeletion"] != true {
		t.Fatal(plan.Acknowledgements, plan.Review)
	}
	if closed := applyAdd(t, h, plan, "delete"); closed.State != "succeeded" || disposal.deletes != 1 {
		t.Fatal(closed.State, closed.Error, disposal.deletes)
	}
	if disposal.state != "absent" {
		t.Fatal("the unused volume is still present")
	}
}

func TestDiskAddDispositionRefusesMismatchedChoices(t *testing.T) {
	// A disk the definition names cannot be deleted here.
	_, _, d, _, job, _ := stuckAddition(t, true)
	if _, err := d.Plan(context.Background(), 1000, disposeRequest(job, "delete")); err == nil || !strings.Contains(err.Error(), "cannot be deleted here") {
		t.Fatal(err)
	}
	// There is nothing to accept when no definition names the disk.
	_, _, unreferenced, disposal, orphan, _ := stuckAddition(t, false)
	if _, err := unreferenced.Plan(context.Background(), 1000, disposeRequest(orphan, "accept")); err == nil || !strings.Contains(err.Error(), "nothing to accept") {
		t.Fatal(err)
	}
	// An already absent volume has nothing to delete either.
	disposal.mu.Lock()
	disposal.state = "absent"
	disposal.mu.Unlock()
	if _, err := unreferenced.Plan(context.Background(), 1000, disposeRequest(orphan, "delete")); err == nil || !strings.Contains(err.Error(), "already absent") {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{{}, {"disposition": "keep"}, {"disposition": "accept", "extra": true}} {
		request := disposeRequest(orphan, "accept")
		request.Input = bad
		if _, err := unreferenced.Plan(context.Background(), 1000, request); err == nil {
			t.Fatal("invalid disposition accepted", bad)
		}
	}
}

// A resolved addition needs no disposition.
func TestDiskAddDispositionRefusesASettledOperation(t *testing.T) {
	h, _, _, request := addFixture(t)
	disposal := &disposeFixtureBackend{referenced: true, state: "present"}
	d := &diskAddDisposeHandler{s: h.s, backend: disposal}
	plan, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	done := applyAdd(t, h, plan, "settled")
	if done.State != "succeeded" {
		t.Fatal(done.State, done.Error)
	}
	if _, err = d.Plan(context.Background(), 1000, disposeRequest(done, "accept")); err == nil {
		t.Fatal("a settled addition was offered a disposition")
	}
}
