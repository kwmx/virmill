//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

type cleanupFixtureBackend struct {
	*fixtureBackend
	graph               string
	changed, referenced bool
	deleteCalls, failAt int
	lostAck             bool
	afterDelete         func(context.Context)
}

func (f *cleanupFixtureBackend) inspect(candidates []domain.CleanupCandidate, deleting bool) (domain.CreationCleanup, error) {
	out := domain.CreationCleanup{Volumes: []domain.CleanupVolume{}}
	if f.defined {
		return out, errors.New("synthetic VM exists")
	}
	if deleting && f.referenced {
		return out, domain.Fail("RESOURCE_BUSY", "synthetic dependency")
	}
	if deleting {
		out.GraphDigest = f.graph
		out.ResourceIDs = []string{"synthetic-graph-pool"}
	}
	for _, c := range candidates {
		state, reason := "absent", ""
		if _, ok := f.volumes[c.Intent.Name]; ok {
			state = "present"
			if c.Allocated == nil || c.Allocated.Generation == "" || f.changed {
				state = "unknown"
				reason = "synthetic uncertain generation"
			}
		}
		out.Volumes = append(out.Volumes, domain.CleanupVolume{Candidate: c, State: state, Reason: reason})
	}
	return out, nil
}
func (f *cleanupFixtureBackend) InspectCreationCleanup(ctx context.Context, uri string, s domain.CreationSpec, c []domain.CleanupCandidate, deleting bool) (domain.CreationCleanup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inspect(c, deleting)
}
func (f *cleanupFixtureBackend) DeleteCreationVolume(ctx context.Context, uri string, s domain.CreationSpec, c []domain.CleanupCandidate, reviewed domain.CreationCleanup, index int) error {
	f.mu.Lock()
	current, err := f.inspect(c, true)
	if err != nil || !same(current, reviewed) {
		f.mu.Unlock()
		return errors.New("synthetic stale graph")
	}
	f.deleteCalls++
	fail := f.deleteCalls == f.failAt
	if fail && !f.lostAck {
		f.mu.Unlock()
		return errors.New("synthetic delete failed before effect")
	}
	delete(f.volumes, c[index].Intent.Name)
	callback := f.afterDelete
	f.mu.Unlock()
	if callback != nil {
		callback(ctx)
	}
	if fail {
		return errors.New("synthetic acknowledgement lost after deletion")
	}
	return nil
}
func cleanupFixture(t *testing.T, failure string) (*cleanupHandler, *cleanupFixtureBackend, domain.Plan, domain.Job, string) {
	t.Helper()
	s, backend, request, dir := creationFixture(t)
	backend.fail = failure
	original, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	parent := awaitCreation(t, s, applyCreation(t, s, original).ID)
	if parent.State != "recovery-required" {
		t.Fatal(parent)
	}
	b := &cleanupFixtureBackend{fixtureBackend: backend, graph: "synthetic-graph-v1"}
	h := &cleanupHandler{s: s, backend: b}
	s.Engine.Handlers["vm.create.cleanup"] = h
	return h, b, original, parent, dir
}
func planCleanup(t *testing.T, h *cleanupHandler, parent domain.Job, disposition string) domain.Plan {
	t.Helper()
	p, err := h.Plan(context.Background(), 1000, app.Request{Connection: "fixture", ID: parent.ID, Input: map[string]any{"disposition": disposition}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func checkCleanupLocks(t *testing.T, s *Service, p domain.Plan, owner string) {
	t.Helper()
	for _, resource := range p.ResourceIDs {
		jobs, err := s.Store.ResourceJobs(resource)
		if err != nil {
			t.Fatal(err)
		}
		if owner == "" && len(jobs) != 0 {
			t.Fatal("locks retained after confirmed disposition", jobs)
		}
		if owner != "" && (len(jobs) != 1 || jobs[0] != owner) {
			t.Fatal("inherited locks lost", jobs)
		}
	}
}
func TestCleanupDeletesOnlyReviewedNewCopiesAndClosesRecipe(t *testing.T) {
	h, b, original, parent, dir := cleanupFixture(t, "populate")
	before, err := os.ReadFile(filepath.Join(dir, "boot"))
	if err != nil {
		t.Fatal(err)
	}
	p := planCleanup(t, h, parent, "delete")
	if b.deleteCalls != 0 {
		t.Fatal("preview deleted")
	}
	child := awaitCreation(t, h.s, applyCreation(t, h.s, p).ID)
	if child.State != "succeeded" || child.RecoveryOf != parent.ID {
		t.Fatal(child)
	}
	b.mu.Lock()
	calls, left := b.deleteCalls, len(b.volumes)
	b.mu.Unlock()
	if calls != 2 || left != 0 {
		t.Fatal(calls, left)
	}
	checkCleanupLocks(t, h.s, p, "")
	after, err := os.ReadFile(filepath.Join(dir, "boot"))
	if err != nil || string(before) != string(after) {
		t.Fatal("original changed", err)
	}
	parent, err = h.s.Store.Job(parent.ID)
	if err != nil || parent.State != "partial" || parent.RecoveryOperationID != child.ID {
		t.Fatal("original creation relabeled", parent, err)
	}
	if _, err = h.s.Result(context.Background(), 1000, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = h.s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: parent.ID}); err == nil {
		t.Fatal("closed recipe resumed")
	}
	_, encoded, err := h.s.Store.Plan(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.s.Reconcile(context.Background(), original, encoded, original.Steps[0]); err == nil {
		t.Fatal("disposed recipe reconciled as VM creation")
	}
	t.Log("synthetic volume deletion adapter and generated source files; no native host deletion or hardware qualification")
}
func TestCleanupRetainsUncertainGenerationAndPreservesPartialAllocationIdentity(t *testing.T) {
	h, b, original, parent, _ := cleanupFixture(t, "allocation-identity")
	receipt, err := h.s.load(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Volumes[0].Allocated == nil || receipt.Volumes[0].Allocated.BackendKey == "" || receipt.Volumes[0].Allocated.Generation != "" || receipt.Volumes[0].Verified {
		t.Fatal("partial returned identity was lost or falsely verified", receipt)
	}
	if _, err = h.Plan(context.Background(), 1000, app.Request{Connection: "fixture", ID: parent.ID, Input: map[string]any{"disposition": "delete"}}); err == nil {
		t.Fatal("unknown generation was deletable")
	}
	p := planCleanup(t, h, parent, "retain")
	child := awaitCreation(t, h.s, applyCreation(t, h.s, p).ID)
	if child.State != "succeeded" {
		t.Fatal(child)
	}
	b.mu.Lock()
	calls, left := b.deleteCalls, len(b.volumes)
	b.mu.Unlock()
	if calls != 0 || left != 1 {
		t.Fatal("retention changed resources", calls, left)
	}
	var pin creationDisposition
	raw, err := h.s.Store.MetadataBytes("vm-creation-disposition", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = wire.Decode(raw, &pin); err != nil || pin.Disposition != "retain" || len(pin.Observation.Volumes) != 2 || pin.Observation.Volumes[0].State != "unknown" {
		t.Fatal("uncertain candidate was not durably pinned", pin, err)
	}
	checkCleanupLocks(t, h.s, p, "")
}
func TestCleanupPartialDeletionRetainsLocksAndNeedsFreshReview(t *testing.T) {
	h, b, _, parent, _ := cleanupFixture(t, "define")
	b.failAt = 2
	p := planCleanup(t, h, parent, "delete")
	child := awaitCreation(t, h.s, applyCreation(t, h.s, p).ID)
	if child.State != "recovery-required" {
		t.Fatal(child)
	}
	checkCleanupLocks(t, h.s, p, child.ID)
	if _, err := h.s.Engine.Reconcile(context.Background(), child.ID); err == nil {
		t.Fatal("remaining volume blindly reconciled")
	}
	b.mu.Lock()
	b.failAt = 0
	b.mu.Unlock()
	p2 := planCleanup(t, h, child, "delete")
	next := awaitCreation(t, h.s, applyCreation(t, h.s, p2).ID)
	if next.State != "succeeded" {
		t.Fatal(next)
	}
	b.mu.Lock()
	calls := b.deleteCalls
	b.mu.Unlock()
	if calls != 3 {
		t.Fatal("already deleted volume replayed", calls)
	}
	checkCleanupLocks(t, h.s, p2, "")
}
func TestCleanupLostFinalAcknowledgementReconcilesAfterJournalReopen(t *testing.T) {
	h, b, _, parent, _ := cleanupFixture(t, "define")
	b.failAt, b.lostAck = 2, true
	p := planCleanup(t, h, parent, "delete")
	child := awaitCreation(t, h.s, applyCreation(t, h.s, p).ID)
	if child.State != "recovery-required" {
		t.Fatal(child)
	}
	var path string
	if err := h.s.Store.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	h.s.Engine.Close()
	if err := h.s.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	h.s.Store = reopened
	h.s.Engine = operations.New(reopened)
	h.s.Engine.Handlers["vm.create.cleanup"] = h
	t.Cleanup(func() { h.s.Engine.Close(); reopened.Close() })
	if err = h.s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	got, err := h.s.Engine.Reconcile(context.Background(), child.ID)
	if err != nil || got.State != "succeeded" {
		t.Fatal(got, err)
	}
	b.mu.Lock()
	calls := b.deleteCalls
	b.mu.Unlock()
	if calls != 2 {
		t.Fatal("reconciliation replayed native effect", calls)
	}
	_, encoded, err := h.s.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var in cleanupInput
	if err = wire.Decode(encoded, &in); err != nil {
		t.Fatal(err)
	}
	proof, err := h.proof(p, in)
	if err != nil || !proof.Absent[0] || !proof.Absent[1] {
		t.Fatal("reconciled absence not durable", proof, err)
	}
	checkCleanupLocks(t, h.s, p, "")
}
func TestCleanupStaleIdentityGraphReferencesAndAuthorityRefuseBeforeDelete(t *testing.T) {
	for _, reason := range []string{"identity", "graph", "native-reference", "manifest-reference", "actor", "connection", "future-input"} {
		t.Run(reason, func(t *testing.T) {
			h, b, original, parent, _ := cleanupFixture(t, "define")
			p := planCleanup(t, h, parent, "delete")
			switch reason {
			case "identity":
				b.mu.Lock()
				b.changed = true
				b.mu.Unlock()
			case "graph":
				b.mu.Lock()
				b.graph = "changed"
				b.mu.Unlock()
			case "native-reference":
				b.mu.Lock()
				b.referenced = true
				b.mu.Unlock()
			case "manifest-reference":
				r, err := h.s.load(original.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err = h.s.Store.Put("snapshot-manifest", "protected", map[string]any{"source": r.Volumes[0].Allocated.Path}); err != nil {
					t.Fatal(err)
				}
			case "actor", "connection", "future-input":
				uid, connection := uint32(1000), "fixture"
				in := map[string]any{"disposition": "delete"}
				if reason == "actor" {
					uid = 1001
				}
				if reason == "connection" {
					connection = "other"
				}
				if reason == "future-input" {
					in["futureMeaning"] = true
				}
				if _, err := h.Plan(context.Background(), uid, app.Request{Connection: connection, ID: parent.ID, Input: in}); err == nil {
					t.Fatal("invalid authority/input accepted")
				}
				return
			}
			if _, err := h.s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements}); err == nil {
				t.Fatal("stale/unsafe cleanup accepted")
			}
			b.mu.Lock()
			calls := b.deleteCalls
			b.mu.Unlock()
			if calls != 0 {
				t.Fatal("refusal deleted a file")
			}
			checkCleanupLocks(t, h.s, original, parent.ID)
		})
	}
}
func TestCleanupCancellationRetainsRemainingVolumeAndLocks(t *testing.T) {
	h, b, _, parent, _ := cleanupFixture(t, "define")
	b.afterDelete = func(ctx context.Context) {
		if _, err := h.s.Engine.Cancel(operations.OperationID(ctx)); err != nil {
			t.Error(err)
		}
	}
	p := planCleanup(t, h, parent, "delete")
	child := awaitCreation(t, h.s, applyCreation(t, h.s, p).ID)
	if child.State != "recovery-required" {
		t.Fatal(child)
	}
	b.mu.Lock()
	calls, left := b.deleteCalls, len(b.volumes)
	b.mu.Unlock()
	if calls != 1 || left != 1 {
		t.Fatal("cancel failed to retain remaining volume", calls, left)
	}
	checkCleanupLocks(t, h.s, p, child.ID)
	if _, err := h.s.Engine.Reconcile(context.Background(), child.ID); err == nil {
		t.Fatal("canceled incomplete cleanup falsely completed")
	}
}
func TestCleanupVersionedRecordRefusesMissingPinsOrFutureMeaning(t *testing.T) {
	h, _, original, parent, _ := cleanupFixture(t, "define")
	p := planCleanup(t, h, parent, "retain")
	child := awaitCreation(t, h.s, applyCreation(t, h.s, p).ID)
	if child.State != "succeeded" {
		t.Fatal(child)
	}
	raw, err := h.s.Store.MetadataBytes("vm-creation-disposition", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, err := h.s.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"version", "pins", "future"} {
		var changed map[string]any
		if err = json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "version":
			changed["schemaVersion"] = 2
		case "pins":
			changed["observation"].(map[string]any)["volumes"] = []any{}
		case "future":
			changed["futureMeaning"] = true
		}
		if err = h.s.Store.Put("vm-creation-disposition", original.ID, changed); err != nil {
			t.Fatal(err)
		}
		if ok, err := h.Reconcile(context.Background(), p, encoded, p.Steps[0]); err == nil || ok {
			t.Fatal("incompatible disposition accepted", kind)
		}
	}
}

func TestCleanupProtectsReferencesOlderThanRecentJobs(t *testing.T) {
	h, _, original, parent, _ := cleanupFixture(t, "define")
	p := planCleanup(t, h, parent, "delete")
	receipt, err := h.s.load(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	other := domain.Plan{ID: domain.ID(), ActorUID: 1000, ConnectionID: "fixture"}
	encoded, err := json.Marshal(map[string]any{"volume": receipt.Volumes[0].Allocated.Path})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.s.Store.SavePlan(other, encoded); err != nil {
		t.Fatal(err)
	}
	tx, err := h.s.Store.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1002; i++ {
		state := "succeeded"
		if i == 0 {
			state = "recovery-required"
		}
		job := domain.Job{ID: domain.ID(), PlanID: other.ID, State: state}
		b, e := json.Marshal(job)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec("INSERT INTO jobs(id,plan_id,body) VALUES(?,?,?)", job.ID, job.PlanID, b); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = h.s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("old unresolved reference was hidden by recent-job pagination")
	}
	checkCleanupLocks(t, h.s, original, parent.ID)
}
