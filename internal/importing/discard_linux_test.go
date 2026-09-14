//go:build linux && amd64

package importing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

type discardFixture struct {
	app                     *app.Service
	db                      *store.Store
	discard                 *DiscardService
	parent, artifact, stage string
	sibling                 string
	preparation             domain.Job
}

// newDiscardFixture journals one successful disk-set preparation whose prepared
// and work folders exist beside an unrelated sibling folder.
func newDiscardFixture(t *testing.T) *discardFixture {
	t.Helper()
	s, db := sourcesFixture(t)
	f := &discardFixture{app: s, db: db, discard: &DiscardService{Store: db, Engine: s.Engine}}
	s.Engine.Handlers["import.discard"] = f.discard
	f.parent = filepath.Join(t.TempDir(), "imports")
	if err := os.Mkdir(f.parent, 0700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(f.parent, "prepared-vm")
	input, err := operations.Canonical(map[string]any{"destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := operations.Digest(json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	p := domain.Plan{APIVersion: domain.APIVersion, ID: domain.ID(), ActorUID: 1000, ConnectionID: "local", Operation: "import.prepare-disks", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), InputDigest: digest, ResourceIDs: []string{}, Before: map[string]string{}, Steps: []domain.Step{}, Acknowledgements: []string{}, Risks: []string{}}
	if p.Digest, err = operations.PlanDigest(p); err != nil {
		t.Fatal(err)
	}
	if err = db.SavePlan(p, input); err != nil {
		t.Fatal(err)
	}
	if f.preparation, err = db.Accept(p, domain.ID(), p.Digest); err != nil {
		t.Fatal(err)
	}
	f.preparation.State = "succeeded"
	if err = db.Update(f.preparation, "Synthetic journal fixture; no image conversion."); err != nil {
		t.Fatal(err)
	}
	if err = db.Put("import-artifact", p.ID, Artifact{APIVersion: domain.APIVersion, Kind: "PreparedDiskSet", PlanID: p.ID, OperationID: f.preparation.ID, InputDigest: digest, Disks: []PreparedDisk{}}); err != nil {
		t.Fatal(err)
	}
	f.artifact, f.stage, f.sibling = destination, filepath.Join(f.parent, diskSetStage(p)), filepath.Join(f.parent, "other-import")
	for _, dir := range []string{f.artifact, filepath.Join(f.artifact, "disks"), f.stage, filepath.Join(f.stage, "source"), f.sibling} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(f.artifact, "disks", "disk-001.qcow2"), filepath.Join(f.stage, "source", "member.vmdk"), filepath.Join(f.sibling, "keep.qcow2")} {
		if err = os.WriteFile(file, []byte(strings.Repeat("fixture\n", 1024)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *discardFixture) plan(t *testing.T, input map[string]any) (domain.Plan, error) {
	t.Helper()
	return f.discard.Plan(context.Background(), 1000, app.Request{ID: f.preparation.ID, Action: "discard", Input: input})
}

func (f *discardFixture) apply(t *testing.T, p domain.Plan, key string) domain.Job {
	t.Helper()
	j, err := f.app.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: key, Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	for range 200 {
		if j, err = f.db.Job(j.ID); err != nil || domain.Terminal(j.State) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return j
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func stepIDs(p domain.Plan) []string {
	out := []string{}
	for _, s := range p.Steps {
		out = append(out, s.ID)
	}
	return out
}

func listedSources(t *testing.T, f *discardFixture) int {
	t.Helper()
	rows, err := f.app.Extensions["import.sources"](context.Background(), 1000, app.Request{})
	if err != nil {
		t.Fatal(err)
	}
	return len(rows.([]map[string]any))
}

func TestDiscardRemovesWorkAndPreparedFoldersOnly(t *testing.T) {
	f := newDiscardFixture(t)
	if listedSources(t, f) != 1 {
		t.Fatal("fixture preparation not offered")
	}
	p, err := f.plan(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Operation != "import.discard" || !slices.Equal(stepIDs(p), []string{"stage", "artifact"}) || !slices.Equal(p.Acknowledgements, []string{"delete-prepared-copy"}) || p.Review["removesImages"] != true || p.Review["sourceFilesChanged"] != false || !strings.Contains(p.Estimates.Notes, "Frees about") {
		t.Fatal("discard plan", p)
	}
	if j := f.apply(t, p, "discard-all"); j.State != "succeeded" {
		t.Fatal(j)
	}
	if exists(f.artifact) || exists(f.stage) || !exists(filepath.Join(f.sibling, "keep.qcow2")) || !exists(f.parent) {
		t.Fatal("wrong folders removed or kept")
	}
	if listedSources(t, f) != 0 {
		t.Fatal("removed preparation still offered to Create VM")
	}
	var e *domain.Error
	if _, err = f.plan(t, nil); !errors.As(err, &e) || e.Code != "INVALID_STATE" {
		t.Fatal("second removal planned", err)
	}
}

func TestDiscardCanKeepPreparedImages(t *testing.T) {
	f := newDiscardFixture(t)
	p, err := f.plan(t, map[string]any{"keepImages": true})
	if err != nil || !slices.Equal(stepIDs(p), []string{"stage"}) || p.Review["removesImages"] != false {
		t.Fatal("keep-images plan", p, err)
	}
	if j := f.apply(t, p, "discard-stage"); j.State != "succeeded" || exists(f.stage) || !exists(filepath.Join(f.artifact, "disks", "disk-001.qcow2")) || listedSources(t, f) != 1 {
		t.Fatal("work folder removal changed the prepared images", j)
	}
	if _, err = f.plan(t, map[string]any{"keepImages": true}); err == nil {
		t.Fatal("nothing left to remove was planned")
	}
}

func TestDiscardRefusesBusyReplacedLinkedOrForeignPreparations(t *testing.T) {
	f := newDiscardFixture(t)
	// A running job whose plan names the preparation keeps it in use.
	input, _ := operations.Canonical(map[string]any{"sourceOperationID": f.preparation.ID})
	digest, _ := operations.Digest(json.RawMessage(input))
	user := domain.Plan{APIVersion: domain.APIVersion, ID: domain.ID(), ActorUID: 1000, ConnectionID: "qemu:///session", Operation: "vm.create.devices-v1", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), InputDigest: digest, ResourceIDs: []string{}, Before: map[string]string{}, Steps: []domain.Step{}, Acknowledgements: []string{}, Risks: []string{}}
	user.Digest, _ = operations.PlanDigest(user)
	if err := f.db.SavePlan(user, input); err != nil {
		t.Fatal(err)
	}
	job, err := f.db.Accept(user, domain.ID(), user.Digest)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"running", "recovery-required"} {
		job.State = state
		if err = f.db.Update(job, "Synthetic creation using the preparation."); err != nil {
			t.Fatal(err)
		}
		var e *domain.Error
		if _, err = f.plan(t, nil); !errors.As(err, &e) || e.Code != "RESOURCE_BUSY" {
			t.Fatal(state, "creation did not keep the preparation in use", err)
		}
	}
	job.State = "succeeded"
	if err = f.db.Update(job, "Synthetic creation finished."); err != nil {
		t.Fatal(err)
	}

	// A folder replaced after review is never removed.
	p, err := f.plan(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(f.artifact); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(f.artifact, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.artifact, "new.qcow2"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	// Apply rechecks before accepting, so no job is created at all.
	var stale *domain.Error
	_, err = f.app.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "discard-replaced", Acknowledgements: p.Acknowledgements})
	if !errors.As(err, &stale) || stale.Code != "STALE_PLAN" || !exists(filepath.Join(f.artifact, "new.qcow2")) || !exists(f.stage) {
		t.Fatal("replaced folder accepted for removal", err)
	}

	// A symlink in place of a reviewed folder is refused before planning.
	g := newDiscardFixture(t)
	outside := t.TempDir()
	if err = os.WriteFile(filepath.Join(outside, "keep"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(g.stage); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, g.stage); err != nil {
		t.Fatal(err)
	}
	if _, err = g.plan(t, nil); err == nil || !exists(filepath.Join(outside, "keep")) {
		t.Fatal("symlinked work folder accepted", err)
	}

	// Another user's or a failed preparation is refused.
	h := newDiscardFixture(t)
	if _, err = h.discard.Plan(context.Background(), 1001, app.Request{ID: h.preparation.ID, Action: "discard"}); err == nil {
		t.Fatal("another user's preparation accepted")
	}
	for _, r := range []app.Request{{ID: h.preparation.ID, Action: "remove"}, {ID: "not-an-id", Action: "discard"}, {ID: h.preparation.ID, Action: "discard", Input: map[string]any{"path": "/"}}} {
		if _, err = h.discard.Plan(context.Background(), 1000, r); err == nil {
			t.Fatal("invalid removal request planned", r)
		}
	}
}
