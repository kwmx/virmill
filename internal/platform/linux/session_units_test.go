//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unitLines reads a packaged unit file and returns its non-comment lines.
func unitLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "systemd", name))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func requireLines(t *testing.T, unit string, lines []string, wanted ...string) {
	t.Helper()
	have := map[string]bool{}
	for _, line := range lines {
		have[line] = true
	}
	for _, line := range wanted {
		if !have[line] {
			t.Errorf("%s must contain %q", unit, line)
		}
	}
}

// ADR 0064: session VMs start without a manual step only while each of these
// holds. A daemon a client starts inherits the client's restrictions, so the
// coordinator must find the socket already listening and the daemon must be
// started by systemd, and must never set the restriction itself.
func TestPackagedUnitsStartTheSessionDaemonOutsideTheCoordinator(t *testing.T) {
	coordinator := unitLines(t, "virmilld.service")
	requireLines(t, "virmilld.service", coordinator,
		"Wants=virmill-virtqemud.socket", "After=virmill-virtqemud.socket",
		// The hardening stays: the fix is the socket, not relaxing this.
		"NoNewPrivileges=yes")

	socket := unitLines(t, "virmill-virtqemud.socket")
	requireLines(t, "virmill-virtqemud.socket", socket,
		"ListenStream=%t/libvirt/virtqemud-sock",
		// libvirt only adopts a socket it is handed under this name.
		"FileDescriptorName=virtqemud.socket",
		"Service=virmill-virtqemud.service",
		"SocketMode=0600",
		// Never compete with a distribution's own per-user socket, and never
		// listen where no daemon could answer.
		"ConditionPathExists=!/usr/lib/systemd/user/virtqemud.socket",
		"ConditionFileIsExecutable=/usr/sbin/virtqemud")

	daemon := unitLines(t, "virmill-virtqemud.service")
	requireLines(t, "virmill-virtqemud.service", daemon,
		"Requires=virmill-virtqemud.socket", "After=virmill-virtqemud.socket",
		"ExecStart=/usr/sbin/virtqemud --timeout 120")
	for _, line := range daemon {
		if strings.HasPrefix(line, "NoNewPrivileges") {
			t.Errorf("virmill-virtqemud.service must not set %q: the daemon launches QEMU", line)
		}
	}
}
