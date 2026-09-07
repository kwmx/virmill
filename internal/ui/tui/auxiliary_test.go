package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/auxiliary"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const auxiliaryTUIVM = "12345678-1234-4234-8234-123456789abc"

type auxiliaryTUINative struct {
	state domain.ColdStateInspection
	calls int
	err   error
}

func (n *auxiliaryTUINative) InspectColdState(_ context.Context, uri, id string) (domain.ColdStateInspection, error) {
	n.calls++
	if uri != "qemu:///system" || id != auxiliaryTUIVM {
		return domain.ColdStateInspection{}, errors.New("unexpected native selection")
	}
	return n.state, n.err
}

type auxiliaryTUIHelper struct {
	native   *auxiliaryTUINative
	roots    []string
	requests []helper.Request
	err      error
	change   bool
	cancel   context.CancelFunc
}

func (h *auxiliaryTUIHelper) Root(id string) (string, error) {
	h.roots = append(h.roots, id)
	if id != "state" {
		return "", domain.Fail("PERMISSION_DENIED", "fixture root is not approved")
	}
	return "/fixture/approved-state", nil
}
func (*auxiliaryTUIHelper) KeyID() (string, error) { return strings.Repeat("a", 64), nil }
func (h *auxiliaryTUIHelper) InspectAuxiliary(_ context.Context, r helper.Request) (helper.AuxiliaryResponse, error) {
	h.requests = append(h.requests, r)
	if h.err != nil {
		return helper.AuxiliaryResponse{}, h.err
	}
	binding, err := helper.AuxiliaryBinding(r)
	if err != nil {
		return helper.AuxiliaryResponse{}, err
	}
	out := helper.AuxiliaryResponse{Version: 1, JobID: r.JobID, Binding: binding, Stage: "inspected", Inventory: &helper.AuxiliaryInventory{
		Version: 1, Resource: h.native.state.Resource, Fingerprint: h.native.state.Fingerprint, Layout: h.native.state.Layout,
		Root: helper.AuxiliaryRoot{ID: r.RootID, Path: "/fixture/approved-state"}, Directories: []helper.AuxiliaryDirectory{},
		Members: []helper.AuxiliaryMember{{ID: "members/000", Kind: "nvram", RelativePath: "vars.fd", State: helper.AuxiliaryFileState{Generation: "synthetic-generation", Size: 4096, Links: 1, Mode: 0600, UID: 1000, GID: 1000}}}, TotalBytes: 4096,
	}}
	if h.change {
		out.Inventory.Layout.Firmware.Loader = "/fixture/changed-code.fd"
	}
	if h.cancel != nil {
		h.cancel()
	}
	return out, nil
}

type auxiliaryTUIClient struct {
	service   *app.Service
	native    *auxiliaryTUINative
	helper    *auxiliaryTUIHelper
	methods   []string
	requests  []app.Request
	responses []app.Response
	transport error
	uid       uint32
	cancelAt  string
}

func (c *auxiliaryTUIClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	c.methods = append(c.methods, method)
	c.requests = append(c.requests, r)
	if c.transport != nil {
		return app.Response{}, c.transport
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.cancelAt == "pre" {
		cancel()
	}
	if c.cancelAt == "post" {
		c.helper.cancel = cancel
	}
	response := c.service.Call(ctx, c.uid, method, r)
	c.responses = append(c.responses, response)
	return response, nil
}

func auxiliaryTUIFixture() *auxiliaryTUIClient {
	layout := domain.ColdStateLayout{VMID: auxiliaryTUIVM, Firmware: domain.ColdFirmware{NVRAM: &domain.ColdNVRAM{Path: "/fixture/approved-state/vars.fd", Format: "raw"}}, SecretReferences: []string{}}
	native := &auxiliaryTUINative{state: domain.ColdStateInspection{Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: auxiliaryTUIVM}, State: "stopped", Persistent: true, Fingerprint: strings.Repeat("b", 64), Layout: layout, Source: &domain.ColdSourceLayout{State: layout}}}
	h := &auxiliaryTUIHelper{native: native}
	s := &app.Service{Extensions: map[string]func(context.Context, uint32, app.Request) (any, error){}}
	(&auxiliary.Service{Backend: native, Helper: h}).Register(s)
	return &auxiliaryTUIClient{service: s, native: native, helper: h, uid: 1000}
}

func auxiliaryTUIForm(t *testing.T, c *auxiliaryTUIClient, connection string) Model {
	t.Helper()
	m := New(c, connection)
	for i, section := range sections {
		if section == "Protection" {
			m.Section = i
		}
	}
	found := false
	for i, action := range m.actions() {
		if action.Command == "vm recovery auxiliary inspect" {
			if action.Method != "vm.recovery.auxiliary.inspect" || action.Mutation != "" {
				t.Fatal("auxiliary action changed shared method or acquired mutation semantics")
			}
			m.Selected, found = i, true
		}
	}
	if !found {
		t.Fatal("auxiliary inspection inaccessible in Protection")
	}
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if command != nil || !m.Editing || m.Confirm || m.Busy {
		t.Fatal("auxiliary inspection failed to open a read-only form")
	}
	return m
}

