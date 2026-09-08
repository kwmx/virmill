package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func referenceProviderManifest() Manifest {
	m := Manifest{ManifestVersion: "1", ID: "example.virmill.provider-fixture", Version: "0.2.0", ExtensionTypes: []string{"provider"}, Network: "none", Permissions: []Permission{}}
	m.Protocol.MinVersion, m.Protocol.MaxVersion, m.Protocol.Transport = "1.0", "1.0", "stdio-jsonrpc"
	return m
}

func providerTestJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestProviderConformanceRejectsBeforeStart(t *testing.T) {
	for name, change := range map[string]func(*Manifest){
		"mixed extensions": func(m *Manifest) { m.ExtensionTypes = []string{"provider", "action"} },
		"no extension":     func(m *Manifest) { m.ExtensionTypes = nil },
		"permission":       func(m *Manifest) { m.Permissions = []Permission{{Name: "vm.read", Scope: "*"}} },
		"network":          func(m *Manifest) { m.Network = "outbound" },
		"provider ID":      func(m *Manifest) { m.ID = "example.remote" },
		"old version":      func(m *Manifest) { m.Version = "0.1.0" },
		"manifest version": func(m *Manifest) { m.ManifestVersion = "2" },
		"protocol min":     func(m *Manifest) { m.Protocol.MinVersion = "0.9" },
		"protocol max":     func(m *Manifest) { m.Protocol.MaxVersion = "2.0" },
		"transport":        func(m *Manifest) { m.Protocol.Transport = "socket" },
	} {
		t.Run(name, func(t *testing.T) {
			m := referenceProviderManifest()
			change(&m)
			report, err := providerConformance(context.Background(), "/missing-provider-must-not-start", t.TempDir(), m)
			if err == nil || !strings.Contains(err.Error(), "supports only") || report.Confined || len(report.Checks) != 0 {
				t.Fatalf("invalid manifest reached execution or successful checks: %+v, %v", report, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := providerConformance(ctx, "/missing-provider-must-not-start", t.TempDir(), referenceProviderManifest())
	if !errors.Is(err, context.Canceled) || report.Confined || len(report.Checks) != 0 {
		t.Fatalf("cancellation reported successful checks: %+v, %v", report, err)
	}
}

func TestProviderCapabilityResultObjectOrder(t *testing.T) {
	// Capabilities comes from a fixture struct, whose nested field order differs
	// from the sorted map order in the expected semantic model.
	raw := json.RawMessage(`{"providerID":"fixture","connectionID":"fixture:///default","simulated":true,"capabilities":{"create":true,"start":true,"stop":true,"delete":true,"configurationEdits":false,"networks":false,"devices":false,"snapshots":false,"backups":false,"guestTransports":false,"cancel":false},"create":true,"backup":false,"usb":false,"cancel":false}`)
	if err := checkProviderCapabilityResult(raw); err != nil {
		t.Fatal("equivalent object ordering rejected:", err)
	}
	var reordered map[string]any
	if err := json.Unmarshal(raw, &reordered); err != nil {
		t.Fatal(err)
	}
	if err := checkProviderCapabilityResult(providerTestJSON(t, reordered)); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"wrong support":     strings.Replace(string(raw), `"backups":false`, `"backups":true`, 1),
		"null support":      strings.Replace(string(raw), `"cancel":false`, `"cancel":null`, 1),
		"missing support":   strings.Replace(string(raw), `"start":true,`, "", 1),
		"extra support":     strings.Replace(string(raw), `"create":true,`, `"extra":false,"create":true,`, 1),
		"duplicate support": strings.Replace(string(raw), `"create":true,`, `"create":true,"create":true,`, 1),
		"duplicate outer":   strings.Replace(string(raw), `"simulated":true,`, `"simulated":true,"simulated":true,`, 1),
		"not simulated":     strings.Replace(string(raw), `"simulated":true`, `"simulated":false`, 1),
		"other connection":  strings.Replace(string(raw), "fixture:///default", "ssh://remote", 1),
		"null result":       "null",
		"trailing document": string(raw) + "{}",
	} {
		t.Run(name, func(t *testing.T) {
			if checkProviderCapabilityResult(json.RawMessage(invalid)) == nil {
				t.Fatal("invalid capability result accepted")
			}
		})
	}
}

func TestProviderResourceIdentityAndCapabilities(t *testing.T) {
	if err := checkProviderResource(providerSampleResource()); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*providerFixtureResource){
		"provider namespace":   func(r *providerFixtureResource) { r.Key.ProviderID = "libvirt" },
		"connection namespace": func(r *providerFixtureResource) { r.Key.ConnectionID = "qemu:///system" },
		"resource kind":        func(r *providerFixtureResource) { r.Key.Kind = "network" },
		"external identity":    func(r *providerFixtureResource) { r.Key.ExternalID += "-different" },
		"foreign ID":           func(r *providerFixtureResource) { r.ID = "vm-schema" },
		"unsafe ID":            func(r *providerFixtureResource) { r.ID += "/../x"; r.Key.ExternalID = r.ID },
		"empty name":           func(r *providerFixtureResource) { r.Name = "" },
		"name bound":           func(r *providerFixtureResource) { r.Name = strings.Repeat("n", 129) },
		"zero revision":        func(r *providerFixtureResource) { r.Revision = 0 },
		"unknown state":        func(r *providerFixtureResource) { r.State = "complete" },
		"operation bound":      func(r *providerFixtureResource) { r.OperationID = strings.Repeat("x", 121) },
		"operation Unicode":    func(r *providerFixtureResource) { r.OperationID = "op-λ" },
		"missing capabilities": func(r *providerFixtureResource) { r.Capabilities = nil },
		"missing cancel":       func(r *providerFixtureResource) { delete(r.Capabilities, "cancel") },
		"invented backup":      func(r *providerFixtureResource) { r.Capabilities["backups"] = json.RawMessage("true") },
	} {
		t.Run(name, func(t *testing.T) {
			r := providerSampleResource()
			change(&r)
			if checkProviderResource(r) == nil {
				t.Fatal("invalid resource accepted")
			}
		})
	}
}

// These synthetic schemas isolate the declaration gate. Live fixture schemas
// and actual method payloads are exercised separately through confined Start.
func providerTestDescription(t *testing.T) providerDescription {
	t.Helper()
	d := providerDescription{ProviderID: "fixture", ConnectionID: "fixture:///default", Simulated: true, Capabilities: fixtureCapabilities(), Methods: map[string]providerMethodSchema{}}
	properties := map[string]any{}
	for _, name := range []string{"connectionID", "limit", "id", "name", "operation", "operationID", "idempotencyKey"} {
		properties[name] = true
	}
	input := providerTestJSON(t, map[string]any{"type": "object", "properties": properties, "additionalProperties": false})
	for _, method := range []string{"capabilities", "inventory", "get", "plan", "apply", "status", "reconcile", "cancel"} {
		// Require a field particular to each result, rejecting null/empty results.
		field := map[string]string{"capabilities": "capabilities", "inventory": "resources", "get": "id", "plan": "planToken", "apply": "status", "status": "status", "reconcile": "status"}[method]
		output := providerTestJSON(t, map[string]any{"type": "object", "required": []string{field}})
		if method == "cancel" {
			output = json.RawMessage("false")
		}
		d.Methods["provider."+method] = providerMethodSchema{Input: input, Output: output}
	}
	return d
}

func TestProviderDescriptionRequiresUsableSchemas(t *testing.T) {
	if _, err := checkProviderDescription(providerTestJSON(t, providerTestDescription(t))); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*providerDescription){
		"real provider":    func(d *providerDescription) { d.Simulated = false },
		"other provider":   func(d *providerDescription) { d.ProviderID = "remote" },
		"other connection": func(d *providerDescription) { d.ConnectionID = "fixture:///other" },
		"missing method":   func(d *providerDescription) { delete(d.Methods, "provider.status") },
		"unknown method":   func(d *providerDescription) { d.Methods["provider.unknown"] = d.Methods["provider.status"] },
		"missing schema":   func(d *providerDescription) { d.Methods["provider.get"] = providerMethodSchema{} },
		"permissive input": func(d *providerDescription) {
			s := d.Methods["provider.apply"]
			s.Input = json.RawMessage("true")
			d.Methods["provider.apply"] = s
		},
		"no usable input": func(d *providerDescription) {
			s := d.Methods["provider.get"]
			s.Input = json.RawMessage("false")
			d.Methods["provider.get"] = s
		},
		"empty output passes": func(d *providerDescription) {
			s := d.Methods["provider.status"]
			s.Output = json.RawMessage("true")
			d.Methods["provider.status"] = s
		},
		"null output passes": func(d *providerDescription) {
			s := d.Methods["provider.get"]
			s.Output = json.RawMessage(`{"anyOf":[{"type":"null"},{"type":"object","required":["id"]}]}`)
			d.Methods["provider.get"] = s
		},
		"missing output": func(d *providerDescription) {
			s := d.Methods["provider.get"]
			s.Output = nil
			d.Methods["provider.get"] = s
		},
		"cancel success": func(d *providerDescription) {
			s := d.Methods["provider.cancel"]
			s.Output = json.RawMessage("true")
			d.Methods["provider.cancel"] = s
		},
		"external schema": func(d *providerDescription) {
			s := d.Methods["provider.get"]
			s.Output = json.RawMessage(`{"$ref":"https://example.invalid/schema.json"}`)
			d.Methods["provider.get"] = s
		},
		"bad schema": func(d *providerDescription) {
			s := d.Methods["provider.get"]
			s.Output = json.RawMessage(`{"type":"unknown"}`)
			d.Methods["provider.get"] = s
		},
		"wrong capability": func(d *providerDescription) { d.Capabilities["backups"] = json.RawMessage("true") },
	} {
		t.Run(name, func(t *testing.T) {
			d := providerTestDescription(t)
			change(&d)
			if _, err := checkProviderDescription(providerTestJSON(t, d)); err == nil {
				t.Fatal("invalid description accepted")
			}
		})
	}
	for _, raw := range []string{"null", `{}`, strings.Repeat(" ", 512<<10+1), `{"providerID":"fixture","providerID":"fixture"}`, `{"unexpected":true}`} {
		if _, err := checkProviderDescription([]byte(raw)); err == nil {
			t.Fatal("malformed/incomplete description accepted")
		}
	}
}

func TestProviderRefusalsRequireTypedApplicationError(t *testing.T) {
	const valid = `{"code":-32010,"message":"partial fixture effect","data":{"code":"PARTIAL_EFFECT","retryable":false,"safeNextActions":[]}}`
	wrap := func(s string) error { return errors.New("plugin returned application error: " + s) }
	if err := expectedProviderError(wrap(valid), "PARTIAL_EFFECT"); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"success": nil, "transport": errors.New("EOF: PARTIAL_EFFECT"), "cancellation": context.Canceled,
		"wrong code":        wrap(strings.Replace(valid, "PARTIAL_EFFECT", "STALE_PLAN", 1)),
		"message only":      wrap(strings.Replace(valid, `"code":"PARTIAL_EFFECT"`, `"code":"OTHER"`, 1)),
		"wrong RPC code":    wrap(strings.Replace(valid, "-32010", "-32603", 1)),
		"missing retryable": wrap(strings.Replace(valid, `"retryable":false,`, "", 1)),
		"retryable true":    wrap(strings.Replace(valid, `"retryable":false`, `"retryable":true`, 1)),
		"null actions":      wrap(strings.Replace(valid, `"safeNextActions":[]`, `"safeNextActions":null`, 1)),
		"empty message":     wrap(strings.Replace(valid, "partial fixture effect", "", 1)),
		"duplicate code":    wrap(strings.Replace(valid, `"code":-32010,`, `"code":-32010,"code":-32010,`, 1)),
		"extra data":        wrap(strings.Replace(valid, `"retryable":false,`, `"unrecognized":true,"retryable":false,`, 1)),
		"oversized":         wrap(strings.Repeat(" ", 16<<10) + valid),
	} {
		t.Run(name, func(t *testing.T) {
			if expectedProviderError(err, "PARTIAL_EFFECT") == nil {
				t.Fatal("unproven refusal counted as passing conformance")
			}
		})
	}
}

func TestProviderHarnessBoundsBeforeSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &providerHarness{ctx: ctx}
	if _, err := h.call("initialize", map[string]any{}); !errors.Is(err, context.Canceled) || h.calls != 0 {
		t.Fatalf("canceled request advanced: %v, %d", err, h.calls)
	}
	h = &providerHarness{ctx: context.Background(), calls: 96}
	if _, err := h.call("initialize", map[string]any{}); err == nil || !strings.Contains(err.Error(), "call bound") {
		t.Fatalf("call bound not enforced: %v", err)
	}
	h = &providerHarness{ctx: context.Background(), description: providerTestDescription(t)}
	if _, err := h.call("provider.get", map[string]any{"unexpected": true}); err == nil || !strings.Contains(err.Error(), "input does not match") {
		t.Fatalf("invalid schema input reached session: %v", err)
	}
}

