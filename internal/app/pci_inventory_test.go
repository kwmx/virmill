package app

import (
	"context"
	"errors"
	"testing"
	"virmill.local/core/internal/domain"
)

type pciServiceFixture struct {
	fixtureProvider
	uri string
	err error
}

func (p *pciServiceFixture) InspectPCI(ctx context.Context, uri string) (domain.PCIInventory, error) {
	p.uri = uri
	return domain.PCIInventory{Devices: []domain.PCIDevice{{Name: "fixture", Address: "0000:00:01.0", GroupMembers: []string{}}}, Warnings: []string{"synthetic discovery only"}}, p.err
}
func TestPCIServiceDoesNotInferAssignmentAuthority(t *testing.T) {
	p := &pciServiceFixture{}
	s := &Service{Provider: p}
	r := s.Call(context.Background(), 1000, "host.pci.list", Request{Connection: "qemu:///session"})
	if r.Error != nil || p.uri != "qemu:///session" || p.calls != 0 {
		t.Fatal(r, p)
	}
	d, ok := r.Data.(domain.PCIInventory)
	if !ok || len(d.Devices) != 1 || d.Devices[0].IOMMUGroup != nil || len(d.Warnings) != 1 {
		t.Fatal("unknown topology invented", r)
	}
	p.err = errors.New("native observation refused")
	if r = s.Call(context.Background(), 1000, "host.pci.list", Request{}); r.Error == nil || p.calls != 0 {
		t.Fatal("native error hidden")
	}
	s.Provider = &fixtureProvider{}
	if r = s.Call(context.Background(), 1000, "host.pci.list", Request{}); r.Error == nil || r.Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatal("unsupported discovery invented", r)
	}
}
