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
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

// This fake exercises journal phases only; its output is deliberately not an image.
type filesPhaseTool struct {
	phaseTool
	calls   atomic.Int32
	started chan struct{}
	after   func()
}

// MeasureFiles reports 1 MiB, well under the synthetic 8 MiB virtual size.
func (f *filesPhaseTool) MeasureFiles(context.Context, []platform.DiskSourceFile, string, string, string) (int64, error) {
	return 1 << 20, nil
}
func (f *filesPhaseTool) InspectFiles(ctx context.Context, sources []platform.DiskSourceFile, work, name, format string, bound int64) ([]image.Info, error) {
	return f.phaseTool.Inspect(ctx, "", work, name, format, bound, nil)
}
func (f *filesPhaseTool) ConvertFiles(ctx context.Context, sources []platform.DiskSourceFile, work, name, format string, size, bound int64) error {
	f.calls.Add(1)
	if f.started != nil {
		if err := os.WriteFile(filepath.Join(work, "disk.qcow2"), []byte("synthetic partial output"), 0600); err != nil {
			return err
		}
		close(f.started)
		<-ctx.Done()
		return ctx.Err()
	}
	err := f.phaseTool.Convert(ctx, "", work, name, format, size, bound)
	if f.after != nil {
		f.after()
	}
	return err
}
func diskServiceFixture(t *testing.T, tool FilesDiskTool) (*DiskSetService, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "journal.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &DiskSetService{Service: &Service{Store: db, Engine: operations.New(db)}, FilesTool: tool}
	s.Engine.Handlers["import.prepare-disks"] = s
	t.Cleanup(func() { s.Engine.Close(); s.Store.Close() })
	return s, path
}
func selectedDiskFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"boot.raw", "data.raw"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("synthetic source "+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func diskRequest(source, destination string) app.Request {
	return app.Request{Path: source, Input: map[string]any{
		"offlineSources": true, "destination": destination,
		"files": []any{map[string]any{"path": "boot.raw"}, map[string]any{"path": "data.raw"}},
		"disks": []diskSetMapping{{ID: "boot", Path: "boot.raw", Format: "raw", MaximumVirtualBytes: 16 << 20}, {ID: "data", Path: "data.raw", Format: "raw", MaximumVirtualBytes: 16 << 20}},
	}}
}
func TestDiskSetSourceSelectionAndDriftRefusal(t *testing.T) {
	s, _ := diskServiceFixture(t, &filesPhaseTool{})
	source := selectedDiskFixture(t)
	for _, change := range []func(*app.Request){
		func(r *app.Request) { delete(r.Input, "offlineSources") },
		func(r *app.Request) { r.Input["offlineSources"] = false },
		func(r *app.Request) { r.Input["future"] = true },
		func(r *app.Request) { r.Input["files"] = []any{map[string]any{"path": "../boot.raw"}} },
		func(r *app.Request) {
			r.Input["files"] = []any{map[string]any{"path": "boot.raw", "sha256": strings.Repeat("f", 64)}}
		},
		func(r *app.Request) { r.Input["files"] = []any{map[string]any{"path": "boot.raw"}} },
		func(r *app.Request) {
			r.Input["disks"] = []diskSetMapping{{ID: "boot", Path: "boot.raw", Format: "raw", MaximumVirtualBytes: 16 << 20}, {ID: "boot", Path: "data.raw", Format: "raw", MaximumVirtualBytes: 16 << 20}}
		},
	} {
		r := diskRequest(source, filepath.Join(t.TempDir(), "prepared"))
		change(&r)
		if _, err := s.Plan(context.Background(), 1000, r); err == nil {
			t.Fatal("unsafe or incomplete selection accepted", r)
		}
	}
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, diskRequest(source, dest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("preview published files", err)
	}
	path := filepath.Join(source, "boot.raw")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Restore the exact content and mtime. The intervening write still changes ctime.
	if err = os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("source write with restored content/mtime accepted")
	}
}
func TestDiskSetRefusesSymlinkSourcesAndReplacedDirectories(t *testing.T) {
	s, _ := diskServiceFixture(t, &filesPhaseTool{})
	source := selectedDiskFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plan(context.Background(), 1000, diskRequest(alias, filepath.Join(t.TempDir(), "prepared"))); err == nil {
		t.Fatal("symlink source ancestor accepted")
	}
	p, err := s.Plan(context.Background(), 1000, diskRequest(source, filepath.Join(t.TempDir(), "prepared")))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(source, source+"-held"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(source + "-held") })
	if err = os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"boot.raw", "data.raw"} {
		if err = os.Link(filepath.Join(source+"-held", name), filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	_, input, _ := s.Store.Plan(p.ID)
	if err = s.Validate(context.Background(), p, input); err == nil {
		t.Fatal("replacement source directory accepted")
	}
	if err = os.Remove(filepath.Join(source, "boot.raw")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(source+"-held", "boot.raw"), filepath.Join(source, "boot.raw")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Plan(context.Background(), 1000, diskRequest(source, filepath.Join(t.TempDir(), "prepared"))); err == nil {
		t.Fatal("symlink selected file accepted")
	}
}
func TestDiskSetFailureRetainsStageAndOriginals(t *testing.T) {
	s, _ := diskServiceFixture(t, &filesPhaseTool{phaseTool: phaseTool{fail: true}})
	source := selectedDiskFixture(t)
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, diskRequest(source, dest))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s.Service, accepted(t, s.Service, p).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("partial disk set published", err)
	}
	_, input, _ := s.Store.Plan(p.ID)
	var in diskSetInput
	if err = wire.Decode(input, &in); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(diskSetStagePath(p, in)); err != nil {
		t.Fatal("uncertain stage not retained", err)
	}
	for _, name := range []string{"boot.raw", "data.raw"} {
		b, err := os.ReadFile(filepath.Join(source, name))
		if err != nil || string(b) != "synthetic source "+name {
			t.Fatal("original changed", name, err)
		}
	}
	if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
		t.Fatal("incomplete conversion reconciled as successful")
	}
}

