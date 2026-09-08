package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"virmill.local/sdk"
)

func newFixture(t *testing.T) (*provider, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := openProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	return p, root
}
func request(t *testing.T, p *provider, method string, q Params) (any, error) {
	t.Helper()
	data, _ := json.Marshal(q)
	var all map[string]any
	json.Unmarshal(data, &all)
	for k, v := range all {
		if v == "" || v == false || v == float64(0) {
			delete(all, k)
		}
	}
	data, _ = json.Marshal(all)
	return p.handle(context.Background(), method, data)
}
func mustRequest(t *testing.T, p *provider, method string, q Params) any {
	t.Helper()
	out, err := request(t, p, method, q)
	if err != nil {
		t.Fatal(method, err)
	}
	return out
}
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var rpc *sdk.Error
	if !errors.As(err, &rpc) || rpc == nil || rpc.Data["code"] != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func created(t *testing.T, p *provider, op string) Outcome {
	t.Helper()
	return mustRequest(t, p, "apply", Params{Operation: "create", Name: "Synthetic " + op, OperationID: op, IdempotencyKey: "key-" + op}).(Outcome)
}
func snapshot(t *testing.T, root string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "provider-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestProviderLifecyclePlansCapabilitiesAndUnsupportedEffects(t *testing.T) {
	p, root := newFixture(t)
	advertised := mustRequest(t, p, "capabilities", Params{}).(map[string]any)
	if advertised["simulated"] != true || advertised["capabilities"] != capabilities() {
		t.Fatal(advertised)
	}
	before := copyState(p.state)
	plan := mustRequest(t, p, "plan", Params{Operation: "create", Name: "Synthetic"}).(map[string]any)
	if !reflect.DeepEqual(before, p.state) || plan["simulated"] != true || len(plan["effects"].([]string)) != 1 {
		t.Fatal("planning changed state", plan)
	}
	if _, err := os.Stat(filepath.Join(root, "provider-state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("planning wrote fixture state", err)
	}
	r := mustRequest(t, p, "apply", Params{Operation: "create", Name: "Synthetic", OperationID: "op-1", IdempotencyKey: "key-1", PlanToken: plan["planToken"].(string)}).(Outcome)
	if r.ID != "fixture-op-1" || r.State != "defined" || r.Key != (ResourceKey{"fixture", connection, "vm", r.ID}) || r.Capabilities != capabilities() {
		t.Fatal(r)
	}
	for _, unsupported := range []string{"backup", "snapshot", "usb", "network", "configure", "reboot", "pause", "restore"} {
		for _, method := range []string{"plan", "apply"} {
			_, err := request(t, p, method, Params{Operation: unsupported, ID: r.ID, OperationID: map[string]string{"apply": "unsupported"}[method], IdempotencyKey: map[string]string{"apply": "unsupported-key"}[method]})
			requireCode(t, err, "UNSUPPORTED_CAPABILITY")
		}
	}
	_, err := request(t, p, "cancel", Params{ID: r.ID})
	requireCode(t, err, "UNSUPPORTED_CAPABILITY")
	_, err = request(t, p, "apply", Params{Operation: "stop", ID: r.ID, OperationID: "too-early", IdempotencyKey: "too-early"})
	requireCode(t, err, "STALE_PLAN")
	for _, step := range []struct{ operation, state string }{{"start", "running"}, {"stop", "stopped"}, {"start", "running"}, {"stop", "stopped"}, {"delete", "deleted"}} {
		token := mustRequest(t, p, "plan", Params{Operation: step.operation, ID: r.ID}).(map[string]any)["planToken"].(string)
		op := fmt.Sprintf("op-%d", r.Revision+1)
		q := Params{Operation: step.operation, ID: r.ID, OperationID: op, IdempotencyKey: "key-" + op, PlanToken: token}
		r = mustRequest(t, p, "apply", q).(Outcome)
		if r.State != step.state || r.Status != "succeeded" {
			t.Fatal(r)
		}
		if repeat := mustRequest(t, p, "apply", q).(Outcome); repeat != r {
			t.Fatal("repeat changed receipt", repeat, r)
		}
		if r.State == "running" {
			_, err = request(t, p, "plan", Params{Operation: "delete", ID: r.ID})
			requireCode(t, err, "RESOURCE_BUSY")
		}
	}
	if page := mustRequest(t, p, "inventory", Params{}).(Page); len(page.Resources) != 0 || page.NextCursor != nil {
		t.Fatal("deleted resource leaked into live inventory", page)
	}
	_, err = request(t, p, "get", Params{ID: r.ID})
	requireCode(t, err, "RESOURCE_MISSING")
	status := mustRequest(t, p, "status", Params{OperationID: r.OperationID}).(Outcome)
	if status.State != "deleted" {
		t.Fatal("deleted receipt disappeared", status)
	}
	reopened, err := openProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRequest(t, reopened, "status", Params{ID: r.ID}).(Outcome); got != status {
		t.Fatal("restart changed deleted receipt")
	}
}

func TestProviderPaginationStableAcrossRestartAndRejectsForgedStaleCursors(t *testing.T) {
	p, root := newFixture(t)
	for _, i := range []int{3, 1, 4, 0, 2} {
		created(t, p, fmt.Sprintf("op-%d", i))
	}
	first := mustRequest(t, p, "inventory", Params{Limit: 2}).(Page)
	if len(first.Resources) != 2 || first.Resources[0].ID != "fixture-op-0" || first.NextCursor == nil {
		t.Fatal(first)
	}
	saved := snapshot(t, root)
	p, err := openProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	second := mustRequest(t, p, "inventory", Params{Limit: 2, Cursor: *first.NextCursor}).(Page)
	third := mustRequest(t, p, "inventory", Params{Limit: 2, Cursor: *second.NextCursor}).(Page)
	ids := []string{}
	for _, page := range []Page{first, second, third} {
		for _, r := range page.Resources {
			ids = append(ids, r.ID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"fixture-op-0", "fixture-op-1", "fixture-op-2", "fixture-op-3", "fixture-op-4"}) || third.NextCursor != nil || !bytes.Equal(saved, snapshot(t, root)) {
		t.Fatal("pagination skipped, duplicated, changed or persisted inventory", ids)
	}
	for _, q := range []Params{{Limit: 1, Cursor: *first.NextCursor}, {Limit: 2, Cursor: "forged"}, {Limit: 2, Cursor: (*first.NextCursor)[:len(*first.NextCursor)-1] + "X"}} {
		_, err = request(t, p, "inventory", q)
		var rpc *sdk.Error
		if !errors.As(err, &rpc) || rpc.Code != -32602 {
			t.Fatal("forged/mismatched cursor accepted", err)
		}
	}
	mustRequest(t, p, "reconcile", Params{OperationID: "op-0"})
	if next := mustRequest(t, p, "inventory", Params{Limit: 2, Cursor: *first.NextCursor}).(Page); !reflect.DeepEqual(next, second) {
		t.Fatal("receipt-only observation changed inventory generation")
	}
	created(t, p, "op-5")
	_, err = request(t, p, "inventory", Params{Limit: 2, Cursor: *first.NextCursor})
	requireCode(t, err, "STALE_PLAN")
}

func TestProviderPartialReceiptNeedsExplicitReconcileWithoutResourceMutation(t *testing.T) {
	p, root := newFixture(t)
	q := Params{Operation: "create", Name: "Partial synthetic", OperationID: "op-partial", IdempotencyKey: "partial-key", Partial: true}
	_, err := request(t, p, "apply", q)
	requireCode(t, err, "PARTIAL_EFFECT")
	saved := snapshot(t, root)
	resource := p.state.Resources["fixture-op-partial"]
	q.Partial = false
	_, err = request(t, p, "apply", q)
	requireCode(t, err, "PARTIAL_EFFECT")
	if !bytes.Equal(saved, snapshot(t, root)) {
		t.Fatal("retry promoted or replayed partial receipt")
	}
	_, err = request(t, p, "plan", Params{Operation: "start", ID: resource.ID})
	requireCode(t, err, "RECOVERY_REQUIRED")
	p, err = openProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	status := mustRequest(t, p, "status", Params{IdempotencyKey: q.IdempotencyKey}).(Outcome)
	if status.Status != "partial" || status.Reconciled {
		t.Fatal(status)
	}
	generation := p.state.Generation
	reconciled := mustRequest(t, p, "reconcile", Params{IdempotencyKey: q.IdempotencyKey}).(Outcome)
	if reconciled.Status != "partial" || !reconciled.Reconciled || p.state.Resources[resource.ID] != resource || p.state.Generation != generation {
		t.Fatal("reconcile changed resource or erased partial history", reconciled)
	}
	if again := mustRequest(t, p, "apply", q).(Outcome); again != reconciled {
		t.Fatal("deduplication changed reconciled receipt")
	}
	p, err = openProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	if again := mustRequest(t, p, "status", Params{OperationID: q.OperationID}).(Outcome); again != reconciled {
		t.Fatal("restart erased reconciliation evidence")
	}
}

func TestProviderIdentityReuseAndStalePlanNeverOverwrite(t *testing.T) {
	p, root := newFixture(t)
	r := created(t, p, "op-1")
	saved := snapshot(t, root)
	for _, q := range []Params{
		{Operation: "create", Name: "Changed", OperationID: "op-1", IdempotencyKey: "key-op-1"},
		{Operation: "create", Name: r.Name, OperationID: "op-2", IdempotencyKey: "key-op-1"},
		{Operation: "create", Name: r.Name, OperationID: "op-1", IdempotencyKey: "other-key"},
		{Operation: "start", ID: r.ID, OperationID: "op-1", IdempotencyKey: "key-op-1"},
	} {
		_, err := request(t, p, "apply", q)
		requireCode(t, err, "STALE_PLAN")
		if !bytes.Equal(saved, snapshot(t, root)) {
			t.Fatal("identity reuse modified state")
		}
	}
	token := mustRequest(t, p, "plan", Params{Operation: "start", ID: r.ID}).(map[string]any)["planToken"].(string)
	mustRequest(t, p, "apply", Params{Operation: "start", ID: r.ID, OperationID: "start-1", IdempotencyKey: "start-key"})
	mustRequest(t, p, "apply", Params{Operation: "stop", ID: r.ID, OperationID: "stop-1", IdempotencyKey: "stop-key"})
	saved = snapshot(t, root)
	_, err := request(t, p, "apply", Params{Operation: "start", ID: r.ID, OperationID: "start-2", IdempotencyKey: "other-start", PlanToken: token})
	requireCode(t, err, "STALE_PLAN")
	if !bytes.Equal(saved, snapshot(t, root)) {
		t.Fatal("stale plan performed effect")
	}
	_, err = request(t, p, "reconcile", Params{OperationID: "op-1"})
	requireCode(t, err, "SOURCE_CHANGED")
}

func TestProviderPersistenceLostAcknowledgementRequiresRestartWithoutReplay(t *testing.T) {
	p, root := newFixture(t)
	actual := p.persist
	p.persist = func(s State) error {
		if err := actual(s); err != nil {
			return err
		}
		return errors.New("generated lost persistence acknowledgement")
	}
	q := Params{Operation: "create", Name: "Durable synthetic", OperationID: "lost-op", IdempotencyKey: "lost-key"}
	_, err := request(t, p, "apply", q)
	requireCode(t, err, "RECOVERY_REQUIRED")
	saved := snapshot(t, root)
	_, err = request(t, p, "apply", q)
	requireCode(t, err, "RECOVERY_REQUIRED")
	if !bytes.Equal(saved, snapshot(t, root)) {
		t.Fatal("uncertain in-memory state replayed")
	}
	p, err = openProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	r := mustRequest(t, p, "reconcile", Params{OperationID: q.OperationID}).(Outcome)
	if r.ID != "fixture-lost-op" || !r.Reconciled || len(p.state.Resources) != 1 || p.state.Generation != 1 {
		t.Fatal(r)
	}
}

func TestProviderBoundedMalformedCanceledAndCorruptStateRefused(t *testing.T) {
	p, root := newFixture(t)
	created(t, p, "op-1")
	saved := snapshot(t, root)
	for _, raw := range []string{`null`, `[]`, `{"Limit":2}`, `{"limit":0}`, `{"limit":101}`, `{"limit":1,"limit":2}`, `{"limit":null}`, `{"cursor":"x","id":"fixture-op-1"}`, `{"unknown":true}`} {
		_, err := p.handle(context.Background(), "inventory", []byte(raw))
		var rpc *sdk.Error
		if !errors.As(err, &rpc) || rpc.Code != -32602 {
			t.Fatal(raw, err)
		}
	}
	for _, method := range []string{"get", "plan", "apply", "status", "reconcile"} {
		_, err := p.handle(context.Background(), method, []byte(`{}`))
		var rpc *sdk.Error
		if !errors.As(err, &rpc) || rpc.Code != -32602 {
			t.Fatal("missing parameters did not produce invalid-params", method, err)
		}
	}
	_, err := request(t, p, "get", Params{ConnectionID: "qemu:///system", ID: "fixture-op-1"})
	requireCode(t, err, "UNSUPPORTED_CAPABILITY")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.handle(ctx, "apply", []byte(`{"operation":"create","name":"Canceled","operationID":"op-2","idempotencyKey":"key-2"}`))
	requireCode(t, err, "CANCELED")
	if !bytes.Equal(saved, snapshot(t, root)) {
		t.Fatal("invalid or canceled input changed state")
	}
	for _, kind := range []string{"keys", "operation-digest", "version", "oversize", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			os.Chmod(dir, 0700)
			var s State
			if err := json.Unmarshal(saved, &s); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "keys":
				s.Keys["key-op-1"] = "other-op"
			case "operation-digest":
				op := s.Operations["op-1"]
				op.Intent.Name = "Changed"
				s.Operations["op-1"] = op
			case "version":
				s.Version = 2
			}
			data, _ := json.Marshal(s)
			if kind == "oversize" {
				data = []byte(strings.Repeat("x", stateLimit+1))
			}
			path := filepath.Join(dir, "provider-state.json")
			if kind == "symlink" {
				target := filepath.Join(dir, "generated-other.json")
				if err := os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := openProvider(dir); err == nil {
				t.Fatal("corrupt fixture state accepted")
			}
		})
	}
}

