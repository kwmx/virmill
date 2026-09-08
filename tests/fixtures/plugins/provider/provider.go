package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"virmill.local/sdk"
	"virmill.local/sdk/protocol"
)

const connection = "fixture:///default"
const stateLimit = 4 << 20
const resourceLimit = 1024
const operationLimit = 2048
const pageLimit = 100

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type ResourceKey struct {
	ProviderID   string `json:"providerID"`
	ConnectionID string `json:"connectionID"`
	Kind         string `json:"kind"`
	ExternalID   string `json:"externalID"`
}
type Capabilities struct {
	Create             bool `json:"create"`
	Start              bool `json:"start"`
	Stop               bool `json:"stop"`
	Delete             bool `json:"delete"`
	ConfigurationEdits bool `json:"configurationEdits"`
	Networks           bool `json:"networks"`
	Devices            bool `json:"devices"`
	Snapshots          bool `json:"snapshots"`
	Backups            bool `json:"backups"`
	GuestTransports    bool `json:"guestTransports"`
	Cancel             bool `json:"cancel"`
}

func capabilities() Capabilities {
	return Capabilities{Create: true, Start: true, Stop: true, Delete: true}
}

type Resource struct {
	ID           string       `json:"id"`
	Key          ResourceKey  `json:"key"`
	Name         string       `json:"name"`
	State        string       `json:"state"`
	OperationID  string       `json:"operationID"`
	Revision     uint64       `json:"revision"`
	Capabilities Capabilities `json:"capabilities"`
}
type Intent struct {
	Operation      string `json:"operation"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	OperationID    string `json:"operationID"`
	IdempotencyKey string `json:"idempotencyKey"`
	PlanToken      string `json:"planToken"`
}
type Receipt struct {
	Intent     Intent   `json:"intent"`
	Digest     string   `json:"digest"`
	Resource   Resource `json:"resource"`
	Status     string   `json:"status"`
	Reconciled bool     `json:"reconciled"`
}
type State struct {
	Version    int                 `json:"version"`
	Generation uint64              `json:"generation"`
	CursorKey  string              `json:"cursorKey"`
	Resources  map[string]Resource `json:"resources"`
	Operations map[string]Receipt  `json:"operations"`
	Keys       map[string]string   `json:"keys"`
}
type Params struct {
	ConnectionID   string `json:"connectionID"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Operation      string `json:"operation"`
	OperationID    string `json:"operationID"`
	IdempotencyKey string `json:"idempotencyKey"`
	Partial        bool   `json:"partial"`
	PlanToken      string `json:"planToken"`
	Cursor         string `json:"cursor"`
	Limit          int    `json:"limit"`
}
type Outcome struct {
	Resource
	Status     string `json:"status"`
	Reconciled bool   `json:"reconciled"`
	Simulated  bool   `json:"simulated"`
}
type Page struct {
	Resources  []Resource `json:"resources"`
	NextCursor *string    `json:"nextCursor"`
	Generation uint64     `json:"generation"`
	Simulated  bool       `json:"simulated"`
}
type provider struct {
	mu        sync.Mutex
	state     State
	persist   func(State) error
	uncertain bool
}

