//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type acceptanceBackendFixture struct {
	*fixtureBackend
	lock             sync.Mutex
	observation      domain.CreationAcceptanceObservation
	reads, failRead  int
	started, release chan struct{}
}

func (f *acceptanceBackendFixture) InspectCreationAcceptance(ctx context.Context, uri string, original domain.CreationTarget, policy *domain.CreationDevicePolicy, volumes []domain.CreatedVolume, binding string) (domain.CreationAcceptanceObservation, error) {
	if err := ctx.Err(); err != nil {
		return domain.CreationAcceptanceObservation{}, err
	}
	f.mu.Lock()
	got := original
	got.Spec.DevicePolicy = policy
	valid := f.defined && same(got, f.target) && binding == f.binding
	f.mu.Unlock()
	if !valid {
		return domain.CreationAcceptanceObservation{}, errors.New("synthetic observed configuration differs")
	}
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.observation, nil
}
func (f *acceptanceBackendFixture) VerifyCreationAcceptance(ctx context.Context, uri string, target domain.CreationTarget, policy *domain.CreationDevicePolicy, volumes []domain.CreatedVolume, binding string, expected domain.CreationAcceptanceObservation) error {
	f.lock.Lock()
	f.reads++
	fail, wait, started := f.reads == f.failRead, f.release, f.started
	f.lock.Unlock()
	if wait != nil {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
		}
	}
	if fail {
		return errors.New("synthetic retained read failure")
	}
	got, err := f.InspectCreationAcceptance(ctx, uri, target, policy, volumes, binding)
	if err != nil {
		return err
	}
	if !same(got, expected) {
		return errors.New("synthetic stale acceptance observation")
	}
	for _, v := range volumes {
		if err := f.VerifyCreatedVolume(ctx, uri, v); err != nil {
			return err
		}
	}
	return nil
}

