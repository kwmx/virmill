//go:build linux && amd64

package localbackup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func receiptFixture() domain.BackupReceipt {
	return domain.BackupReceipt{APIVersion: domain.APIVersion, Kind: "BackupReceipt", OperationID: "11111111-2222-4333-8444-555555555555", Connection: "qemu:///system", Repository: "/media/absent-repository", CaptureID: captureID, SnapshotID: resticID, ManifestSHA256: strings.Repeat("b", 64), VerifiedAt: time.Date(2026, 9, 11, 14, 0, 0, 0, time.UTC)}
}

func receiptFile(t *testing.T, raw []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup-receipt.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBackupReceiptProjectionAndRecentHistoryAreReadOnly(t *testing.T) {
	h := newHarness(t)
	uid := uint32(os.Geteuid())
	ctx := context.Background()
	job := h.apply(t, h.plan(t, "create"))
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	check := h.apply(t, h.plan(t, "repository-check"))
	if check.State != "succeeded" {
		t.Fatal(check)
	}
	beforeJobs, _ := h.db.Jobs()
	beforeCalls := []int{h.tool.count("identity"), h.tool.count("backup"), h.tool.count("restore"), h.tool.count("check"), h.tool.count("observe")}
	value, err := h.s.Receipt(ctx, uid, app.Request{Connection: "qemu:///system", ID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	receipt, ok := value.(domain.BackupReceipt)
	if !ok || receipt.OperationID != job.ID || receipt.CaptureID != captureID || receipt.Repository != h.repo.Path || receipt.ManifestSHA256 != h.receipt.ManifestSHA256 || receipt.SnapshotID != resticID {
		t.Fatal("projection lost exact selectors", value)
	}
	raw, _ := json.Marshal(receipt)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "generated-only-credential") || strings.Contains(string(raw), "planDigest") {
		t.Fatal("receipt leaked credentials or internal authority", string(raw))
	}
	list, err := h.s.Receipts(ctx, uid, app.Request{Connection: "qemu:///system"})
	if err != nil || !reflect.DeepEqual(list, []domain.BackupReceipt{receipt}) {
		t.Fatal("recent history includes non-backup operation", list, err)
	}
	other, err := h.s.Receipts(ctx, uid, app.Request{Connection: "qemu:///session"})
	if err != nil || len(other.([]domain.BackupReceipt)) != 0 {
		t.Fatal("different connection leaked receipts", other, err)
	}
	if _, err := h.s.Receipt(ctx, uid, app.Request{Connection: "qemu:///system", ID: check.ID}); err == nil {
		t.Fatal("repository check exported recovery selectors")
	}
	afterJobs, _ := h.db.Jobs()
	afterCalls := []int{h.tool.count("identity"), h.tool.count("backup"), h.tool.count("restore"), h.tool.count("check"), h.tool.count("observe")}
	if !reflect.DeepEqual(beforeJobs, afterJobs) || !reflect.DeepEqual(beforeCalls, afterCalls) {
		t.Fatal("receipt reads invoked repository work or changed jobs")
	}
}

func TestBackupReceiptActorConnectionAndProofCannotBeBypassed(t *testing.T) {
	h := newHarness(t)
	uid := uint32(os.Geteuid())
	ctx := context.Background()
	job := h.apply(t, h.plan(t, "create"))
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	for _, r := range []app.Request{{Connection: "qemu:///session", ID: job.ID}, {Connection: "qemu:///system", ID: job.ID, Path: "/tmp/input"}, {Connection: "qemu:///system", ID: job.ID, Action: "restore"}, {Connection: "qemu:///system", ID: job.ID, Input: map[string]any{"passwordFile": "/private/key"}}} {
		if _, err := h.s.Receipt(ctx, uid, r); err == nil {
			t.Fatal("invalid export accepted", r)
		}
	}
	for _, actor := range []uint32{0, uid + 1} {
		if _, err := h.s.Receipt(ctx, actor, app.Request{Connection: "qemu:///system", ID: job.ID}); err == nil {
			t.Fatal("foreign actor exported receipt")
		}
		if _, err := h.s.Receipts(ctx, actor, app.Request{Connection: "qemu:///system"}); err == nil {
			t.Fatal("foreign actor listed receipts")
		}
	}
	var state progress
	if err := h.db.Get(records, job.ID, &state); err != nil {
		t.Fatal(err)
	}
	state.Proof.ManifestSHA256 = strings.Repeat("d", 64)
	if err := h.db.Put(records, job.ID, state); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Receipt(ctx, uid, app.Request{Connection: "qemu:///system", ID: job.ID}); err == nil {
		t.Fatal("tampered proof exported receipt")
	}
	if _, err := h.s.Receipts(ctx, uid, app.Request{Connection: "qemu:///system"}); err == nil {
		t.Fatal("listing skipped exact proof validation")
	}
}

func TestBackupReceiptIncompleteJobsAreNotExportable(t *testing.T) {
	h := newHarness(t)
	uid := uint32(os.Geteuid())
	ctx := context.Background()
	job := h.apply(t, h.plan(t, "create"))
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	for _, state := range []string{"canceled", "failed", "recovery-required", "running"} {
		job.State = state
		if err := h.db.Update(job, "fixture-only terminal scope"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.s.Receipt(ctx, uid, app.Request{Connection: "qemu:///system", ID: job.ID}); err == nil {
			t.Fatal("incomplete job exported receipt", state)
		}
		list, err := h.s.Receipts(ctx, uid, app.Request{Connection: "qemu:///system"})
		if err != nil || len(list.([]domain.BackupReceipt)) != 0 {
			t.Fatal("incomplete job listed", state, list, err)
		}
	}
}

func TestBackupReceiptReadWithoutDatabaseOrRepository(t *testing.T) {
	receipt := receiptFixture()
	raw, _ := json.Marshal(receipt)
	path := receiptFile(t, raw)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	value, err := s.ReadReceipt(context.Background(), uint32(os.Geteuid()), app.Request{Connection: "qemu:///session", Path: path})
	if err != nil || !reflect.DeepEqual(value, receipt) {
		t.Fatal("independent receipt import failed", value, err)
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reading modified original receipt")
	}
}

func TestBackupReceiptReadRejectsMalformedDocuments(t *testing.T) {
	receipt := receiptFixture()
	raw, _ := json.Marshal(receipt)
	samples := [][]byte{nil, []byte("{"), []byte(strings.Repeat(" ", backupReceiptLimit+1)), append(append([]byte{}, raw...), raw...), []byte(`{"result":` + string(raw) + `}`), []byte(strings.Replace(string(raw), `"kind":"BackupReceipt"`, `"kind":"BackupReceipt","kind":"BackupReceipt"`, 1)), []byte(strings.Replace(string(raw), `"kind":"BackupReceipt"`, `"kind":"BackupReceipt","passwordFile":"/private/key"`, 1))}
	for _, change := range []func(*domain.BackupReceipt){
		func(r *domain.BackupReceipt) { r.APIVersion = "virmill/v2" }, func(r *domain.BackupReceipt) { r.Kind = "Proof" }, func(r *domain.BackupReceipt) { r.OperationID = "00000000-0000-0000-0000-000000000000" }, func(r *domain.BackupReceipt) { r.CaptureID = "bad" }, func(r *domain.BackupReceipt) { r.SnapshotID = strings.Repeat("0", 64) }, func(r *domain.BackupReceipt) { r.ManifestSHA256 = strings.Repeat("A", 64) }, func(r *domain.BackupReceipt) { r.Repository = "/tmp/../repo" }, func(r *domain.BackupReceipt) { r.Connection = "qemu+ssh://remote/system" }, func(r *domain.BackupReceipt) { r.VerifiedAt = time.Time{} },
	} {
		r := receipt
		change(&r)
		value, _ := json.Marshal(r)
		samples = append(samples, value)
	}
	s := &Service{}
	for i, sample := range samples {
		path := receiptFile(t, sample)
		if _, err := s.ReadReceipt(context.Background(), uint32(os.Geteuid()), app.Request{Connection: "qemu:///system", Path: path}); err == nil {
			t.Fatalf("malformed receipt %d accepted", i)
		}
	}
}

func TestBackupReceiptReadRejectsSymlinksHardlinksAndConflictingWriter(t *testing.T) {
	raw, _ := json.Marshal(receiptFixture())
	path := receiptFile(t, raw)
	s := &Service{}
	uid := uint32(os.Geteuid())
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadReceipt(context.Background(), uid, app.Request{Connection: "qemu:///system", Path: link}); err == nil {
		t.Fatal("symlink receipt accepted")
	}
	writer, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err = unix.FcntlFlock(writer.Fd(), unix.F_OFD_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadReceipt(context.Background(), uid, app.Request{Connection: "qemu:///system", Path: path}); err == nil {
		t.Fatal("concurrent guarded writer accepted")
	}
	writer.Close()
	hard := filepath.Join(t.TempDir(), "hard.json")
	if err = os.Link(path, hard); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadReceipt(context.Background(), uid, app.Request{Connection: "qemu:///system", Path: path}); err == nil {
		t.Fatal("multiply linked receipt accepted")
	}
}

func TestBackupReceiptReadRequestAndCancellation(t *testing.T) {
	raw, _ := json.Marshal(receiptFixture())
	path := receiptFile(t, raw)
	s := &Service{}
	uid := uint32(os.Geteuid())
	for _, r := range []app.Request{{Connection: "qemu:///system", Path: "relative"}, {Connection: "qemu+ssh://other/system", Path: path}, {Connection: "qemu:///system", Path: path, ID: captureID}, {Connection: "qemu:///system", Path: path, After: 1}, {Connection: "qemu:///system", Path: path, Action: "restore"}} {
		if _, err := s.ReadReceipt(context.Background(), uid, r); err == nil {
			t.Fatal("invalid read request accepted", r)
		}
	}
	if _, err := s.ReadReceipt(context.Background(), uid+1, app.Request{Connection: "qemu:///system", Path: path}); err == nil {
		t.Fatal("foreign actor imported receipt")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, call := range []func(context.Context, uint32, app.Request) (any, error){s.ReadReceipt, s.Receipt, s.Receipts} {
		if _, err := call(ctx, uid, app.Request{Connection: "qemu:///system", Path: path}); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled read proceeded", err)
		}
	}
}

func TestBackupReceiptRecentLimitAndForeignPlanFiltering(t *testing.T) {
	h := newHarness(t)
	uid := uint32(os.Geteuid())
	ctx := context.Background()
	job := h.apply(t, h.plan(t, "create"))
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	original, input, err := h.db.Plan(job.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	// Unrelated successful plans intentionally have no backup proof. Listing must
	// filter their scope before attempting backup-specific proof decoding.
	for i := 0; i < 3; i++ {
		plan := original
		plan.ID = domain.ID()
		switch i {
		case 0:
			plan.ActorUID = uid + 1
		case 1:
			plan.ConnectionID = "qemu:///session"
		case 2:
			plan.Operation = "vm.start"
		}
		if err = h.db.SavePlan(plan, input); err != nil {
			t.Fatal(err)
		}
		unrelated := domain.Job{ID: domain.ID(), PlanID: plan.ID, State: "succeeded", CreatedAt: time.Now().UTC()}
		raw, _ := json.Marshal(unrelated)
		if _, err = h.db.DB.Exec("INSERT INTO jobs(id,plan_id,body) VALUES(?,?,?)", unrelated.ID, plan.ID, raw); err != nil {
			t.Fatal(err)
		}
	}
	value, err := h.s.Receipts(ctx, uid, app.Request{Connection: "qemu:///system"})
	if err != nil || len(value.([]domain.BackupReceipt)) != 1 {
		t.Fatal("unrelated history decoded as backup", value, err)
	}
	tx, err := h.db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		queued := domain.Job{ID: domain.ID(), PlanID: original.ID, State: "queued", CreatedAt: time.Now().UTC()}
		raw, _ := json.Marshal(queued)
		if _, err = tx.Exec("INSERT INTO jobs(id,plan_id,body) VALUES(?,?,?)", queued.ID, original.ID, raw); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	value, err = h.s.Receipts(ctx, uid, app.Request{Connection: "qemu:///system"})
	if err != nil || len(value.([]domain.BackupReceipt)) != 0 {
		t.Fatal("recent history exceeded1000-job bound", value, err)
	}
	if _, err = h.s.Receipt(ctx, uid, app.Request{Connection: "qemu:///system", ID: job.ID}); err != nil {
		t.Fatal("exact receipt became unavailable outside recent history", err)
	}
}
