package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/update"
)

func TestWorkspaceShowsAvailableUpdate(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	if m.checkUpdates() != nil {
		t.Fatal("a workspace without RunOptions must not contact GitHub")
	}
	next, _ := m.Update(updateReply{Result: update.Result{Current: "1.0.0-beta.3", Latest: &update.Release{Version: "1.0.0-beta.99"}}})
	m = next.(Workspace)
	if view := m.View(); !strings.Contains(view, "Update    Virmill 1.0.0-beta.99 is available · run virmill update") {
		t.Fatalf("home lacks the update line:\n%s", view)
	}
	m.Section, m.Height = 10, 60
	if view := m.View(); !strings.Contains(view, "Updates: Virmill 1.0.0-beta.99 is available") || !strings.Contains(view, "Install it from a terminal: virmill update") {
		t.Fatalf("settings lack the update:\n%s", view)
	}
}

func TestWorkspaceUpdateSettingsStates(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	for _, tc := range []struct {
		name  string
		setup func(*Workspace)
		want  string
	}{
		{"off", func(m *Workspace) { m.updatesOff = true }, "Turn them on with: virmill update checks on"},
		{"never", func(m *Workspace) {}, "Updates: not checked yet"},
		{"checking", func(m *Workspace) { m.updateCheck = func(context.Context) update.Result { return update.Result{} } }, "Updates: checking GitHub..."},
		{"failed", func(m *Workspace) { m.Updates = &update.Result{Error: "GitHub did not answer in time"} }, "the last check failed (GitHub did not answer in time)"},
		{"current", func(m *Workspace) {
			m.Updates = &update.Result{Current: "1.0.0-beta.3", CheckedAt: now.Add(-2 * time.Hour)}
		}, "Updates: up to date (checked 2 h ago)"},
	} {
		m := fixtureWorkspace()
		m.Width, m.Height, m.Section = 100, 60, 10
		tc.setup(&m)
		if view := m.View(); !strings.Contains(view, tc.want) {
			t.Fatalf("%s: settings lack %q:\n%s", tc.name, tc.want, view)
		}
		if tc.name != "failed" && strings.Contains(m.View(), "Update    ") {
			t.Fatalf("%s: home shows an update line without a newer release", tc.name)
		}
	}
	m := fixtureWorkspace()
	m.updateCheck = func(context.Context) update.Result { return update.Result{Current: "x"} }
	if reply, ok := m.checkUpdates()().(updateReply); !ok || reply.Result.Current != "x" {
		t.Fatal("check result not delivered", reply)
	}
}