func auxiliaryTUIEnter(t *testing.T, m Model, input string) (Model, tea.Cmd) {
	t.Helper()
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(input)})
	m = next.(Model)
	if command != nil || m.Input != input {
		t.Fatal("JSON form keystrokes lost or prematurely dispatched")
	}
	next, command = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model), command
}

func TestAuxiliaryTUIJSONFormAndCurrentPagedSharedObservation(t *testing.T) {
	c := auxiliaryTUIFixture()
	before, _ := json.Marshal(c.native.state)
	m := auxiliaryTUIForm(t, c, "qemu:///system")
	m, command := auxiliaryTUIEnter(t, m, `{"id":"`+auxiliaryTUIVM+`","input":{"rootID":"state"}}`)
	if command == nil || !m.Busy || m.Editing {
		t.Fatal("JSON form did not dispatch")
	}
	next, followup := m.Update(command())
	m = next.(Model)
	if followup != nil || m.Busy || m.Plan != nil || m.Confirm || m.Offset != 0 {
		t.Fatal("metadata result acquired mutation/approval state")
	}
	if !reflect.DeepEqual(c.methods, []string{"vm.recovery.auxiliary.inspect"}) || c.native.calls != 1 || len(c.helper.requests) != 1 || len(c.responses) != 1 {
		t.Fatal("TUI bypassed shared auxiliary service", c.methods)
	}
	request := c.requests[0]
	if request.ID != auxiliaryTUIVM || request.Connection != "qemu:///system" || request.Path != "" || request.Action != "" || request.Apply != nil || request.Input["rootID"] != "state" {
		t.Fatal("TUI changed UUID/root/authority", request)
	}
	want, _ := json.MarshalIndent(c.responses[0], "", "  ")
	if m.Output != string(want) || c.responses[0].Error != nil {
		t.Fatal("TUI did not retain the actual shared-service envelope", m.Output)
	}
	result := c.responses[0].Data.(auxiliary.Result)
	if result.APIVersion != domain.APIVersion || result.CaptureVerified || result.IndependentRestoreVerified || result.GuestBootVerified || result.Observation.Artifact != nil || result.Observation.Stage != "inspected" {
		t.Fatal("TUI metadata became capture/restore proof", result)
	}
	// Read the current Model.View at the normal 80x24 size, then advance with
	// actual PgDown updates. This exercises rendering, not terminal emulation.
	visible := map[string]bool{}
	pages := 0
	for m.Offset <= len(wrap(m.Output, m.Width)) {
		view := m.View()
		if strings.Count(view, "\n") > m.Height {
			t.Fatal("default viewport overflows", view)
		}
		for _, line := range strings.Split(view, "\n") {
			visible[line] = true
		}
		pages++
		next, followup = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(Model)
		if followup != nil || len(c.methods) != 1 || pages > 128 {
			t.Fatal("paging redispatched or exceeded bounded fixture output")
		}
	}
	for _, line := range wrap(string(want), m.Width) {
		if !visible[line] {
			t.Fatalf("shared result line is inaccessible in current paged view: %q", line)
		}
	}
	if pages < 2 {
		t.Fatal("fixture did not exercise paging")
	}
	req := c.helper.requests[0]
	if req.Mode != "inspect" || req.Access != nil || req.Auxiliary.Expected != nil || req.Signature != "" || !req.ExpiresAt.IsZero() || req.ActorUID != 1000 {
		t.Fatal("metadata form supplied capture authority", req)
	}
	digest := req.PlanDigest
	req.PlanDigest = ""
	if value, err := operations.Digest(req); err != nil || value != digest {
		t.Fatal("helper read correlation was not preserved", err)
	}
	next, followup = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if followup != nil || m.Confirm || len(c.methods) != 1 {
		t.Fatal("inspection result authorizes apply")
	}
	after, _ := json.Marshal(c.native.state)
	if !bytes.Equal(before, after) || c.service.Engine != nil {
		t.Fatal("metadata read changed native state or required a mutation engine")
	}
}

