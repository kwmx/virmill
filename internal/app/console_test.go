package app

import (
	"context"
	"testing"
	"virmill.local/core/internal/domain"
)

type consoleTestProvider struct {
	domain.ComputeProvider
	calls int
}

func (p *consoleTestProvider) InspectConsole(_ context.Context, connection, id string) (domain.ConsoleInfo, error) {
	p.calls++
	return domain.ConsoleInfo{Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: connection, Kind: "vm", UUID: id}}, nil
}
func TestConsoleSharedServiceRoutesOnlyReadOnlySelection(t *testing.T) {
	p := &consoleTestProvider{}
	s := &Service{Provider: p}
	request := Request{Connection: "qemu:///system", ID: "12345678-1234-4234-8234-123456789abc"}
	result := s.Call(context.Background(), 1000, "vm.console.show", request)
	if result.Error != nil || p.calls != 1 {
		t.Fatal(result)
	}
	for _, r := range []Request{{}, {ID: request.ID, Action: "start"}, {ID: request.ID, Path: "/tmp/image"}, {ID: request.ID, Input: map[string]any{"command": "anything"}}, {ID: request.ID, After: 1}} {
		result = s.Call(context.Background(), 1000, "vm.console.show", r)
		if result.Error == nil || p.calls != 1 {
			t.Fatal("console accepted mutation fields", r, result)
		}
	}
}