func invalid() error                     { return &sdk.Error{Code: -32602, Message: "invalid bounded fixture parameters"} }
func failure(code, message string) error { return sdk.Failure(code, message) }
func hash(value any) string {
	data, _ := json.Marshal(value)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func validName(name string) bool {
	if name == "" || len(name) > 128 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func parseParams(method string, raw []byte) (Params, error) {
	var p Params
	var fields map[string]json.RawMessage
	if len(raw) > 16<<10 || protocol.Decode(raw, &fields) != nil || fields == nil {
		return p, invalid()
	}
	allowed := map[string]bool{"connectionID": true}
	lists := map[string]string{"capabilities": "", "inventory": "cursor limit", "get": "id", "plan": "operation id name", "apply": "operation id name operationID idempotencyKey partial planToken", "status": "id operationID idempotencyKey", "reconcile": "id operationID idempotencyKey", "cancel": "id operationID idempotencyKey"}
	list, ok := lists[method]
	if !ok {
		return p, &sdk.Error{Code: -32601, Message: "method not found"}
	}
	for _, key := range strings.Fields(list) {
		allowed[key] = true
	}
	for key, value := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return p, invalid()
		}
	}
	if protocol.Decode(raw, &p) != nil {
		return p, invalid()
	}
	for _, key := range []string{"connectionID", "id", "name", "operation", "operationID", "idempotencyKey", "planToken"} {
		if value, present := fields[key]; present && bytes.Equal(bytes.TrimSpace(value), []byte(`""`)) {
			return p, invalid()
		}
	}
	if (method == "get" && p.ID == "") || ((method == "plan" || method == "apply") && p.Operation == "") || ((method == "status" || method == "reconcile") && p.ID == "" && p.OperationID == "" && p.IdempotencyKey == "") {
		return p, invalid()
	}
	if p.PlanToken != "" {
		decoded, err := hex.DecodeString(p.PlanToken)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != p.PlanToken {
			return p, invalid()
		}
	}
	if p.ConnectionID != "" && p.ConnectionID != connection {
		return p, failure("UNSUPPORTED_CAPABILITY", "only the simulated fixture connection is supported")
	}
	for _, value := range []string{p.ID, p.OperationID, p.IdempotencyKey} {
		if value != "" && !identifier.MatchString(value) {
			return p, invalid()
		}
	}
	if len(p.Cursor) > 1024 || len(p.PlanToken) > 64 {
		return p, invalid()
	}
	if _, ok := fields["limit"]; ok && (p.Limit < 1 || p.Limit > pageLimit) {
		return p, invalid()
	}
	if _, ok := fields["name"]; ok && !validName(p.Name) {
		return p, invalid()
	}
	return p, nil
}

