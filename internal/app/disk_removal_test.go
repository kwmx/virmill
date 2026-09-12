package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type diskRemovalFixture struct {
	*removalFixture
	s          *Service
	inspection domain.DiskRemoval
	mu         sync.Mutex
	states     []string
	deleted    []int
	failAt     int
	lostAck    bool
	changed    bool
}

func (f *diskRemovalFixture) InspectDiskRemoval(_ context.Context, _, _ string, targets []string) (domain.DiskRemoval, error) {
	if !reflect.DeepEqual(targets, diskRemovalTargets(f.inspection)) {
		return domain.DiskRemoval{}, errors.New("wrong selected targets")
	}
	return f.inspection, nil
}
func (f *diskRemovalFixture) CheckDiskRemoval(_ context.Context, r domain.DiskRemoval, absent bool) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.changed || !reflect.DeepEqual(r, f.inspection) || f.absent.Load() != absent {
		return nil, domain.Fail("SOURCE_CHANGED", "fixture native state changed")
	}
	return slices.Clone(f.states), nil
}
func (f *diskRemovalFixture) DeleteRemovalDisk(ctx context.Context, _ domain.DiskRemoval, index int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.absent.Load() {
		return errors.New("definition still present")
	}
	b, e := f.s.Engine.Store.MetadataBytes(diskRemovalReceiptKind, operations.OperationID(ctx))
	if e != nil {
		return e
	}
	var receipt diskRemovalReceipt
	if wire.Decode(b, &receipt) != nil || receipt.Definition != "removed" || receipt.Disks[index] != "intent" {
		return errors.New("deletion occurred without durable per-file intent")
	}
	f.deleted = append(f.deleted, index)
	if index == f.failAt && !f.lostAck {
		return errors.New("fixture disk deletion failed")
	}
	f.states[index] = "absent"
	if index == f.failAt {
		return errors.New("fixture lost delete acknowledgement")
	}
	return nil
}
func diskRemovalService(t *testing.T) (*Service, *diskRemovalFixture) {
	s, base, _ := removalService(t)
	base.definition.RetainedSources = []string{"/fixture/a.qcow2", "/fixture/b.qcow2", "/fixture/installer.iso"}
	r := domain.DiskRemoval{Definition: base.definition, GraphDigest: strings.Repeat("c", 64), ResourceIDs: []string{base.VM.Key.String()}}
	for i, target := range []string{"vda", "vdb"} {
		name := []string{"a.qcow2", "b.qcow2"}[i]
		r.Disks = append(r.Disks, domain.RemovalDisk{Target: target, Path: "/fixture/" + name, PoolID: "11111111-2222-4333-8444-555555555555", VolumeName: name, VolumeKey: "/fixture/" + name, Generation: "fixture-generation-" + target, Fingerprint: strings.Repeat("d", 64), Format: "qcow2", CapacityBytes: 8 << 20, AllocatedBytes: 196608})
	}
	f := &diskRemovalFixture{removalFixture: base, s: s, inspection: r, states: []string{"present", "present"}, failAt: -1}
	s.Provider = f
	return s, f
}
func diskRemovalPlan(t *testing.T, s *Service, f *diskRemovalFixture) domain.Plan {
	t.Helper()
	p, e := s.planRemoval(context.Background(), 1000, Request{Connection: f.VM.Key.ConnectionID, ID: f.VM.Key.UUID, Action: "remove", Input: map[string]any{"deleteDisks": []string{"vdb", "vda"}}})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestDiskRemovalExplicitPlanAndAcknowledgement(t *testing.T) {
	s, f := diskRemovalService(t)
	p := diskRemovalPlan(t, s, f)
	if p.Operation != diskRemovalOperation || p.Review["diskDeletion"] != true || p.Review["backupsDeleted"] != false || !reflect.DeepEqual(p.Review["retainedSources"], []string{"/fixture/installer.iso"}) {
		t.Fatal(p.Review)
	}
	b, _ := json.Marshal(p)
	if e := validation.Schema("operation-plan", b); e != nil {
		t.Fatal(e)
	}
	acks := slices.DeleteFunc(slices.Clone(p.Acknowledgements), func(s string) bool { return s == "data-loss-delete-disks" })
	if _, e := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: "missing-data-loss", Acknowledgements: acks}); e == nil || f.removals.Load() != 0 {
		t.Fatal("missing explicit data-loss acknowledgement accepted")
	}
	j := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if j.State != "succeeded" || !reflect.DeepEqual(f.deleted, []int{0, 1}) {
		t.Fatal(j, f.deleted)
	}
	raw, e := s.Engine.Store.MetadataBytes(diskRemovalReceiptKind, j.ID)
	if e != nil {
		t.Fatal(e)
	}
	var r diskRemovalReceipt
	if wire.Decode(raw, &r) != nil || r.Definition != "removed" || !diskStatesAre(r.Disks, 2, "deleted") {
		t.Fatal(string(raw))
	}
}
func TestDiskRemovalUncertainEffectsRetainRemainingDisksAndNeverReplay(t *testing.T) {
	for _, fault := range []string{"definition-ack", "first-disk", "second-disk", "disk-ack"} {
		t.Run(fault, func(t *testing.T) {
			s, f := diskRemovalService(t)
			switch fault {
			case "definition-ack":
				f.lostAcknowledgement = true
			case "first-disk":
				f.failAt = 0
			case "second-disk":
				f.failAt = 1
			case "disk-ack":
				f.failAt = 0
				f.lostAck = true
			}
			p := diskRemovalPlan(t, s, f)
			j := awaitConfig(t, s, applyConfig(t, s, p).ID)
			if j.State != "recovery-required" {
				t.Fatal(j)
			}
			before := slices.Clone(f.deleted)
			states := slices.Clone(f.states)
			if _, e := s.Engine.Reconcile(context.Background(), j.ID); e == nil {
				t.Fatal("incomplete receipt reconciled as success")
			}
			if !reflect.DeepEqual(before, f.deleted) || !reflect.DeepEqual(states, f.states) {
				t.Fatal("reconciliation replayed effects")
			}
			if fault == "first-disk" && !diskStatesAre(states, 2, "present") {
				t.Fatal("remaining disk lost")
			}
			if fault == "second-disk" && !reflect.DeepEqual(states, []string{"absent", "present"}) {
				t.Fatal("partial files not retained")
			}
			var locks int
			if e := s.Engine.Store.DB.QueryRow("SELECT count(*) FROM locks WHERE job_id=?", j.ID).Scan(&locks); e != nil || locks == 0 {
				t.Fatal("uncertain deletion lost locks", e)
			}
		})
	}
}
func TestDiskRemovalReconcileCompletedReceiptAndReplacementRefusal(t *testing.T) {
	s, f := diskRemovalService(t)
	p := diskRemovalPlan(t, s, f)
	j := awaitConfig(t, s, applyConfig(t, s, p).ID)
	j.State = "recovery-required"
	if e := s.Engine.Store.Update(j, "fixture coordinator stopped before terminal acknowledgement"); e != nil {
		t.Fatal(e)
	}
	f.changed = true
	if _, e := s.Engine.Reconcile(context.Background(), j.ID); e == nil {
		t.Fatal("replacement accepted")
	}
	f.changed = false
	got, e := s.Engine.Reconcile(context.Background(), j.ID)
	if e != nil || got.State != "succeeded" || len(f.deleted) != 2 {
		t.Fatal(got, e)
	}
}
func TestDiskRemovalMetadataDependenciesBlockBeforeUndefine(t *testing.T) {
	for _, kind := range []string{"prepared-image", "snapshot", "backup", "template", "future-metadata"} {
		t.Run(kind, func(t *testing.T) {
			s, f := diskRemovalService(t)
			if e := s.Engine.Store.Put(kind, "reference", map[string]any{"source": f.inspection.Disks[0].Path}); e != nil {
				t.Fatal(e)
			}
			if _, e := s.planDiskRemoval(context.Background(), 1000, Request{Connection: f.VM.Key.ConnectionID, ID: f.VM.Key.UUID, Input: map[string]any{"deleteDisks": []string{"vda", "vdb"}}}); e == nil || f.removals.Load() != 0 {
				t.Fatal("referenced disk accepted")
			}
			removalNoPlans(t, s)
		})
	}
}
func TestDiskRemovalOwnAllocationHistoryIsNotLiveDependency(t *testing.T) {
	s, f := diskRemovalService(t)
	planID := "22222222-3333-4444-8555-666666666666"
	opID := "33333333-4444-4555-8666-777777777777"
	own := removalOwnership{Version: 1, Key: f.VM.Key, CreationPlanID: planID, OperationID: opID, Binding: strings.Repeat("e", 64)}
	receipt := removalCreationReceipt{Version: 1, PlanID: planID, OperationID: opID, Binding: own.Binding, VMID: f.VM.Key.UUID, Connection: f.VM.Key.ConnectionID, Defined: true, VolumesVerified: true}
	for _, d := range f.inspection.Disks {
		v := domain.CreatedVolume{Intent: domain.VolumeIntent{PoolID: d.PoolID, Name: d.VolumeName}, Path: d.Path, BackendKey: d.VolumeKey, Generation: d.Generation}
		own.Volumes = append(own.Volumes, v)
		receipt.Volumes = append(receipt.Volumes, removalVolumeProgress{Intent: v.Intent, Allocated: &v, Verified: true})
	}
	if e := s.Engine.Store.Put("created-vm", own.Key.String(), own); e != nil {
		t.Fatal(e)
	}
	if e := s.Engine.Store.Put("vm-creation", planID, receipt); e != nil {
		t.Fatal(e)
	}
	p := diskRemovalPlan(t, s, f)
	j := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
}
func TestDiskRemovalRejectsMalformedSelection(t *testing.T) {
	for _, value := range []any{true, []string{}, []string{"vda", "vda"}, []string{"/fixture/a.qcow2"}, []string{"*"}, []string{""}} {
		s, f := diskRemovalService(t)
		if _, e := s.planRemoval(context.Background(), 1000, Request{Connection: f.VM.Key.ConnectionID, ID: f.VM.Key.UUID, Input: map[string]any{"deleteDisks": value}}); e == nil {
			t.Fatal("invalid deletion accepted", value)
		}
		removalNoPlans(t, s)
	}
}
