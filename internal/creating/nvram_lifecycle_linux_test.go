//go:build linux && amd64

package creating

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

// These wrappers extend the existing synthetic volume backend with complete
// declaration observations. They never open firmware/NVRAM or invoke libvirt.
// Keeping inspection on a separate wrapper also tests its absence at planning.
type nvramLifecycleCore struct {
	*fixtureBackend
	firmwareDigest string
	fingerprints   []string
	observations   int
	current        string
}

func (f *nvramLifecycleCore) PreflightCreation(ctx context.Context, uri string, spec domain.CreationSpec) (domain.CreationTarget, error) {
	target, err := f.fixtureBackend.PreflightCreation(ctx, uri, spec)
	f.mu.Lock()
	target.FirmwareDigest = f.firmwareDigest
	f.mu.Unlock()
	return target, err
}

func (f *nvramLifecycleCore) ObserveCreatedVM(ctx context.Context, uri string, target domain.CreationTarget, volumes []domain.CreatedVolume, binding string) (domain.VM, bool, error) {
	vm, present, err := f.fixtureBackend.ObserveCreatedVM(ctx, uri, target, volumes, binding)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observations++
	f.current = f.fingerprints[min(f.observations-1, len(f.fingerprints)-1)]
	vm.Key = domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: target.Spec.UUID}
	vm.Fingerprint = f.current
	return vm, present, err
}

type nvramLifecycleBackend struct {
	*nvramLifecycleCore
	paths             []string
	inspections       int
	inspectionErr     error
	inspectionStarted chan struct{}
	inspectionRelease chan struct{}
	inspectionBlockAt int
	beforeInspection  func(context.Context, int) error
}

func (f *nvramLifecycleBackend) InspectColdState(ctx context.Context, uri, id string) (domain.ColdStateInspection, error) {
	f.mu.Lock()
	f.inspections++
	call := f.inspections
	path := f.paths[min(call-1, len(f.paths)-1)]
	fingerprint, wanted := f.current, f.target.Spec.Firmware
	started, release, failure, before := f.inspectionStarted, f.inspectionRelease, f.inspectionErr, f.beforeInspection
	blockAt := max(1, f.inspectionBlockAt)
	f.mu.Unlock()
	if before != nil {
		if err := before(ctx, call); err != nil {
			return domain.ColdStateInspection{}, err
		}
	}
	if call == blockAt && started != nil {
		close(started)
		select {
		case <-ctx.Done():
			return domain.ColdStateInspection{}, ctx.Err()
		case <-release:
		}
	}
	if failure != nil {
		return domain.ColdStateInspection{}, failure
	}
	secure := "no"
	if wanted.SecureBoot {
		secure = "yes"
	}
	return domain.ColdStateInspection{
		Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id},
		State:    "stopped", Fingerprint: fingerprint,
		Layout: domain.ColdStateLayout{VMID: id, Firmware: domain.ColdFirmware{
			Loader: wanted.Code, LoaderType: "pflash", LoaderReadOnly: "yes", LoaderSecure: secure, LoaderFormat: wanted.Format,
			NVRAM: &domain.ColdNVRAM{Path: path, Format: wanted.Format, Template: wanted.Template, TemplateFormat: wanted.Format},
		}},
	}, nil
}

func nvramLifecycleFixture(t *testing.T) (*Service, *nvramLifecycleBackend, app.Request, string) {
	t.Helper()
	s, original, request, directory := creationFixture(t)
	f := &nvramLifecycleBackend{
		nvramLifecycleCore: &nvramLifecycleCore{fixtureBackend: original, firmwareDigest: strings.Repeat("f", 64), fingerprints: []string{strings.Repeat("a", 64)}},
		paths:              []string{"/synthetic/nvram/first.fd"},
	}
	s.Backend = f
	request.Connection = "qemu:///system"
	hardware := request.Input["hardware"].(map[string]any)
	hardware["firmware"] = map[string]any{"mode": "uefi", "code": "/synthetic/firmware/code.fd", "template": "/synthetic/firmware/template.fd", "format": "raw", "secureBoot": false, "tpm": true}
	return s, f, request, directory
}