func uncertainAcceptance(t *testing.T) (*Service, *acceptanceHandler, *acceptanceBackendFixture, domain.Plan, domain.Job, app.Request) {
	t.Helper()
	s, backend, request, _ := creationFixture(t)
	p, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := s.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var in input
	if err = wire.Decode(b, &in); err != nil {
		t.Fatal(err)
	}
	in.Target.Spec.DevicePolicy = nil
	steps := slices.Clone(p.Steps)
	steps[0].Action = "vm.create"
	legacy, err := s.Engine.Plan(context.Background(), 1000, p.ConnectionID, "vm.create", p.ResourceIDs, p.Before, in, steps, p.Acknowledgements, p.Risks)
	if err != nil {
		t.Fatal(err)
	}
	backend.fail = "ack"
	parent := awaitCreation(t, s, applyCreation(t, s, legacy).ID)
	if parent.State != "recovery-required" {
		t.Fatal(parent)
	}
	policy, err := domain.DefaultCreationDevices(in.Target.Spec.Machine)
	if err != nil {
		t.Fatal(err)
	}
	policy.USBController, policy.MemoryBalloon, policy.WatchdogAction = "qemu-xhci", "virtio", "reset"
	backend.mu.Lock()
	backend.target.Spec.DevicePolicy = policy // Explicit synthetic unreviewed backend additions.
	backend.mu.Unlock()
	if _, err = s.Engine.Reconcile(context.Background(), parent.ID); err == nil {
		t.Fatal("legacy matcher accepted synthetic extra devices")
	}
	f := &acceptanceBackendFixture{fixtureBackend: backend, observation: domain.CreationAcceptanceObservation{VMFingerprint: strings.Repeat("a", 64), PersistentXMLSHA256: strings.Repeat("b", 64), TargetFingerprint: strings.Repeat("c", 64), VolumeFingerprints: []string{strings.Repeat("d", 64), strings.Repeat("e", 64)}}}
	h := &acceptanceHandler{s: s, backend: f}
	s.Engine.Handlers[acceptanceOperation] = h
	b, _ = json.Marshal(map[string]any{"devicePolicy": policy})
	var params map[string]any
	if err = json.Unmarshal(b, &params); err != nil {
		t.Fatal(err)
	}
	return s, h, f, legacy, parent, app.Request{Connection: "fixture", ID: parent.ID, Input: params}
}
func assertAcceptanceLocks(t *testing.T, s *Service, p domain.Plan, owner string) {
	t.Helper()
	for _, resource := range p.ResourceIDs {
		jobs, err := s.Store.ResourceJobs(resource)
		if err != nil || (owner == "" && len(jobs) != 0) || (owner != "" && (len(jobs) != 1 || jobs[0] != owner)) {
			t.Fatal("acceptance lock state differs", jobs, err)
		}
	}
}
func TestAcceptanceKeepsOriginalRecipeAndRegistersSeparateProof(t *testing.T) {
	s, h, f, original, parent, request := uncertainAcceptance(t)
	oldPlan, oldInput, _ := s.Store.Plan(original.ID)
	oldReceipt, _ := s.Store.MetadataBytes("vm-creation", original.ID)
	p, err := h.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	if f.reads != 0 || !p.Estimates.RequiresDowntime || p.Operation != acceptanceOperation || !slices.Contains(p.Acknowledgements, "watchdog-reset") {
		t.Fatal("preview read bytes or omitted explicit policy", p)
	}
	for _, key := range []string{"changesVM", "startsVM", "allocatesVolumes", "uploadsVolumes", "deletesFiles", "guestBootVerified"} {
		if p.Review[key] != false {
			t.Fatal("unexpected reviewed effect", key)
		}
	}
	for _, ack := range p.Acknowledgements {
		acks := slices.DeleteFunc(slices.Clone(p.Acknowledgements), func(s string) bool { return s == ack })
		if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: acks}); err == nil {
			t.Fatal("missing acknowledgement accepted", ack)
		}
	}
	child := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if child.State != "succeeded" {
		t.Fatal(child.Error)
	}
	parent, _ = s.Store.Job(parent.ID)
	if parent.State != "partial" || parent.RecoveryOperationID != child.ID {
		t.Fatal("parent incorrectly completed", parent)
	}
	assertAcceptanceLocks(t, s, p, "")
	newPlan, newInput, _ := s.Store.Plan(original.ID)
	newReceipt, _ := s.Store.MetadataBytes("vm-creation", original.ID)
	if !same(oldPlan, newPlan) || string(oldInput) != string(newInput) || string(oldReceipt) != string(newReceipt) {
		t.Fatal("old creation semantics rewritten")
	}
	result, err := s.Result(context.Background(), 1000, child.ID)
	if err != nil || result.(map[string]any)["complete"] != true || result.(map[string]any)["guestBootVerified"] != false {
		t.Fatal(result, err)
	}
	if _, err = s.Result(context.Background(), 1000, parent.ID); err == nil {
		t.Fatal("partial parent reported successful")
	}
	if _, err = s.Result(context.Background(), 1001, child.ID); err == nil {
		t.Fatal("wrong actor read acceptance")
	}
	receipt, _ := s.load(original.ID)
	vm := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "fixture", Kind: "vm", UUID: receipt.VMID}, Ownership: "external", PersistentXML: `<domain><uuid>` + receipt.VMID + `</uuid><metadata><v:creation xmlns:v="urn:virmill:v1" apiVersion="virmill/v1" binding="` + receipt.Binding + `"/></metadata></domain>`}
	managed, err := s.Ownership(context.Background(), vm)
	if err != nil || managed.Ownership != "managed" {
		t.Fatal(managed, err)
	}
	encodedRecord, _ := s.Store.MetadataBytes("created-vm", vm.Key.String())
	var record ownershipRecord
	if err = wire.Decode(encodedRecord, &record); err != nil {
		t.Fatal(err)
	}
	if record.Version != 2 || record.AcceptancePlanID != p.ID {
		t.Fatal("ownership lacks downgrade barrier or provenance")
	}
	record.Volumes[0].Path = "/synthetic/unrelated"
	if err = s.Store.Put("created-vm", vm.Key.String(), record); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ownership(context.Background(), vm); err == nil {
		t.Fatal("acceptance catalog volume substitution accepted")
	}
	if _, err = s.Result(context.Background(), 1000, child.ID); err == nil {
		t.Fatal("result accepted substituted catalog")
	}
	if err = s.Store.Put("created-vm", vm.Key.String(), json.RawMessage(encodedRecord)); err != nil {
		t.Fatal(err)
	}
	vm.PersistentXML = strings.Replace(vm.PersistentXML, receipt.Binding, strings.Repeat("0", 64), 1)
	if _, err = s.Ownership(context.Background(), vm); err == nil {
		t.Fatal("ownership ignored replaced identity")
	}
	if f.allocated != 2 || f.populated != 2 || f.definitions != 1 || f.reads != 2 {
		t.Fatal("acceptance replayed a host effect", f)
	}
	t.Log("synthetic coordinator only; no native VM, disk or guest qualification")
}

