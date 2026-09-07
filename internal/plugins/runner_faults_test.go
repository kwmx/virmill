//go:build linux && amd64

package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/wire"
)

// startFaultTransport is a TEST-ONLY process/pipe seam. It runs only the checked-in
// synthetic Python fixture, as the ordinary user. It bypasses the sandbox launch
// constructor, never production Session.Call, and gives no confinement evidence.
// TestPythonProtocolFaultConfined exercises the production constructor separately.
func startFaultTransport(t *testing.T) *Session {
	t.Helper()
	if os.Getuid() == 0 {
		t.Fatal("the test-only protocol peer must never run as root")
	}
	fixture := faultFixturePath(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-B", fixture)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=/usr/bin", "LANG=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
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
	s := &Session{stopped: ctx.Done(), cmd: cmd, input: input, frames: make(chan frameResult, 2), cleanup: func() {}, cancel: cancel, done: make(chan error, 1), log: log}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		reader := bufio.NewReader(output)
		for {
			data, err := wire.ReadFrame(reader)
			select {
			case s.frames <- frameResult{data: data, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { <-readerDone; s.done <- cmd.Wait() }()
	t.Cleanup(s.Close)
	return s
}

func faultFixturePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../tests/fixtures/plugins/protocol-faults/main.py")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func startFaultConfined(t *testing.T) *Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Start(ctx, faultFixturePath(t), workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func assertFaultWorkerExited(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(s.cmd.Process.Pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("failed protocol worker remains alive after the call returned")
}

func runPythonProtocolFaults(t *testing.T, start func(*testing.T) *Session) {
	for _, fault := range []string{"partial-exit", "unterminated-exit", "duplicate-key", "oversized-line", "wrong-id", "invalid-utf8", "stdout-log", "request-response-hybrid", "wrong-version", "unknown-notification", "stderr-flood"} {
		t.Run(fault, func(t *testing.T) {
			s := start(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err != nil {
				t.Fatal("fixture initialization:", err)
			}
			result, err := s.Call(ctx, "action.execute", map[string]any{"fixtureFault": fault})
			if err == nil || len(result) != 0 {
				t.Fatalf("fault produced success: result bytes=%d, error=%v", len(result), err)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("protocol violation only failed at the caller deadline")
			}
			t.Logf("observable protocol rejection: %v", err)
			assertFaultWorkerExited(t, s)
			if fault == "stderr-flood" {
				s.log.mu.Lock()
				logBytes := len(s.log.b)
				s.log.mu.Unlock()
				if logBytes != 64<<10 {
					t.Errorf("stderr retention: got %d bytes, want 64 KiB from a 256 KiB flood", logBytes)
				}
			}
			after, stop := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer stop()
			if result, err = s.Call(after, "ping", map[string]any{}); err == nil || len(result) != 0 {
				t.Errorf("poisoned session reused successfully: result bytes=%d, error=%v", len(result), err)
			}
		})
	}
	for _, fault := range []string{"negotiated-version-mismatch", "missing-protocol-version", "non-object-initialize-result"} {
		t.Run(fault, func(t *testing.T) {
			s := start(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}, "fixtureFault": fault})
			if err == nil || len(result) != 0 {
				t.Fatalf("invalid negotiation produced success: result bytes=%d, error=%v", len(result), err)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("invalid negotiation only failed at the caller deadline")
			}
			assertFaultWorkerExited(t, s)
			if result, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err == nil || len(result) != 0 {
				t.Errorf("invalid negotiation allowed session reuse: result bytes=%d, error=%v", len(result), err)
			}
		})
	}
	t.Run("host-call-before-initialize", func(t *testing.T) {
		s := start(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}, "fixtureFault": "apply-before-initialize"})
		if err == nil || len(result) != 0 {
			t.Errorf("host call before initialization left a usable session: result bytes=%d, error=%v", len(result), err)
		}
		assertFaultWorkerExited(t, s)
	})
	t.Run("read-scope-is-not-mutation-authority", func(t *testing.T) {
		s := start(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}, "permissions": []Permission{{Name: "vm.read", Scope: "selection"}}}); err != nil {
			t.Fatal(err)
		}
		result, err := s.Call(ctx, "action.plan", map[string]any{"fixtureFault": "apply-during-plan"})
		if err == nil || len(result) != 0 || !strings.Contains(err.Error(), `"fixtureDeniedMutation": true`) && !strings.Contains(err.Error(), `"fixtureDeniedMutation":true`) {
			t.Fatalf("fixture did not observe a correlated PERMISSION_DENIED host response: result bytes=%d, error=%v", len(result), err)
		}
		if _, err = s.Call(ctx, "ping", map[string]any{}); err != nil {
			t.Fatalf("valid application failure incorrectly poisoned the transport: %v", err)
		}
	})
	for _, method := range []string{"initialize", "action.execute"} {
		t.Run("deadline-"+method, func(t *testing.T) {
			s := start(t)
			if method != "initialize" {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			result, err := s.Call(ctx, method, map[string]any{"fixtureFault": "hang"})
			if !errors.Is(err, context.DeadlineExceeded) || len(result) != 0 {
				t.Fatalf("hung peer deadline: result bytes=%d, error=%v", len(result), err)
			}
			assertFaultWorkerExited(t, s)
		})
	}
	t.Run("cancel-active-execution", func(t *testing.T) {
		s := start(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err != nil {
			t.Fatal(err)
		}
		active, stop := context.WithCancel(ctx)
		defer stop()
		done := make(chan error, 1)
		go func() {
			result, err := s.Call(active, "action.execute", map[string]any{"fixtureFault": "hang"})
			if len(result) != 0 {
				err = errors.New("canceled execution returned a successful result")
			}
			done <- err
		}()
		waitFaultReady(t, s)
		stop()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("active cancellation: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("active cancellation did not complete promptly")
		}
		assertFaultWorkerExited(t, s)
	})
	t.Run("cancel-blocked-write", func(t *testing.T) {
		s := start(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}, "fixtureFault": "stop-reading"}); err != nil {
			t.Fatal(err)
		}
		waitFaultReady(t, s)
		blocked, stop := context.WithTimeout(ctx, 250*time.Millisecond)
		defer stop()
		result, err := s.Call(blocked, "action.execute", map[string]any{"padding": strings.Repeat("x", 1<<20)})
		if !errors.Is(err, context.DeadlineExceeded) || len(result) != 0 {
			t.Fatalf("blocked protocol write ignored deadline: result bytes=%d, error=%v", len(result), err)
		}
		assertFaultWorkerExited(t, s)
	})
	t.Run("close-flooding-stdout", func(t *testing.T) {
		s := start(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Call(ctx, "action.execute", map[string]any{"fixtureFault": "flood-after-reply"}); err != nil {
			t.Fatal(err)
		}
		began := time.Now()
		s.Close()
		if elapsed := time.Since(began); elapsed > time.Second {
			t.Errorf("closing a stdout-flooding worker took %v", elapsed)
		}
		assertFaultWorkerExited(t, s)
		if result, err := s.Call(ctx, "ping", map[string]any{}); err == nil || len(result) != 0 {
			t.Errorf("stdout after Close produced success: result bytes=%d, error=%v", len(result), err)
		}
	})
}

