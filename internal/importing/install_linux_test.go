//go:build linux && amd64

package importing

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

// Synthetic phase output is deliberately not a qcow2 image; only journal semantics are tested.
type emptyPhaseTool struct {
	phaseTool
	calls   atomic.Int32
	fail    bool
	started chan struct{}
	after   func()
}

func (f *emptyPhaseTool) CreateEmpty(ctx context.Context, work string, size, bound int64) error {
	count := f.calls.Add(1)
	if err := os.WriteFile(filepath.Join(work, "disk.qcow2"), []byte("synthetic empty-disk worker output, not a qcow2 image"), 0600); err != nil {
		return err
	}
	if f.started != nil {
		close(f.started)
		<-ctx.Done()
		return ctx.Err()
	}
	if f.after != nil {
		f.after()
	}
	if f.fail && count == 2 {
		return errors.New("synthetic blank disk worker crash")
	}
	return nil
}
func installationFixture(t *testing.T, tool EmptyDiskTool) (*InstallationService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "journal.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &InstallationService{Service: &Service{Store: db, Engine: operations.New(db)}, EmptyTool: tool}
	s.Engine.Handlers["import.prepare-install"] = s
	t.Cleanup(func() { s.Engine.Close(); s.Store.Close() })
	return s, path
}
func syntheticMedia(t *testing.T) string {
	t.Helper()
	b := make([]byte, 40*2048)
	copy(b[16*2048:], []byte{1, 'C', 'D', '0', '0', '1', 1})
	path := filepath.Join(t.TempDir(), "fixture.iso")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func installRequest(source, dest string) app.Request {
	return app.Request{Path: source, Input: map[string]any{"offlineSources": true, "destination": dest, "mediaID": "installer", "disks": []blankDisk{{ID: "boot", VirtualBytes: 8 << 20}, {ID: "data", VirtualBytes: 16 << 20}}}}
}
func TestMediaRecognitionDoesNotInferBootability(t *testing.T) {
	path := syntheticMedia(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got, err := recognizeMedia(f, 40*2048); err != nil || got != "iso9660-volume-identifier" {
		t.Fatal(got, err)
	}
	for _, size := range []int64{0, 16 * 2048, 40*2048 + 1, 64<<30 + 2048} {
		if _, err := recognizeMedia(f, size); err == nil {
			t.Fatal("invalid extent accepted", size)
		}
	}
	for _, identifiers := range [][]string{{"BEA01", "NSR03", "TEA01"}, {"BEA01", "TEA01", "NSR02"}, {"NSR03", "TEA01"}} {
		b := make([]byte, 40*2048)
		for i, sig := range identifiers {
			copy(b[(16+i)*2048+1:], sig)
			b[(16+i)*2048+6] = 1
		}
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := recognizeMedia(f, int64(len(b)))
		if identifiers[1] == "NSR03" {
			if err != nil || got != "udf-volume-identifiers" {
				t.Fatal(got, err)
			}
		} else if err == nil {
			t.Fatal("out of order UDF sequence accepted")
		}
	}
}
func TestInstallationPlanRefusesUnsafeInputsAndSourceDrift(t *testing.T) {
	tool := &emptyPhaseTool{}
	s, _ := installationFixture(t, tool)
	source := syntheticMedia(t)
	for _, change := range []func(*app.Request){func(r *app.Request) { r.Input["offlineSources"] = false }, func(r *app.Request) { r.Input["unknown"] = true }, func(r *app.Request) { r.Input["sha256"] = strings.Repeat("f", 64) }, func(r *app.Request) { r.Input["disks"] = []blankDisk{{ID: "installer", VirtualBytes: 1 << 20}} }, func(r *app.Request) { r.Input["disks"] = []blankDisk{{ID: "boot", VirtualBytes: 1048577}} }, func(r *app.Request) { r.Input["mediaID"] = "../escape" }, func(r *app.Request) { r.Input["destination"] = "/tmp/../escape" }} {
		r := installRequest(source, filepath.Join(t.TempDir(), "prepared"))
		change(&r)
		if _, err := s.Plan(context.Background(), 1000, r); err == nil {
			t.Fatal("unsafe input accepted", r)
		}
	}
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, installRequest(source, dest))
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls.Load() != 0 {
		t.Fatal("plan created disks")
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("plan published files", err)
	}
	f, err := os.OpenFile(source, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte("changed"), 0)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("changed source applied")
	}
}

type installationLostAck struct{ *InstallationService }

