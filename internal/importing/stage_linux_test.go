//go:build linux && amd64

package importing

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const fixtureOVF = `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData"><References><File ovf:id="f1" ovf:href="boot.vmdk"/><File ovf:id="f2" ovf:href="data.vmdk"/></References><DiskSection><Disk ovf:diskId="boot" ovf:fileRef="f1"/><Disk ovf:diskId="data" ovf:fileRef="f2"/></DiskSection><VirtualSystem ovf:id="appliance"><Name>Synthetic appliance</Name><VirtualHardwareSection><Item><rasd:ResourceType>17</rasd:ResourceType><rasd:Parent>controller0</rasd:Parent><rasd:AddressOnParent>0</rasd:AddressOnParent><rasd:HostResource>ovf:/disk/boot</rasd:HostResource></Item><Item><rasd:ResourceType>17</rasd:ResourceType><rasd:Parent>controller0</rasd:Parent><rasd:AddressOnParent>1</rasd:AddressOnParent><rasd:HostResource>ovf:/disk/data</rasd:HostResource></Item></VirtualHardwareSection></VirtualSystem></Envelope>`

func fixtureArchive(t *testing.T, real bool) string {
	t.Helper()
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "appliance.ovf"), []byte(fixtureOVF), 0600)
	for _, name := range []string{"boot", "data"} {
		if real {
			raw := filepath.Join(t.TempDir(), name+".raw")
			f, err := os.OpenFile(raw, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			f.Truncate(8 << 20)
			f.WriteAt([]byte("Virmill synthetic "+name+" contents"), 4096)
			f.Close()
			cmd := exec.Command("/usr/bin/qemu-img", "convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=twoGbMaxExtentSparse", raw, filepath.Join(source, name+".vmdk"))
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatal(err, string(b))
			}
		} else {
			os.WriteFile(filepath.Join(source, name+".vmdk"), []byte("synthetic phase fixture "+name), 0600)
		}
	}
	archive := filepath.Join(t.TempDir(), "appliance.ova")
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err = tw.WriteHeader(&tar.Header{Name: entry.Name(), Mode: 0600, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err = tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err = tw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return archive
}

type phaseTool struct{ fail bool }

func (phaseTool) Identity(context.Context) (image.Identity, error) {
	return image.Identity{Path: "fixture-only", SHA256: strings.Repeat("a", 64), Version: "synthetic-phase-test"}, nil
}
func (phaseTool) Inspect(ctx context.Context, source, work, name, format string, bound int64, members map[string]bool) ([]image.Info, error) {
	return []image.Info{{Filename: "/source/" + name, Format: format, VirtualSize: 8 << 20}}, nil
}
func (p phaseTool) Convert(ctx context.Context, source, work, name, format string, size, bound int64) error {
	os.WriteFile(filepath.Join(work, "disk.qcow2"), []byte("synthetic output; not a real qcow2"), 0600)
	if p.fail && strings.HasPrefix(name, "data") {
		return errors.New("fixture converter crash after partial output")
	}
	return nil
}

func serviceFixture(t *testing.T, tool DiskTool) *Service {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state", "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	s := &Service{Engine: engine, Store: db, Tool: tool}
	engine.Handlers["import.prepare"] = s
	t.Cleanup(func() { engine.Close(); db.Close() })
	return s
}
func stageRequest(source, destination string) app.Request {
	return app.Request{Path: source, Input: map[string]any{"destination": destination, "systemID": "appliance", "disks": []Mapping{{ID: "boot", Format: "vmdk", MaximumVirtualBytes: 16 << 20}, {ID: "data", Format: "vmdk", MaximumVirtualBytes: 16 << 20}}}}
}
func accepted(t *testing.T, s *Service, p domain.Plan) domain.Job {
	t.Helper()
	j, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func finish(t *testing.T, s *Service, id string) domain.Job {
	t.Helper()
	end := time.Now().Add(30 * time.Second)
	for time.Now().Before(end) {
		j, err := s.Store.Job(id)
		if err != nil {
			t.Fatal(err)
		}
		if domain.Terminal(j.State) {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return domain.Job{}
}

func TestAllDiskMappingAndSourceDrift(t *testing.T) {
	s := serviceFixture(t, phaseTool{})
	source := fixtureArchive(t, false)
	dest := filepath.Join(t.TempDir(), "prepared")
	r := stageRequest(source, dest)
	r.Input["disks"] = []Mapping{{ID: "boot", Format: "vmdk", MaximumVirtualBytes: 16 << 20}}
	if _, err := s.Plan(context.Background(), 1000, r); err == nil {
		t.Fatal("incomplete disk selection accepted")
	}
	r = stageRequest(source, dest)
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("preview created output")
	}
	os.WriteFile(source, []byte("changed after preview"), 0600)
	if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "changed", Acknowledgements: p.Acknowledgements}); err == nil {
		t.Fatal("changed source accepted")
	}
}

func TestConverterFailurePreservesOriginalAndDoesNotPublish(t *testing.T) {
	s := serviceFixture(t, phaseTool{fail: true})
	source := fixtureArchive(t, false)
	before, _ := os.ReadFile(source)
	destination := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(source, destination))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s, accepted(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal("partial conversion was not reported", j)
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("incomplete disk set published")
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(before, after) {
		t.Fatal("original archive changed")
	}
	_, input, _ := s.Store.Plan(p.ID)
	var in stageInput
	json.Unmarshal(input, &in)
	if _, err = os.Stat(stagePath(p, in)); err != nil {
		t.Fatal("uncertain staging vanished", err)
	}
	if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
		t.Fatal("incomplete artifacts reconciled as complete")
	}
}