func planNVRAMLifecycle(t *testing.T, s *Service, request app.Request) (domain.Plan, input, []byte) {
	t.Helper()
	p, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	stored, encoded, err := s.Store.Plan(p.ID)
	if err != nil || !same(stored, p) {
		t.Fatal("shared plan differs from journal", err)
	}
	var in input
	if err := wire.Decode(encoded, &in); err != nil {
		t.Fatal(err)
	}
	if in.NVRAMDeclarationVersion != 1 || p.Review["nvramDeclarationVersion"] != 1 || p.Review["nvramInitializationVerified"] != false {
		t.Fatal("new UEFI plan lacks explicit declaration-only policy", p.Review, in.NVRAMDeclarationVersion)
	}
	return p, in, encoded
}

func nvramLifecycleLocks(t *testing.T, s *Service, p domain.Plan, owner string) {
	t.Helper()
	for _, resource := range p.ResourceIDs {
		owners, err := s.Store.ResourceJobs(resource)
		if err != nil || owner == "" && len(owners) != 0 || owner != "" && (len(owners) != 1 || owners[0] != owner) {
			t.Fatalf("wrong retained lock for %s: %v, %v", resource, owners, err)
		}
	}
}

func nvramLifecycleEffects(t *testing.T, f *nvramLifecycleBackend) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allocated != 2 || f.populated != 2 || f.definitions != 1 || len(f.volumes) != 2 || len(f.verified) != 2 || !f.defined {
		t.Fatalf("effect replay or loss: allocations=%d uploads=%d definitions=%d retained=%d verified=%d defined=%t", f.allocated, f.populated, f.definitions, len(f.volumes), len(f.verified), f.defined)
	}
}

func nvramLifecycleResult(t *testing.T, s *Service, job domain.Job, complete, bound bool, status string) map[string]any {
	t.Helper()
	value, err := s.Result(context.Background(), 1000, job.ID)
	if (err == nil) != complete {
		t.Fatalf("result error differs from completion: complete=%t error=%v", complete, err)
	}
	result, ok := value.(map[string]any)
	if !ok || result["complete"] != complete || result["nvramDeclarationBound"] != bound || result["nvramDeclarationStatus"] != status {
		t.Fatal("declaration stage differs", result, err)
	}
	for _, key := range []string{"nvramInitializationVerified", "guestBootVerified", "setupVerified", "connectivityVerified"} {
		if result[key] != false {
			t.Fatal("unverified stage claimed", key, result)
		}
	}
	return result
}

func reopenNVRAMLifecycle(t *testing.T, s *Service) {
	t.Helper()
	var path string
	if err := s.Store.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	s.Engine.Close()
	if err := s.Store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Store = reopened
	s.Engine = operations.New(reopened)
	s.Engine.Handlers["vm.create"] = s
	s.Engine.Handlers["vm.create.devices-v1"] = s
	t.Cleanup(func() { s.Engine.Close(); reopened.Close() })
	if err := s.Engine.Recover(); err != nil {
		t.Fatal(err)
	}
}