func waitFaultReady(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.log.mu.Lock()
		ready := strings.Contains(string(s.log.b), "FAULT_READY\n")
		s.log.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fixture did not reach the active execution boundary")
}

func TestPythonProtocolFaultTransport(t *testing.T) {
	t.Log("TEST-ONLY TRANSPORT SEAM: actual Python peer, production Call; no sandbox or host-security evidence")
	runPythonProtocolFaults(t, startFaultTransport)
}

func TestPythonProtocolFaultConfined(t *testing.T) {
	requireFaultSandbox(t)
	runPythonProtocolFaults(t, startFaultConfined)
}

func requireFaultSandbox(t *testing.T) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := platform.ProbeSandbox(ctx, workspace); err != nil {
		if os.Getenv("VIRMILL_TEST_CONFORMANCE") == "1" {
			t.Fatalf("required real confinement unavailable; no acceptance evidence: %v", err)
		}
		t.Skipf("real confinement unavailable; no acceptance evidence: %v", err)
	}
}

func TestPythonProtocolSandboxBoundaries(t *testing.T) {
	requireFaultSandbox(t)
	secret := filepath.Join(t.TempDir(), "undeclared-test-fixture")
	const content = "synthetic host-only fixture; never a real credential"
	if err := os.WriteFile(secret, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	s := startFaultConfined(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "initialize", map[string]any{"protocolVersions": []string{"1.0"}}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Call(ctx, "action.execute", map[string]any{"fixtureFault": "sandbox-observation", "hostFixturePath": secret})
	if err != nil {
		t.Fatal(err)
	}
	var observation struct {
		HostFileReadable     bool `json:"hostFileReadable"`
		SensitiveEnvironment bool `json:"sensitiveEnvironment"`
		UID                  int  `json:"uid"`
		Sockets              map[string]struct {
			Allowed bool `json:"allowed"`
			Errno   int  `json:"errno"`
		} `json:"sockets"`
	}
	if err := json.Unmarshal(result, &observation); err != nil {
		t.Fatal(err)
	}
	if observation.HostFileReadable || observation.SensitiveEnvironment || observation.UID == 0 {
		t.Fatalf("sandbox exposed undeclared file/environment or ran as root: %s", result)
	}
	for _, name := range []string{"inet", "unix"} {
		probe, ok := observation.Sockets[name]
		if !ok || probe.Allowed || probe.Errno != int(syscall.EPERM) {
			t.Errorf("sandbox did not deny %s socket creation with EPERM: %s", name, result)
		}
	}
	unchanged, err := os.ReadFile(secret)
	if err != nil || string(unchanged) != content {
		t.Fatal("generated host fixture changed", err)
	}
	t.Log("real confined Python worker: undeclared generated host file unavailable; AF_INET and AF_UNIX creation denied; no host connection or VM operation attempted")
}