func TestAuxiliaryTUIFailuresClearStaleMetadataAndApproval(t *testing.T) {
	for _, kind := range []string{"unknown-input", "missing-root", "unapproved-root", "session", "root-actor", "native-error", "helper-denied", "source-change", "pre-cancel", "post-cancel", "transport"} {
		t.Run(kind, func(t *testing.T) {
			c := auxiliaryTUIFixture()
			input, connection, code := `{"rootID":"state"}`, "qemu:///system", "INVALID_INPUT"
			nativeCalls, helperCalls := 0, 0
			switch kind {
			case "unknown-input":
				input = `{"rootID":"state","path":"/unapproved"}`
			case "missing-root":
				input = `{}`
			case "unapproved-root":
				input, code = `{"rootID":"unconfigured"}`, "PERMISSION_DENIED"
			case "session":
				connection, code = "qemu:///session", "PERMISSION_DENIED"
			case "root-actor":
				c.uid, code = 0, "PERMISSION_DENIED"
			case "native-error":
				c.native.err = domain.Fail("UNSUPPORTED_CAPABILITY", "fixture native adapter unavailable")
				code, nativeCalls = "UNSUPPORTED_CAPABILITY", 1
			case "helper-denied":
				c.helper.err = domain.Fail("PERMISSION_DENIED", "fixture administrator policy denied")
				code, nativeCalls, helperCalls = "PERMISSION_DENIED", 1, 1
			case "source-change":
				c.helper.change = true
				code, nativeCalls, helperCalls = "SOURCE_CHANGED", 1, 1
			case "pre-cancel":
				c.cancelAt, code = "pre", "OPERATION_FAILED"
			case "post-cancel":
				c.cancelAt, code, nativeCalls, helperCalls = "post", "OPERATION_FAILED", 1, 1
			case "transport":
				c.transport = errors.New("fixture disconnected")
			}
			before, _ := json.Marshal(c.native.state)
			m := auxiliaryTUIForm(t, c, connection)
			m, command := auxiliaryTUIEnter(t, m, `{"id":"`+auxiliaryTUIVM+`","input":`+input+`}`)
			if command == nil {
				t.Fatal("valid JSON did not reach shared validation")
			}
			m.Output = "earlier successful synthetic metadata"
			m.Plan = &domain.Plan{ID: "old-plan", Digest: "old-digest"}
			m.Confirm, m.Offset = true, 999
			next, followup := m.Update(command())
			m = next.(Model)
			if followup != nil || m.Plan != nil || m.Confirm || m.Busy || m.Offset != 0 || strings.Contains(m.Output, "earlier successful") || strings.Contains(m.View(), "Plan is a preview") {
				t.Fatal("failure retained stale metadata or approval", m.Output)
			}
			if kind == "transport" {
				if !strings.Contains(m.View(), "fixture disconnected") {
					t.Fatal("transport failure not visible")
				}
			} else {
				var response app.Response
				if err := wire.Decode([]byte(m.Output), &response); err != nil || response.Error == nil || response.Error.Code != code || response.Data != nil {
					t.Fatal("failure retained metadata or lost error", err, m.Output)
				}
				if !strings.Contains(m.View(), code) {
					t.Fatal("error missing from current viewport", m.View())
				}
			}
			if c.native.calls != nativeCalls || len(c.helper.requests) != helperCalls || !reflect.DeepEqual(c.methods, []string{"vm.recovery.auxiliary.inspect"}) {
				t.Fatal("failure changed observer boundaries", c.native.calls, len(c.helper.requests), c.methods)
			}
			next, followup = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			m = next.(Model)
			if followup != nil || m.Confirm || len(c.methods) != 1 {
				t.Fatal("failed metadata read authorizes apply")
			}
			after, _ := json.Marshal(c.native.state)
			if !bytes.Equal(before, after) {
				t.Fatal("failure changed native source")
			}
		})
	}
}

func TestAuxiliaryTUIStrictJSONAndEscapeNeverDispatch(t *testing.T) {
	for _, input := range []string{
		auxiliaryTUIVM,
		`{"id":"` + auxiliaryTUIVM + `","id":"other","input":{"rootID":"state"}}`,
		`{"id":"` + auxiliaryTUIVM + `","input":{"rootID":"state","rootID":"other"}}`,
		`{"id":"` + auxiliaryTUIVM + `","input":{"rootID":"state"},"apply":{}}`,
		`{"id":`,
	} {
		t.Run(input, func(t *testing.T) {
			c := auxiliaryTUIFixture()
			m := auxiliaryTUIForm(t, c, "qemu:///system")
			m, command := auxiliaryTUIEnter(t, m, input)
			if command != nil || !m.Editing || m.Busy || m.Confirm || len(c.methods) != 0 || c.native.calls != 0 || len(c.helper.requests) != 0 || !strings.Contains(m.View(), "Enter JSON") {
				t.Fatal("ambiguous form reached observers or hid error", m.View())
			}
		})
	}
	c := auxiliaryTUIFixture()
	m := auxiliaryTUIForm(t, c, "qemu:///system")
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(`{"id":"` + auxiliaryTUIVM + `","input":{"rootID":"state"}}`)})
	m = next.(Model)
	if command != nil {
		t.Fatal("typing submitted inspection")
	}
	next, command = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if command != nil || m.Editing || m.Confirm || m.Busy || m.Input != "" || len(c.methods) != 0 || c.native.calls != 0 || len(c.helper.requests) != 0 {
		t.Fatal("Escape did not cancel unsubmitted form")
	}
}