func TestNVRAMLifecycleBindsBeforeDefinitionSuccessAndReusesRecord(t *testing.T) {
	s, f, request, _ := nvramLifecycleFixture(t)
	p, in, _ := planNVRAMLifecycle(t, s, request)
	// Enforce the ordering at the durable boundary, not just at final readback.
	for _, event := range []string{"INSERT", "UPDATE"} {
		statement := fmt.Sprintf(`CREATE TRIGGER nvram_before_defined_%s BEFORE %s ON metadata
WHEN NEW.kind='vm-creation' AND json_extract(CAST(NEW.body AS TEXT),'$.defined')=1
AND NOT EXISTS (SELECT 1 FROM metadata WHERE kind='creation-nvram-declaration' AND id=NEW.id)
BEGIN SELECT RAISE(ABORT,'defined receipt before durable NVRAM binding'); END`, event, event)
		if _, err := s.Store.DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	f.beforeInspection = func(ctx context.Context, call int) error {
		r, err := s.load(p.ID)
		if err != nil {
			return err
		}
		if call == 1 && r.Defined {
			return errors.New("definition marked complete before first inspection")
		}
		return nil
	}
	// A later same-mapping observation may have a new overall fingerprint;
	// the binding retains the first durably recorded observation fingerprint.
	f.fingerprints = []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	job := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	result := nvramLifecycleResult(t, s, job, true, true, "declaration-bound")
	binding := result["nvramDeclaration"].(*nvramDeclarationBinding)
	if binding.PlanID != p.ID || binding.InputDigest != p.InputDigest || binding.OperationID != job.ID || binding.FirmwareDigest != in.Target.FirmwareDigest || binding.Firmware.NVRAM.Path != f.paths[0] || binding.ObservedFingerprint != strings.Repeat("a", 64) {
		t.Fatal("wrong durable declaration identity", binding)
	}
	before, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, err := s.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if ok, err := s.Reconcile(context.Background(), p, encoded, p.Steps[0]); err != nil || !ok {
			t.Fatal("same declaration is not idempotent", ok, err)
		}
	}
	after, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("repeat changed historical record", err)
	}
	nvramLifecycleEffects(t, f)
	nvramLifecycleLocks(t, s, p, "")
}

func TestNVRAMLifecyclePlanRefusesMissingInspectionOrFirmwareDigest(t *testing.T) {
	for _, missing := range []string{"inspector", "firmware digest"} {
		t.Run(missing, func(t *testing.T) {
			s, f, request, _ := nvramLifecycleFixture(t)
			if missing == "inspector" {
				s.Backend = f.nvramLifecycleCore
			} else {
				f.firmwareDigest = ""
			}
			p, err := s.Plan(context.Background(), 1000, request)
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != "UNSUPPORTED_CAPABILITY" || p.ID != "" {
				t.Fatal("unsupported inspection plan accepted", p, err)
			}
			var plans int
			if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM plans").Scan(&plans); err != nil || plans != 0 {
				t.Fatal("failed preview persisted a plan", plans, err)
			}
			if f.allocated != 0 || f.populated != 0 || f.definitions != 0 || f.inspections != 0 {
				t.Fatal("failed preview performed creation or inspection")
			}
		})
	}
}

func TestNVRAMLifecycleLostDefinitionAcknowledgementReopensWithoutReplay(t *testing.T) {
	s, f, request, _ := nvramLifecycleFixture(t)
	f.fail = "ack"
	p, _, originalInput := planNVRAMLifecycle(t, s, request)
	job := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if job.State != "recovery-required" || f.inspections != 0 {
		t.Fatal("lost acknowledgement was silently completed", job)
	}
	nvramLifecycleResult(t, s, job, false, false, "pending")
	nvramLifecycleLocks(t, s, p, job.ID)
	reopenNVRAMLifecycle(t, s)
	// A canceled reconciliation may receive a valid inspection response, but
	// must not publish that response after the caller has withdrawn the request.
	ctx, cancel := context.WithCancel(context.Background())
	f.beforeInspection = func(context.Context, int) error { cancel(); return nil }
	if _, err := s.Engine.Reconcile(ctx, job.ID); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled reconciliation accepted returned observation", err)
	}
	if record, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID); err != nil || record != nil {
		t.Fatal("canceled reconciliation published declaration", err)
	}
	nvramLifecycleLocks(t, s, p, job.ID)
	f.beforeInspection = nil
	recovered, err := s.Engine.Reconcile(context.Background(), job.ID)
	if err != nil || recovered.State != "succeeded" {
		t.Fatal("exact observed definition did not recover", recovered, err)
	}
	stored, currentInput, err := s.Store.Plan(p.ID)
	if err != nil || stored.Digest != p.Digest || !bytes.Equal(originalInput, currentInput) {
		t.Fatal("recovery changed reviewed authority", err)
	}
	nvramLifecycleResult(t, s, recovered, true, true, "declaration-bound")
	nvramLifecycleEffects(t, f)
	nvramLifecycleLocks(t, s, p, "")
}

