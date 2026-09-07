package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

type coldInspectionFixture struct {
	fixtureProvider
	uri, id       string
	inspections   int
	err           error
	onInspect     func()
	ignoreContext bool
}

func (p *coldInspectionFixture) InspectColdState(ctx context.Context, uri, id string) (domain.ColdStateInspection, error) {
	p.uri, p.id = uri, id
	p.inspections++
	if p.onInspect != nil {
		p.onInspect()
	}
	out := domain.ColdStateInspection{Resource: domain.ResourceKey{UUID: id}, State: "running", Warnings: []string{"synthetic configuration observation only"}}
	if p.err != nil {
		return out, p.err
	}
	if p.ignoreContext {
		return out, nil
	}
	return out, ctx.Err()
}

func TestColdInspectionUsesReadOnlyProviderContract(t *testing.T) {
	p := &coldInspectionFixture{}
	s := &Service{Provider: p}
	r := s.Call(context.Background(), 1000, "vm.recovery.inspect", Request{ID: "vm-id", Connection: "qemu:///session"})
	if r.Error != nil || p.uri != "qemu:///session" || p.id != "vm-id" || p.calls != 0 {
		t.Fatal(r, p)
	}
	for _, req := range []Request{{}, {ID: "vm-id", Action: "capture"}, {ID: "vm-id", Input: map[string]any{"copy": true}}} {
		if r := s.Call(context.Background(), 1000, "vm.recovery.inspect", req); r.Error == nil || r.Data != nil {
			t.Fatal("invalid inspection accepted", r)
		}
	}
	s.Provider = &fixtureProvider{}
	if r := s.Call(context.Background(), 1000, "vm.recovery.inspect", Request{ID: "vm-id"}); r.Error == nil || r.Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatal("unimplemented capture inspection invented", r)
	}
}

func recoveryServiceManifestFixture(t *testing.T) (string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	m := protection.Manifest{APIVersion: "virmill/v1", ID: "declared-backup", VMID: "declared-vm", Mode: "cold", Consistency: "cold-complete", IndependentlyRecoverable: true}
	for _, kind := range []string{"disk", "persistent-xml"} {
		data := []byte("synthetic declared " + kind)
		if err := os.WriteFile(filepath.Join(dir, kind), data, 0600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(data)
		m.Required = append(m.Required, kind)
		m.Members = append(m.Members, protection.Member{ID: kind, Kind: kind, Path: kind, Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])})
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, path, b
}

