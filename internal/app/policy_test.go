package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/wire"
)

func policyFile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../virmill-v1-spec/examples/backup-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestBackupPolicySharedValidationAndPreview(t *testing.T) {
	s := &Service{}
	path := policyFile(t)
	for _, method := range []string{"document.validate", "backup.policy.validate", "backup.policy.preview"} {
		r := Request{Path: path}
		if method == "backup.policy.preview" {
			r.Input = map[string]any{"after": "2026-09-08T00:00:00Z", "count": float64(2)}
		}
		out := s.Call(context.Background(), 1000, method, r)
		if out.Error != nil {
			t.Fatal(method, out.Error)
		}
		report, ok := out.Data.(protection.PolicyReport)
		if !ok || !report.Valid || report.ScheduleInstalled || report.CaptureVerified || report.IndependentRecoveryVerified || report.Schedule.Timezone != "Asia/Riyadh" {
			t.Fatal("invalid scope", out)
		}
		if method == "backup.policy.preview" && (len(report.NextRuns) != 2 || !report.NextRuns[0].Equal(time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC))) {
			t.Fatal("wrong actual occurrences", report)
		}
	}
}
func TestBackupPolicyFailuresHaveNoSuccessData(t *testing.T) {
	s := &Service{}
	path := policyFile(t)
	for _, input := range []map[string]any{{"count": 1.5}, {"count": float64(33)}, {"count": "2"}, {"after": "tomorrow"}, {"after": true}, {"secret": "must-not-echo"}} {
		out := s.Call(context.Background(), 1000, "backup.policy.preview", Request{Path: path, Input: input})
		if out.Error == nil || out.Error.Code != "INVALID_INPUT" || out.Data != nil || strings.Contains(out.Error.Error(), "must-not-echo") {
			t.Fatal("invalid preview response", out)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.Replace(string(b), "Asia/Riyadh", "Missing/Timezone", 1))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"document.validate", "backup.policy.validate", "backup.policy.preview"} {
		out := s.Call(context.Background(), 1000, method, Request{Path: path})
		if out.Error == nil || out.Data != nil {
			t.Fatal("bad timezone accepted", out)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out := s.Call(ctx, 1000, "backup.policy.preview", Request{Path: path}); out.Error == nil || out.Data != nil {
		t.Fatal("canceled preview accepted", out)
	}
}
func TestDeclarationRefusesOversizedFilesAndDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(wire.MaxFrame + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, p := range []string{path, filepath.Dir(path)} {
		if _, err := readDeclaration(context.Background(), p); err == nil {
			t.Fatal("unbounded or nonregular declaration accepted")
		}
	}
}