func TestAcceptanceStaleWrongActorAndReadFailureRetainLocks(t *testing.T) {
	for _, mode := range []string{"actor", "connection", "stale", "bytes", "old-handler", "source", "policy"} {
		t.Run(mode, func(t *testing.T) {
			s, h, f, _, parent, r := uncertainAcceptance(t)
			if mode == "actor" {
				if _, err := h.Plan(context.Background(), 1001, r); err == nil {
					t.Fatal("wrong actor accepted")
				}
				return
			}
			if mode == "connection" {
				r.Connection = "other"
				if _, err := h.Plan(context.Background(), 1000, r); err == nil {
					t.Fatal("wrong connection accepted")
				}
				return
			}
			if mode == "policy" {
				r.Input["devicePolicy"].(map[string]any)["watchdogAction"] = "none"
				if _, err := h.Plan(context.Background(), 1000, r); err == nil {
					t.Fatal("different device policy accepted")
				}
				return
			}
			p, err := h.Plan(context.Background(), 1000, r)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "stale":
				f.observation.VMFingerprint = strings.Repeat("f", 64)
			case "old-handler":
				delete(s.Engine.Handlers, acceptanceOperation)
			case "source":
				s.LoadSource = func(context.Context, *store.Store, uint32, string) (a importing.Artifact, d string, e error) {
					return a, "", errors.New("synthetic source replaced")
				}
			case "bytes":
				f.mu.Lock()
				for key := range f.volumes {
					f.volumes[key][0] = 'X'
				}
				f.mu.Unlock()
			}
			j, err := s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements})
			owner := parent.ID
			if mode == "bytes" {
				if err != nil {
					t.Fatal(err)
				}
				j = awaitCreation(t, s, j.ID)
				if j.State != "recovery-required" {
					t.Fatal(j)
				}
				owner = j.ID
				if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
					t.Fatal("missing verification proof reconciled")
				}
			} else if err == nil {
				t.Fatal("invalid acceptance accepted", mode)
			}
			assertAcceptanceLocks(t, s, p, owner)
			if f.definitions != 1 || f.allocated != 2 || f.populated != 2 {
				t.Fatal("host effect replayed")
			}
		})
	}
}

func TestAcceptanceInterruptedAfterProofReopensAndReconcilesWithoutDefinition(t *testing.T) {
	s, h, f, original, _, r := uncertainAcceptance(t)
	p, err := h.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	f.failRead = 2 // Proof persisted; verification before catalog commit fails.
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	assertAcceptanceLocks(t, s, p, j.ID)
	var seq int
	var name, path string
	if err = s.Store.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	s.Engine.Close()
	s.Store.Close()
	s.Store, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(s.Store)
	s.Engine.Handlers[acceptanceOperation] = h
	t.Cleanup(func() { s.Engine.Close(); s.Store.Close() })
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || got.State != "succeeded" {
		t.Fatal(got, err)
	}
	assertAcceptanceLocks(t, s, p, "")
	receipt, err := s.load(original.ID)
	if err != nil || receipt.Defined {
		t.Fatal("legacy receipt modified", receipt, err)
	}
	if f.reads != 3 || f.definitions != 1 || f.allocated != 2 || f.populated != 2 {
		t.Fatal("recovery replayed host effects")
	}
}

func TestAcceptanceCancellationAndFreshReviewInheritLocks(t *testing.T) {
	s, h, f, _, parent, r := uncertainAcceptance(t)
	p, err := h.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	f.started, f.release = make(chan struct{}), make(chan struct{})
	j := applyCreation(t, s, p)
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not start")
	}
	if _, err = s.Engine.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	j = awaitCreation(t, s, j.ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	assertAcceptanceLocks(t, s, p, j.ID)
	f.lock.Lock()
	f.release = nil
	f.lock.Unlock()
	r.ID = j.ID
	p2, err := h.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j2 := awaitCreation(t, s, applyCreation(t, s, p2).ID)
	if j2.State != "succeeded" || j2.RecoveryOf != j.ID {
		t.Fatal(j2)
	}
	ancestor, _ := s.Store.Job(parent.ID)
	if ancestor.State != "partial" || ancestor.RecoveryOperationID != j.ID {
		t.Fatal("ancestry lost", ancestor)
	}
}

