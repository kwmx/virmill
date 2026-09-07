package protection

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func copiedCaptureFixture(t *testing.T) (string, string, []byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../tests/fixtures/protection/capture-manifest")); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "manifest.json")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return root, filename, data
}

func TestVersionedCaptureVerificationDoesNotAuthenticateSelfDeclarations(t *testing.T) {
	root, filename, _ := copiedCaptureFixture(t)
	for _, memberRoot := range []string{"", root} {
		report, err := CheckManifestContext(context.Background(), filename, memberRoot)
		if runtime.GOOS != "linux" {
			if err == nil || report != nil {
				t.Fatal("unsupported file reader claimed verification", report, err)
			}
			continue
		}
		if err != nil || report["manifestKind"] != "ColdRecoveryPoint" || report["manifestVersion"] != 1 || report["membersChecked"] != (memberRoot != "") {
			t.Fatal("versioned manifest did not use its strict contract", report, err)
		}
		for _, claim := range []string{"completeCaptureVerified", "independentRecoveryVerified", "bootTested"} {
			if report[claim] != false {
				t.Fatal("synthetic inventory became native recovery evidence", claim, report)
			}
		}
	}
	if runtime.GOOS != "linux" {
		return
	}
	m, err := ReadCaptureManifestContext(context.Background(), filename)
	if err != nil || m.ID == "" {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data.fixture"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyContext(context.Background(), root); err == nil {
		t.Fatal("second disk corruption was skipped")
	}
	if report, err := CheckManifestContext(context.Background(), filename, root); err == nil || report != nil {
		t.Fatal("failed member verification retained a success report", report, err)
	}
}

func TestVersionedCaptureDispatchCannotDowngradeToLegacy(t *testing.T) {
	_, filename, valid := copiedCaptureFixture(t)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(valid, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "kind")
	withoutKind, _ := json.Marshal(fields)
	for _, data := range [][]byte{
		withoutKind,
		[]byte(strings.Replace(string(valid), `"ColdRecoveryPoint"`, `"UnknownKind"`, 1)),
		[]byte(strings.Replace(string(valid), `"ColdRecoveryPoint"`, `null`, 1)),
		[]byte(strings.Replace(string(valid), `"version": 1`, `"version": 2`, 1)),
		[]byte(strings.Replace(string(valid), `"kind":`, `"kind":"ColdRecoveryPoint","kind":`, 1)),
		[]byte(strings.TrimSuffix(strings.TrimSpace(string(valid)), "}") + `,"unknown":true}`),
		[]byte("null"),
	} {
		if err := os.WriteFile(filename, data, 0600); err != nil {
			t.Fatal(err)
		}
		if report, err := CheckManifestContext(context.Background(), filename, ""); err == nil || report != nil {
			t.Fatal("ambiguous manifest produced weaker verification", report, err)
		}
		if m, err := ReadCaptureManifestContext(context.Background(), filename); err == nil || m.ID != "" {
			t.Fatal("failed strict read returned a partial manifest", m, err)
		}
	}
}

func TestVersionedCaptureCancellationDoesNotOpenOrReturnData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if report, err := CheckManifestContext(ctx, "/unopened/manifest.json", "/unopened/root"); err != context.Canceled || report != nil {
		t.Fatal(report, err)
	}
	if m, err := ReadCaptureManifestContext(ctx, "/unopened/manifest.json"); err != context.Canceled || m.ID != "" {
		t.Fatal(m, err)
	}
	if err := (CaptureManifest{}).VerifyContext(ctx, "/unopened/root"); err != context.Canceled {
		t.Fatal(err)
	}
}
