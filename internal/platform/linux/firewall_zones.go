//go:build linux

package linux

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"virmill.local/core/internal/domain"
)

// zoneName is what firewalld accepts for interface and zone names; anything
// else is not asked about or reported.
var zoneName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// firewallCmd runs firewall-cmd and returns its output and exit code.
type firewallCmd func(ctx context.Context, args ...string) (string, int, error)

// BridgeFirewallZones asks firewalld, without root, which zone holds each
// bridge. ok is false when firewalld is not installed, not running or does not
// answer. It only reads; it never changes a zone.
func BridgeFirewallZones(ctx context.Context, bridges []string) (domain.FirewallZones, bool) {
	path, err := exec.LookPath("firewall-cmd")
	if err != nil {
		return domain.FirewallZones{}, false
	}
	return bridgeFirewallZones(ctx, bridges, func(ctx context.Context, args ...string) (string, int, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) && ctx.Err() == nil {
			return out.String(), exit.ExitCode(), nil
		}
		return out.String(), 0, err
	})
}

func bridgeFirewallZones(ctx context.Context, bridges []string, run firewallCmd) (domain.FirewallZones, bool) {
	out, code, err := run(ctx, "--get-default-zone")
	zone := strings.TrimSpace(out)
	if err != nil || code != 0 || !zoneName.MatchString(zone) {
		return domain.FirewallZones{}, false
	}
	zones := domain.FirewallZones{Default: zone, Bridges: map[string]string{}}
	for _, bridge := range bridges {
		if !zoneName.MatchString(bridge) || len(bridge) > 15 {
			continue
		}
		out, code, err := run(ctx, "--get-zone-of-interface="+bridge)
		zone := strings.TrimSpace(out)
		switch {
		case err != nil:
			return domain.FirewallZones{}, false
		case code == 0 && zoneName.MatchString(zone):
			zones.Bridges[bridge] = zone
		case zone == "no zone":
			zones.Bridges[bridge] = ""
		}
	}
	return zones, true
}
