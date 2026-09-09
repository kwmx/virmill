package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

type creationChoicesFixture struct {
	fixtureProvider
	choicesCalls int
	connection   string
	machine      string
	result       domain.CreationOptions
	failure      error
}

func (p *creationChoicesFixture) CreationOptions(ctx context.Context, connection, machine string) (domain.CreationOptions, error) {
	p.choicesCalls++
	p.connection, p.machine = connection, machine
	if err := ctx.Err(); err != nil {
		return domain.CreationOptions{}, err
	}
	return p.result, p.failure
}

func TestCreationOptionsSharedReadOnlyDispatch(t *testing.T) {
	observed := domain.CreationOptions{Architecture: "x86_64", Machines: []string{"pc-q35-observed"}, Machine: "pc-q35-observed", MaxVCPUs: 64, HostMemoryMiB: 8192, CPUModes: []string{"host-model"}, CPUModels: []string{}, Firmware: []domain.CreationFirmwareOption{{Label: "BIOS", Firmware: domain.CreationFirmware{Mode: "bios"}}}, DiskBuses: []string{"virtio"}, Graphics: []string{"none"}}
	p := &creationChoicesFixture{result: observed}
	// A nil engine is intentional: this read-only method must not plan or apply.
	s := &Service{Provider: p}
	for _, request := range []Request{{}, {Connection: "qemu:///system"}, {Connection: "qemu:///session", Input: map[string]any{"machine": "pc-q35-observed"}}, {Connection: "qemu:///session", Input: map[string]any{"machine": ""}}} {
		previous := p.choicesCalls
		response := s.Call(context.Background(), 1000, "vm.creation.options", request)
		wantConnection := request.Connection
		if wantConnection == "" {
			wantConnection = "qemu:///system"
		}
		wantMachine, _ := request.Input["machine"].(string)
		if response.Error != nil || response.APIVersion != domain.APIVersion || !reflect.DeepEqual(response.Data, observed) || p.choicesCalls != previous+1 || p.connection != wantConnection || p.machine != wantMachine || p.calls != 0 {
			t.Fatalf("read-only dispatch changed observed options or invoked effects: %+v %+v", response, p)
		}
	}
}

func TestCreationOptionsRejectsExtraAndRemoteInputsBeforeBackend(t *testing.T) {
	p := &creationChoicesFixture{}
	s := &Service{Provider: p}
	for name, request := range map[string]Request{
		"id": {ID: "other"}, "path": {Path: "/tmp/file"}, "action": {Action: "create"},
		"after": {After: 1}, "negative cursor": {After: -1}, "apply": {Apply: &operations.ApplyRequest{}},
		"unknown field":     {Input: map[string]any{"other": "q35"}},
		"multiple fields":   {Input: map[string]any{"machine": "q35", "other": true}},
		"nonstring machine": {Input: map[string]any{"machine": 3}},
		"null machine":      {Input: map[string]any{"machine": nil}},
		"oversized machine": {Input: map[string]any{"machine": strings.Repeat("a", 129)}},
		"escape machine":    {Input: map[string]any{"machine": "q35\x1b[31m"}},
		"remote":            {Connection: "qemu+ssh://unapproved/system"},
		"test provider":     {Connection: "test:///default"},
	} {
		t.Run(name, func(t *testing.T) {
			response := s.Call(context.Background(), 1000, "vm.creation.options", request)
			if response.Error == nil || response.Data != nil || p.choicesCalls != 0 || p.calls != 0 {
				t.Fatalf("invalid input reached backend: %+v %+v", response, p)
			}
			want := "INVALID_INPUT"
			if name == "remote" || name == "test provider" {
				want = "UNSUPPORTED_CAPABILITY"
			}
			if response.Error.Code != want {
				t.Fatalf("error %q want %q", response.Error.Code, want)
			}
		})
	}
}

func TestCreationOptionsCancellationUnsupportedAndBackendErrors(t *testing.T) {
	p := &creationChoicesFixture{}
	s := &Service{Provider: p}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if response := s.Call(ctx, 1000, "vm.creation.options", Request{}); response.Error == nil || p.choicesCalls != 0 || p.calls != 0 {
		t.Fatalf("canceled request reached backend: %+v %+v", response, p)
	}
	s.Provider = &fixtureProvider{}
	if response := s.Call(context.Background(), 1000, "vm.creation.options", Request{}); response.Error == nil || response.Error.Code != "UNSUPPORTED_CAPABILITY" || response.Error.Message == "" {
		t.Fatalf("unsupported backend did not explain missing choices: %+v", response)
	}
	s.Provider = p
	for _, failure := range []error{domain.Fail("UNSUPPORTED_CAPABILITY", "No advertised KVM machine; enable KVM."), errors.New("native inventory unavailable")} {
		p.failure = failure
		response := s.Call(context.Background(), 1000, "vm.creation.options", Request{})
		if response.Error == nil || response.Error.Message == "" || p.calls != 0 {
			t.Fatalf("backend error swallowed or executed mutation: %+v", response)
		}
	}
}
