package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/auxiliary"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const auxiliaryCLIVM = "12345678-1234-4234-8234-123456789abc"

type auxiliaryCLINative struct {
	state domain.ColdStateInspection
	calls int
	err   error
}

func (n *auxiliaryCLINative) InspectColdState(_ context.Context, uri, id string) (domain.ColdStateInspection, error) {
	n.calls++
	if uri != "qemu:///system" || id != auxiliaryCLIVM {
		return domain.ColdStateInspection{}, errors.New("unexpected native selection")
	}
	return n.state, n.err
}

type auxiliaryCLIHelper struct {
	native   *auxiliaryCLINative
	roots    []string
	requests []helper.Request
	err      error
	change   bool
	cancel   context.CancelFunc
}

func (h *auxiliaryCLIHelper) Root(id string) (string, error) {
	h.roots = append(h.roots, id)
	if id != "state" {
		return "", domain.Fail("PERMISSION_DENIED", "fixture root is not approved")
	}
	return "/fixture/approved-state", nil
}
func (*auxiliaryCLIHelper) KeyID() (string, error) { return strings.Repeat("a", 64), nil }
func (h *auxiliaryCLIHelper) InspectAuxiliary(_ context.Context, r helper.Request) (helper.AuxiliaryResponse, error) {
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

type auxiliaryCLIClient struct {
	service   *app.Service
	native    *auxiliaryCLINative
	helper    *auxiliaryCLIHelper
	methods   []string
	requests  []app.Request
	responses []app.Response
	transport error
	uid       uint32
}

func (c *auxiliaryCLIClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	c.methods = append(c.methods, method)
	c.requests = append(c.requests, r)
	if c.transport != nil {
		return app.Response{}, c.transport
	}
	response := c.service.Call(ctx, c.uid, method, r)
	c.responses = append(c.responses, response)
	return response, nil
}

func auxiliaryCLIFixture() *auxiliaryCLIClient {
	layout := domain.ColdStateLayout{VMID: auxiliaryCLIVM, Firmware: domain.ColdFirmware{NVRAM: &domain.ColdNVRAM{Path: "/fixture/approved-state/vars.fd", Format: "raw"}}, SecretReferences: []string{}}
	native := &auxiliaryCLINative{state: domain.ColdStateInspection{Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: auxiliaryCLIVM}, State: "stopped", Persistent: true, Fingerprint: strings.Repeat("b", 64), Layout: layout, Source: &domain.ColdSourceLayout{State: layout}}}
	h := &auxiliaryCLIHelper{native: native}
	s := &app.Service{Extensions: map[string]func(context.Context, uint32, app.Request) (any, error){}}
	(&auxiliary.Service{Backend: native, Helper: h}).Register(s)
	return &auxiliaryCLIClient{service: s, native: native, helper: h, uid: 1000}
}

func auxiliaryCLIArgs(input, format, connection string) []string {
	return []string{"vm", "recovery", "auxiliary", "inspect", auxiliaryCLIVM, "--input", input, "--output", format, "--connection", connection, "--non-interactive"}
}

