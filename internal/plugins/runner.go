package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/wire"
)

type boundedLog struct {
	mu sync.Mutex
	b  []byte
}

func (b *boundedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := (64 << 10) - len(b.b)
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.b = append(b.b, p...)
	}
	return n, nil
}

type frameResult struct {
	data []byte
	err  error
}

var workerSlots = make(chan struct{}, 4)

type Session struct {
	cmd         *exec.Cmd
	input       io.WriteCloser
	frames      chan frameResult
	cleanup     func()
	cancel      context.CancelFunc
	mu          sync.Mutex
	sequence    int
	closed      bool
	terminalErr error
	stopped     <-chan struct{}
	closeOnce   sync.Once
	initialized bool
	done        chan error
	log         *boundedLog
}

func Start(ctx context.Context, exe, workspace string) (*Session, error) {
	return startSession(ctx, exe, workspace, "", "")
}

func startPackage(ctx context.Context, directory, entrypoint, workspace string) (*Session, error) {
	return startSession(ctx, "", workspace, directory, entrypoint)
}

func startSession(ctx context.Context, exe, workspace, directory, entrypoint string) (*Session, error) {
	select {
	case workerSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := true
	defer func() {
		if release {
			<-workerSlots
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	var cmd *exec.Cmd
	var cleanup func()
	var e error
	if directory != "" {
		cmd, cleanup, e = platform.ConfinedPackageCommand(ctx, directory, entrypoint, workspace)
	} else {
		cmd, cleanup, e = platform.ConfinedCommand(ctx, exe, workspace, nil)
	}
	if e != nil {
		cancel()
		return nil, e
	}
	input, e := cmd.StdinPipe()
	if e != nil {
		cleanup()
		cancel()
		return nil, e
	}
	output, e := cmd.StdoutPipe()
	if e != nil {
		cleanup()
		cancel()
		return nil, e
	}
	log := &boundedLog{}
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		cleanup()
		cancel()
		return nil, e
	}
	cleanupWorker := func() { cleanup(); <-workerSlots }
	s := &Session{stopped: ctx.Done(), cmd: cmd, input: input, frames: make(chan frameResult, 2), cleanup: cleanupWorker, cancel: cancel, done: make(chan error, 1), log: log}
	release = false
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		r := bufio.NewReader(output)
		for {
			b, e := wire.ReadFrame(r)
			select {
			case s.frames <- frameResult{b, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	// StdoutPipe must be drained before Wait closes its read descriptor.
	go func() { <-readerDone; s.done <- cmd.Wait() }()
	return s, nil
}
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		// Wake Call before taking its serialization mutex. Reader shutdown may
		// drop EOF after context cancellation, so Call observes stopped directly.
		s.cancel()
		_ = s.input.Close()
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
		}
		s.cleanup()
	})
}

// failLocked permanently retires a transport after an ambiguous or invalid wire
// exchange. Close still owns cleanup and releasing the worker slot.
func (s *Session) failLocked(err error) (json.RawMessage, error) {
	s.terminalErr = err
	s.cancel()
	_ = s.input.Close()
	return nil, err
}

func (s *Session) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("plugin session closed")
	}
	if s.terminalErr != nil {
		return nil, fmt.Errorf("plugin session failed: %w", s.terminalErr)
	}
	select {
	case <-s.stopped:
		return s.failLocked(errors.New("plugin session stopped"))
	default:
	}
	if !s.initialized && method != "initialize" {
		return nil, errors.New("plugin initialization required")
	}
	if s.initialized && method == "initialize" {
		return nil, errors.New("plugin already initialized")
	}
	s.sequence++
	id := fmt.Sprintf("h-%d", s.sequence)
	b, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if e != nil {
		return nil, e
	}
	if len(b) > wire.MaxFrame {
		return nil, errors.New("request size limit")
	}
	if e = s.write(ctx, append(b, '\n')); e != nil {
		return s.failLocked(e)
	}
	for {
		select {
		case <-ctx.Done():
			return s.failLocked(ctx.Err())
		case <-s.stopped:
			return s.failLocked(errors.New("plugin session stopped"))
		case result := <-s.frames:
			if result.err != nil {
				return s.failLocked(fmt.Errorf("plugin process/framing failure: %w", result.err))
			}
			frame := result.data
			if e = wire.Validate(frame); e != nil {
				return s.failLocked(e)
			}
			var response struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      *string         `json:"id"`
				Method  string          `json:"method"`
				Params  json.RawMessage `json:"params"`
				Result  json.RawMessage `json:"result"`
				Error   json.RawMessage `json:"error"`
			}
			if e = wire.Decode(frame, &response); e != nil {
				return s.failLocked(e)
			}
			if response.JSONRPC != "2.0" {
				return s.failLocked(errors.New("wrong protocol envelope"))
			}
			if response.Method != "" {
				if len(response.Result) > 0 || len(response.Error) > 0 {
					return s.failLocked(errors.New("plugin request/notification contains response fields"))
				}
				if response.ID != nil && !s.initialized {
					return s.failLocked(errors.New("plugin called host API before initialization succeeded"))
				}
				if response.ID != nil { // Host mediation currently denies all plugin-origin API requests; no implicit grants.
					if !strings.HasPrefix(*response.ID, "p-") {
						return s.failLocked(errors.New("plugin-origin request ID must use p- prefix"))
					}
					reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *response.ID, "error": map[string]any{"code": -32010, "message": "Host API unavailable in conformance invocation", "data": map[string]any{"code": "PERMISSION_DENIED"}}})
					if e = s.write(ctx, append(reply, '\n')); e != nil {
						return s.failLocked(e)
					}
					continue
				}
				if response.Method != "plugin.heartbeat" && response.Method != "plugin.progress" {
					return s.failLocked(errors.New("unknown plugin notification"))
				}
				continue
			}
			if response.ID == nil || *response.ID != id {
				return s.failLocked(errors.New("unsolicited or wrong plugin response ID"))
			}
			if len(response.Result) > 0 && len(response.Error) > 0 && string(response.Error) != "null" {
				return s.failLocked(errors.New("plugin response contains both result and error"))
			}
			if len(response.Error) > 0 && string(response.Error) != "null" {
				return nil, fmt.Errorf("plugin returned application error: %s", response.Error)
			}
			if len(response.Result) == 0 {
				return s.failLocked(errors.New("result missing"))
			}
			if method == "initialize" {
				var negotiated struct {
					ProtocolVersion string `json:"protocolVersion"`
				}
				if err := json.Unmarshal(response.Result, &negotiated); err != nil || negotiated.ProtocolVersion != "1.0" {
					return s.failLocked(errors.New("unsupported plugin protocol version"))
				}
				s.initialized = true
			}
			return response.Result, nil
		}
	}
}

