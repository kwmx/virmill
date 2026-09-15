//go:build linux

package linux

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

type answer struct {
	out  string
	code int
	err  error
}

// fakeFirewall answers like firewall-cmd; unexpected questions fail the run.
func fakeFirewall(answers map[string]answer) (firewallCmd, *[]string) {
	asked := []string{}
	return func(_ context.Context, args ...string) (string, int, error) {
		key := strings.Join(args, " ")
		asked = append(asked, key)
		a, ok := answers[key]
		if !ok {
			return "", 0, errors.New("unexpected question " + key)
		}
		return a.out, a.code, a.err
	}, &asked
}

func TestBridgeFirewallZonesReadsDefaultAndBridgeZones(t *testing.T) {
	// These are the answers of a Fedora host whose libvirt network started
	// before firewalld: its bridge is in no zone and the default is public.
	run, asked := fakeFirewall(map[string]answer{
		"--get-default-zone":             {out: "public\n"},
		"--get-zone-of-interface=virbr0": {out: "no zone\n", code: 2},
		"--get-zone-of-interface=vmlab":  {out: "trusted\n"},
		"--get-zone-of-interface=virbr1": {out: "Error: INVALID_INTERFACE\n", code: 1},
	})
	zones, ok := bridgeFirewallZones(context.Background(), []string{"virbr0", "vmlab", "bad;name", "virbr1", "bridge-name-too-long"}, run)
	want := domain.FirewallZones{Default: "public", Bridges: map[string]string{"virbr0": "", "vmlab": "trusted"}}
	if !ok || !reflect.DeepEqual(zones, want) {
		t.Fatalf("zones = %+v, %v; want %+v", zones, ok, want)
	}
	if strings.Contains(strings.Join(*asked, " "), "bad;name") || strings.Contains(strings.Join(*asked, " "), "too-long") {
		t.Fatalf("asked firewalld about a name it cannot hold: %q", *asked)
	}
}

func TestBridgeFirewallZonesNeedsAnAnsweringFirewalld(t *testing.T) {
	for name, a := range map[string]answer{
		"not running":   {out: "FirewallD is not running\n", code: 252},
		"not allowed":   {out: "Authorization failed.\n    Make sure polkit agent is running or run the application as superuser.\n", code: 11},
		"timed out":     {err: context.DeadlineExceeded},
		"odd zone name": {out: "public zone\n"},
	} {
		t.Run(name, func(t *testing.T) {
			run, _ := fakeFirewall(map[string]answer{"--get-default-zone": a})
			if zones, ok := bridgeFirewallZones(context.Background(), []string{"virbr0"}, run); ok {
				t.Fatalf("reported %+v without an answering firewalld", zones)
			}
		})
	}
	run, _ := fakeFirewall(map[string]answer{
		"--get-default-zone":             {out: "public\n"},
		"--get-zone-of-interface=virbr0": {err: context.DeadlineExceeded},
	})
	if zones, ok := bridgeFirewallZones(context.Background(), []string{"virbr0"}, run); ok {
		t.Fatalf("reported %+v after firewalld stopped answering", zones)
	}
}
