//go:build linux && amd64

package plugins

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

type closeCallResult struct {
	result json.RawMessage
	err    error
}

// These tests use a real synthetic Python peer and the production Session.Call
// and Session.Close methods through startFaultTransport's TEST-ONLY constructor.
// They establish lifecycle behavior, not sandbox or real worker-slot evidence.
func TestProtocolCloseInterruptsActiveCall(t *testing.T) {
	t.Log("TEST-ONLY TRANSPORT SEAM: active-call shutdown and process reaping; no confinement evidence")
	s := startFaultTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err != nil {
		t.Fatal(err)
	}
	callDone := make(chan closeCallResult, 1)
	go func() {
		result, err := s.Call(ctx, "action.execute", map[string]any{"fixtureFault": "hang"})
		callDone <- closeCallResult{result: result, err: err}
	}()
	waitFaultReady(t, s)
	closeDone := make(chan struct{})
	go func() {
		s.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		if ctx.Err() != nil {
			t.Error("Close depended on canceling the active call's own context")
		}
	case <-time.After(time.Second):
		t.Error("Close did not interrupt the active hung Call promptly")
		// Failure cleanup must not leave the intentionally hung peer behind.
		cancel()
		select {
		case <-closeDone:
		case <-time.After(2 * time.Second):
			t.Fatal("Close remained blocked after fallback cancellation")
		}
	}
	select {
	case completed := <-callDone:
		if completed.err == nil || len(completed.result) != 0 {
			t.Errorf("closing active execution produced success: result bytes=%d, error=%v", len(completed.result), completed.err)
		}
	case <-time.After(time.Second):
		t.Error("Close returned but the active Call remained blocked")
		cancel()
		select {
		case <-callDone:
		case <-time.After(2 * time.Second):
			t.Fatal("active Call remained blocked after fallback cancellation")
		}
	}
	assertFaultWorkerExited(t, s)
	assertProtocolCallsAfterCloseRejected(t, s)
}

func TestProtocolConcurrentCloseReleasesWorkerOnce(t *testing.T) {
	t.Log("TEST-ONLY TRANSPORT SEAM: concurrent Close, one cleanup callback, and process reaping; no confinement evidence")
	s := startFaultTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err != nil {
		t.Fatal(err)
	}
	var releases atomic.Int32
	cleanup := s.cleanup
	s.cleanup = func() {
		releases.Add(1)
		cleanup()
	}
	const closers = 8
	start := make(chan struct{})
	done := make(chan struct{}, closers)
	for range closers {
		go func() {
			<-start
			s.Close()
			done <- struct{}{}
		}()
	}
	close(start)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for range closers {
		select {
		case <-done:
		case <-deadline.C:
			t.Fatal("concurrent Close calls did not finish promptly")
		}
	}
	assertFaultWorkerExited(t, s)
	if got := releases.Load(); got != 1 {
		t.Errorf("concurrent Close released the test cleanup callback %d times, want once", got)
	}
	assertProtocolCallsAfterCloseRejected(t, s)
	s.Close()
	s.Close()
	if got := releases.Load(); got != 1 {
		t.Errorf("idempotent Close repeated test cleanup: got %d releases", got)
	}
}

func assertProtocolCallsAfterCloseRejected(t *testing.T, s *Session) {
	t.Helper()
	for _, method := range []string{"initialize", "ping", "action.execute"} {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		result, err := s.Call(ctx, method, map[string]any{"protocolVersions": []string{"1.0"}})
		if err == nil || len(result) != 0 {
			t.Errorf("%s succeeded after Close: result bytes=%d, error=%v", method, len(result), err)
		}
		if ctx.Err() != nil {
			t.Errorf("%s after Close waited for its deadline instead of refusing immediately", method)
		}
		cancel()
	}
}
