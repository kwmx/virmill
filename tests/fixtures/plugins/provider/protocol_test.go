package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"virmill.local/sdk"
	"virmill.local/sdk/protocol"
)

// A test subprocess exercises the real SDK framing and fixture entry point. It
// receives only ordinary pipes and a fresh generated workspace, never a socket,
// native virtualization handle or host configuration path.
func TestProviderProcess(t *testing.T) {
	if os.Getenv("VIRMILL_PROVIDER_TEST_PROCESS") != "1" {
		return
	}
	main()
	os.Exit(0)
}

type boundedLog struct {
	mu       sync.Mutex
	data     []byte
	exceeded bool
}

func (b *boundedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(b.data)+n > 64<<10 {
		b.exceeded = true
		return 0, io.ErrShortWrite
	}
	b.data = append(b.data, p...)
	return n, nil
}

type processFixture struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	reader *bufio.Reader
	log    *boundedLog
	next   int
	done   bool
	cancel context.CancelFunc
}

func startProcess(t *testing.T, root string) *processFixture {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestProviderProcess$")
	cmd.Dir = root
	cmd.Env = []string{"VIRMILL_PROVIDER_TEST_PROCESS=1", "PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	log := &boundedLog{}
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	p := &processFixture{cmd: cmd, input: in, reader: bufio.NewReader(out), log: log, cancel: cancel}
	t.Cleanup(func() {
		if !p.done {
			p.input.Close()
			p.cmd.Process.Kill()
			p.cmd.Wait()
		}
		cancel()
	})
	return p
}
func (p *processFixture) call(t *testing.T, method string, params any) (json.RawMessage, *sdk.Error) {
	t.Helper()
	p.next++
	id := fmt.Sprintf("h-%d", p.next)
	if err := json.NewEncoder(p.input).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		t.Fatal(err)
	}
	frame, err := protocol.ReadFrame(p.reader)
	if err != nil {
		t.Fatal("bounded protocol child read", err)
	}
	if len(frame) > 64<<10 {
		t.Fatal("fixture response exceeded its test bound")
	}
	var response sdk.Message
	if err = protocol.Decode(frame, &response); err != nil {
		t.Fatal(err)
	}
	if response.JSONRPC != "2.0" || response.ID == nil || *response.ID != id || response.Method != "" || (len(response.Result) == 0) == (response.Error == nil) {
		t.Fatal("invalid correlated response", string(frame))
	}
	return response.Result, response.Error
}
func (p *processFixture) stop(t *testing.T, crash bool) {
	t.Helper()
	if crash {
		if err := p.cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
	} else {
		_, err := p.call(t, "shutdown", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
	}
	p.input.Close()
	err := p.cmd.Wait()
	p.done = true
	p.cancel()
	if !crash && err != nil {
		t.Fatal(err)
	}
	p.log.mu.Lock()
	defer p.log.mu.Unlock()
	if len(p.log.data) != 0 || p.log.exceeded {
		t.Fatal("unexpected child stderr", string(p.log.data))
	}
}
func TestProviderProtocolCrashRestartAndLegacyConformanceCalls(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	child := startProcess(t, root)
	_, rpc := child.call(t, "provider.inventory", map[string]any{})
	requireCode(t, rpc, "PERMISSION_DENIED")
	initialize := func(p *processFixture) {
		t.Helper()
		raw, err := p.call(t, "initialize", map[string]any{"protocolVersions": []string{"1.0"}})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(`"provider"`)) {
			t.Fatal(string(raw))
		}
	}
	initialize(child)
	description, descriptionErr := child.call(t, "describe", map[string]any{})
	if descriptionErr != nil || !bytes.Contains(description, []byte(`"inputSchema"`)) || !bytes.Contains(description, []byte(`"outputSchema"`)) {
		t.Fatal("missing provider schema description", string(description), descriptionErr)
	}
	_, rpc = child.call(t, "provider.unknown", map[string]any{})
	if rpc == nil || rpc.Code != -32601 {
		t.Fatal(rpc)
	}
	q := map[string]any{"operation": "create", "name": "synthetic", "operationID": "op-1", "idempotencyKey": "key-1", "partial": true}
	_, rpc = child.call(t, "provider.apply", q)
	requireCode(t, rpc, "PARTIAL_EFFECT")
	durable, err := os.ReadFile(filepath.Join(root, "provider-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	child.stop(t, true)
	child = startProcess(t, root)
	initialize(child)
	delete(q, "partial")
	_, rpc = child.call(t, "provider.apply", q)
	requireCode(t, rpc, "PARTIAL_EFFECT")
	unchanged, err := os.ReadFile(filepath.Join(root, "provider-state.json"))
	if err != nil || !bytes.Equal(durable, unchanged) {
		t.Fatal("restart retry changed partial history", err)
	}
	raw, rpc := child.call(t, "provider.reconcile", map[string]any{"idempotencyKey": "key-1"})
	if rpc != nil {
		t.Fatal(rpc)
	}
	var receipt Outcome
	if err = json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ID != "fixture-op-1" || receipt.Status != "partial" || !receipt.Reconciled || !receipt.Simulated {
		t.Fatal(receipt)
	}
	raw, rpc = child.call(t, "provider.apply", q)
	if rpc != nil {
		t.Fatal(rpc)
	}
	var again Outcome
	json.Unmarshal(raw, &again)
	if again != receipt {
		t.Fatal("legacy retry did not retain exact receipt")
	}
	_, rpc = child.call(t, "provider.plan", map[string]any{"operation": "backup"})
	requireCode(t, rpc, "UNSUPPORTED_CAPABILITY")
	child.stop(t, false)
}

func TestProviderProtocolInventoryGetAndLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	child := startProcess(t, root)
	_, rpc := child.call(t, "initialize", map[string]any{"protocolVersions": []string{"1.0"}})
	if rpc != nil {
		t.Fatal(rpc)
	}
	raw, rpc := child.call(t, "provider.capabilities", map[string]any{})
	if rpc != nil {
		t.Fatal(rpc)
	}
	var advertised struct {
		Capabilities Capabilities `json:"capabilities"`
		Simulated    bool         `json:"simulated"`
	}
	if err := json.Unmarshal(raw, &advertised); err != nil || !advertised.Simulated || advertised.Capabilities != capabilities() {
		t.Fatal("incorrect wire capabilities", string(raw), err)
	}
	for _, op := range []string{"wire-2", "wire-0", "wire-1"} {
		_, rpc = child.call(t, "provider.apply", map[string]any{"operation": "create", "name": op, "operationID": op, "idempotencyKey": "key-" + op})
		if rpc != nil {
			t.Fatal(rpc)
		}
	}
	raw, rpc = child.call(t, "provider.inventory", map[string]any{"limit": 2})
	if rpc != nil {
		t.Fatal(rpc)
	}
	var first Page
	if err := json.Unmarshal(raw, &first); err != nil || len(first.Resources) != 2 || first.NextCursor == nil {
		t.Fatal(string(raw), err)
	}
	raw, rpc = child.call(t, "provider.inventory", map[string]any{"limit": 2, "cursor": *first.NextCursor})
	if rpc != nil {
		t.Fatal(rpc)
	}
	var second Page
	if err := json.Unmarshal(raw, &second); err != nil || len(second.Resources) != 1 || second.NextCursor != nil {
		t.Fatal(string(raw), err)
	}
	if first.Resources[0].ID != "fixture-wire-0" || first.Resources[1].ID != "fixture-wire-1" || second.Resources[0].ID != "fixture-wire-2" {
		t.Fatal("wire inventory is incomplete or unordered", first, second)
	}
	id := first.Resources[0].ID
	raw, rpc = child.call(t, "provider.get", map[string]any{"id": id})
	if rpc != nil {
		t.Fatal(rpc)
	}
	var resource Resource
	if err := json.Unmarshal(raw, &resource); err != nil || resource != first.Resources[0] || resource.Key.ConnectionID != connection {
		t.Fatal("get differs from namespaced inventory", resource, err)
	}
	for _, operation := range []string{"start", "stop", "delete"} {
		raw, rpc = child.call(t, "provider.plan", map[string]any{"operation": operation, "id": id})
		if rpc != nil {
			t.Fatal(rpc)
		}
		var plan struct {
			PlanToken   string `json:"planToken"`
			ResultState string `json:"resultState"`
		}
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatal(err)
		}
		op := "wire-" + operation
		raw, rpc = child.call(t, "provider.apply", map[string]any{"operation": operation, "id": id, "operationID": op, "idempotencyKey": "key-" + op, "planToken": plan.PlanToken})
		if rpc != nil {
			t.Fatal(rpc)
		}
		var result Outcome
		if err := json.Unmarshal(raw, &result); err != nil || result.State != plan.ResultState || result.Status != "succeeded" {
			t.Fatal(result, err)
		}
		raw, rpc = child.call(t, "provider.status", map[string]any{"operationID": op})
		if rpc != nil {
			t.Fatal(rpc)
		}
		var status Outcome
		if err := json.Unmarshal(raw, &status); err != nil || status != result {
			t.Fatal("wire operation status differs", status, err)
		}
	}
	_, rpc = child.call(t, "provider.get", map[string]any{"id": id})
	requireCode(t, rpc, "RESOURCE_MISSING")
	_, rpc = child.call(t, "provider.inventory", map[string]any{"limit": 2, "cursor": *first.NextCursor})
	requireCode(t, rpc, "STALE_PLAN")
	_, rpc = child.call(t, "provider.cancel", map[string]any{"operationID": "wire-delete"})
	requireCode(t, rpc, "UNSUPPORTED_CAPABILITY")
	child.stop(t, false)
}