func TestDiskSetRevalidatesAfterNoncooperatingSourceWrite(t *testing.T) {
	source := selectedDiskFixture(t)
	tool := &filesPhaseTool{after: func() {
		// Deliberate synthetic external writer ignores advisory QEMU locks.
		if err := os.WriteFile(filepath.Join(source, "boot.raw"), []byte("external changed bytes"), 0600); err != nil {
			t.Error(err)
		}
	}}
	s, _ := diskServiceFixture(t, tool)
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, diskRequest(source, dest))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s.Service, accepted(t, s.Service, p).ID)
	if j.State != "recovery-required" {
		t.Fatal("changed source published", j)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("changed source artifact present", err)
	}
	if receipt, e := s.Store.MetadataBytes("import-artifact", p.ID); e != nil || len(receipt) != 0 {
		t.Fatal("changed source committed as verified artifact")
	}
}

func TestDocumentedSelectedDiskSchemaAndNativeIdentityPrecision(t *testing.T) {
	b, err := os.ReadFile("../../examples/import/selected-disks.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("disk-set-preparation-input", b); err != nil {
		t.Fatal(err)
	}
	proof := DiskSetFile{Path: "disk.raw", OriginalPath: "/source/disk.raw", Identity: identity{Device: ^uint64(0), Inode: ^uint64(0) - 1, Size: 512 << 30, ModifiedNS: 1788755123123456789}, Changed: "1788755123:123456789", SHA256: strings.Repeat("a", 64)}
	b, err = operations.Canonical(proof)
	if err != nil {
		t.Fatal(err)
	}
	var actual DiskSetFile
	if err = wire.Decode(b, &actual); err != nil || proof != actual {
		t.Fatal("native source proof lost precision", actual, err)
	}
}
func TestDiskSetCancellationJoinsWorkerBeforeRemovingStage(t *testing.T) {
	tool := &filesPhaseTool{started: make(chan struct{})}
	s, _ := diskServiceFixture(t, tool)
	source := selectedDiskFixture(t)
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, diskRequest(source, dest))
	if err != nil {
		t.Fatal(err)
	}
	j := accepted(t, s.Service, p)
	select {
	case <-tool.started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never started")
	}
	if _, err = s.Engine.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	j = finish(t, s.Service, j.ID)
	if j.State != "canceled" {
		t.Fatal(j)
	}
	for _, path := range []string{dest, filepath.Join(filepath.Dir(dest), diskSetStage(p))} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("canceled stage remains", path, err)
		}
	}
	for _, name := range []string{"boot.raw", "data.raw"} {
		b, err := os.ReadFile(filepath.Join(source, name))
		if err != nil || string(b) != "synthetic source "+name {
			t.Fatal("original changed", name, err)
		}
	}
}

type diskLostAck struct{ *DiskSetService }