func TestManifestServiceNeverTurnsDeclaredIntegrityIntoRecoveryProof(t *testing.T) {
	dir, path, _ := recoveryServiceManifestFixture(t)
	s := &Service{}
	if runtime.GOOS != "linux" {
		r := s.Call(context.Background(), 1000, "backup.verify-manifest", Request{Path: path, Input: map[string]any{"root": dir}})
		if r.Error == nil || r.Error.Code != "UNSUPPORTED_CAPABILITY" || r.Data != nil {
			t.Fatal("unsupported platform returned verification", r)
		}
		return
	}
	for _, withRoot := range []bool{false, true} {
		rq := Request{Path: path}
		if withRoot {
			rq.Input = map[string]any{"root": dir}
		}
		r := s.Call(context.Background(), 1000, "backup.verify-manifest", rq)
		if r.Error != nil {
			t.Fatal(r.Error)
		}
		data, ok := r.Data.(map[string]any)
		if !ok || data["verification"] != "manifest-checked" || data["membersChecked"] != withRoot || data["completeCaptureVerified"] != false || data["independentRecoveryVerified"] != false || data["bootTested"] != false {
			t.Fatal("declared metadata became recovery proof", r)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "disk"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, rq := range []Request{{Path: path, Input: map[string]any{"root": dir}}, {Path: path, Input: map[string]any{"root": true}}, {Path: path, Input: map[string]any{"unexpected": true}}} {
		if r := s.Call(context.Background(), 1000, "backup.verify-manifest", rq); r.Error == nil || r.Data != nil {
			t.Fatal("failed verification returned success data", r)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := s.Call(ctx, 1000, "backup.verify-manifest", Request{Path: path}); r.Error == nil || r.Data != nil {
		t.Fatal("canceled read succeeded", r)
	}
}

func recoveryReadOnlyService(t *testing.T) (*Service, *coldInspectionFixture) {
	t.Helper()
	s, _, _ := configService(t)
	p := &coldInspectionFixture{}
	s.Provider = p
	t.Cleanup(func() {
		if p.calls != 0 {
			t.Error("recovery inspection performed a provider mutation")
		}
		for _, table := range []string{"plans", "jobs", "events", "dedup", "locks", "metadata"} {
			var count int
			if err := s.Engine.Store.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
				t.Errorf("recovery read changed %s: count=%d error=%v", table, count, err)
			}
		}
	})
	return s, p
}

func TestColdInspectionErrorsDiscardTypedPartialResults(t *testing.T) {
	for _, err := range []error{domain.Fail("UNSUPPORTED_CAPABILITY", "fixture native platform unsupported"), domain.Fail("INVALID_STATE", "fixture native XML incomplete"), errors.New("fixture native connection failed")} {
		s, p := recoveryReadOnlyService(t)
		p.err = err
		r := s.Call(context.Background(), 1000, "vm.recovery.inspect", Request{ID: "vm-id"})
		if r.Error == nil || r.Data != nil || p.inspections != 1 {
			t.Fatal("native error retained typed partial inspection data", r)
		}
		if d, ok := err.(*domain.Error); ok && r.Error.Code != d.Code {
			t.Fatal("native capability/state error code lost", r)
		}
	}
}

func TestColdInspectionCancellationAndInvalidRequestsNeverBecomeObservations(t *testing.T) {
	for _, phase := range []string{"before-provider", "during-provider"} {
		t.Run(phase, func(t *testing.T) {
			s, p := recoveryReadOnlyService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "before-provider" {
				cancel()
			} else {
				p.onInspect, p.ignoreContext = cancel, true
			}
			r := s.Call(ctx, 1000, "vm.recovery.inspect", Request{ID: "vm-id"})
			if r.Error == nil || r.Data != nil || !strings.Contains(r.Error.Message, "canceled") {
				t.Fatal("canceled inspection returned observation", r)
			}
			if phase == "before-provider" && p.inspections != 0 {
				t.Fatal("pre-canceled request reached provider")
			}
		})
	}
	for _, request := range []Request{
		{ID: "vm-id", Path: "unexpected"}, {ID: "vm-id", After: 1}, {ID: "vm-id", Apply: &operations.ApplyRequest{}},
		{ID: "vm-id", Connection: "qemu+ssh://unrelated.example/system"}, {ID: "vm-id", Connection: "test:///default"}, {ID: "vm-id", Connection: "qemu:///system?socket=/tmp/other"},
	} {
		s, p := recoveryReadOnlyService(t)
		r := s.Call(context.Background(), 1000, "vm.recovery.inspect", request)
		if r.Error == nil || r.Data != nil || p.inspections != 0 {
			t.Fatal("invalid recovery request reached native inspection", r)
		}
	}
}

func TestManifestServiceStrictInputAndCancellationHaveNoSuccessPayload(t *testing.T) {
	dir, filename, valid := recoveryServiceManifestFixture(t)
	for _, input := range []map[string]any{{"root": false}, {"root": ""}, {"root": nil}, {"unknown": true}} {
		s, p := recoveryReadOnlyService(t)
		r := s.Call(context.Background(), 1000, "backup.verify-manifest", Request{Path: filepath.Join(dir, "missing.json"), Input: input})
		if r.Error == nil || r.Error.Code != "INVALID_INPUT" || r.Data != nil || p.inspections != 0 {
			t.Fatal("invalid verification input produced success or accessed provider", r)
		}
	}
	for _, body := range []string{
		strings.Replace(string(valid), `"backupID":`, `"backupID":"duplicate","backupID":`, 1),
		strings.TrimSuffix(string(valid), "}") + `,"unknown":true}`,
	} {
		if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		s, _ := recoveryReadOnlyService(t)
		r := s.Call(context.Background(), 1000, "backup.verify-manifest", Request{Path: filename})
		if r.Error == nil || r.Data != nil {
			t.Fatal("shared service bypassed strict manifest reading", r)
		}
	}
	if err := os.WriteFile(filename, valid, 0600); err != nil {
		t.Fatal(err)
	}
	for _, withRoot := range []bool{false, true} {
		s, _ := recoveryReadOnlyService(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		request := Request{Path: filename}
		if withRoot {
			request.Input = map[string]any{"root": dir}
		}
		r := s.Call(ctx, 1000, "backup.verify-manifest", request)
		if r.Error == nil || r.Data != nil || !strings.Contains(r.Error.Message, "canceled") {
			t.Fatal("canceled declaration/member check returned success", r)
		}
	}
}
