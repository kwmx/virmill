package tui

import (
	"errors"

	"virmill.local/core/internal/domain"
)

type coordinatorConnection struct {
	Unavailable, Expanded bool
	observedToken         uint64
}

// Observe only ordinary inventory replies. Synthetic result bundles can contain
// failed reads inside a successful envelope and are not a connectivity check.
// The caller first validates the per-request token; this additional ordering
// prevents an older concurrent inventory reply replacing newer connection facts.
func (m *Workspace) observeCoordinatorConnection(reply workspaceReply) {
	switch reply.Kind {
	case "vms", "jobs", "pools", "health":
	default:
		return
	}
	if reply.Token < m.coordinator.observedToken {
		return
	}
	m.coordinator.observedToken = reply.Token
	err := reply.Err
	if err == nil && reply.Response.Error != nil {
		err = reply.Response.Error
	}
	previous := m.coordinator.Unavailable
	var failure *domain.Error
	if errors.As(err, &failure) && failure != nil && failure.Code == "COORDINATOR_UNAVAILABLE" {
		m.coordinator.Unavailable = true
	} else if reply.Err == nil {
		// A service-level refusal still proves that the coordinator answered.
		m.coordinator.Unavailable, m.coordinator.Expanded = false, false
	}
	if previous != m.coordinator.Unavailable {
		// Never shift a focused VM action onto an asynchronously inserted button.
		m.ButtonFocus = false
		m.ButtonIndex = 0
	}
}

func (m Workspace) connectionRecoveryVisible() bool {
	return m.coordinator.Unavailable && (m.Section == 0 || m.Section == 10) && m.Detail == nil
}

func (m Workspace) connectionRecoveryButtons() []workspaceButton {
	help := "More help"
	if m.coordinator.Expanded {
		help = "Less help"
	}
	return []workspaceButton{{"Retry connection", "r"}, {help, "connection-help"}}
}

func (m Workspace) connectionRecoveryView(width, height int) []string {
	lines := []string{
		"Background service unavailable", "",
		"Virmill cannot reach its background service.",
		"Your VMs may still be running; their current state is unknown.", "",
		"Start the installed service as your normal user:",
		"  systemctl --user start virmilld.service",
		"Then choose Retry connection.",
	}
	if m.coordinator.Expanded {
		lines = append(lines, "",
			"Optional: start Virmill's service at future sign-ins:",
			"  systemctl --user enable virmilld.service",
			"This does not enable guest autostart or background service after logout.",
			"If startup fails, inspect its status:",
			"  systemctl --user status virmilld.service",
			"Source build without the unit? Run virmilld in another terminal.",
			"PgUp/PgDn scroll. Less help returns to the short view.")
	}
	return pageLines(lines, width, height, m.Offset)
}
