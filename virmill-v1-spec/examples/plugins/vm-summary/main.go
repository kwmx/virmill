// VM Summary is a synthetic read-only protocol example, not the Virmill app.
// It uses only the inventory passed by its caller. It never opens host resources.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxFrame = 8 * 1024 * 1024
const pluginID = "example.virmill.vm-summary"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type permission struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}
type initializeParams struct {
	ProtocolVersions []string `json:"protocolVersions"`
	Host             struct {
		Version string `json:"version"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	} `json:"host"`
	SessionID   string       `json:"sessionID"`
	Permissions []permission `json:"permissions"`
	Limits      struct {
		MaxMessageBytes int `json:"maxMessageBytes"`
	} `json:"limits"`
}
type vm struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}
type input struct {
	SortBy string `json:"sortBy,omitempty"`
}
type context struct {
	SelectedVMs []vm `json:"selectedVMs"`
}
type actionParams struct {
	Action      string   `json:"action"`
	Input       input    `json:"input"`
	Context     context  `json:"context"`
	PlanToken   string   `json:"planToken,omitempty"`
	OperationID string   `json:"operationID,omitempty"`
	Grants      []string `json:"grants,omitempty"`
}
type server struct {
	initialized bool
	allowed     bool
	sessionID   string
	stop        bool
}
type rpcError struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func appError(code, message string) *rpcError {
	return &rpcError{-32010, message, map[string]any{"code": code, "retryable": false}}
}
func invalid(message string) *rpcError { return &rpcError{-32602, message, nil} }

// Validate duplicate keys before decoding structs. The public protocol rejects
// ambiguous frames, including duplicates nested inside otherwise valid params.
func checkValue(dec *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("JSON nesting exceeds example limit")
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for dec.More() {
			t, e := dec.Token()
			if e != nil {
				return e
			}
			key, ok := t.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if keys[key] {
				return errors.New("duplicate JSON key")
			}
			keys[key] = true
			if e = checkValue(dec, depth+1); e != nil {
				return e
			}
		}
		t, e := dec.Token()
		if e != nil {
			return e
		}
		if t != json.Delim('}') {
			return errors.New("invalid closing delimiter")
		}
	case '[':
		for dec.More() {
			if e := checkValue(dec, depth+1); e != nil {
				return e
			}
		}
		t, e := dec.Token()
		if e != nil {
			return e
		}
		if t != json.Delim(']') {
			return errors.New("invalid closing delimiter")
		}
	default:
		return errors.New("unexpected closing delimiter")
	}
	return nil
}
func validateJSON(b []byte) error {
	if !utf8.Valid(b) || bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}) {
		return errors.New("invalid UTF-8 or BOM")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := checkValue(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func decode(b []byte, value any) error {
	if len(b) == 0 {
		b = []byte("{}")
	}
	if len(bytes.TrimSpace(b)) == 0 || bytes.TrimSpace(b)[0] != '{' {
		return errors.New("expected an object")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(value)
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
func validateAction(p actionParams) *rpcError {
	if p.Action != "summary" {
		return invalid("unknown action")
	}
	if p.Input.SortBy != "" && p.Input.SortBy != "name" && p.Input.SortBy != "state" {
		return invalid("sortBy must be name or state")
	}
	if len(p.Context.SelectedVMs) == 0 || len(p.Context.SelectedVMs) > 5000 {
		return invalid("select 1 to 5000 synthetic VM records")
	}
	seen := map[string]bool{}
	for _, v := range p.Context.SelectedVMs {
		if v.ID == "" || v.Name == "" || v.State == "" || len(v.Name) > 4096 || seen[v.ID] {
			return invalid("invalid or duplicate VM record")
		}
		seen[v.ID] = true
	}
	return nil
}

// This token is opaque, session-bound example data, NOT a permission or the
// host's canonical approved-plan signature. Production host approval is separate.
func token(p actionParams, session string) string {
	b, _ := json.Marshal(struct {
		Session string  `json:"session"`
		Action  string  `json:"action"`
		Input   input   `json:"input"`
		Context context `json:"context"`
	}{session, p.Action, p.Input, p.Context})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (s *server) call(r rpcRequest) (any, *rpcError) {
	if r.Method == "initialize" {
		if s.initialized {
			return nil, appError("ALREADY_INITIALIZED", "session already initialized")
		}
		var p initializeParams
		if err := decode(r.Params, &p); err != nil {
			return nil, invalid("invalid initialize parameters")
		}
		found := false
		for _, v := range p.ProtocolVersions {
			if v == "1.0" {
				found = true
			}
		}
		if !found {
			s.stop = true
			return nil, appError("PROTOCOL_VERSION_UNSUPPORTED", "protocol 1.0 is required")
		}
		if p.SessionID == "" || p.Limits.MaxMessageBytes < maxFrame {
			return nil, invalid("nonempty session and 8 MiB frame allowance required by example")
		}
		for _, g := range p.Permissions {
			if g.Name == "vm.read" && g.Scope == "selection" {
				s.allowed = true
			}
		}
		s.initialized = true
		s.sessionID = p.SessionID
		return map[string]any{"protocolVersion": "1.0", "plugin": map[string]any{"id": pluginID, "version": "0.1.0"}, "extensionTypes": []string{"action"}}, nil
	}
	if !s.initialized {
		return nil, appError("NOT_INITIALIZED", "initialize first")
	}
	switch r.Method {
	case "describe":
		return map[string]any{"actions": []any{map[string]any{"id": "summary", "title": "Summarize selected VMs", "readOnly": true, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"sortBy": map[string]any{"enum": []string{"name", "state"}}}, "additionalProperties": false}, "outputSchema": map[string]any{"type": "object", "required": []string{"count", "rows"}, "properties": map[string]any{"count": map[string]any{"type": "integer", "minimum": 0}, "rows": map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []string{"id", "name", "state"}, "properties": map[string]any{"id": map[string]string{"type": "string"}, "name": map[string]string{"type": "string"}, "state": map[string]string{"type": "string"}}, "additionalProperties": false}}}, "additionalProperties": false}}}}, nil
	case "ping":
		return map[string]any{"ok": true}, nil
	case "shutdown":
		s.stop = true
		return map[string]any{"ok": true}, nil
	case "action.plan", "action.execute":
		if !s.allowed {
			return nil, appError("PERMISSION_DENIED", "VM read permission is not granted")
		}
		var p actionParams
		if err := decode(r.Params, &p); err != nil {
			return nil, invalid("invalid action parameters")
		}
		if e := validateAction(p); e != nil {
			return nil, e
		}
		expected := token(p, s.sessionID)
		if r.Method == "action.plan" {
			if p.PlanToken != "" || p.OperationID != "" || len(p.Grants) > 0 {
				return nil, invalid("planning cannot receive execution fields")
			}
			return map[string]any{"summary": "Summarize selected VM records without host access", "effects": []any{}, "requires": []permission{{"vm.read", "selection"}}, "planToken": expected}, nil
		}
		if p.PlanToken != expected {
			return nil, appError("STALE_PLAN", "input or context differs from the planned request")
		}
		if p.OperationID == "" {
			return nil, invalid("operationID is required")
		}
		rows := append([]vm(nil), p.Context.SelectedVMs...)
		for i := range rows {
			rows[i].ID = clean(rows[i].ID)
			rows[i].Name = clean(rows[i].Name)
			rows[i].State = clean(rows[i].State)
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if p.Input.SortBy == "state" && rows[i].State != rows[j].State {
				return rows[i].State < rows[j].State
			}
			if rows[i].Name != rows[j].Name {
				return rows[i].Name < rows[j].Name
			}
			return rows[i].ID < rows[j].ID
		})
		return map[string]any{"count": len(rows), "rows": rows}, nil
	case "action.reconcile":
		return nil, appError("UNSUPPORTED_CAPABILITY", "immediate read-only example has no persisted external effects")
	default:
		return nil, &rpcError{-32601, "method not found", nil}
	}
}
func respond(out *json.Encoder, id any, result any, err *rpcError) error {
	response := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		response["error"] = err
	} else {
		response["result"] = result
	}
	return out.Encode(response)
}
func run(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), maxFrame+1)
	enc := json.NewEncoder(out)
	s := &server{}
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) > maxFrame {
			return errors.New("frame exceeds limit")
		}
		if err := validateJSON(b); err != nil {
			if err = respond(enc, nil, nil, &rpcError{-32700, "invalid JSON frame", nil}); err != nil {
				return err
			}
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			if err = respond(enc, nil, nil, &rpcError{-32600, "invalid request", nil}); err != nil {
				return err
			}
			continue
		}
		if id, exists := raw["id"]; exists && bytes.Equal(bytes.TrimSpace(id), []byte("null")) {
			if err := respond(enc, nil, nil, &rpcError{-32600, "request IDs must be strings", nil}); err != nil {
				return err
			}
			continue
		}
		if params, exists := raw["params"]; exists && (len(bytes.TrimSpace(params)) == 0 || bytes.TrimSpace(params)[0] != '{') {
			if err := respond(enc, nil, nil, &rpcError{-32600, "params must be an object", nil}); err != nil {
				return err
			}
			continue
		}
		var r rpcRequest
		if err := decode(b, &r); err != nil || r.JSONRPC != "2.0" || r.Method == "" || (r.ID != nil && (!strings.HasPrefix(*r.ID, "h-") || len(*r.ID) <= 2)) {
			if err = respond(enc, nil, nil, &rpcError{-32600, "invalid request", nil}); err != nil {
				return err
			}
			continue
		}
		if r.ID == nil {
			// Notifications never receive a response. This synchronous example
			// has no outstanding work to cancel and ignores unknown notices.
			continue
		}
		value, e := s.call(r)
		if err := respond(enc, *r.ID, value, e); err != nil {
			return err
		}
		if s.stop {
			return nil
		}
	}
	return scanner.Err()
}
func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "protocol example stopped:", err)
		os.Exit(1)
	}
}