type lostAck struct{ *Service }

func (s lostAck) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	if err := s.Service.Execute(ctx, p, b, step); err != nil {
		return err
	}
	return errors.New("fixture lost publication acknowledgement")
}
func TestPublishedArtifactReconcilesWithoutConversionReplay(t *testing.T) {
	s := serviceFixture(t, phaseTool{})
	s.Engine.Handlers["import.prepare"] = lostAck{s}
	p, err := s.Plan(context.Background(), 1000, stageRequest(fixtureArchive(t, false), filepath.Join(t.TempDir(), "prepared")))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s, accepted(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || recovered.State != "succeeded" {
		t.Fatal("durable receipt did not reconcile", recovered, err)
	}
}

func TestRealMultiDiskSplitVMDKPreparation(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit isolated disk-tool fixture execution required; not a boot test")
	}
	s := serviceFixture(t, image.Tool{})
	source := fixtureArchive(t, true)
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(source, destination))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s, accepted(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatalf("native disk preparation failed: %+v", j.Error)
	}
	artifact, err := Verify(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Disks) != 2 || artifact.VMDefined || artifact.GuestBootVerified {
		t.Fatal("disk set lost or boot readiness fabricated", artifact)
	}
	if artifact.Disks[0].SourceID != "boot" || artifact.Disks[1].SourceID != "data" || artifact.System.Items[1].AddressOnParent != "1" {
		t.Fatal("disk/controller mapping lost")
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("original archive changed", err)
	}
	for _, d := range artifact.Disks {
		t.Logf("%s -> %s: %d virtual bytes, SHA-256 %s", d.SourceID, d.Path, d.VirtualBytes, d.SHA256)
	}
	t.Logf("Original OVA SHA-256 %s preserved. Actual qemu-img conversions, checks and comparisons executed in confinement; no VM registration or guest boot.", hashBytes(original))
	// Altering a published disk is detected by artifact verification.
	path := filepath.Join(destination, artifact.Disks[0].Path)
	os.Chmod(path, 0600)
	os.WriteFile(path, []byte("tampered"), 0600)
	if _, err = Verify(context.Background(), destination); err == nil {
		t.Fatal("tampered converted artifact accepted")
	}
}

type waitingTool struct {
	phaseTool
	started chan struct{}
}

func (w waitingTool) Convert(ctx context.Context, source, work, name, format string, size, bound int64) error {
	if err := os.WriteFile(filepath.Join(work, "disk.qcow2"), []byte("partial fixture"), 0600); err != nil {
		return err
	}
	close(w.started)
	<-ctx.Done()
	return ctx.Err()
}
func TestCancelStopsWorkerBeforeRemovingOnlyItsStaging(t *testing.T) {
	w := waitingTool{started: make(chan struct{})}
	s := serviceFixture(t, w)
	source := fixtureArchive(t, false)
	original, _ := os.ReadFile(source)
	destination := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(source, destination))
	if err != nil {
		t.Fatal(err)
	}
	j := accepted(t, s, p)
	select {
	case <-w.started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	if _, err = s.Engine.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	j = finish(t, s, j.ID)
	if j.State != "canceled" {
		t.Fatal("safe worker cancellation not recorded", j)
	}
	_, b, _ := s.Store.Plan(p.ID)
	var in stageInput
	if err = wire.Decode(b, &in); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{destination, stagePath(p, in)} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("canceled output remains", path, err)
		}
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(original, after) {
		t.Fatal("cancellation changed original source")
	}
}

func TestReceiptSchemaCompatibilityAndIdentityPrecision(t *testing.T) {
	id := identity{Device: ^uint64(0), Inode: ^uint64(0) - 1, Size: 512 << 30, ModifiedNS: 1788755123123456789}
	b, err := operations.Canonical(id)
	if err != nil {
		t.Fatal(err)
	}
	var decoded identity
	if err = wire.Decode(b, &decoded); err != nil || decoded != id {
		t.Fatal("canonical identity lost precision", string(b), decoded, err)
	}
	s := serviceFixture(t, phaseTool{})
	destination := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(fixtureArchive(t, false), destination))
	if err != nil {
		t.Fatal(err)
	}
	if j := finish(t, s, accepted(t, s, p).ID); j.State != "succeeded" {
		t.Fatal(j)
	}
	b, err = os.ReadFile(filepath.Join(destination, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("prepared-import", b); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	json.Unmarshal(b, &receipt)
	// Optional resource metadata must retain compatibility with older receipts.
	system := receipt["system"].(map[string]any)
	item := map[string]any{"resourceType": "4", "instanceID": "memory", "parent": "", "addressOnParent": "", "quantity": "2048", "hostResources": []string{}, "connections": []string{}, "allocationUnits": "byte * 2^20", "memoryMiB": 2048}
	system["hardware"] = []any{item}
	for _, enriched := range []bool{true, false} {
		if !enriched {
			delete(item, "allocationUnits")
			delete(item, "memoryMiB")
		}
		raw, _ := json.Marshal(receipt)
		if err := validation.Schema("prepared-import", raw); err != nil {
			t.Fatal("resource receipt compatibility", enriched, err)
		}
	}
	for _, field := range []string{"apiVersion", "guestBootVerified", "unrecognizedField"} {
		changed := make(map[string]any)
		for k, v := range receipt {
			changed[k] = v
		}
		if field == "apiVersion" {
			changed[field] = "virmill/v2"
		} else {
			changed[field] = true
		}
		invalid, _ := json.Marshal(changed)
		if err = validation.Schema("prepared-import", invalid); err == nil {
			t.Fatal("invalid/newer receipt accepted", field)
		}
	}
}
