package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/update"
)

type updateReply struct{ Result update.Result }

// updateChecker returns the daily check, or nil and true when checks are off.
// The TUI never downloads or installs; `virmill update` does that in a terminal.
func updateChecker() (func(context.Context) update.Result, bool) {
	paths, err := update.UserPaths()
	if err != nil {
		return nil, false
	}
	if !paths.ChecksEnabled() {
		return nil, true
	}
	return func(ctx context.Context) update.Result {
		return update.CheckIfDue(ctx, update.Default(), paths, buildinfo.Version, time.Now())
	}, false
}

func (m Workspace) checkUpdates() tea.Cmd {
	check := m.updateCheck
	if check == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return updateReply{Result: check(ctx)}
	}
}

// updateNotice is the Overview line; empty unless a newer release exists.
func (m Workspace) updateNotice() string {
	if m.Updates == nil || !m.Updates.Available() {
		return ""
	}
	return "Virmill " + m.Updates.Latest.Version + " is available" + m.separator() + "run virmill update"
}

// updateLines describe the update check on the Settings page.
func (m Workspace) updateLines() []string {
	r := m.Updates
	switch {
	case m.updatesOff:
		return []string{"Updates: daily checks are off", "  Turn them on with: virmill update checks on"}
	case r == nil && m.updateCheck != nil:
		return []string{"Updates: checking GitHub..."}
	case r == nil:
		return []string{"Updates: not checked yet", "  Check now with: virmill update check"}
	case r.Available():
		return []string{"Updates: Virmill " + r.Latest.Version + " is available", "  Install it from a terminal: virmill update"}
	case r.Error != "":
		return []string{"Updates: the last check failed (" + r.Error + ")", "  Try again with: virmill update check"}
	}
	return []string{"Updates: up to date (checked " + ago(r.CheckedAt.Format(time.RFC3339Nano), clock()) + ")",
		"  Daily checks are on; turn them off with: virmill update checks off"}
}