func TestNVRAMLifecycleChangedPathRetainsOriginalBindingAndLocks(t *testing.T) {
	s, f, request, _ := nvramLifecycleFixture(t)
	f.paths = []string{"/synthetic/nvram/first.fd", "/synthetic/nvram/replacement.fd"}
	f.fingerprints = []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
	p, _, _ := planNVRAMLifecycle(t, s, request)
	job := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if job.State != "recovery-required" || job.Error == nil || job.Error.Code != "SOURCE_CHANGED" {
		t.Fatal("replacement path completed creation", job)
	}
	before, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
	if err != nil || before == nil {
		t.Fatal("first binding lost", err)
	}
	result := nvramLifecycleResult(t, s, job, false, true, "declaration-bound")
	if result["nvramDeclaration"].(*nvramDeclarationBinding).Firmware.NVRAM.Path != f.paths[0] {
		t.Fatal("result rebound replacement path", result)
	}
	if _, err = s.Engine.Reconcile(context.Background(), job.ID); err == nil {
		t.Fatal("reconciliation adopted replacement path")
	}
	after, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed retry replaced first binding", err)
	}
	nvramLifecycleEffects(t, f)
	nvramLifecycleLocks(t, s, p, job.ID)
	// Once the original mapping is observed again, reconcile without effects.
	f.paths = []string{f.paths[0]}
	recovered, err := s.Engine.Reconcile(context.Background(), job.ID)
	if err != nil || recovered.State != "succeeded" {
		t.Fatal(recovered, err)
	}
	nvramLifecycleEffects(t, f)
	nvramLifecycleLocks(t, s, p, "")
}

