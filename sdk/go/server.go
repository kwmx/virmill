// Package sdk provides the language-neutral Virmill protocol 1.0 Go server.
// It has no libvirt or core internal dependencies.
package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"virmill.local/sdk/protocol"
)

const Version = "1.0"

type Error struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }
func Failure(code, message string) *Error {
	return &Error{Code: -32010, Message: message, Data: map[string]any{"code": code, "retryable": false, "safeNextActions": []string{}}}
}

type Permission struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}
type Identity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Handler func(context.Context, json.RawMessage) (any, error)
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}
type Server struct {
	Identity       Identity
	ExtensionTypes []string
	Description    any
	Methods        map[string]Handler
	Required       []Permission
	mu             sync.Mutex
	writer         io.Writer
	active         map[string]context.CancelFunc
	pending        map[string]chan Message
	seq            atomic.Uint64
	ready          bool
	wg             sync.WaitGroup
}

func (s *Server) send(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(b) > protocol.MaxFrame {
		return errors.New("protocol frame too large")
	}
	_, e = s.writer.Write(append(b, '\n'))
	return e
}
func (s *Server) respond(id *string, result any, err error) error {
	if id == nil {
		return nil
	}
	if err != nil {
		rpc, ok := err.(*Error)
		if !ok {
			rpc = &Error{Code: -32603, Message: "handler failed"}
		}
		return s.send(map[string]any{"jsonrpc": "2.0", "id": *id, "error": rpc})
	}
	return s.send(map[string]any{"jsonrpc": "2.0", "id": *id, "result": result})
}
func (s *Server) Notify(method string, params any) error {
	return s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (s *Server) HostCall(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	if !s.ready {
		s.mu.Unlock()
		return nil, Failure("PERMISSION_DENIED", "not initialized")
	}
	if len(s.pending) >= 32 {
		s.mu.Unlock()
		return nil, Failure("RESOURCE_BUSY", "host request limit")
	}
	id := fmt.Sprintf("p-%d", s.seq.Add(1))
	ch := make(chan Message, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	if e := s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); e != nil {
		return nil, e
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	}
}
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if s.Identity.ID == "" || s.Identity.Version == "" {
		return errors.New("plugin identity required")
	}
	s.writer = out
	s.active = map[string]context.CancelFunc{}
	s.pending = map[string]chan Message{}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		s.mu.Lock()
		for _, c := range s.active {
			c()
		}
		s.mu.Unlock()
		s.wg.Wait()
	}()
	r := bufio.NewReader(in)
	slots := make(chan struct{}, 32)
	for {
		frame, e := protocol.ReadFrame(r)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		var m Message
		if e = protocol.Decode(frame, &m); e != nil {
			if e = s.send(map[string]any{"jsonrpc": "2.0", "id": nil, "error": &Error{Code: -32700, Message: "Invalid JSON"}}); e != nil {
				return e
			}
			continue
		}
		if m.JSONRPC != "2.0" {
			_ = s.respond(m.ID, nil, &Error{Code: -32600, Message: "Invalid request"})
			continue
		}
		if m.Method == "" {
			if m.ID == nil {
				return errors.New("response lacks ID")
			}
			s.mu.Lock()
			ch, ok := s.pending[*m.ID]
			s.mu.Unlock()
			if !ok {
				return errors.New("unsolicited host response")
			}
			select {
			case ch <- m:
			default:
				return errors.New("duplicate host response")
			}
			continue
		}
		if m.ID != nil && (!strings.HasPrefix(*m.ID, "h-") || len(*m.ID) <= 2) {
			_ = s.respond(m.ID, nil, &Error{Code: -32600, Message: "Host request IDs must start with h-"})
			continue
		}
		if len(m.Result) > 0 || m.Error != nil {
			_ = s.respond(m.ID, nil, &Error{Code: -32600, Message: "Request cannot contain response fields"})
			continue
		}
		if m.Method == "initialize" {
			var p struct {
				ProtocolVersions []string       `json:"protocolVersions"`
				Permissions      []Permission   `json:"permissions"`
				Host             any            `json:"host"`
				SessionID        string         `json:"sessionID"`
				Limits           map[string]int `json:"limits"`
			}
			e := json.Unmarshal(m.Params, &p)
			compatible := false
			for _, v := range p.ProtocolVersions {
				if v == Version {
					compatible = true
				}
			}
			s.mu.Lock()
			ready := s.ready
			s.mu.Unlock()
			if e != nil || !compatible || ready {
				_ = s.respond(m.ID, nil, Failure("UNSUPPORTED_CAPABILITY", "protocol mismatch or repeated initialization"))
				continue
			}
			grants := map[Permission]bool{}
			for _, g := range p.Permissions {
				grants[g] = true
			}
			denied := false
			for _, g := range s.Required {
				if !grants[g] {
					denied = true
				}
			}
			if denied {
				_ = s.respond(m.ID, nil, Failure("PERMISSION_DENIED", "required scope absent"))
				continue
			}
			if e = s.respond(m.ID, map[string]any{"protocolVersion": Version, "plugin": s.Identity, "extensionTypes": s.ExtensionTypes}, nil); e != nil {
				return e
			}
			s.mu.Lock()
			s.ready = true
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		ready := s.ready
		s.mu.Unlock()
		if !ready {
			_ = s.respond(m.ID, nil, Failure("PERMISSION_DENIED", "initialize first"))
			continue
		}
		switch m.Method {
		case "describe":
			if e = s.respond(m.ID, s.Description, nil); e != nil {
				return e
			}
			continue
		case "ping":
			if e = s.respond(m.ID, map[string]bool{"alive": true}, nil); e != nil {
				return e
			}
			continue
		case "shutdown":
			if e = s.respond(m.ID, map[string]bool{"shutdown": true}, nil); e != nil {
				return e
			}
			return nil
		case "request.cancel":
			var p struct {
				RequestID string `json:"requestID"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				s.mu.Lock()
				c := s.active[p.RequestID]
				s.mu.Unlock()
				if c != nil {
					c()
				}
			}
			continue
		}
		handler, ok := s.Methods[m.Method]
		if !ok {
			_ = s.respond(m.ID, nil, &Error{Code: -32601, Message: "Method not found"})
			continue
		}
		if m.ID == nil {
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = s.respond(m.ID, nil, Failure("RESOURCE_BUSY", "outstanding request limit"))
			continue
		}
		requestCtx, requestCancel := context.WithCancel(ctx)
		s.mu.Lock()
		_, duplicate := s.active[*m.ID]
		if !duplicate {
			s.active[*m.ID] = requestCancel
		}
		s.mu.Unlock()
		if duplicate {
			requestCancel()
			<-slots
			return errors.New("duplicate active request ID")
		}
		s.wg.Add(1)
		go func(m Message) {
			defer s.wg.Done()
			defer func() { requestCancel(); <-slots; s.mu.Lock(); delete(s.active, *m.ID); s.mu.Unlock() }()
			done := make(chan struct{})
			go func() {
				tick := time.NewTicker(10 * time.Second)
				defer tick.Stop()
				for {
					select {
					case <-done:
						return
					case <-requestCtx.Done():
						return
					case <-tick.C:
						_ = s.Notify("plugin.heartbeat", map[string]any{"requestID": *m.ID})
					}
				}
			}()
			result, err := handler(requestCtx, m.Params)
			close(done)
			if requestCtx.Err() != nil {
				err = Failure("CANCELED", "request canceled")
			}
			_ = s.respond(m.ID, result, err)
		}(m)
	}
}

// Redactor replaces explicitly scoped secret values before emitting diagnostic text.
func Redact(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	return text
}
