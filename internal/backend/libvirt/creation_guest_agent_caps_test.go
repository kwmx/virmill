//go:build linux && cgo

package libvirt

import (
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func TestCreationGuestAgentRequiresExplicitUnixChannelCapability(t *testing.T) {
	for _, tc := range []struct {
		name    string
		channel capDevice
		allowed bool
	}{
		{"missing", capDevice{}, false},
		{"unsupported", capDevice{Supported: "no", Enums: []capEnum{{Name: "type", Values: []string{"unix"}}}}, false},
		{"unknown support", capDevice{Supported: "unknown", Enums: []capEnum{{Name: "type", Values: []string{"unix"}}}}, false},
		{"missing transport", capDevice{Supported: "yes"}, false},
		{"tcp only", capDevice{Supported: "yes", Enums: []capEnum{{Name: "type", Values: []string{"tcp"}}}}, false},
		{"wrong enum", capDevice{Supported: "yes", Enums: []capEnum{{Name: "backend", Values: []string{"unix"}}}}, false},
		{"case alias", capDevice{Supported: "yes", Enums: []capEnum{{Name: "type", Values: []string{"UNIX"}}}}, false},
		{"unix", capDevice{Supported: "yes", Enums: []capEnum{{Name: "type", Values: []string{"unix"}}}}, true},
		{"multiple transports including unix", capDevice{Supported: "yes", Enums: []capEnum{{Name: "type", Values: []string{"tcp", "unix"}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caps, _ := optionsFixture(t)
			caps.Devices.Channel = tc.channel
			spec := domain.CreationSpec{VCPUs: 2, CPU: domain.CreationCPU{Mode: "host-model"}, Firmware: domain.CreationFirmware{Mode: "bios"}, Graphics: "none"}
			if err := checkCaps(caps, spec); err != nil {
				t.Fatal("legacy no-channel request acquired a new prerequisite", err)
			}
			spec.GuestAgent = true
			err := checkCaps(caps, spec)
			if tc.allowed {
				if err != nil {
					t.Fatal("explicit unix capability rejected", err)
				}
				return
			}
			var failure *domain.Error
			if !errors.As(err, &failure) || failure.Code != "UNSUPPORTED_CAPABILITY" || !strings.Contains(failure.Message, "guest-agent channel") {
				t.Fatal("missing unix channel capability did not produce an actionable refusal", err)
			}
		})
	}
}
