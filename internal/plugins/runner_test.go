package plugins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTwoLanguageConfinedConformance(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_CONFORMANCE") != "1" {
		t.Skip("requires prebuilt Go fixture and explicit isolated sandbox test execution")
	}
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	for _, path := range []string{"../../build/sdk-summary", "../../tests/fixtures/plugins/python-summary"} {
		root, e := filepath.Abs(path)
		if e != nil {
			t.Fatal(e)
		}
		report, e := TestWorkspace(ctx, root, t.TempDir())
		if e != nil {
			t.Fatal(path, e)
		}
		if len(report.Checks) < 6 {
			t.Fatal(report)
		}
		t.Logf("%s: %v", report.PluginID, report.Checks)
	}
}

func TestProviderCrashReconcileFixture(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_CONFORMANCE") != "1" {
		t.Skip("requires prebuilt simulated provider")
	}
	exe, err := filepath.Abs("../../build/provider-fixture")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	start := func() *Session {
		s, e := Start(ctx, exe, dir)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); e != nil {
			s.Close()
			t.Fatal(e)
		}
		return s
	}
	s := start()
	if _, e := s.Call(ctx, "provider.apply", map[string]any{"operation": "create", "name": "synthetic", "operationID": "op-1", "idempotencyKey": "key-1", "partial": true}); e == nil {
		s.Close()
		t.Fatal("partial effect hidden")
	}
	s.Close()
	s = start()
	defer s.Close()
	result, e := s.Call(ctx, "provider.reconcile", map[string]any{"idempotencyKey": "key-1"})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(result), "fixture-op-1") {
		t.Fatal("stable effect identity lost", string(result))
	}
	result, e = s.Call(ctx, "provider.apply", map[string]any{"operation": "create", "name": "synthetic", "operationID": "op-1", "idempotencyKey": "key-1"})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(result), "fixture-op-1") {
		t.Fatal("idempotent retry changed identity")
	}
	if _, e = s.Call(ctx, "provider.plan", map[string]any{"operation": "backup"}); e == nil {
		t.Fatal("unsupported feature succeeded")
	}
	t.Log("simulated provider persisted partial fake effect, recovered after process restart, deduplicated and rejected backup; no actual VM or remote host")
}
