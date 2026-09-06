package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
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
type Session struct {
	cmd      *exec.Cmd
	input    io.WriteCloser
	frames   chan frameResult
	cleanup  func()
	cancel   context.CancelFunc
	mu       sync.Mutex
	sequence int
	closed   bool
	done     chan error
	log      *boundedLog
}

func Start(ctx context.Context, exe, workspace string) (*Session, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd, cleanup, e := platform.ConfinedCommand(ctx, exe, workspace, nil)
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
	s := &Session{cmd: cmd, input: input, frames: make(chan frameResult, 32), cleanup: cleanup, cancel: cancel, done: make(chan error, 1), log: log}
	go func() {
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
	go func() { s.done <- cmd.Wait() }()
	return s, nil
}
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.input.Close()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	s.cleanup()
}
func (s *Session) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("plugin session closed")
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
	if _, e = s.input.Write(append(b, '\n')); e != nil {
		return nil, e
	}
	for {
		select {
		case <-ctx.Done():
			s.cancel()
			return nil, ctx.Err()
		case result := <-s.frames:
			if result.err != nil {
				return nil, fmt.Errorf("plugin process/framing failure: %w", result.err)
			}
			frame := result.data
			if e = wire.Validate(frame); e != nil {
				s.cancel()
				return nil, e
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
				s.cancel()
				return nil, e
			}
			if response.JSONRPC != "2.0" {
				return nil, errors.New("wrong protocol envelope")
			}
			if response.Method != "" {
				if response.ID != nil { // Host mediation currently denies all plugin-origin API requests; no implicit grants.
					reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *response.ID, "error": map[string]any{"code": -32010, "message": "Host API unavailable in conformance invocation", "data": map[string]any{"code": "PERMISSION_DENIED"}}})
					if _, e = s.input.Write(append(reply, '\n')); e != nil {
						return nil, e
					}
					continue
				}
				if response.Method != "plugin.heartbeat" && response.Method != "plugin.progress" {
					return nil, errors.New("unknown plugin notification")
				}
				continue
			}
			if response.ID == nil || *response.ID != id {
				return nil, errors.New("unsolicited or wrong plugin response ID")
			}
			if len(response.Error) > 0 && string(response.Error) != "null" {
				return nil, fmt.Errorf("plugin returned application error: %s", response.Error)
			}
			if len(response.Result) == 0 {
				return nil, errors.New("result missing")
			}
			return response.Result, nil
		}
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
