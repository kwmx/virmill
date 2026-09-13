package tui

import (
	"strings"
	"testing"
)

func TestWorkspaceHeaderShowsJobsAndConnection(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	first := strings.Split(m.View(), "\n")[0]
	if !strings.Contains(first, "Virmill  / Overview") || !strings.HasSuffix(strings.TrimSpace(first), "qemu:///system") ||
		strings.Contains(first, "beta") || strings.Contains(first, "job") {
		t.Fatalf("idle header %q", first)
	}
	m.Data["jobs"] = []any{map[string]any{"state": "running"}, map[string]any{"state": "running"}, map[string]any{"state": "failed"}}
	first = strings.Split(m.View(), "\n")[0]
	if !strings.Contains(first, "2 jobs running, 1 job needs attention") || !strings.Contains(first, "qemu:///system") {
		t.Fatalf("busy header %q", first)
	}
	if m.status() != "2 jobs running, 1 job needs attention" {
		t.Fatalf("overview jobs line %q", m.status())
	}
}

func TestWorkspaceEmptyListsExplainNextStep(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Section = 2
	m.Data["networks"] = []any{}
	view := m.View()
	if !strings.Contains(view, "No networks yet.") || !strings.Contains(view, "Create network sets up") ||
		strings.Contains(view, "Details") || strings.Contains(view, "Select") {
		t.Fatalf("empty networks page:\n%s", view)
	}
	for _, section := range []int{0, 1, 2, 3, 6, 7, 8, 9} {
		if lines := emptyState(section); len(lines) < 3 || lines[0] == "Nothing to show here yet." {
			t.Fatalf("section %d has no tailored empty state", section)
		}
	}
}

func TestWorkspaceUnfinishedSectionsAreLabeled(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 120, 36
	view := m.View()
	for _, mark := range []string{"Templates  not ready", "Labs       validate only", "Devices    discovery only", "Plugins    preview"} {
		if !strings.Contains(view, mark) {
			t.Fatalf("sidebar lacks %q:\n%s", mark, view)
		}
	}
	m.Section = 4
	if view = m.View(); !strings.Contains(view, "Templates aren't available in this beta yet.") || strings.Contains(view, "under development") {
		t.Fatalf("templates page:\n%s", view)
	}
}

func TestWorkspaceCounterAndHints(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Section = 1
	m.Selected = 1
	view := m.View()
	if !strings.Contains(view, "2 VMs · row 2 of 2") || !strings.Contains(view, "↑/↓ Select   Enter Open") ||
		!strings.Contains(view, ", Settings") || strings.Contains(view, "Jobs: ") {
		t.Fatalf("VM list frame:\n%s", view)
	}
	m.ASCII = true
	if view = m.View(); !strings.Contains(view, "2 VMs - row 2 of 2") || !strings.Contains(view, "Up/Down Select") {
		t.Fatalf("ASCII VM list frame:\n%s", view)
	}
	m.Help = true
	if view = m.View(); !strings.Contains(view, "? or Esc Close help") || strings.Contains(view, "Up/Down Select") {
		t.Fatalf("help frame repeats hints:\n%s", view)
	}
}

func TestWorkspaceDetailsAndMenusShowTheirOwnKeys(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Section = 2
	m.Data["networks"] = []any{map[string]any{"name": "default", "active": true}}
	m.Detail = m.rows()[0]
	m.DetailTitle = "Network details"
	joined := strings.Join(m.footerButtons(), " ")
	if !strings.Contains(joined, "[ a More ]") || !strings.Contains(joined, "[ Esc Back ]") ||
		strings.Contains(joined, "Details") || strings.Contains(joined, "Create network") {
		t.Fatalf("network detail buttons %q", joined)
	}
	m.Detail = nil
	m.advanced()
	if view := m.View(); strings.Contains(view, "Enter Open") || strings.Contains(view, "PgUp/PgDn Scroll") {
		t.Fatalf("menu repeats list hints:\n%s", view)
	}
}

func TestWorkspaceButtonsWrapInsteadOfHiding(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Section = 1
	m.Detail = m.rows()[0]
	m.DetailTitle = "VM details"
	rows := m.footerButtons()
	joined := strings.Join(rows, " ")
	if len(rows) != 2 || strings.Contains(joined, " >") || !strings.Contains(joined, "[ Esc Back ]") {
		t.Fatalf("VM detail buttons %q", rows)
	}
	for _, label := range []string{"Console", "CPU / RAM", "Capture", "More"} {
		if !strings.Contains(joined, label) {
			t.Fatalf("button %q hidden: %q", label, rows)
		}
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) > m.Height || !strings.Contains(lines[len(lines)-1], "Back") {
		t.Fatalf("wrapped footer does not fit: %d lines", len(lines))
	}
	m.Width = 40
	if rows = m.footerButtons(); len(rows) != 1 {
		t.Fatalf("narrow terminal should scroll one row: %q", rows)
	}
}
