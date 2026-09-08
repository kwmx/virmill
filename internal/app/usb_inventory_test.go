package app

import (
	"context"
	"errors"
	"testing"

	"virmill.local/core/internal/domain"
)

type usbInventoryFixture struct {
	domain.ComputeProvider
	uri   string
	calls int
	err   error
}

func (p *usbInventoryFixture) InspectUSB(ctx context.Context, uri string) ([]domain.USBDevice, error) {
	p.uri, p.calls = uri, p.calls+1
	if p.err != nil {
		return nil, p.err
	}
	return []domain.USBDevice{{Name: "usb-fixture", Reason: "discovery only"}}, ctx.Err()
}

func TestUSBDiscoveryIsReadOnlyAndPropagatesFailure(t *testing.T) {
	s, _, _ := configService(t)
	p := &usbInventoryFixture{}
	s.Provider = p
	response := s.Call(context.Background(), 1000, "device.usb.list", Request{Connection: "qemu:///session"})
	if response.Error != nil || p.uri != "qemu:///session" || len(response.Data.([]domain.USBDevice)) != 1 {
		t.Fatal(response)
	}
	for _, bad := range []Request{{ID: "unexpected"}, {Action: "attach"}, {Input: map[string]any{"reset": true}}} {
		if r := s.Call(context.Background(), 1000, "device.usb.list", bad); r.Error == nil || p.calls != 1 {
			t.Fatal(r)
		}
	}
	p.err = errors.New("observation unavailable")
	if r := s.Call(context.Background(), 1000, "device.usb.list", Request{}); r.Error == nil || r.Data != nil {
		t.Fatal(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := s.Call(ctx, 1000, "device.usb.list", Request{}); r.Error == nil || p.calls != 2 {
		t.Fatal(r)
	}
	s.Provider = nil
	if r := s.Call(context.Background(), 1000, "device.usb.list", Request{}); r.Error == nil || r.Error.Code != "UNSUPPORTED_CAPABILITY" {
		t.Fatal(r)
	}
	for _, table := range []string{"plans", "jobs", "events", "dedup", "locks", "metadata"} {
		var count int
		if err := s.Engine.Store.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("discovery changed %s: %d %v", table, count, err)
		}
	}
}