func TestProviderPartialLifecycleReceiptsRemainExplicitAcrossRestart(t *testing.T) {
	for _, operation := range []string{"start", "stop", "delete"} {
		t.Run(operation, func(t *testing.T) {
			p, root := newFixture(t)
			r := created(t, p, "create-1")
			if operation == "stop" {
				mustRequest(t, p, "apply", Params{Operation: "start", ID: r.ID, OperationID: "start-1", IdempotencyKey: "start-key"})
			}
			q := Params{Operation: operation, ID: r.ID, OperationID: "partial-1", IdempotencyKey: "partial-key", Partial: true}
			_, err := request(t, p, "apply", q)
			requireCode(t, err, "PARTIAL_EFFECT")
			after := p.state.Resources[r.ID]
			p, err = openProvider(root)
			if err != nil {
				t.Fatal(err)
			}
			q.Partial = false
			_, err = request(t, p, "apply", q)
			requireCode(t, err, "PARTIAL_EFFECT")
			result := mustRequest(t, p, "reconcile", Params{OperationID: q.OperationID}).(Outcome)
			if result.Status != "partial" || !result.Reconciled || result.Resource != after {
				t.Fatal("partial lifecycle effect was replaced", result)
			}
			if repeat := mustRequest(t, p, "apply", q).(Outcome); repeat != result {
				t.Fatal("reconciled lifecycle receipt changed")
			}
		})
	}
}