// A plugin that never reads stdin must not trap a coordinator goroutine in Write.
// Cancellation terminates the supervised process group and closes the pipe.
func (s *Session) write(ctx context.Context, b []byte) error {
	done := make(chan error, 1)
	go func() { _, err := s.input.Write(b); done <- err }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		s.cancel()
		s.input.Close()
		return ctx.Err()
	}
}

type ConformanceReport struct {
	PluginID      string   `json:"pluginID"`
	Checks        []string `json:"checks"`
	EvidenceClass string   `json:"evidenceClass"`
	Confined      bool     `json:"confined"`
}

func Conformance(ctx context.Context, exe, workspace string, m Manifest) (ConformanceReport, error) {
	report := ConformanceReport{PluginID: m.ID, Checks: []string{}, EvidenceClass: "simulated-contract", Confined: true}
	s, e := Start(ctx, exe, workspace)
	if e != nil {
		return report, e
	}
	defer s.Close()
	call := func(method string, p any) (json.RawMessage, error) {
		deadline := 5 * time.Second
		if method == "initialize" {
			deadline = 10 * time.Second
		}
		ctx, c := context.WithTimeout(ctx, deadline)
		defer c()
		return s.Call(ctx, method, p)
	}
	init, e := call("initialize", map[string]any{"protocolVersions": []string{"1.0"}, "host": map[string]string{"version": "0.0.0-dev", "os": "linux", "arch": "amd64"}, "sessionID": "conformance", "permissions": m.Permissions, "limits": map[string]int{"maxMessageBytes": wire.MaxFrame}})
	if e != nil {
		return report, e
	}
	var negotiated struct {
		ProtocolVersion string `json:"protocolVersion"`
		Plugin          struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"plugin"`
	}
	if e = json.Unmarshal(init, &negotiated); e != nil {
		return report, e
	}
	if negotiated.Plugin.ID != m.ID || negotiated.Plugin.Version != m.Version || negotiated.ProtocolVersion != "1.0" {
		return report, errors.New("negotiated identity/protocol mismatch")
	}
	report.Checks = append(report.Checks, "initialize identity and protocol")
	if _, e = call("describe", map[string]any{}); e != nil {
		return report, e
	}
	report.Checks = append(report.Checks, "describe")
	if _, e = call("unrecognized.method", map[string]any{}); e == nil {
		return report, errors.New("unknown method succeeded")
	}
	report.Checks = append(report.Checks, "unknown method rejected")
	params := map[string]any{"action": "summary", "input": map[string]any{"sortBy": "name"}, "context": map[string]any{"selectedVMs": []map[string]string{{"id": "fixture-vm-1", "name": "Synthetic fixture", "state": "stopped"}}}}
	planned, e := call("action.plan", params)
	if e != nil {
		return report, e
	}
	var plan map[string]any
	if e = json.Unmarshal(planned, &plan); e != nil {
		return report, e
	}
	effects, ok := plan["effects"].([]any)
	if !ok || len(effects) != 0 {
		return report, errors.New("summary fixture must declare no effects")
	}
	params["planToken"] = plan["planToken"]
	params["operationID"] = "conformance-operation"
	params["grants"] = []string{}
	if _, e = call("action.execute", params); e != nil {
		return report, e
	}
	report.Checks = append(report.Checks, "read-only plan and execute")
	params["context"] = map[string]any{"selectedVMs": []any{}}
	if _, e = call("action.execute", params); e == nil {
		return report, errors.New("stale/empty context accepted")
	}
	report.Checks = append(report.Checks, "stale context rejected")
	if _, e = call("shutdown", map[string]any{}); e != nil {
		return report, e
	}
	report.Checks = append(report.Checks, "shutdown")
	return report, nil
}
