package auxiliary

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
)

type nativeFixture struct {
	observed domain.ColdStateInspection
	calls    int
	err      error
}

func (n *nativeFixture) InspectColdState(ctx context.Context, uri, id string) (domain.ColdStateInspection, error) {
	n.calls++
	if uri != "qemu:///system" || id != n.observed.Resource.UUID {
		return domain.ColdStateInspection{}, errors.New("unexpected native selection")
	}
	return n.observed, n.err
}

type helperFixture struct {
	native   *nativeFixture
	requests []helper.Request
	change   func(*helper.AuxiliaryResponse)
	err      error
	cancel   func()
}

func (*helperFixture) Root(string) (string, error) { return "/approved", nil }
func (*helperFixture) KeyID() (string, error)      { return strings.Repeat("a", 64), nil }
func (h *helperFixture) InspectAuxiliary(ctx context.Context, r helper.Request) (helper.AuxiliaryResponse, error) {
	h.requests = append(h.requests, r)
	if h.err != nil {
		return helper.AuxiliaryResponse{}, h.err
	}
	binding, err := helper.AuxiliaryBinding(r)
	if err != nil {
		return helper.AuxiliaryResponse{}, err
	}
	out := helper.AuxiliaryResponse{Version: 1, JobID: r.JobID, Binding: binding, Stage: "inspected", Inventory: &helper.AuxiliaryInventory{Version: 1, Resource: h.native.observed.Resource, Fingerprint: h.native.observed.Fingerprint, Layout: h.native.observed.Layout, Root: helper.AuxiliaryRoot{ID: r.RootID, Path: "/approved"}, Directories: []helper.AuxiliaryDirectory{}, Members: []helper.AuxiliaryMember{{ID: "members/000", Kind: "nvram", RelativePath: "nvram", State: helper.AuxiliaryFileState{Size: 1}}}, TotalBytes: 1}}
	if h.change != nil {
		h.change(&out)
	}
	if h.cancel != nil {
		h.cancel()
	}
	return out, nil
}
func serviceFixture() (*Service, *nativeFixture, *helperFixture, app.Request) {
	id := "12345678-1234-4234-8234-123456789abc"
	layout := domain.ColdStateLayout{VMID: id, Firmware: domain.ColdFirmware{NVRAM: &domain.ColdNVRAM{Path: "/approved/nvram", Format: "raw"}}, SecretReferences: []string{}}
	n := &nativeFixture{observed: domain.ColdStateInspection{Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: id}, State: "stopped", Persistent: true, Fingerprint: strings.Repeat("b", 64), Layout: layout, Source: &domain.ColdSourceLayout{State: layout}}}
	h := &helperFixture{native: n}
	return &Service{Backend: n, Helper: h}, n, h, app.Request{Connection: "qemu:///system", ID: id, Input: map[string]any{"rootID": "state"}}
}
func TestSharedAuxiliaryInspectionIsReadOnly(t *testing.T) {
	s, n, h, r := serviceFixture()
	appService := &app.Service{Extensions: map[string]func(context.Context, uint32, app.Request) (any, error){}}
	s.Register(appService)
	for i := 0; i < 2; i++ {
		response := appService.Call(context.Background(), 1000, "vm.recovery.auxiliary.inspect", r)
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		result, ok := response.Data.(Result)
		if !ok || result.APIVersion != domain.APIVersion || result.Observation.Stage != "inspected" || result.CaptureVerified || result.IndependentRestoreVerified || result.GuestBootVerified {
			t.Fatalf("false readiness: %#v", response)
		}
	}
	if n.calls != 2 || len(h.requests) != 2 || h.requests[0].JobID == h.requests[1].JobID {
		t.Fatal("inspection request freshness missing")
	}
	for _, req := range h.requests {
		if req.Access != nil || req.Auxiliary.Expected != nil || req.Mode != "inspect" || req.ActorUID != 1000 || req.Signature != "" || !req.ExpiresAt.IsZero() || req.PlanDigest == "" {
			t.Fatal("unexpected authority", req)
		}
		digest := req.PlanDigest
		req.PlanDigest = ""
		want, err := operations.Digest(req)
		if err != nil || digest != want {
			t.Fatal("read request digest differs")
		}
	}
}
func TestAuxiliaryInspectionRejectsInputBeforeObservers(t *testing.T) {
	for name, change := range map[string]func(*app.Request){
		"missing-root":   func(r *app.Request) { r.Input = map[string]any{} },
		"unknown-input":  func(r *app.Request) { r.Input["path"] = "/unapproved" },
		"root-traversal": func(r *app.Request) { r.Input["rootID"] = "../root" },
		"root-type":      func(r *app.Request) { r.Input["rootID"] = 1 },
		"vm-name":        func(r *app.Request) { r.ID = "name" },
		"zero-uuid":      func(r *app.Request) { r.ID = "00000000-0000-0000-0000-000000000000" },
		"malformed-uuid": func(r *app.Request) { r.ID = "abc\"" },
		"path":           func(r *app.Request) { r.Path = "/approved/nvram" },
		"action":         func(r *app.Request) { r.Action = "capture" },
		"after":          func(r *app.Request) { r.After = 1 },
		"apply":          func(r *app.Request) { r.Apply = &operations.ApplyRequest{} },
		"remote":         func(r *app.Request) { r.Connection = "qemu+ssh://host/system" },
		"session":        func(r *app.Request) { r.Connection = "qemu:///session" },
	} {
		t.Run(name, func(t *testing.T) {
			s, n, h, r := serviceFixture()
			change(&r)
			out, err := s.Inspect(context.Background(), 1000, r)
			if err == nil || out != nil || n.calls != 0 || len(h.requests) != 0 {
				t.Fatal("invalid request reached observers", out, err)
			}
		})
	}
	s, n, h, r := serviceFixture()
	if out, err := s.Inspect(context.Background(), 0, r); err == nil || out != nil || n.calls != 0 || len(h.requests) != 0 {
		t.Fatal("root accepted")
	}
}
func TestAuxiliaryInspectionRejectsNativeStateBeforeHelper(t *testing.T) {
	for name, change := range map[string]func(*domain.ColdStateInspection){
		"active":           func(n *domain.ColdStateInspection) { n.State = "running" },
		"transient":        func(n *domain.ColdStateInspection) { n.Persistent = false },
		"managed-save":     func(n *domain.ColdStateInspection) { n.HasManagedSave = true },
		"autostart":        func(n *domain.ColdStateInspection) { n.Autostart = true },
		"foreign-resource": func(n *domain.ColdStateInspection) { n.Resource.ProviderID = "other" },
		"layout-id":        func(n *domain.ColdStateInspection) { n.Layout.VMID = "other" },
		"source-missing":   func(n *domain.ColdStateInspection) { n.Source = nil },
		"source-state":     func(n *domain.ColdStateInspection) { n.Source.State.VMID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			s, n, h, r := serviceFixture()
			change(&n.observed)
			out, err := s.Inspect(context.Background(), 1000, r)
			if err == nil || out != nil || len(h.requests) != 0 {
				t.Fatal("unsafe native observation reached helper", out, err)
			}
		})
	}
}
func TestAuxiliaryInspectionRejectsChangedHelperProof(t *testing.T) {
	for name, change := range map[string]func(*helper.AuxiliaryResponse){
		"job":      func(r *helper.AuxiliaryResponse) { r.JobID = "other" },
		"binding":  func(r *helper.AuxiliaryResponse) { r.Binding = "other" },
		"capture":  func(r *helper.AuxiliaryResponse) { r.Stage = "captured" },
		"artifact": func(r *helper.AuxiliaryResponse) { r.Artifact = &helper.AuxiliaryArtifact{} },
		"root":     func(r *helper.AuxiliaryResponse) { r.Inventory.Root.Path = "/other" },
		"layout":   func(r *helper.AuxiliaryResponse) { r.Inventory.Layout.Firmware.Loader = "/other" },
	} {
		t.Run(name, func(t *testing.T) {
			s, _, h, r := serviceFixture()
			h.change = change
			out, err := s.Inspect(context.Background(), 1000, r)
			if err == nil || out != nil {
				t.Fatal("changed helper proof accepted", out, err)
			}
		})
	}
}
func TestAuxiliaryInspectionFailureAndCancellationHaveNoProof(t *testing.T) {
	for _, phase := range []string{"native-error", "helper-error", "pre-cancel", "post-cancel"} {
		t.Run(phase, func(t *testing.T) {
			s, n, h, r := serviceFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("fixture observer unavailable")
			switch phase {
			case "native-error":
				n.err = sentinel
			case "helper-error":
				h.err = sentinel
			case "pre-cancel":
				cancel()
			case "post-cancel":
				h.cancel = cancel
			}
			before := n.observed
			out, err := s.Inspect(ctx, 1000, r)
			if err == nil || out != nil || !reflect.DeepEqual(n.observed, before) {
				t.Fatal("failure returned proof or changed native state", out, err)
			}
			if phase == "pre-cancel" && (n.calls != 0 || len(h.requests) != 0) {
				t.Fatal("canceled request observed state")
			}
			if strings.HasSuffix(phase, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}