func TestAuxiliaryCLIMachineOutputPreservesSharedReadOnlyObservation(t *testing.T) {
	c := auxiliaryCLIFixture()
	before, _ := json.Marshal(c.native.state)
	for _, format := range []string{"json", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			cmd := New(c, &out, &diagnostics)
			cmd.SetArgs(auxiliaryCLIArgs(`{"rootID":"state"}`, format, "qemu:///system"))
			if err := cmd.Execute(); err != nil || diagnostics.Len() != 0 {
				t.Fatal("inspection failed", err, diagnostics.String())
			}
			if bytes.Count(out.Bytes(), []byte{'\n'}) != 1 {
				t.Fatal("machine output is not one clean JSON record", out.String())
			}
			var response app.Response
			if err := wire.Decode(out.Bytes(), &response); err != nil || response.Error != nil {
				t.Fatal("machine envelope invalid", err, response)
			}
			want, _ := json.Marshal(c.responses[len(c.responses)-1])
			if !bytes.Equal(bytes.TrimSpace(out.Bytes()), want) {
				t.Fatal("CLI changed the actual shared-service response")
			}
			result := c.responses[len(c.responses)-1].Data.(auxiliary.Result)
			if result.APIVersion != domain.APIVersion || result.CaptureVerified || result.IndependentRestoreVerified || result.GuestBootVerified || result.Observation.Artifact != nil || result.Observation.Stage != "inspected" || result.Observation.Inventory.TotalBytes != 4096 {
				t.Fatal("metadata was promoted to captured or recoverable state", result)
			}
			if result.Observation.Inventory.Resource.UUID != auxiliaryCLIVM || result.Observation.Inventory.Members[0].State.Generation != "synthetic-generation" {
				t.Fatal("metadata identity lost", result)
			}
		})
	}
	if c.native.calls != 2 || len(c.helper.requests) != 2 || c.helper.requests[0].JobID == c.helper.requests[1].JobID {
		t.Fatal("independent inspections did not receive fresh correlation IDs")
	}
	for i, req := range c.helper.requests {
		if c.methods[i] != "vm.recovery.auxiliary.inspect" || c.requests[i].Action != "" || c.requests[i].Apply != nil || req.Mode != "inspect" || req.Access != nil || req.Auxiliary.Expected != nil || req.Signature != "" || !req.ExpiresAt.IsZero() || req.ActorUID != 1000 {
			t.Fatal("CLI metadata read acquired capture/apply authority", req)
		}
		digest := req.PlanDigest
		req.PlanDigest = ""
		if want, err := operations.Digest(req); err != nil || want != digest {
			t.Fatal("read correlation digest changed", err)
		}
	}
	after, _ := json.Marshal(c.native.state)
	if !bytes.Equal(before, after) || c.service.Engine != nil {
		t.Fatal("read-only UI fixture changed source or required a mutation engine")
	}
}

func TestAuxiliaryCLIErrorsAndCancellationDoNotReturnMetadata(t *testing.T) {
	for _, format := range []string{"json", "ndjson"} {
		for _, kind := range []string{"unknown-input", "missing-root", "unapproved-root", "session", "root-actor", "native-error", "helper-denied", "source-change", "pre-cancel", "post-cancel", "transport"} {
			t.Run(format+"/"+kind, func(t *testing.T) {
				c := auxiliaryCLIFixture()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
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
					cancel()
					code = "OPERATION_FAILED"
				case "post-cancel":
					c.helper.cancel = cancel
					code, nativeCalls, helperCalls = "OPERATION_FAILED", 1, 1
				case "transport":
					c.transport, code = errors.New("fixture disconnected"), "OPERATION_FAILED"
				}
				before, _ := json.Marshal(c.native.state)
				var out bytes.Buffer
				cmd := New(c, &out, &out)
				cmd.SetArgs(auxiliaryCLIArgs(input, format, connection))
				err := cmd.ExecuteContext(ctx)
				var response app.Response
				if decodeErr := wire.Decode(out.Bytes(), &response); decodeErr != nil || err == nil || domain.ExitCode(err) == 0 || response.Error == nil || response.Error.Code != code || response.Data != nil {
					t.Fatal("failure hidden or successful metadata leaked", kind, err, decodeErr, out.String())
				}
				if bytes.Count(out.Bytes(), []byte{'\n'}) != 1 || c.native.calls != nativeCalls || len(c.helper.requests) != helperCalls || !reflect.DeepEqual(c.methods, []string{"vm.recovery.auxiliary.inspect"}) {
					t.Fatal("failure changed dispatch/observer boundaries", c.methods, c.native.calls, len(c.helper.requests))
				}
				after, _ := json.Marshal(c.native.state)
				if !bytes.Equal(before, after) {
					t.Fatal("failed observation changed source")
				}
			})
		}
	}
}

func TestAuxiliaryCLIStrictJSONRefusesBeforeDispatch(t *testing.T) {
	for _, input := range []string{`{"rootID":"state","rootID":"other"}`, `{"rootID":`, `[]`, `{"rootID":"state"} {}`} {
		t.Run(input, func(t *testing.T) {
			c := auxiliaryCLIFixture()
			var out bytes.Buffer
			cmd := New(c, &out, &out)
			cmd.SetArgs(auxiliaryCLIArgs(input, "json", "qemu:///system"))
			err := cmd.Execute()
			if err == nil || len(c.methods) != 0 || c.native.calls != 0 || len(c.helper.requests) != 0 || out.Len() != 0 {
				t.Fatal("ambiguous input reached observers or returned success", err, out.String())
			}
		})
	}
}
