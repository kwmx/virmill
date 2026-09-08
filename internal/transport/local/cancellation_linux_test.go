//go:build linux

package local

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

func cancellationServer(t *testing.T) (*Server, Client, *operations.Engine) {
	t.Helper()
	dir, err := os.MkdirTemp("", "virmill-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	db, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	engine := operations.New(db)
	t.Cleanup(engine.Close)
	server, err := Listen(filepath.Join(dir, "control.sock"), app.New(nil, engine))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	done := make(chan error, 1)
	go func() { done <- server.Serve() }()
	t.Cleanup(func() {
		server.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return server, Client{Socket: server.Listener.Addr().String(), Timeout: time.Second}, engine
}

func TestHeavyImportDisconnectCancelsAndRefusesOverlappingScans(t *testing.T) {
	server, client, _ := cancellationServer(t)
	started := make(chan context.Context, 1)
	finished := make(chan struct{}, 1)
	var calls atomic.Int32
	server.Service.Extensions["import.prepare"] = func(ctx context.Context, _ uint32, _ app.Request) (any, error) {
		calls.Add(1)
		started <- ctx
		<-ctx.Done()
		finished <- struct{}{}
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { _, err := client.Call(ctx, "import.prepare", app.Request{}); returned <- err }()
	var requestContext context.Context
	select {
	case requestContext = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	deadline, ok := requestContext.Deadline()
	if !ok || time.Until(deadline) > 20*time.Minute || time.Until(deadline) < 19*time.Minute {
		t.Fatal("heavy request lacks bounded 20 minute context")
	}
	busy, err := client.Call(context.Background(), "import.prepare", app.Request{})
	if err != nil || busy.Error == nil || busy.Error.Code != "RESOURCE_BUSY" || calls.Load() != 1 {
		t.Fatalf("overlapping scan was not refused: %+v %v", busy, err)
	}
	version, err := client.Call(context.Background(), "version", app.Request{})
	if err != nil || version.Error != nil {
		t.Fatalf("inspection blocked ordinary requests: %+v %v", version, err)
	}
	cancel()
	select {
	case err := <-returned:
		var failure *domain.Error
		if !errors.As(err, &failure) || failure.Code != "CLIENT_INTERRUPTED" || !strings.Contains(failure.Message, "no job was submitted") {
			t.Fatalf("incorrect cancellation result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client cancellation did not interrupt socket wait")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("disconnected scan kept running")
	}
	until := time.Now().Add(time.Second)
	for len(server.heavyReads) != 0 && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if len(server.heavyReads) != 0 {
		t.Fatal("canceled scan retained the heavy-read slot")
	}
}

func TestDisconnectMonitorDoesNotConsumePipelinedFrames(t *testing.T) {
	server, client, _ := cancellationServer(t)
	server.Service.Extensions["import.prepare"] = func(context.Context, uint32, app.Request) (any, error) { return "inspected", nil }
	conn, err := net.Dial("unix", client.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	for _, request := range []Request{{JSONRPC: "2.0", ID: "first", Method: "import.prepare"}, {JSONRPC: "2.0", ID: "second", Method: "version"}} {
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(conn)
	for _, expected := range []string{"first", "second"} {
		frame, err := wire.ReadFrame(reader)
		if err != nil {
			t.Fatal(err)
		}
		var response reply
		if err := json.Unmarshal(frame, &response); err != nil {
			t.Fatal(err)
		}
		if response.ID != expected || response.Result == nil || response.Result.Error != nil {
			t.Fatalf("monitor consumed or changed frame: %+v", response)
		}
	}
}

func TestSocketTimeoutMessagesAndExplicitDeadline(t *testing.T) {
	for _, tc := range []struct{ method, contains string }{
		{"import.inspect", "no job was submitted"}, {"import.prepare", "no job was submitted"},
		{"import.prepare-install", "no job was submitted"}, {"import.prepare-disks", "no job was submitted"},
		{"operation.apply", "acceptance is uncertain"}, {"version", "no result was received"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "virmill-timeout-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "rpc.sock"), Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.AcceptUnix()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetReadDeadline(time.Now().Add(time.Second))
				_, _ = wire.ReadFrame(bufio.NewReader(conn))
				var one [1]byte
				_, _ = conn.Read(one[:])
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err = (Client{Socket: listener.Addr().String(), Timeout: time.Millisecond}).Call(ctx, tc.method, app.Request{})
			var failure *domain.Error
			if !errors.As(err, &failure) || failure.Code != "WAIT_TIMEOUT" || !strings.Contains(failure.Message, tc.contains) {
				t.Fatalf("wrong timeout: %v", err)
			}
			if time.Since(start) < 30*time.Millisecond {
				t.Fatal("transport overrode the explicit caller deadline")
			}
			<-done
		})
	}
}

type detachedTransportEffect struct {
	started chan context.Context
	release chan struct{}
}

func (*detachedTransportEffect) Validate(context.Context, domain.Plan, []byte) error { return nil }
func (h *detachedTransportEffect) Execute(ctx context.Context, _ domain.Plan, _ []byte, _ domain.Step) error {
	h.started <- ctx
	select {
	case <-h.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (*detachedTransportEffect) Reconcile(context.Context, domain.Plan, []byte, domain.Step) (bool, error) {
	return true, nil
}

func TestAcceptedJobSurvivesClientContextCancellation(t *testing.T) {
	_, client, engine := cancellationServer(t)
	handler := &detachedTransportEffect{started: make(chan context.Context, 1), release: make(chan struct{})}
	defer close(handler.release)
	engine.Handlers["fixture.detached"] = handler
	plan, err := engine.Plan(context.Background(), uint32(os.Getuid()), "local", "fixture.detached", []string{"fixture-resource"}, nil, map[string]any{}, []domain.Step{{ID: "step", Action: "fixture.detached"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	response, err := client.Call(ctx, "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "detached-socket-test"}})
	if err != nil || response.Error != nil {
		cancel()
		t.Fatalf("apply failed: %+v %v", response, err)
	}
	cancel()
	select {
	case jobContext := <-handler.started:
		select {
		case <-jobContext.Done():
			t.Fatal("client cancellation canceled accepted job")
		case <-time.After(120 * time.Millisecond):
		}
	case <-time.After(time.Second):
		t.Fatal("accepted job did not execute")
	}
}

type delayedAcceptanceEffect struct {
	validations atomic.Int32
	validating  chan context.Context
	release     chan struct{}
	executed    chan struct{}
}

func (h *delayedAcceptanceEffect) Validate(ctx context.Context, _ domain.Plan, _ []byte) error {
	if h.validations.Add(1) == 2 {
		h.validating <- ctx
		select {
		case <-h.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (h *delayedAcceptanceEffect) Execute(context.Context, domain.Plan, []byte, domain.Step) error {
	close(h.executed)
	return nil
}
func (*delayedAcceptanceEffect) Reconcile(context.Context, domain.Plan, []byte, domain.Step) (bool, error) {
	return true, nil
}

func TestApplyWaitTimeoutPreservesCoordinatorAcceptanceContext(t *testing.T) {
	_, client, engine := cancellationServer(t)
	handler := &delayedAcceptanceEffect{validating: make(chan context.Context, 1), release: make(chan struct{}), executed: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(handler.release)
		}
	}()
	engine.Handlers["fixture.accept-later"] = handler
	plan, err := engine.Plan(context.Background(), uint32(os.Getuid()), "local", "fixture.accept-later", []string{"fixture-later"}, nil, map[string]any{}, []domain.Step{{ID: "step", Action: "fixture.accept-later"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		_, err := client.Call(ctx, "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: plan.ID, PlanDigest: plan.Digest, IdempotencyKey: "late-acceptance-test"}})
		returned <- err
	}()
	var acceptanceContext context.Context
	select {
	case acceptanceContext = <-handler.validating:
	case <-time.After(time.Second):
		t.Fatal("apply did not reach validation")
	}
	select {
	case err := <-returned:
		var failure *domain.Error
		if !errors.As(err, &failure) || failure.Code != "WAIT_TIMEOUT" || !strings.Contains(failure.Message, "acceptance is uncertain") {
			t.Fatalf("incorrect acceptance timeout: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not time out")
	}
	if acceptanceContext.Err() != nil {
		t.Fatal("disconnect canceled coordinator-owned acceptance")
	}
	if _, bounded := acceptanceContext.Deadline(); bounded {
		t.Fatal("apply inherited the read-only request deadline")
	}
	close(handler.release)
	released = true
	select {
	case <-handler.executed:
	case <-time.After(time.Second):
		t.Fatal("reviewed operation did not continue after client wait timed out")
	}
}