func openProvider(root string) (*provider, error) {
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode().Perm() != 0700 {
		return nil, errors.New("private mode-0700 fixture workspace required")
	}
	p := &provider{state: State{Version: 1, Resources: map[string]Resource{}, Operations: map[string]Receipt{}, Keys: map[string]string{}}}
	path := filepath.Join(root, "provider-state.json")
	st, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		p.state.CursorKey = hex.EncodeToString(key)
	} else if err != nil {
		return nil, err
	} else {
		if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > stateLimit {
			return nil, errors.New("invalid fixture state file")
		}
		f, e := os.Open(path)
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(io.LimitReader(f, stateLimit+1))
		closeErr := f.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(data) > stateLimit || protocol.Decode(data, &p.state) != nil || validateState(p.state) != nil {
			return nil, errors.New("corrupt or unsupported fixture state; retain for inspection")
		}
	}
	p.persist = func(s State) error { return storeState(root, s) }
	return p, nil
}
func validateState(s State) error {
	bad := func() error { return errors.New("invalid fixture state binding") }
	key, e := hex.DecodeString(s.CursorKey)
	if e != nil || len(key) != 32 || hex.EncodeToString(key) != s.CursorKey || s.Version != 1 || s.Resources == nil || s.Operations == nil || s.Keys == nil || len(s.Resources) > resourceLimit || len(s.Operations) > operationLimit || len(s.Keys) != len(s.Operations) || s.Generation != uint64(len(s.Operations)) {
		return bad()
	}
	for id, r := range s.Resources {
		if id != r.ID || !identifier.MatchString(id) || !validName(r.Name) || r.Key != (ResourceKey{"fixture", connection, "vm", id}) || r.Capabilities != capabilities() || r.Revision < 1 || (r.State != "defined" && r.State != "running" && r.State != "stopped" && r.State != "deleted") {
			return bad()
		}
		op, ok := s.Operations[r.OperationID]
		if !ok || op.Resource != r {
			return bad()
		}
	}
	for id, op := range s.Operations {
		if id != op.Intent.OperationID || !identifier.MatchString(id) || !identifier.MatchString(op.Intent.IdempotencyKey) || s.Keys[op.Intent.IdempotencyKey] != id || op.Digest != hash(op.Intent) || (op.Status != "succeeded" && op.Status != "partial") {
			return bad()
		}
		r, ok := s.Resources[op.Resource.ID]
		if !ok || r.Revision < op.Resource.Revision || op.Resource.OperationID != id || op.Resource.Key != (ResourceKey{"fixture", connection, "vm", op.Resource.ID}) || op.Resource.Capabilities != capabilities() || !validName(op.Resource.Name) {
			return bad()
		}
		switch op.Intent.Operation {
		case "create":
			if op.Intent.ID != "" || op.Resource.ID != "fixture-"+id || op.Resource.Revision != 1 || op.Resource.State != "defined" || op.Resource.Name != op.Intent.Name {
				return bad()
			}
		case "start", "stop", "delete":
			state := map[string]string{"start": "running", "stop": "stopped", "delete": "deleted"}[op.Intent.Operation]
			if op.Intent.ID != op.Resource.ID || op.Intent.Name != "" || op.Resource.Revision < 2 || op.Resource.State != state {
				return bad()
			}
		default:
			return bad()
		}
	}
	return nil
}
func storeState(root string, s State) error {
	if err := validateState(s); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(data) > stateLimit {
		return errors.New("fixture state size limit")
	}
	f, err := os.CreateTemp(root, ".provider-state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, filepath.Join(root, "provider-state.json")); err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	err = dir.Sync()
	return errors.Join(err, dir.Close())
}
func copyState(s State) State {
	out := s
	out.Resources = make(map[string]Resource, len(s.Resources))
	for k, v := range s.Resources {
		out.Resources[k] = v
	}
	out.Operations = make(map[string]Receipt, len(s.Operations))
	for k, v := range s.Operations {
		out.Operations[k] = v
	}
	out.Keys = make(map[string]string, len(s.Keys))
	for k, v := range s.Keys {
		out.Keys[k] = v
	}
	return out
}
func (p *provider) commit(s State) error {
	if err := p.persist(s); err != nil {
		p.uncertain = true
		return failure("RECOVERY_REQUIRED", "fixture persistence acknowledgement failed; restart and reconcile without replay")
	}
	p.state = s
	return nil
}
func (p *provider) server() *sdk.Server {
	methods := map[string]sdk.Handler{}
	for _, name := range []string{"capabilities", "inventory", "get", "plan", "apply", "status", "reconcile", "cancel"} {
		method := name
		methods["provider."+method] = func(ctx context.Context, raw json.RawMessage) (any, error) { return p.handle(ctx, method, raw) }
	}
	return &sdk.Server{Identity: sdk.Identity{ID: "example.virmill.provider-fixture", Version: "0.2.0"}, ExtensionTypes: []string{"provider"}, Description: description(), Methods: methods}
}
func (p *provider) handle(ctx context.Context, method string, raw []byte) (any, error) {
	params, err := parseParams(method, raw)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if ctx.Err() != nil {
		return nil, failure("CANCELED", "fixture request canceled before effect")
	}
	if p.uncertain {
		return nil, failure("RECOVERY_REQUIRED", "restart after uncertain fixture persistence")
	}
	switch method {
	case "capabilities":
		return map[string]any{"providerID": "fixture", "connectionID": connection, "simulated": true, "capabilities": capabilities(), "create": true, "backup": false, "usb": false, "cancel": false}, nil
	case "inventory":
		return p.inventory(params)
	case "get":
		r, ok := p.state.Resources[params.ID]
		if !ok || r.State == "deleted" {
			return nil, failure("RESOURCE_MISSING", "unknown simulated resource")
		}
		return r, nil
	case "cancel":
		return nil, failure("UNSUPPORTED_CAPABILITY", "provider lifecycle cancellation is unsupported; inspect status/reconcile")
	case "plan":
		after, err := p.effect(params)
		if err != nil {
			return nil, err
		}
		return map[string]any{"summary": "Simulate " + params.Operation, "effects": []string{params.Operation + " one private JSON resource"}, "requires": []string{}, "planToken": p.planToken(params), "resultState": after.State, "simulated": true}, nil
	case "apply":
		return p.apply(ctx, params)
	case "status", "reconcile":
		op, err := p.selectReceipt(params)
		if err != nil {
			return nil, err
		}
		if method == "reconcile" {
			if p.state.Resources[op.Resource.ID] != op.Resource {
				return nil, failure("SOURCE_CHANGED", "current simulated resource differs from original effect")
			}
			if !op.Reconciled {
				next := copyState(p.state)
				op.Reconciled = true
				next.Operations[op.Intent.OperationID] = op
				if err = p.commit(next); err != nil {
					return nil, err
				}
			}
		}
		return outcome(op), nil
	}
	return nil, &sdk.Error{Code: -32601, Message: "method not found"}
}
func outcome(op Receipt) Outcome {
	return Outcome{Resource: op.Resource, Status: op.Status, Reconciled: op.Reconciled, Simulated: true}
}
func (p *provider) selectReceipt(q Params) (Receipt, error) {
	id := q.OperationID
	if q.IdempotencyKey != "" {
		byKey := p.state.Keys[q.IdempotencyKey]
		if byKey == "" {
			return Receipt{}, failure("RESOURCE_MISSING", "unknown simulated operation key")
		}
		if id != "" && id != byKey {
			return Receipt{}, invalid()
		}
		id = byKey
	}
	if q.ID != "" {
		r, ok := p.state.Resources[q.ID]
		if !ok {
			return Receipt{}, failure("RESOURCE_MISSING", "unknown simulated resource")
		}
		if id == "" {
			id = r.OperationID
		}
	}
	op, ok := p.state.Operations[id]
	if !ok {
		return Receipt{}, failure("RESOURCE_MISSING", "unknown simulated operation")
	}
	if q.ID != "" && op.Resource.ID != q.ID {
		return Receipt{}, invalid()
	}
	return op, nil
}
func (p *provider) effect(q Params) (Resource, error) {
	if q.Operation != "create" && q.Operation != "start" && q.Operation != "stop" && q.Operation != "delete" {
		return Resource{}, failure("UNSUPPORTED_CAPABILITY", "supported fixture operations are create, start, stop and delete")
	}
	if q.Operation == "create" {
		if q.ID != "" || !validName(q.Name) {
			return Resource{}, invalid()
		}
		return Resource{Name: q.Name, State: "defined", Revision: 1, Capabilities: capabilities()}, nil
	}
	if q.ID == "" || q.Name != "" {
		return Resource{}, invalid()
	}
	r, ok := p.state.Resources[q.ID]
	if !ok || r.State == "deleted" {
		return Resource{}, failure("RESOURCE_MISSING", "unknown simulated resource")
	}
	prior := p.state.Operations[r.OperationID]
	if prior.Status == "partial" && !prior.Reconciled {
		return Resource{}, failure("RECOVERY_REQUIRED", "reconcile the partial original effect before another lifecycle step")
	}
	switch q.Operation {
	case "start":
		if r.State != "defined" && r.State != "stopped" {
			return Resource{}, failure("STALE_PLAN", "start requires defined or stopped state")
		}
		r.State = "running"
	case "stop":
		if r.State != "running" {
			return Resource{}, failure("STALE_PLAN", "stop requires running state")
		}
		r.State = "stopped"
	case "delete":
		if r.State == "running" {
			return Resource{}, failure("RESOURCE_BUSY", "stop the simulated resource before deletion")
		}
		r.State = "deleted"
	}
	r.Revision++
	return r, nil
}
func (p *provider) planToken(q Params) string {
	return hash(struct {
		Operation, ID, Name string
		Before              Resource
	}{q.Operation, q.ID, q.Name, p.state.Resources[q.ID]})
}
func (p *provider) apply(ctx context.Context, q Params) (any, error) {
	if !identifier.MatchString(q.OperationID) || len(q.OperationID) > 120 || !identifier.MatchString(q.IdempotencyKey) {
		return nil, invalid()
	}
	intent := Intent{q.Operation, q.ID, q.Name, q.OperationID, q.IdempotencyKey, q.PlanToken}
	digest := hash(intent)
	if id, ok := p.state.Keys[q.IdempotencyKey]; ok {
		op := p.state.Operations[id]
		if op.Digest != digest {
			return nil, failure("STALE_PLAN", "idempotency key reused with changed logical request")
		}
		if op.Status == "partial" && !op.Reconciled {
			return nil, failure("PARTIAL_EFFECT", "original partial effect remains; explicitly reconcile it")
		}
		return outcome(op), nil
	}
	if _, ok := p.state.Operations[q.OperationID]; ok {
		return nil, failure("STALE_PLAN", "operation ID already belongs to another idempotency request")
	}
	after, err := p.effect(q)
	if err != nil {
		return nil, err
	}
	if q.PlanToken != "" && q.PlanToken != p.planToken(q) {
		return nil, failure("STALE_PLAN", "reviewed fixture input or resource state changed")
	}
	if len(p.state.Operations) >= operationLimit || (q.Operation == "create" && len(p.state.Resources) >= resourceLimit) {
		return nil, failure("RESOURCE_BUSY", "bounded fixture history is full")
	}
	if q.Operation == "create" {
		after.ID = "fixture-" + q.OperationID
		after.Key = ResourceKey{"fixture", connection, "vm", after.ID}
		if _, ok := p.state.Resources[after.ID]; ok {
			return nil, failure("RESOURCE_BUSY", "stable fake identity already exists")
		}
	}
	after.OperationID = q.OperationID
	op := Receipt{Intent: intent, Digest: digest, Resource: after, Status: "succeeded"}
	if q.Partial {
		op.Status = "partial"
	}
	next := copyState(p.state)
	next.Generation++
	next.Resources[after.ID] = after
	next.Operations[q.OperationID] = op
	next.Keys[q.IdempotencyKey] = q.OperationID
	if ctx.Err() != nil {
		return nil, failure("CANCELED", "fixture request canceled before commit")
	}
	if err = p.commit(next); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, failure("RECOVERY_REQUIRED", "fixture effect committed during cancellation; reconcile original operation")
	}
	if q.Partial {
		return nil, failure("PARTIAL_EFFECT", "simulated effect persisted; synthetic readiness acknowledgement failed")
	}
	return outcome(op), nil
}

