package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVersionedCaptureServiceChecksAllMembersAndKeepsNoRecoveryProof(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../tests/fixtures/protection/capture-manifest")); err != nil {
		t.Fatal(err)
	}
	s, p := recoveryReadOnlyService(t)
	request := Request{Path: filepath.Join(root, "manifest.json"), Input: map[string]any{"root": root}}
	response := s.Call(context.Background(), 1000, "backup.verify-manifest", request)
	if runtime.GOOS != "linux" {
		if response.Error == nil || response.Data != nil {
			t.Fatal(response)
		}
		return
	}
	report, ok := response.Data.(map[string]any)
	if response.Error != nil || !ok || report["manifestKind"] != "ColdRecoveryPoint" || report["manifestVersion"] != 1 || report["membersChecked"] != true || report["completeCaptureVerified"] != false || report["independentRecoveryVerified"] != false || report["bootTested"] != false || p.inspections != 0 {
		t.Fatal("versioned declarations became a capture or native observation", response)
	}
	if err := os.Remove(filepath.Join(root, "tpm.fixture")); err != nil {
		t.Fatal(err)
	}
	response = s.Call(context.Background(), 1000, "backup.verify-manifest", request)
	if response.Error == nil || response.Data != nil {
		t.Fatal("missing required auxiliary state retained successful verification", response)
	}
}