func (s diskLostAck) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	if err := s.DiskSetService.Execute(ctx, p, b, step); err != nil {
		return err
	}
	return errors.New("synthetic lost publication acknowledgement")
}
func TestDiskSetPublicationReconcilesAfterJournalReopen(t *testing.T) {
	tool := &filesPhaseTool{}
	s, journal := diskServiceFixture(t, tool)
	s.Engine.Handlers["import.prepare-disks"] = diskLostAck{s}
	source := selectedDiskFixture(t)
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, diskRequest(source, dest))
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
	s.Engine.Handlers["import.prepare-disks"] = s
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	// Published independent copies no longer depend on original files for recovery.
	if err = os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	j, err = s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || j.State != "succeeded" || tool.calls.Load() != 2 {
		t.Fatal("recovery replayed conversion or lost receipt", j, err, tool.calls.Load())
	}
	a, directory, err := Approved(context.Background(), s.Store, 1000, j.ID)
	if err != nil || directory != dest || a.Kind != "PreparedDiskSet" || len(a.Disks) != 2 || len(a.System.Items) != 0 || a.VMDefined || a.GuestBootVerified {
		t.Fatal(a, directory, err)
	}
	if _, err = s.Result(context.Background(), 1001, j.ID); err == nil {
		t.Fatal("foreign actor result accepted")
	}
	if _, _, err = Approved(context.Background(), s.Store, 1001, j.ID); err == nil {
		t.Fatal("foreign actor source accepted")
	}
	manifest, err := operations.Canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("prepared-disk-set", manifest); err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("prepared-import", manifest); err == nil {
		t.Fatal("legacy OVA schema accepted disk-only hardware provenance")
	}
	var receipt map[string]any
	if err = wire.Decode(manifest, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt["futureAuthority"] = true
	if err = s.Store.Put("import-artifact", p.ID, receipt); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err == nil {
		t.Fatal("future private authority ignored")
	}
	if err = s.Store.Put("import-artifact", p.ID, a); err != nil {
		t.Fatal(err)
	}
	a.SourceFiles[0].SHA256 = strings.Repeat("f", 64)
	manifest, err = operations.Canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(dest, "manifest.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dest, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err == nil {
		t.Fatal("rewritten public source proof accepted as conversion authority")
	}
	t.Log("synthetic phase/authority test: actual SQLite reopen and atomic generated-file publication; no image-format or VM claim")
}
func TestRealSelectedDiskSetPreparationPublishesIndependentCopies(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit actual qemu-img generated-file fixture required; no VM boot")
	}
	s, _ := diskServiceFixture(t, image.Tool{})
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "disks"), 0700); err != nil {
		t.Fatal(err)
	}
	qemu := func(args ...string) {
		t.Helper()
		if b, err := exec.Command("/usr/bin/qemu-img", args...).CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
	}
	qemu("create", "-f", "raw", filepath.Join(source, "base.raw"), "8M")
	f, err := os.OpenFile(filepath.Join(source, "base.raw"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("Virmill generated selected disk content"), 4096); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	qemu("create", "-f", "qcow2", "-b", "../base.raw", "-F", "raw", filepath.Join(source, "disks", "boot.qcow2"), "8M")
	qemu("convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=twoGbMaxExtentFlat", filepath.Join(source, "base.raw"), filepath.Join(source, "disks", "data.vmdk"))
	files := []any{}
	originals := map[string][]byte{}
	if err = filepath.WalkDir(source, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		originals[rel] = b
		files = append(files, map[string]any{"path": rel, "sha256": hashBytes(b)})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "prepared")
	r := app.Request{Path: source, Input: map[string]any{"offlineSources": true, "destination": dest, "files": files, "disks": []diskSetMapping{{ID: "boot", Path: "disks/boot.qcow2", Format: "qcow2", MaximumVirtualBytes: 16 << 20}, {ID: "data", Path: "disks/data.vmdk", Format: "vmdk", MaximumVirtualBytes: 16 << 20}}}}
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s.Service, accepted(t, s.Service, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	a, _, err := Approved(context.Background(), s.Store, 1000, j.ID)
	if err != nil || len(a.Disks) != 2 || len(a.SourceFiles) != len(files) || a.Kind != "PreparedDiskSet" || a.VMDefined || a.GuestBootVerified {
		t.Fatal(a, err)
	}
	for path, before := range originals {
		after, e := os.ReadFile(filepath.Join(source, path))
		if e != nil || !bytes.Equal(before, after) {
			t.Fatal("original changed", path, e)
		}
	}
	for _, disk := range a.Disks {
		t.Logf("%s -> %s; virtual bytes %d; SHA-256 %s", disk.SourceID, disk.Path, disk.VirtualBytes, disk.SHA256)
	}
	if err = os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err != nil {
		t.Fatal("published copies still depend on source files", err)
	}
	t.Log("actual confined multi-disk conversion/check/compare from selected held files and durable publication; no VM definition, guest boot or hardware validation")
}
