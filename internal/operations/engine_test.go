package operations

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/store"
)

type effect struct {
	path  string
	calls atomic.Int32
	crash bool
}

func (h *effect) Validate(context.Context, domain.Plan, []byte) error { return nil }
func (h *effect) Execute(context.Context, domain.Plan, []byte, domain.Step) error {
	h.calls.Add(1)
	f, e := os.OpenFile(h.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.WriteString("effect"); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	f.Close()
	if h.crash {
		os.Exit(83)
	}
	return nil
}
func (h *effect) Reconcile(context.Context, domain.Plan, []byte, domain.Step) (bool, error) {
	b, e := os.ReadFile(h.path)
	return string(b) == "effect", e
}
func makePlan(t *testing.T, e *Engine) domain.Plan {
	t.Helper()
	p, err := e.Plan(context.Background(), 1000, "test-connection", "fixture.effect", []string{"test-resource"}, nil, map[string]any{"intent": "local test file"}, []domain.Step{{ID: "create", Action: "create fixture", Preconditions: []string{}, Idempotency: "reconcile-before-retry", Compensation: "preserve", Reconciliation: "inspect", CompletionPredicate: "file present"}}, []string{"fixture-effect"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func openEngine(t *testing.T, dir string) (*Engine, *effect) {
	t.Helper()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	e := New(s)
	h := &effect{path: filepath.Join(dir, "effect")}
	e.Handlers["fixture.effect"] = h
	t.Cleanup(func() { e.Close(); s.Close() })
	return e, h
}
func waitJob(t *testing.T, e *Engine, id string) domain.Job {
	t.Helper()
	for i := 0; i < 1000; i++ {
		j, err := e.Store.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(j.State) {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return domain.Job{}
}
func TestIdempotencyActorExpiryAndSubstitution(t *testing.T) {
	e, h := openEngine(t, t.TempDir())
	p := makePlan(t, e)
	r := ApplyRequest{p.ID, p.Digest, "repeat-key", []string{"fixture-effect"}}
	for _, uid := range []uint32{0, 1001} {
		if _, err := e.Apply(context.Background(), uid, r); err == nil {
			t.Fatal("wrong actor accepted")
		}
	}
	bad := r
	bad.PlanDigest = "wrong"
	if _, err := e.Apply(context.Background(), 1000, bad); err == nil {
		t.Fatal("substitution accepted")
	}
	bad = r
	bad.Acknowledgements = nil
	if _, err := e.Apply(context.Background(), 1000, bad); err == nil {
		t.Fatal("missing approval accepted")
	}
	j, err := e.Apply(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, e, j.ID)
	if done.State != "succeeded" {
		t.Fatalf("%+v", done)
	}
	j2, err := e.Apply(context.Background(), 1000, r)
	if err != nil || j2.ID != j.ID || h.calls.Load() != 1 {
		t.Fatal("duplicate effect", err)
	}
	bad = r
	bad.PlanID = "changed"
	if _, err := e.Apply(context.Background(), 1000, bad); err == nil {
		t.Fatal("changed request reused key")
	}
	events, err := e.Store.Events(j.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range events {
		if event.Seq != int64(i+1) {
			t.Fatal("out of order events")
		}
	}
	p = makePlan(t, e)
	p.ExpiresAt = time.Now().Add(-time.Minute)
	p.Digest, _ = PlanDigest(p)
	b, _ := json.Marshal(p)
	e.Store.DB.Exec("UPDATE plans SET body=? WHERE id=?", b, p.ID)
	if _, err = e.Apply(context.Background(), 1000, ApplyRequest{p.ID, p.Digest, "expired", []string{"fixture-effect"}}); err == nil {
		t.Fatal("expired plan accepted")
	}
}
func TestCrashAfterEffectBeforeJournal(t *testing.T) {
	if dir := os.Getenv("VIRMILL_TEST_CRASH_DIR"); dir != "" {
		e, h := openEngine(t, dir)
		h.crash = true
		p := makePlan(t, e)
		if _, err := e.Apply(context.Background(), 1000, ApplyRequest{p.ID, p.Digest, "crash", []string{"fixture-effect"}}); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashAfterEffectBeforeJournal$")
	cmd.Env = append(os.Environ(), "VIRMILL_TEST_CRASH_DIR="+dir)
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 83 {
		t.Fatal("unexpected crash fixture result", err)
	}
	e, h := openEngine(t, dir)
	if err = e.Recover(); err != nil {
		t.Fatal(err)
	}
	jobs, err := e.Store.Jobs()
	if err != nil || len(jobs) != 1 {
		t.Fatal(err, jobs)
	}
	if jobs[0].State != "recovery-required" {
		t.Fatal("uncertainty hidden")
	}
	j, err := e.Reconcile(context.Background(), jobs[0].ID)
	if err != nil || j.State != "succeeded" || h.calls.Load() != 0 {
		t.Fatal("replayed instead of observed", j, err)
	}
}
func TestCanonicalRFC8785Vectors(t *testing.T) {
	raw := json.RawMessage(`{"numbers":[333333333.33333329,1E30,4.50,2e-3,0.000000000000000000000000001],"string":"€$\u000f\nA'B\"\\\"/","literals":[null,true,false]}`)
	got, err := Canonical(raw)
	expected := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\"/"}`
	if err != nil || string(got) != expected {
		t.Fatalf("%s %v", got, err)
	}
}

func TestRecoveryIncludesJobsOlderThanRecentList(t *testing.T) {
	e, _ := openEngine(t, t.TempDir())
	p := makePlan(t, e)
	j, err := e.Store.Accept(p, "old-pending", "old-request")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := e.Store.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		body, _ := json.Marshal(domain.Job{ID: domain.ID(), PlanID: p.ID, State: "succeeded"})
		var value domain.Job
		json.Unmarshal(body, &value)
		if _, err = tx.Exec("INSERT INTO jobs(id,plan_id,body) VALUES(?,?,?)", value.ID, p.ID, body); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = e.Recover(); err != nil {
		t.Fatal(err)
	}
	got, err := e.Store.Job(j.ID)
	if err != nil || got.State != "recovery-required" {
		t.Fatal("old pending operation omitted from recovery", got, err)
	}
}
