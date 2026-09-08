//go:build linux && amd64

package coldcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/store"
)

// Capture and restore use real generated files and the durable engine. Native
// allocation/definition and disk-format inspection are deterministic boundaries;
// generated output is not a native QCOW2/ISO or guest qualification claim.
type restoreInspectTool struct{ *diskToolFixture }

func (d restoreInspectTool) InspectFiles(ctx context.Context, files []platform.DiskSourceFile, work, name, format string, limit int64) ([]image.Info, error) {
	if name != "disk" {
		return d.diskToolFixture.InspectFiles(ctx, files, work, name, format, limit)
	}
	if len(files) != 1 || files[0].Path != "disk" {
		return nil, errors.New("unbound generated restore inspection")
	}
	st, err := files[0].File.Stat()
	if err != nil {
		return nil, err
	}
	return []image.Info{{Filename: "disk", Format: format, VirtualSize: max(st.Size(), 4096), ActualSize: st.Size()}}, ctx.Err()
}

type restoreBackendFixture struct {
	domain.CreationBackend
	domain.ResourceInventory
	mu                                                sync.Mutex
	pool                                              domain.StoragePool
	directory                                         string
	journal                                           *store.Store
	volumes                                           map[string]domain.CreatedVolume
	events                                            []string
	allocated, populated, verified, defined, observed int
	failUpload, failVerify                            int
	identityBusy, loseDefineAck                       bool
	definition                                        *domain.ColdRestoreDefinition
	vm                                                domain.VM
}

