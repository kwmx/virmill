package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

// The live XML of libvirt's standard NAT network, as libvirt reports it.
const natNetworkXML = `<network connections='1'><name>default</name><forward mode='nat'><nat><port start='1024' end='65535'/></nat></forward>` +
	`<bridge name='virbr0' stp='on' delay='0'/><ip address='192.168.122.1' netmask='255.255.255.0'><dhcp><range start='192.168.122.2' end='192.168.122.254'/></dhcp></ip></network>`

func firewallDoctor(t *testing.T, r Request, networks []domain.VirtualNetwork, zones domain.FirewallZones, ok bool) ([]domain.Capability, [][]string) {
	t.Helper()
	var asked [][]string
	s := &Service{
		Provider:  &cidrInventoryFixture{networks: networks},
		Inspector: func() []domain.Capability { return []domain.Capability{{ID: "kvm"}} },
		BridgeZones: func(_ context.Context, bridges []string) (domain.FirewallZones, bool) {
			asked = append(asked, bridges)
			return zones, ok
		},
	}
	out, err := s.dispatch(context.Background(), 1000, "host.doctor", r)
	if err != nil {
		t.Fatal(err)
	}
	checks := out.([]domain.Capability)
	if len(checks) == 0 || checks[0].ID != "kvm" {
		t.Fatalf("platform checks were replaced: %+v", checks)
	}
	return checks, asked
}

func firewallCheck(checks []domain.Capability) (domain.Capability, bool) {
	for _, c := range checks {
		if c.ID == "network-firewall" {
			return c, true
		}
	}
	return domain.Capability{}, false
}

func TestDoctorReportsBridgesFirewalldHoldsInNoZone(t *testing.T) {
	networks := []domain.VirtualNetwork{
		{Name: "default", Active: true, LiveXML: natNetworkXML},
		{Name: "lab", Active: true, LiveXML: `<network><name>lab</name><bridge name='vmlab' zone='trusted'/><ip address='10.9.0.1' prefix='24'/></network>`},
		{Name: "lan", Active: true, LiveXML: `<network><name>lan</name><forward mode='bridge'/><bridge name='br0'/></network>`},
		{Name: "off", Active: false, LiveXML: "", PersistentXML: natNetworkXML},
		{Name: "odd", Active: true, LiveXML: `<network><name>odd</name><forward mode='nat'/><bridge name='bad;name'/></network>`},
	}
	checks, asked := firewallDoctor(t, Request{}, networks, domain.FirewallZones{Default: "public", Bridges: map[string]string{"virbr0": "", "vmlab": ""}}, true)
	if !reflect.DeepEqual(asked, [][]string{{"virbr0", "vmlab"}}) {
		t.Fatalf("asked firewalld about %q; want only the bridges libvirt runs", asked)
	}
	c, ok := firewallCheck(checks)
	if !ok || c.Status != "supported-with-prerequisites" || c.ReasonCode != "NETWORK_BRIDGE_ZONE_MISSING" || c.Purpose == "" {
		t.Fatalf("check = %+v", c)
	}
	for _, want := range []string{"virbr0 (network default)", "vmlab (network lab)", "default zone public", "DHCP"} {
		if !strings.Contains(c.Reason, want) {
			t.Errorf("reason %q lacks %q", c.Reason, want)
		}
	}
	want := []string{"sudo firewall-cmd --zone=libvirt --change-interface=virbr0", "sudo firewall-cmd --zone=trusted --change-interface=vmlab"}
	if !reflect.DeepEqual(c.Alternatives, want) {
		t.Fatalf("fixes = %q; want %q", c.Alternatives, want)
	}
}

func TestDoctorReportsBridgesInTheirZoneAsReady(t *testing.T) {
	networks := []domain.VirtualNetwork{{Name: "default", Active: true, LiveXML: natNetworkXML}}
	for zones, reason := range map[*domain.FirewallZones]string{
		{Default: "public", Bridges: map[string]string{"virbr0": "libvirt"}}: "virbr0 (network default) is in zone libvirt",
		{Default: "libvirt", Bridges: map[string]string{"virbr0": ""}}:       "virbr0 (network default) uses the default zone libvirt",
	} {
		checks, _ := firewallDoctor(t, Request{}, networks, *zones, true)
		c, ok := firewallCheck(checks)
		if !ok || c.ReasonCode != "NETWORK_BRIDGE_ZONE_READY" || len(c.Alternatives) != 0 || !strings.Contains(c.Reason, reason) {
			t.Fatalf("check = %+v; want ready with %q", c, reason)
		}
	}
}

func TestDoctorSkipsTheFirewallCheckWithNothingToAsk(t *testing.T) {
	nat := []domain.VirtualNetwork{{Name: "default", Active: true, LiveXML: natNetworkXML}}
	bridged := []domain.VirtualNetwork{{Name: "lan", Active: true, LiveXML: `<network><forward mode='bridge'/><bridge name='br0'/></network>`}}
	for name, tc := range map[string]struct {
		r        Request
		networks []domain.VirtualNetwork
		ok       bool
		asks     bool
	}{
		"firewalld not answering": {Request{}, nat, false, true},
		"session connection":      {Request{Connection: "qemu:///session"}, nat, true, false},
		"no bridge libvirt runs":  {Request{}, bridged, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			checks, asked := firewallDoctor(t, tc.r, tc.networks, domain.FirewallZones{Default: "public", Bridges: map[string]string{"virbr0": ""}}, tc.ok)
			if _, found := firewallCheck(checks); found || (len(asked) > 0) != tc.asks {
				t.Fatalf("checks = %+v, asked = %q", checks, asked)
			}
		})
	}
}