func TestProviderInventoryMeasurements10_100_500(t *testing.T) {
	for _, count := range []int{10, 100, 500} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			p, root := newFixture(t)
			for i := 0; i < count; i++ {
				created(t, p, fmt.Sprintf("vm-%04d", i))
			}
			stateBytes := len(snapshot(t, root))
			if stateBytes > stateLimit {
				t.Fatal("documented inventory cannot fit bounded state")
			}
			reopened, err := openProvider(root)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			cursorValue := ""
			seen := map[string]bool{}
			pages, responseBytes := 0, 0
			previous := ""
			for {
				page := mustRequest(t, reopened, "inventory", Params{Limit: 100, Cursor: cursorValue}).(Page)
				data, err := json.Marshal(page)
				if err != nil {
					t.Fatal(err)
				}
				responseBytes += len(data)
				pages++
				for _, r := range page.Resources {
					if seen[r.ID] || r.ID <= previous {
						t.Fatal("unstable or duplicated inventory", r.ID)
					}
					seen[r.ID] = true
					previous = r.ID
				}
				if page.NextCursor == nil {
					break
				}
				cursorValue = *page.NextCursor
				if pages > 10 {
					t.Fatal("unbounded pagination")
				}
			}
			elapsed := time.Since(start)
			if len(seen) != count {
				t.Fatal("incomplete inventory", len(seen), count)
			}
			t.Logf("simulated inventory resources=%d pages=%d elapsed=%s responseBytes=%d durableStateBytes=%d; direct fixture handler plus JSON encoding, no host/TUI/remote performance or release budget claim", count, pages, elapsed, responseBytes, stateBytes)
		})
	}
}