func (b *restoreBackendFixture) GetStoragePool(ctx context.Context, uri, id string) (domain.StoragePool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if uri != b.pool.Key.ConnectionID || id != b.pool.Key.UUID {
		return domain.StoragePool{}, errors.New("unexpected generated restore pool")
	}
	return clone(b.pool), ctx.Err()
}
func (b *restoreBackendFixture) CheckCreationIdentity(ctx context.Context, _ string, id, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.identityBusy || b.definition != nil && (b.definition.UUID == id || b.definition.Name == name) {
		return domain.Fail("STALE_PLAN", "new restore identity is no longer absent")
	}
	return ctx.Err()
}
func (b *restoreBackendFixture) VolumeAbsent(ctx context.Context, _ string, intent domain.VolumeIntent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.volumes[intent.Name]; ok {
		return domain.Fail("STALE_PLAN", "restore volume name already exists")
	}
	return ctx.Err()
}
func (b *restoreBackendFixture) AllocateVolume(ctx context.Context, _ string, intent domain.VolumeIntent) (domain.CreatedVolume, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.CreatedVolume{}, err
	}
	job, err := b.journal.Job(operations.OperationID(ctx))
	if err != nil || job.State != "running" {
		return domain.CreatedVolume{}, errors.New("volume allocation preceded durable job intent")
	}
	var progress restoreProgress
	if err = b.journal.Get("cold-restore", job.PlanID, &progress); err != nil || progress.OperationID != job.ID {
		return domain.CreatedVolume{}, errors.New("allocation preceded restore receipt")
	}
	path := filepath.Join(b.directory, intent.Name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return domain.CreatedVolume{}, err
	}
	if err = f.Close(); err != nil {
		return domain.CreatedVolume{}, err
	}
	id, err := fileidentity.Observe(path, false)
	if err != nil {
		return domain.CreatedVolume{}, err
	}
	v := domain.CreatedVolume{Intent: intent, Path: path, BackendKey: "generated:" + path, Generation: id.Generation}
	b.volumes[intent.Name] = v
	b.allocated++
	b.events = append(b.events, "allocate:"+intent.SourceID)
	return v, nil
}
func (b *restoreBackendFixture) PopulateVolume(ctx context.Context, _ string, v domain.CreatedVolume, source io.Reader) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(b.volumes[v.Intent.Name], v) {
		return errors.New("unbound generated upload")
	}
	job, err := b.journal.Job(operations.OperationID(ctx))
	if err != nil {
		return err
	}
	var progress restoreProgress
	if err = b.journal.Get("cold-restore", job.PlanID, &progress); err != nil {
		return err
	}
	if len(progress.Volumes) == 0 || progress.Volumes[len(progress.Volumes)-1].Volume != v {
		return errors.New("upload preceded durable allocation receipt")
	}
	data, err := io.ReadAll(io.LimitReader(source, int64(v.Intent.FileBytes)+1))
	if err != nil {
		return err
	}
	if uint64(len(data)) != v.Intent.FileBytes || captureDigest(data) != v.Intent.SHA256 {
		return errors.New("upload did not use the exact captured member")
	}
	b.populated++
	b.events = append(b.events, "populate:"+v.Intent.SourceID)
	if b.failUpload == b.populated {
		if err := os.WriteFile(v.Path, data[:len(data)/2], 0600); err != nil {
			return err
		}
		return errors.New("generated interrupted upload")
	}
	return os.WriteFile(v.Path, data, 0600)
}
func (b *restoreBackendFixture) verify(v domain.CreatedVolume) error {
	if !reflect.DeepEqual(b.volumes[v.Intent.Name], v) {
		return errors.New("generated restore volume receipt differs")
	}
	id, err := fileidentity.Observe(v.Path, false)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(v.Path)
	if err != nil {
		return err
	}
	if id.Generation != v.Generation || uint64(len(data)) != v.Intent.FileBytes || captureDigest(data) != v.Intent.SHA256 {
		return domain.Fail("SOURCE_CHANGED", "generated restored volume bytes or generation differ")
	}
	return nil
}
func (b *restoreBackendFixture) VerifyCreatedVolume(ctx context.Context, _ string, v domain.CreatedVolume) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.verified++
	b.events = append(b.events, "verify:"+v.Intent.SourceID)
	if b.failVerify == b.verified {
		return errors.New("generated volume verification failure")
	}
	if err := b.verify(v); err != nil {
		return err
	}
	return ctx.Err()
}
func restoreFixtureXML(d domain.ColdRestoreDefinition) (string, error) {
	patch := xmlpatch.ColdRestorePatch{UUID: d.UUID, Name: d.Name, DisconnectNICs: d.DisconnectNICs}
	for _, disk := range d.Disks {
		patch.Disks = append(patch.Disks, xmlpatch.ColdRestoreDisk{Target: disk.Target, Path: disk.Volume.Path, Format: disk.Format})
	}
	return xmlpatch.ColdRestore(d.SourceXML, patch)
}
func (b *restoreBackendFixture) DefineRestoredVM(ctx context.Context, uri string, d domain.ColdRestoreDefinition) (domain.VM, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.VM{}, err
	}
	job, err := b.journal.Job(operations.OperationID(ctx))
	if err != nil {
		return domain.VM{}, err
	}
	var progress restoreProgress
	if err = b.journal.Get("cold-restore", job.PlanID, &progress); err != nil {
		return domain.VM{}, err
	}
	if len(progress.Volumes) != len(d.Disks) || !d.DisconnectNICs {
		return domain.VM{}, errors.New("definition preceded complete disconnected restore staging")
	}
	for i, disk := range d.Disks {
		if !progress.Volumes[i].Verified || progress.Volumes[i].Volume != disk.Volume {
			return domain.VM{}, errors.New("definition preceded durable all-volume verification")
		}
		if err := b.verify(disk.Volume); err != nil {
			return domain.VM{}, err
		}
	}
	raw, err := restoreFixtureXML(d)
	if err != nil {
		return domain.VM{}, err
	}
	copy := clone(d)
	b.definition = &copy
	b.defined++
	b.events = append(b.events, "define")
	b.vm = domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: d.UUID}, Name: d.Name, State: "stopped", PersistentXML: raw, Fingerprint: captureDigest([]byte(raw))}
	if b.loseDefineAck {
		return domain.VM{}, domain.Fail("RECOVERY_REQUIRED", "generated lost native definition acknowledgement")
	}
	return clone(b.vm), nil
}
func (b *restoreBackendFixture) ObserveRestoredVM(ctx context.Context, _ string, d domain.ColdRestoreDefinition) (domain.VM, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observed++
	b.events = append(b.events, "observe")
	if err := ctx.Err(); err != nil {
		return domain.VM{}, false, err
	}
	if b.definition == nil {
		return domain.VM{}, false, nil
	}
	if !reflect.DeepEqual(*b.definition, d) {
		return domain.VM{}, false, domain.Fail("SOURCE_CHANGED", "generated definition differs")
	}
	for _, disk := range d.Disks {
		if err := b.verify(disk.Volume); err != nil {
			return domain.VM{}, false, err
		}
	}
	return clone(b.vm), true, nil
}