func TestProviderReferenceConfinedConformance(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_CONFORMANCE") != "1" {
		t.Skip("requires prebuilt build/provider-fixture and explicit confined fixture execution")
	}
	exe, err := filepath.Abs("../../build/provider-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(exe); err != nil {
		t.Fatal("prebuilt provider fixture required:", err)
	}
	workspace := t.TempDir()
	if err = os.Chmod(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Conformance exercises the same dispatch used by the shared plugin test
	// service, including the provider branch before action-specific Start.
	report, err := Conformance(ctx, exe, workspace, referenceProviderManifest())
	if err != nil {
		t.Fatalf("%v; completed checks: %v", err, report.Checks)
	}
	if report.PluginID != "example.virmill.provider-fixture" || report.EvidenceClass != "simulated-contract" || !report.Confined || len(report.Checks) != 9 {
		t.Fatalf("incorrect scope/proof report: %+v", report)
	}
	for _, claim := range []string{"status=partial", "reconciled=true", "without apply replay", "no real remote-provider or hardware qualification"} {
		if !strings.Contains(strings.Join(report.Checks, "\n"), claim) {
			t.Fatalf("report lost partial receipt or evidence boundary %q: %+v", claim, report)
		}
	}
	// Only generated private fixture JSON is read here. The durable state must
	// have exactly six logical effects despite dedup/restart/status/reconcile.
	path := filepath.Join(workspace, "provider-state.json")
	info, err := os.Stat(path)
	if err != nil || info.Size() > 4<<20 || info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected generated fixture state: %v, %v", info, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Version    int
		Generation uint64
		Resources  map[string]providerFixtureResource
		Operations map[string]struct {
			Status     string
			Reconciled bool
			Resource   providerFixtureResource
		}
		Keys map[string]string
	}
	if err = json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 || state.Generation != 6 || len(state.Resources) != 3 || len(state.Operations) != 6 || len(state.Keys) != 6 {
		t.Fatalf("logical effects replayed or receipts missing: %+v", state)
	}
	partial := state.Operations["conformance-partial"]
	if partial.Status != "partial" || !partial.Reconciled || partial.Resource.ID != "fixture-conformance-partial" || partial.Resource.Revision != 1 || !reflect.DeepEqual(partial.Resource, state.Resources[partial.Resource.ID]) {
		t.Fatalf("partial effect promoted or replayed: %+v", partial)
	}
	if state.Resources["fixture-conformance-create-a"].State != "deleted" || state.Resources["fixture-conformance-create-b"].State != "defined" {
		t.Fatal("lifecycle effects missing")
	}
	// A retained workspace cannot be mistaken for a fresh successful run.
	reused, err := providerConformance(ctx, exe, workspace, referenceProviderManifest())
	if err == nil || !strings.Contains(err.Error(), "fresh fixture workspace required") || len(reused.Checks) >= len(report.Checks) {
		t.Fatalf("reused workspace passed: %+v, %v", reused, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(b) {
		t.Fatal("refusing reused workspace changed retained simulated receipts", err)
	}
	t.Logf("%s: %v", report.PluginID, report.Checks)
}