func (s installationLostAck) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	if err := s.InstallationService.Execute(ctx, p, b, step); err != nil {
		return err
	}
	return errors.New("synthetic publication acknowledgement lost")
}
func TestInstallationPublicationRecoversWithoutReplayingOrOriginalMedia(t *testing.T) {
	tool := &emptyPhaseTool{}
	s, journal := installationFixture(t, tool)
	s.Engine.Handlers["import.prepare-install"] = installationLostAck{s}
	source := syntheticMedia(t)
	dest := filepath.Join(t.TempDir(), "prepared")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Plan(context.Background(), 1000, installRequest(source, dest))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s.Service, accepted(t, s.Service, p).ID)
	if j.State != "recovery-required" || tool.calls.Load() != 2 {
		t.Fatal(j, tool.calls.Load())
	}
	s.Engine.Close()
	if err = s.Store.Close(); err != nil {
		t.Fatal(err)
	}
	s.Store, err = store.Open(journal)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(s.Store)
	s.Engine.Handlers["import.prepare-install"] = s
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	j, err = s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || j.State != "succeeded" || tool.calls.Load() != 2 {
		t.Fatal(j, err, tool.calls.Load())
	}
	a, dir, err := Approved(context.Background(), s.Store, 1000, j.ID)
	if err != nil || dir != dest || a.Kind != "PreparedInstallation" || len(a.Media) != 1 || len(a.Disks) != 2 || a.VMDefined || a.GuestBootVerified {
		t.Fatal(a, dir, err)
	}
	copied, err := os.ReadFile(filepath.Join(dest, a.Media[0].Path))
	if err != nil || !bytes.Equal(copied, original) {
		t.Fatal("media copy differs", err)
	}
	for _, disk := range a.Disks {
		if disk.SourcePath != "" || len(disk.SourceChain) != 0 {
			t.Fatal("invented blank disk source", disk)
		}
	}
	if _, err = s.Result(context.Background(), 1001, j.ID); err == nil {
		t.Fatal("foreign actor accepted")
	}
	b, err := operations.Canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("prepared-installation", b); err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("prepared-disk-set", b); err == nil {
		t.Fatal("new receipt accepted by legacy schema")
	}
	path := filepath.Join(dest, a.Media[0].Path)
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err == nil {
		t.Fatal("tampered media accepted")
	}
}
func TestInstallationFailureAndCancellationNeverPublishPartialArtifacts(t *testing.T) {
	for _, mode := range []string{"failure", "cancel", "source-write"} {
		t.Run(mode, func(t *testing.T) {
			source := syntheticMedia(t)
			original, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			tool := &emptyPhaseTool{fail: mode == "failure"}
			if mode == "cancel" {
				tool.started = make(chan struct{})
			}
			if mode == "source-write" {
				tool.after = func() {
					if err := os.WriteFile(source, make([]byte, len(original)), 0600); err != nil {
						t.Error(err)
					}
				}
			}
			s, _ := installationFixture(t, tool)
			dest := filepath.Join(t.TempDir(), "prepared")
			p, err := s.Plan(context.Background(), 1000, installRequest(source, dest))
			if err != nil {
				t.Fatal(err)
			}
			j := accepted(t, s.Service, p)
			if mode == "cancel" {
				select {
				case <-tool.started:
				case <-time.After(5 * time.Second):
					t.Fatal("worker did not start")
				}
				if _, err = s.Engine.Cancel(j.ID); err != nil {
					t.Fatal(err)
				}
			}
			j = finish(t, s.Service, j.ID)
			want := "recovery-required"
			if mode == "cancel" {
				want = "canceled"
			}
			if j.State != want {
				t.Fatal(j)
			}
			if _, err = os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("partial published", err)
			}
			_, err = os.Stat(filepath.Join(filepath.Dir(dest), installStage(p)))
			if mode == "cancel" {
				if !os.IsNotExist(err) {
					t.Fatal("canceled stage remains", err)
				}
			} else if err != nil {
				t.Fatal("uncertain stage lost", err)
			}
			if mode != "source-write" {
				after, err := os.ReadFile(source)
				if err != nil || !bytes.Equal(after, original) {
					t.Fatal("original modified", err)
				}
			}
			if mode != "cancel" {
				if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
					t.Fatal("partial reconciled as complete")
				}
			}
		})
	}
}
func TestRealISOAndConfinedBlankDiskPreparation(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("enable actual generated ISO/QEMU fixtures explicitly")
	}
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "README.txt"), []byte("Virmill generated nonbootable ISO fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "fixture.iso")
	cmd := exec.Command("/usr/bin/genisoimage", "-quiet", "-V", "VIRMILL_TEST", "-o", source, sourceDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture creation: %v %s", err, out)
	}
	s, _ := installationFixture(t, image.Tool{})
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, installRequest(source, dest))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s.Service, accepted(t, s.Service, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	a, _, err := Approved(context.Background(), s.Store, 1000, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Media[0].Recognition != "iso9660-volume-identifier" || len(a.Disks) != 2 {
		t.Fatal(a)
	}
	for _, d := range a.Disks {
		if d.Verification != "virtual-size+qemu-check+zero-map" {
			t.Fatal(d)
		}
	}
	t.Logf("actual generated nonbootable ISO copied and SHA-256 checked, two confined QEMU empty disks checked/zero-mapped; media=%s; no VM defined or guest executed", a.Media[0].SHA256)
}

func TestInstallationExamplesMatchStrictSchemas(t *testing.T) {
	for _, test := range []struct{ path, schema string }{{"../../examples/import/installation.json", "installation-preparation-input"}, {"../../examples/creation/prepared-installation.json", "vm-creation-input"}} {
		b, err := os.ReadFile(test.path)
		if err != nil {
			t.Fatal(err)
		}
		if err = validation.Schema(test.schema, b); err != nil {
			t.Fatal(test.path, err)
		}
	}
}
