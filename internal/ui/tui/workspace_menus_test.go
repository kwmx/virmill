package tui

import (
	"strings"
	"testing"
)

func menuCommands(m Workspace) map[string]bool {
	out := map[string]bool{}
	for _, a := range m.catalog() {
		out[a.Command] = true
	}
	return out
}

// vmMenus returns the More and Advanced tasks for the first VM in a given state.
func vmMenus(t *testing.T, state string, saved bool) (common, advanced map[string]bool) {
	t.Helper()
	m := fixtureWorkspace()
	m.Section = 1
	row := object(m.rows()[0])
	row["state"], row["hasManagedSave"] = state, saved
	m, _ = wk(m, "a")
	common = menuCommands(m)
	m, _ = wk(m, "A")
	if !m.CatalogExpert {
		t.Fatal("A did not open advanced tools")
	}
	advanced = menuCommands(m)
	for c := range common {
		if advanced[c] {
			t.Fatalf("%s: %s is listed in both More and Advanced", state, c)
		}
	}
	return common, advanced
}

func TestVMMenusOfferOnlyPowerTasksTheStateAllows(t *testing.T) {
	for _, tc := range []struct {
		state      string
		saved      bool
		want, hide []string
	}{
		{"stopped", false, []string{"vm start", "vm remove", "vm creation cleanup", "vm creation resume"},
			[]string{"vm stop", "vm reboot", "vm pause", "vm resume", "vm save", "vm restore-saved", "vm stop --hard"}},
		{"stopped", true, []string{"vm start", "vm restore-saved"}, []string{"vm stop", "vm resume"}},
		{"running", false, []string{"vm stop", "vm stop --hard", "vm reboot", "vm pause", "vm save", "vm remove"}, []string{"vm start", "vm resume", "vm restore-saved"}},
		{"paused", false, []string{"vm resume", "vm stop --hard"}, []string{"vm start", "vm stop", "vm pause", "vm save"}},
		{"crashed", false, []string{"vm start", "vm stop", "vm resume"}, nil},
	} {
		common, advanced := vmMenus(t, tc.state, tc.saved)
		for _, c := range tc.want {
			if !common[c] && !advanced[c] {
				t.Fatalf("%s saved=%v: %s missing from both menus", tc.state, tc.saved, c)
			}
		}
		for _, c := range tc.hide {
			if common[c] || advanced[c] {
				t.Fatalf("%s saved=%v: %s offered", tc.state, tc.saved, c)
			}
		}
	}
	if common, _ := vmMenus(t, "stopped", false); !common["vm remove"] {
		t.Fatal("Remove VM is not in More for a stopped VM")
	}
}

func buttonText(m Workspace) string {
	var b strings.Builder
	for _, button := range m.buttons() {
		b.WriteString("[" + button.label + "]")
	}
	return b.String()
}

func TestJobsPageOffersJobTasksOnly(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 8
	m.Data["jobs"] = []any{map[string]any{"operationID": "11111111-1111-4111-8111-111111111111", "state": "succeeded"}}
	if got := buttonText(m); strings.Contains(got, "Create VM") || !strings.Contains(got, "[Activity]") {
		t.Fatalf("jobs buttons %s", got)
	}
	m.Data["jobs"] = []any{}
	if got := buttonText(m); strings.Contains(got, "Activity") || strings.Contains(got, "Details") || !strings.Contains(got, "[Refresh]") {
		t.Fatalf("empty jobs buttons %s", got)
	}
}

func TestHelpIsGroupedAndFits80x24(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Help = true
	view := m.View()
	for _, want := range []string{"Keyboard guide", "Sections   1 Overview", "7 Protection  8 Devices  9 Jobs  0 Plugins  , Settings",
		"Moving     Tab moves", "Tasks      a opens more tasks", "Safety     Every change opens a review first", "? or Esc Close help"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help lacks %q:\n%s", want, view)
		}
	}
	if lines := strings.Split(view, "\n"); len(lines) > 24 {
		t.Fatalf("help is %d lines", len(lines))
	}
	m.ASCII = true
	if view = m.View(); !strings.Contains(view, "Up/Down select") {
		t.Fatalf("ASCII help:\n%s", view)
	}
}
