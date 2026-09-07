// Package auxiliary exposes native firmware/TPM metadata inspection through the
// shared application service. It has no capture or storage mutation handler.
package auxiliary

import (
	"context"
	"reflect"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type Client interface {
	Root(string) (string, error)
	KeyID() (string, error)
	InspectAuxiliary(context.Context, helper.Request) (helper.AuxiliaryResponse, error)
}

type Service struct {
	Backend domain.ColdStateInspector
	Helper  Client
}

type Result struct {
	APIVersion                 string                   `json:"apiVersion"`
	Observation                helper.AuxiliaryResponse `json:"observation"`
	CaptureVerified            bool                     `json:"captureVerified"`
	IndependentRestoreVerified bool                     `json:"independentRestoreVerified"`
	GuestBootVerified          bool                     `json:"guestBootVerified"`
}

func (s *Service) Register(appService *app.Service) {
	appService.Extensions["vm.recovery.auxiliary.inspect"] = s.Inspect
}

func (s *Service) Inspect(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uid == 0 || r.Connection != "qemu:///system" {
		return nil, domain.Fail("PERMISSION_DENIED", "auxiliary inspection requires an ordinary actor on the local system connection")
	}
	if r.ID == "" || r.Action != "" || r.Path != "" || r.After != 0 || r.Apply != nil {
		return nil, domain.Fail("INVALID_INPUT", "auxiliary inspection accepts a stable VM UUID and rootID only")
	}
	b, err := operations.Canonical(r.Input)
	if err != nil {
		return nil, err
	}
	if err = validation.Schema("auxiliary-inspect-input", b); err != nil {
		return nil, domain.Fail("INVALID_INPUT", err.Error())
	}
	var args struct {
		RootID string `json:"rootID"`
	}
	if err = wire.Decode(b, &args); err != nil {
		return nil, err
	}
	// A VM name is not an authorization identity. Validate its canonical UUID
	// before consulting native configuration or the administrator's helper policy.
	identity, err := operations.Canonical(map[string]string{"id": r.ID})
	if err != nil {
		return nil, err
	}
	if err = validation.Schema("auxiliary-vm-identity", identity); err != nil {
		return nil, domain.Fail("INVALID_INPUT", "canonical nonzero VM UUID required")
	}
	if s.Backend == nil || s.Helper == nil {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native auxiliary metadata adapter unavailable")
	}
	root, err := s.Helper.Root(args.RootID)
	if err != nil {
		return nil, err
	}
	key, err := s.Helper.KeyID()
	if err != nil {
		return nil, err
	}
	native, err := s.Backend.InspectColdState(ctx, r.Connection, r.ID)
	if err != nil {
		return nil, err
	}
	resource := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "vm", UUID: r.ID}
	if native.Resource != resource || native.Layout.VMID != r.ID || !native.Persistent || native.State != "stopped" || native.HasManagedSave || native.Autostart || native.Source == nil || !reflect.DeepEqual(native.Layout, native.Source.State) {
		return nil, domain.Fail("INVALID_STATE", "exact persistent stopped VM without managed save or autostart required")
	}
	req := helper.Request{APIVersion: domain.APIVersion, ActorUID: uid, Operation: "state.auxiliary", ResourceID: r.ID, RootID: args.RootID, JobID: domain.ID(), KeyID: key, Mode: "inspect", Auxiliary: &helper.AuxiliaryRequest{Version: 1, Fingerprint: native.Fingerprint}}
	// This digest and random request ID correlate a read, not a durable job or a
	// reviewed capture plan. No helper journal entry or coordinator job is created.
	req.PlanDigest, err = operations.Digest(req)
	if err != nil {
		return nil, err
	}
	observed, err := s.Helper.InspectAuxiliary(ctx, req)
	if err != nil {
		return nil, err
	}
	if err = helper.ValidateAuxiliaryInspection(req, &observed); err != nil {
		return nil, err
	}
	if observed.Inventory.Root.Path != root || !reflect.DeepEqual(observed.Inventory.Layout, native.Layout) {
		return nil, domain.Fail("SOURCE_CHANGED", "helper root or native auxiliary layout changed during inspection")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return Result{APIVersion: domain.APIVersion, Observation: observed}, nil
}