func TestAcceptanceProofAndSchemaFailClosed(t *testing.T) {
	b, err := os.ReadFile("../../examples/creation/accept-retained-devices.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("vm-creation-accept-input", b); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, `{"devicePolicy":null}`, strings.Replace(string(b), `"version": 1`, `"version": 2`, 1), strings.Replace(string(b), `"audio": "none"`, `"audio": "none", "unknown": true`, 1)} {
		if validation.Schema("vm-creation-accept-input", []byte(bad)) == nil {
			t.Fatal("invalid acceptance schema accepted", bad)
		}
	}
	for _, field := range []string{"schemaVersion", "operationID", "inputDigest", "unknown"} {
		t.Run(field, func(t *testing.T) {
			s, h, f, _, _, r := uncertainAcceptance(t)
			p, err := h.Plan(context.Background(), 1000, r)
			if err != nil {
				t.Fatal(err)
			}
			f.failRead = 2
			j := awaitCreation(t, s, applyCreation(t, s, p).ID)
			b, err := s.Store.MetadataBytes("vm-creation-acceptance", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			var proof map[string]any
			if err = json.Unmarshal(b, &proof); err != nil {
				t.Fatal(err)
			}
			if field == "schemaVersion" {
				proof[field] = 2
			} else {
				proof[field] = "changed"
			}
			if err = s.Store.Put("vm-creation-acceptance", p.ID, proof); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
				t.Fatal("corrupt proof reconciled")
			}
			if _, err = s.Result(context.Background(), 1000, j.ID); err == nil {
				t.Fatal("corrupt proof exposed as valid result")
			}
			assertAcceptanceLocks(t, s, p, j.ID)
		})
	}
}

func TestActiveAcceptanceResultIsIncompleteAndDoesNotChangeLocks(t *testing.T) {
	for _, state := range []string{"queued", "validating", "running", "verifying"} {
		for _, present := range []bool{false, true} {
			t.Run(state+"/"+map[bool]string{false: "missing", true: "proof"}[present], func(t *testing.T) {
				s, h, f, _, parent, r := uncertainAcceptance(t)
				p, err := h.Plan(context.Background(), 1000, r)
				if err != nil {
					t.Fatal(err)
				}
				j, err := s.Store.AcceptRecovery(p, domain.ID(), "synthetic-acceptance", parent.ID)
				if err != nil {
					t.Fatal(err)
				}
				j.State = state
				if err = s.Store.Update(j, "synthetic progress"); err != nil {
					t.Fatal(err)
				}
				if present {
					_, b, _ := s.Store.Plan(p.ID)
					var in acceptanceInput
					if err = wire.Decode(b, &in); err != nil {
						t.Fatal(err)
					}
					proof := acceptanceProof{Version: 1, PlanID: p.ID, InputDigest: p.InputDigest, OperationID: j.ID, CreationPlanID: in.CreationPlanID, Observation: in.Observation}
					if err = s.Store.Put("vm-creation-acceptance", p.ID, proof); err != nil {
						t.Fatal(err)
					}
				}
				result, err := s.Result(context.Background(), 1000, j.ID)
				if err != nil {
					t.Fatal(err)
				}
				data := result.(map[string]any)
				if data["complete"] != false || data["acceptanceProofAvailable"] != present || data["guestBootVerified"] != false || f.reads != 0 {
					t.Fatal("progress fabricated completion or read bytes", data)
				}
				assertAcceptanceLocks(t, s, p, j.ID)
				if _, err = s.Engine.Reconcile(context.Background(), parent.ID); err == nil {
					t.Fatal("partial ancestor reconciled")
				}
			})
		}
	}
}

func TestAcceptanceCatalogCommitSurvivesFailedTerminalJournalWrite(t *testing.T) {
	s, h, f, _, _, r := uncertainAcceptance(t)
	p, err := h.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	// A SQL trigger models the exact durable boundary after ownership commit and
	// before the job can record success. It is not a real process/power-loss test.
	_, err = s.Store.DB.Exec(`CREATE TRIGGER fail_acceptance_finish BEFORE UPDATE ON jobs WHEN json_extract(NEW.body,'$.state')='succeeded' BEGIN SELECT RAISE(FAIL,'synthetic terminal journal failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	j := applyCreation(t, s, p)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		records, err := s.Store.Metadata("created-vm")
		if err != nil {
			t.Fatal(err)
		}
		if len(records) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	s.Engine.Close()
	j, err = s.Store.Job(j.ID)
	if err != nil || j.State != "verifying" {
		t.Fatal("fault missed terminal journal boundary", j, err)
	}
	result, err := s.Result(context.Background(), 1000, j.ID)
	if err != nil || result.(map[string]any)["complete"] != false {
		t.Fatal(result, err)
	}
	assertAcceptanceLocks(t, s, p, j.ID)
	if _, err = s.Store.DB.Exec("DROP TRIGGER fail_acceptance_finish"); err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(s.Store)
	s.Engine.Handlers[acceptanceOperation] = h
	t.Cleanup(s.Engine.Close)
	if err = s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
	j, err = s.Engine.Reconcile(context.Background(), j.ID)
	if err != nil || j.State != "succeeded" {
		t.Fatal(j, err)
	}
	if f.reads != 3 || f.definitions != 1 || f.populated != 2 || f.allocated != 2 {
		t.Fatal("catalog reconciliation repeated host effects")
	}
	assertAcceptanceLocks(t, s, p, "")
}