func TestNVRAMLifecycleInspectionAndJournalFailureRetainResources(t *testing.T) {
	for _, failure := range []string{"inspection", "binding journal", "defined receipt journal"} {
		t.Run(failure, func(t *testing.T) {
			s, f, request, directory := nvramLifecycleFixture(t)
			original, err := os.ReadFile(filepath.Join(directory, "boot"))
			if err != nil {
				t.Fatal(err)
			}
			p, _, _ := planNVRAMLifecycle(t, s, request)
			switch failure {
			case "inspection":
				f.inspectionErr = errors.New("synthetic native inspection failed")
			case "binding journal":
				if _, err := s.Store.DB.Exec(`CREATE TRIGGER fail_nvram_binding BEFORE INSERT ON metadata WHEN NEW.kind='creation-nvram-declaration' BEGIN SELECT RAISE(ABORT,'synthetic binding journal failure'); END`); err != nil {
					t.Fatal(err)
				}
			case "defined receipt journal":
				if _, err := s.Store.DB.Exec(`CREATE TRIGGER fail_nvram_receipt BEFORE UPDATE ON metadata WHEN NEW.kind='vm-creation' AND json_extract(CAST(NEW.body AS TEXT),'$.defined')=1 BEGIN SELECT RAISE(ABORT,'synthetic defined receipt failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			job := awaitCreation(t, s, applyCreation(t, s, p).ID)
			if job.State != "recovery-required" {
				t.Fatal("failed declaration observation completed creation", job)
			}
			receipt, err := s.load(p.ID)
			if err != nil || receipt.Defined || !receipt.VolumesVerified {
				t.Fatal("definition completed without its declaration", receipt, err)
			}
			bound := failure == "defined receipt journal"
			beforeBinding, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
			if err != nil || (beforeBinding != nil) != bound {
				t.Fatal("wrong durable binding after failure", err)
			}
			status := "pending"
			if bound {
				status = "declaration-bound"
			}
			nvramLifecycleResult(t, s, job, false, bound, status)
			nvramLifecycleEffects(t, f)
			nvramLifecycleLocks(t, s, p, job.ID)
			after, err := os.ReadFile(filepath.Join(directory, "boot"))
			if err != nil || !bytes.Equal(original, after) {
				t.Fatal("failure changed original", err)
			}
			// A saved declaration survives the interrupted final receipt write;
			// reopen and reconcile the same definition after removing the fault.
			if bound {
				reopenNVRAMLifecycle(t, s)
				if _, err := s.Store.DB.Exec("DROP TRIGGER fail_nvram_receipt"); err != nil {
					t.Fatal(err)
				}
				recovered, err := s.Engine.Reconcile(context.Background(), job.ID)
				if err != nil || recovered.State != "succeeded" {
					t.Fatal("durable declaration did not survive receipt interruption", recovered, err)
				}
				afterBinding, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
				if err != nil || !bytes.Equal(beforeBinding, afterBinding) {
					t.Fatal("recovery replaced saved declaration", err)
				}
				nvramLifecycleEffects(t, f)
				nvramLifecycleLocks(t, s, p, "")
			}
		})
	}
}

func TestNVRAMLifecycleCancelDuringInspectionRetainsResources(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		blockAt int
		bound   bool
	}{
		{"operation cancel during execution", 1, false},
		{"operation cancel during verification", 2, true},
		{"coordinator shutdown", 1, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			s, f, request, _ := nvramLifecycleFixture(t)
			f.inspectionStarted, f.inspectionRelease = make(chan struct{}), make(chan struct{})
			f.inspectionBlockAt = tc.blockAt
			p, _, _ := planNVRAMLifecycle(t, s, request)
			job := applyCreation(t, s, p)
			select {
			case <-f.inspectionStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("inspection did not start")
			}
			bindingBefore, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
			if err != nil || (bindingBefore != nil) != tc.bound {
				t.Fatal("unexpected durable declaration at cancellation boundary", err)
			}
			if tc.mode != "coordinator shutdown" {
				if _, err := s.Engine.Cancel(job.ID); err != nil {
					t.Fatal(err)
				}
				close(f.inspectionRelease)
			} else {
				s.Engine.Close()
			}
			job = awaitCreation(t, s, job.ID)
			if job.State != "recovery-required" || tc.mode != "coordinator shutdown" && !job.CancelRequested {
				t.Fatal("canceled inspection completed creation", job)
			}
			status := "pending"
			if tc.bound {
				status = "declaration-bound"
			}
			nvramLifecycleResult(t, s, job, false, tc.bound, status)
			nvramLifecycleEffects(t, f)
			nvramLifecycleLocks(t, s, p, job.ID)
			bindingAfter, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
			if err != nil || !bytes.Equal(bindingBefore, bindingAfter) {
				t.Fatal("cancellation changed durable declaration", err)
			}
			if tc.mode == "coordinator shutdown" {
				reopenNVRAMLifecycle(t, s)
			}
			// Explicit recovery can observe despite a retained cancellation flag;
			// it must not resume allocation/upload/definition or invent freshness.
			recovered, err := s.Engine.Reconcile(context.Background(), job.ID)
			if err != nil || recovered.State != "succeeded" || recovered.CancelRequested != job.CancelRequested {
				t.Fatal("explicit reconciliation lost cancellation history or replayed work", recovered, err)
			}
			nvramLifecycleResult(t, s, recovered, true, true, "declaration-bound")
			nvramLifecycleEffects(t, f)
			nvramLifecycleLocks(t, s, p, "")
		})
	}
}

func TestNVRAMLifecycleJournalReopenPreservesRecipeVersionMeaning(t *testing.T) {
	for _, scenario := range []string{"legacy", "versioned bound", "versioned record missing"} {
		t.Run(scenario, func(t *testing.T) {
			s, f, request, _ := nvramLifecycleFixture(t)
			p, in, encoded := planNVRAMLifecycle(t, s, request)
			if scenario == "legacy" {
				in.NVRAMDeclarationVersion = 0
				in.Target.Spec.DevicePolicy = nil
				s.Backend = f.nvramLifecycleCore // Historical recipe needs no new inspector.
				steps := append([]domain.Step(nil), p.Steps...)
				steps[0].Action = "vm.create"
				var err error
				p, err = s.Engine.Plan(context.Background(), 1000, p.ConnectionID, "vm.create", p.ResourceIDs, p.Before, in, steps, p.Acknowledgements, p.Risks)
				if err != nil {
					t.Fatal(err)
				}
				_, encoded, err = s.Store.Plan(p.ID)
				if err != nil || bytes.Contains(encoded, []byte("nvramDeclarationVersion")) {
					t.Fatal("historical recipe unexpectedly acquired version field", err)
				}
			}
			job := awaitCreation(t, s, applyCreation(t, s, p).ID)
			if job.State != "succeeded" {
				t.Fatal(job.Error)
			}
			if scenario == "versioned record missing" {
				// Model an inconsistent/restored journal: a declared-complete v1
				// receipt must not authorize success without the separate record.
				if _, err := s.Store.DB.Exec("DELETE FROM metadata WHERE kind=? AND id=?", nvramDeclarationKind, p.ID); err != nil {
					t.Fatal(err)
				}
			}
			bindingBefore, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			receiptBefore, err := s.Store.MetadataBytes("vm-creation", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			reopenNVRAMLifecycle(t, s)
			status, complete, bound := "declaration-bound", true, true
			if scenario == "legacy" {
				status, bound = "legacy-unbound", false
				if f.inspections != 0 || bindingBefore != nil {
					t.Fatal("legacy execution acquired new authority")
				}
			} else if scenario == "versioned record missing" {
				status, complete, bound = "pending", false, false
			}
			nvramLifecycleResult(t, s, job, complete, bound, status)
			stored, afterInput, err := s.Store.Plan(p.ID)
			if err != nil || stored.Digest != p.Digest || !bytes.Equal(encoded, afterInput) {
				t.Fatal("reopen silently upgraded recipe", err)
			}
			for kind, before := range map[string][]byte{nvramDeclarationKind: bindingBefore, "vm-creation": receiptBefore} {
				after, err := s.Store.MetadataBytes(kind, p.ID)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("reopen/result changed historical metadata", kind, err)
				}
			}
			nvramLifecycleEffects(t, f)
		})
	}
}

func TestNVRAMLifecycleRecoveryRefusesPersistedPolicyDowngrade(t *testing.T) {
	for _, changed := range []string{"recipe version", "reviewed plan policy"} {
		t.Run(changed, func(t *testing.T) {
			s, f, request, _ := nvramLifecycleFixture(t)
			f.fail = "ack"
			p, in, originalInput := planNVRAMLifecycle(t, s, request)
			originalPlan, err := operations.Canonical(p)
			if err != nil {
				t.Fatal(err)
			}
			job := awaitCreation(t, s, applyCreation(t, s, p).ID)
			if job.State != "recovery-required" {
				t.Fatal(job)
			}
			receiptBefore, err := s.Store.MetadataBytes("vm-creation", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if changed == "recipe version" {
				in.NVRAMDeclarationVersion = 0
				corrupt, err := operations.Canonical(in)
				if err != nil {
					t.Fatal(err)
				}
				// Leave the plan, input digest and receipt binding untouched.
				if _, err := s.Store.DB.Exec("UPDATE plans SET input=? WHERE id=?", corrupt, p.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				p.Review["nvramDeclarationVersion"] = 0
				corrupt, err := operations.Canonical(p)
				if err != nil {
					t.Fatal(err)
				}
				// Leave the original plan digest and recipe untouched.
				if _, err := s.Store.DB.Exec("UPDATE plans SET body=? WHERE id=?", corrupt, p.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err = s.Engine.Reconcile(context.Background(), job.ID)
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != "SOURCE_CHANGED" {
				t.Fatal("stored policy downgrade reached reconciliation", err)
			}
			if f.observations != 0 || f.inspections != 0 {
				t.Fatal("changed recovery authority reached backend observation")
			}
			afterJob, err := s.Store.Job(job.ID)
			if err != nil || !same(job, afterJob) {
				t.Fatal("rejected downgrade changed uncertain operation", afterJob, err)
			}
			receiptAfter, err := s.Store.MetadataBytes("vm-creation", p.ID)
			if err != nil || !bytes.Equal(receiptBefore, receiptAfter) {
				t.Fatal("rejected downgrade changed retained receipt", err)
			}
			if record, err := s.Store.MetadataBytes(nvramDeclarationKind, p.ID); err != nil || record != nil {
				t.Fatal("rejected downgrade published a declaration", err)
			}
			nvramLifecycleEffects(t, f)
			nvramLifecycleLocks(t, s, p, job.ID)
			// Recovering the exact original persisted authority enables observation
			// again; no new creation plan or side effect is needed in this fixture.
			if _, err := s.Store.DB.Exec("UPDATE plans SET body=?,input=? WHERE id=?", originalPlan, originalInput, p.ID); err != nil {
				t.Fatal(err)
			}
			recovered, err := s.Engine.Reconcile(context.Background(), job.ID)
			if err != nil || recovered.State != "succeeded" {
				t.Fatal("original reviewed authority no longer reconciles", recovered, err)
			}
			nvramLifecycleResult(t, s, recovered, true, true, "declaration-bound")
			nvramLifecycleEffects(t, f)
			nvramLifecycleLocks(t, s, p, "")
		})
	}
}

func TestNVRAMLifecycleResultRefusesPersistedPolicyDowngrade(t *testing.T) {
	for _, changed := range []string{"recipe version", "reviewed plan policy"} {
		t.Run(changed, func(t *testing.T) {
			s, f, request, _ := nvramLifecycleFixture(t)
			p, in, originalInput := planNVRAMLifecycle(t, s, request)
			originalPlan, err := operations.Canonical(p)
			if err != nil {
				t.Fatal(err)
			}
			job := awaitCreation(t, s, applyCreation(t, s, p).ID)
			if job.State != "succeeded" {
				t.Fatal(job)
			}
			nvramLifecycleResult(t, s, job, true, true, "declaration-bound")
			metadataBefore, err := s.Store.MetadataRecords()
			if err != nil {
				t.Fatal(err)
			}
			eventsBefore, err := s.Store.Events(job.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			observations, inspections := f.observations, f.inspections
			f.mu.Unlock()
			if changed == "recipe version" {
				in.NVRAMDeclarationVersion = 0
				corrupt, err := operations.Canonical(in)
				if err != nil {
					t.Fatal(err)
				}
				// The existing v1 binding and original plan digest still exist;
				// changed recipe bytes cannot select legacy result semantics.
				if _, err := s.Store.DB.Exec("UPDATE plans SET input=? WHERE id=?", corrupt, p.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				p.Review["nvramDeclarationVersion"] = 0
				corrupt, err := operations.Canonical(p)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.Store.DB.Exec("UPDATE plans SET body=? WHERE id=?", corrupt, p.ID); err != nil {
					t.Fatal(err)
				}
			}
			changedPlan, changedInput, err := s.Store.Plan(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			value, err := s.Result(context.Background(), 1000, job.ID)
			var typed *domain.Error
			if value != nil || !errors.As(err, &typed) || typed.Code != "SOURCE_CHANGED" {
				t.Fatal("changed authority exposed a successful creation result", value, err)
			}
			afterJob, err := s.Store.Job(job.ID)
			if err != nil || !same(job, afterJob) {
				t.Fatal("rejected result changed completed operation", afterJob, err)
			}
			afterPlan, afterInput, err := s.Store.Plan(p.ID)
			if err != nil || !same(changedPlan, afterPlan) || !bytes.Equal(changedInput, afterInput) {
				t.Fatal("rejected result repaired or changed persisted authority", err)
			}
			metadataAfter, err := s.Store.MetadataRecords()
			if err != nil || !same(metadataBefore, metadataAfter) {
				t.Fatal("rejected result changed receipt, binding or ownership metadata", err)
			}
			eventsAfter, err := s.Store.Events(job.ID, 0)
			if err != nil || !same(eventsBefore, eventsAfter) {
				t.Fatal("rejected result changed operation history", err)
			}
			nvramLifecycleEffects(t, f)
			nvramLifecycleLocks(t, s, p, "")
			// Restore only the deliberately corrupted fixture bytes. The
			// existing binding must again support the original declared result.
			if _, err := s.Store.DB.Exec("UPDATE plans SET body=?,input=? WHERE id=?", originalPlan, originalInput, p.ID); err != nil {
				t.Fatal(err)
			}
			nvramLifecycleResult(t, s, job, true, true, "declaration-bound")
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.observations != observations || f.inspections != inspections {
				t.Fatal("result read reached native observation")
			}
		})
	}
}