type cursor struct {
	Generation uint64 `json:"generation"`
	After      string `json:"after"`
	Limit      int    `json:"limit"`
	Connection string `json:"connection"`
}

func (p *provider) cursor(c cursor) string {
	data, _ := json.Marshal(c)
	key, _ := hex.DecodeString(p.state.CursorKey)
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + hex.EncodeToString(mac.Sum(nil))
}
func (p *provider) inventory(q Params) (Page, error) {
	page := Page{Resources: []Resource{}, Generation: p.state.Generation, Simulated: true}
	limit := q.Limit
	if limit == 0 {
		limit = 25
	}
	after := ""
	if q.Cursor != "" {
		parts := strings.Split(q.Cursor, ".")
		if len(parts) != 2 {
			return Page{}, invalid()
		}
		data, err := base64.RawURLEncoding.DecodeString(parts[0])
		var c cursor
		if err != nil || protocol.Decode(data, &c) != nil || q.Cursor != p.cursor(c) || c.Limit != limit || c.Connection != connection {
			return Page{}, invalid()
		}
		if c.Generation != p.state.Generation {
			return Page{}, failure("STALE_PLAN", "inventory changed; restart pagination")
		}
		after = c.After
	}
	ids := []string{}
	for id, r := range p.state.Resources {
		if r.State != "deleted" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	start := 0
	if after != "" {
		i := sort.SearchStrings(ids, after)
		if i == len(ids) || ids[i] != after {
			return Page{}, invalid()
		}
		start = i + 1
	}
	end := start + limit
	if end > len(ids) {
		end = len(ids)
	}
	for _, id := range ids[start:end] {
		page.Resources = append(page.Resources, p.state.Resources[id])
	}
	if end < len(ids) {
		token := p.cursor(cursor{p.state.Generation, ids[end-1], limit, connection})
		page.NextCursor = &token
	}
	return page, nil
}