type restoreHarness struct {
	*captureHarness
	target       *restoreBackendFixture
	capture      coldstore.Receipt
	captureBytes map[string][]byte
	sourceVM     domain.VM
}

func newRestoreHarness(t *testing.T) *restoreHarness {
	t.Helper()
	h := newCaptureHarness(t)
	iso := bytes.Repeat([]byte{'I'}, 32768)
	h.originals["installer.iso"] = iso
	if err := os.WriteFile(filepath.Join(h.root, "installer.iso"), iso, 0600); err != nil {
		t.Fatal(err)
	}
	info := h.tool.infos["installer.iso"]
	info.VirtualSize = int64(len(iso))
	h.tool.infos["installer.iso"] = info
	xml := `<domain type='kvm' xmlns:x='urn:restore:test'><name>original-guest</name><uuid>` + h.backend.vm.Key.UUID + `</uuid><metadata><x:opaque original='keep'> exact original bytes </x:opaque></metadata><memory unit='KiB'>524288</memory><vcpu>2</vcpu><os><type arch='x86_64' machine='pc-q35-synthetic'>hvm</type></os><devices>` +
		`<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='` + filepath.Join(h.root, "images/leaf.qcow2") + `'/><backingStore type='file'><format type='raw'/><source file='` + filepath.Join(h.root, "base.raw") + `'/><backingStore/></backingStore><target dev='vda' bus='virtio'/><serial>retain-serial</serial><x:disk keep='yes'/></disk>` +
		`<disk type='file' device='disk'><driver name='qemu' type='raw'/><source file='` + filepath.Join(h.root, "second.raw") + `'/><target dev='vdb' bus='virtio'/></disk>` +
		`<disk type='file' device='cdrom'><driver name='qemu' type='raw'/><source file='` + filepath.Join(h.root, "installer.iso") + `'/><target dev='sda' bus='sata'/><readonly/></disk>` +
		`<disk type='file' device='cdrom'><source/><target dev='sdb' bus='sata'/><readonly/></disk>` +
		`<interface type='network'><source network='original-lan'/><mac address='52:54:00:11:22:33'/></interface><interface type='bridge'><source bridge='original-bridge'/><x:policy/></interface><video><model type='virtio'/></video></devices></domain><!-- retained tail -->`
	h.backend.vm.Name = "original-guest"
	h.backend.vm.PersistentXML = xml
	h.backend.vm.Fingerprint = captureDigest([]byte(xml))
	h.backend.native.Fingerprint = h.backend.vm.Fingerprint
	p := h.plan(t)
	j := h.await(t, h.apply(t, p).ID)
	if j.State != "succeeded" {
		t.Fatal("restore fixture capture failed", j)
	}
	var in recipe
	_, raw, err := h.db.Plan(p.ID)
	if err != nil || json.Unmarshal(raw, &in) != nil {
		t.Fatal(err)
	}
	receipt, err := coldstore.Inspect(context.Background(), h.s.Catalog, in.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(filepath.Dir(h.root), "target-volumes")
	if err = os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	poolID := "22222222-3333-4444-8555-666666666666"
	available := uint64(512 << 20)
	backend := &restoreBackendFixture{directory: directory, journal: h.db, volumes: map[string]domain.CreatedVolume{}, pool: domain.StoragePool{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "pool", UUID: poolID}, Name: "generated-restore-pool", Type: "dir", State: "running", Active: true, Persistent: true, AvailableBytes: &available, XML: `<pool type='dir'><name>generated-restore-pool</name><uuid>` + poolID + `</uuid><capacity unit='bytes'>1073741824</capacity><allocation unit='bytes'>0</allocation><available unit='bytes'>536870912</available><target><path>` + directory + `</path></target></pool>`}}
	h.s.Tool = restoreInspectTool{h.tool}
	handler := &restorer{s: h.s, backend: backend}
	h.s.Engine.Handlers[restoreOperation] = handler
	h.app.Extensions["snapshot.restore"] = handler.Plan
	result := &restoreHarness{captureHarness: h, target: backend, capture: receipt, sourceVM: clone(h.backend.vm), captureBytes: map[string][]byte{}}
	for _, member := range receipt.Manifest.Members {
		path := filepath.Join(h.s.Catalog, receipt.SnapshotID, member.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result.captureBytes[path] = data
	}
	for _, name := range []string{"manifest.json", "receipt.json"} {
		path := filepath.Join(h.s.Catalog, receipt.SnapshotID, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result.captureBytes[path] = data
	}
	return result
}
func (h *restoreHarness) restoreRequest() app.Request {
	return app.Request{ID: h.capture.SnapshotID, Connection: "qemu:///system", Action: "restore", Input: map[string]any{"name": "restored-copy", "poolID": h.target.pool.Key.UUID}}
}
func (h *restoreHarness) restorePlan(t *testing.T) domain.Plan {
	t.Helper()
	r := h.app.Call(context.Background(), uint32(os.Getuid()), "snapshot.restore", h.restoreRequest())
	p, ok := r.Data.(domain.Plan)
	if r.Error != nil || !ok || p.Digest == "" {
		t.Fatal("restore preview failed", r)
	}
	return p
}
func (h *restoreHarness) preserved(t *testing.T) {
	t.Helper()
	h.unchanged(t)
	if !reflect.DeepEqual(h.backend.vm, h.sourceVM) {
		t.Fatal("original native guest was changed")
	}
	for path, want := range h.captureBytes {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("original capture was changed", path, err)
		}
	}
}
func (h *restoreHarness) noRestoreEffects(t *testing.T) {
	t.Helper()
	if h.target.allocated != 0 || h.target.populated != 0 || h.target.defined != 0 {
		t.Fatal("restore refusal caused an effect", h.target.events)
	}
	jobs, err := h.db.Jobs()
	if err != nil || len(jobs) != 1 {
		t.Fatal("restore refusal created an extra job", jobs, err)
	}
}

func TestColdRestorePublicCompleteSetDefinesLastAndPreservesOriginal(t *testing.T) {
	h := newRestoreHarness(t)
	p := h.restorePlan(t)
	if p.Review["disconnectAllNICs"] != true || p.Review["guestBootVerified"] != false || p.Review["newName"] != "restored-copy" || p.Estimates.AdditionalBytes <= 64<<20 {
		t.Fatal("restore review lost identity/isolation/space intent", p.Review, p.Estimates)
	}
	h.noRestoreEffects(t)
	h.preserved(t)
	j := h.await(t, h.apply(t, p).ID)
	if j.State != "succeeded" {
		t.Fatal("restore did not finish", j)
	}
	b := h.target
	if b.allocated != 3 || b.populated != 3 || b.verified != 3 || b.defined != 1 || b.observed != 1 {
		t.Fatal("restore omitted staging or repeated effects", b.events)
	}
	for i := 0; i < 3; i++ {
		id := h.capture.Manifest.Disks[i].MemberID
		want := []string{"allocate:" + id, "populate:" + id, "verify:" + id}
		if !reflect.DeepEqual(b.events[i*3:i*3+3], want) {
			t.Fatal("staging order changed", b.events)
		}
	}
	if !reflect.DeepEqual(b.events[9:], []string{"define", "observe"}) {
		t.Fatal("definition was not last", b.events)
	}
	vm := b.vm
	if vm.Key.UUID == h.sourceVM.Key.UUID || vm.Name != "restored-copy" || vm.State != "stopped" || vm.Autostart || strings.Contains(vm.PersistentXML, "<interface") || strings.Contains(vm.PersistentXML, "backingStore") {
		t.Fatal("new disconnected independent identity not enforced", vm)
	}
	for _, opaque := range []string{`<x:opaque original='keep'> exact original bytes </x:opaque>`, `<serial>retain-serial</serial>`, `<x:disk keep='yes'/>`, `<!-- retained tail -->`} {
		if !strings.Contains(vm.PersistentXML, opaque) {
			t.Fatal("opaque source configuration lost", opaque)
		}
	}
	for _, disk := range b.definition.Disks {
		if !strings.Contains(vm.PersistentXML, disk.Volume.Path) || strings.HasPrefix(disk.Volume.Path, h.root+string(filepath.Separator)) {
			t.Fatal("restore reused source storage", disk)
		}
		if disk.Target == "sda" && (disk.Volume.Intent.ContentType != "cdrom-iso" || disk.Format != "raw") {
			t.Fatal("media was not independently staged")
		}
	}
	var progress restoreProgress
	if err := h.db.Get("cold-restore", p.ID, &progress); err != nil || !progress.Defined || len(progress.Volumes) != 3 {
		t.Fatal("completion receipt missing", progress, err)
	}
	h.preserved(t)
}

func TestColdRestoreLostDefineAcknowledgementRecoversWithoutAllocationReplay(t *testing.T) {
	h := newRestoreHarness(t)
	p := h.restorePlan(t)
	h.target.loseDefineAck = true
	j := h.await(t, h.apply(t, p).ID)
	if j.State != "recovery-required" || h.target.defined != 1 || h.target.allocated != 3 {
		t.Fatal("lost acknowledgement was not retained", j, h.target.events)
	}
	var progress restoreProgress
	if err := h.db.Get("cold-restore", p.ID, &progress); err != nil || progress.Defined || len(progress.Volumes) != 3 {
		t.Fatal("uncertain native acknowledgement was invented", progress, err)
	}
	r := h.app.Call(context.Background(), uint32(os.Getuid()), "operation.reconcile", app.Request{ID: j.ID})
	recovered, ok := r.Data.(domain.Job)
	if r.Error != nil || !ok || recovered.State != "succeeded" || h.target.defined != 1 || h.target.allocated != 3 || h.target.populated != 3 {
		t.Fatal("recovery repeated restore effects", r, h.target.events)
	}
	h.preserved(t)
}

func TestColdRestoreUploadAndVerificationFailuresRetainVolumesNeverDefine(t *testing.T) {
	for _, fault := range []string{"upload-first", "upload-second", "verify-second"} {
		t.Run(fault, func(t *testing.T) {
			h := newRestoreHarness(t)
			p := h.restorePlan(t)
			switch fault {
			case "upload-first":
				h.target.failUpload = 1
			case "upload-second":
				h.target.failUpload = 2
			case "verify-second":
				h.target.failVerify = 2
			}
			j := h.await(t, h.apply(t, p).ID)
			if j.State != "recovery-required" || h.target.defined != 0 {
				t.Fatal("failed population reached definition", j, h.target.events)
			}
			var progress restoreProgress
			if err := h.db.Get("cold-restore", p.ID, &progress); err != nil || len(progress.Volumes) != h.target.allocated || len(progress.Volumes) == 0 || progress.Volumes[len(progress.Volumes)-1].Verified {
				t.Fatal("uncertain volume receipt was discarded or verified", progress, err)
			}
			for _, v := range progress.Volumes {
				if _, err := os.Stat(v.Volume.Path); err != nil {
					t.Fatal("failure removed retained volume", err)
				}
			}
			before := append([]string{}, h.target.events...)
			r := h.app.Call(context.Background(), uint32(os.Getuid()), "operation.reconcile", app.Request{ID: j.ID})
			if r.Error == nil || !reflect.DeepEqual(before, h.target.events) {
				t.Fatal("partial restore was replayed or promoted", r, h.target.events)
			}
			for _, resource := range p.ResourceIDs {
				ids, err := h.db.ResourceJobs(resource)
				if err != nil || len(ids) != 1 || ids[0] != j.ID {
					t.Fatal("uncertain restore lost a resource lease", ids, err)
				}
			}
			h.preserved(t)
		})
	}
}

func TestColdRestorePlanningAndApplyRefuseUnavailableOrStaleTargets(t *testing.T) {
	for _, fault := range []string{"insufficient-plan", "unknown-capacity", "inactive-pool", "stale-pool", "insufficient-apply", "identity-conflict"} {
		t.Run(fault, func(t *testing.T) {
			h := newRestoreHarness(t)
			var p domain.Plan
			if fault == "stale-pool" || fault == "insufficient-apply" || fault == "identity-conflict" {
				p = h.restorePlan(t)
			}
			switch fault {
			case "insufficient-plan", "insufficient-apply":
				n := uint64(1)
				h.target.pool.AvailableBytes = &n
			case "unknown-capacity":
				h.target.pool.AvailableBytes = nil
			case "inactive-pool":
				h.target.pool.Active = false
			case "stale-pool":
				h.target.pool.XML = strings.Replace(h.target.pool.XML, "generated-restore-pool", "changed-pool", 1)
			case "identity-conflict":
				h.target.identityBusy = true
			}
			var r app.Response
			if p.ID == "" {
				r = h.app.Call(context.Background(), uint32(os.Getuid()), "snapshot.restore", h.restoreRequest())
			} else {
				r = h.app.Call(context.Background(), uint32(os.Getuid()), "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "refused-" + p.ID, Acknowledgements: p.Acknowledgements}})
			}
			if r.Error == nil {
				t.Fatal("unsafe target accepted", r)
			}
			h.noRestoreEffects(t)
			h.preserved(t)
		})
	}
}

func TestColdRestoreBadCaptureMissingMemberAndAuxiliaryRefuse(t *testing.T) {
	for _, fault := range []string{"missing-member", "changed-member", "auxiliary"} {
		t.Run(fault, func(t *testing.T) {
			h := newRestoreHarness(t)
			request := h.restoreRequest()
			if fault == "auxiliary" {
				m := clone(h.capture.Manifest)
				m.ID = domain.ID()
				m.OperationID = domain.ID()
				m.Source.State.TPM = &domain.ColdTPM{Model: "tpm-crb", Version: "2.0", SourceType: "dir", SourcePath: "/generated/unsupported-tpm"}
				m.NativeVersions["swtpm"] = "synthetic"
				m.AuxiliaryInventoryMember = "aux-inventory"
				m.TPMMembers = []protection.CapturedTPMFile{{Name: "permanent", MemberID: "tpm"}}
				sources := []coldstore.Source{}
				for _, member := range m.Members {
					data, err := os.ReadFile(filepath.Join(h.s.Catalog, h.capture.SnapshotID, member.Path))
					if err != nil {
						t.Fatal(err)
					}
					sources = append(sources, coldstore.Source{Member: member, Reader: bytes.NewReader(data)})
				}
				for _, item := range []struct {
					id, kind, path string
					data           []byte
				}{{"tpm", "tpm", "unsupported/permanent", []byte{}}, {"aux-inventory", "auxiliary-inventory", "unsupported/inventory.json", []byte(`{"synthetic":true}`)}} {
					member := protection.CaptureMember{ID: item.id, Kind: item.kind, Path: item.path, Size: int64(len(item.data)), SHA256: captureDigest(item.data)}
					m.Members = append(m.Members, member)
					sources = append(sources, coldstore.Source{Member: member, Reader: bytes.NewReader(item.data)})
				}
				if _, err := coldstore.Publish(context.Background(), h.s.Catalog, m, sources); err != nil {
					t.Fatal(err)
				}
				request.ID = m.ID
			} else {
				path := filepath.Join(h.s.Catalog, h.capture.SnapshotID, h.capture.Manifest.Members[1].Path)
				if fault == "missing-member" {
					if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(filepath.Dir(path), 0500); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Chmod(path, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("changed capture bytes"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, 0400); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := h.app.Call(context.Background(), uint32(os.Getuid()), "snapshot.restore", request)
			if r.Error == nil || fault == "auxiliary" && r.Error.Code != "UNSUPPORTED_CAPABILITY" {
				t.Fatal("incomplete/unsupported capture restored", r)
			}
			h.noRestoreEffects(t)
			h.unchanged(t)
		})
	}
}
