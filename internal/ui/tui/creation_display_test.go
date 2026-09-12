package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCreationDisplayPrefersOnlyAdvertisedSPICEForNewForms(t *testing.T) {
	base := creationFormFixture()
	for _, tt := range []struct {
		name     string
		graphics []string
		want     string
	}{
		{"spice after vnc", []string{"none", "vnc-unix", "spice-unix"}, "spice-unix"},
		{"spice before vnc", []string{"spice-unix", "none", "vnc-unix"}, "spice-unix"},
		{"legacy host", []string{"none", "vnc-unix"}, "vnc-unix"},
		{"headless host", []string{"none"}, "none"},
		{"empty observation", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			options := base.Options
			options.Graphics = tt.graphics
			f := NewCreationForm(base.OperationID, base.Source, options, base.Pools, base.Networks)
			if f.Spec.Graphics != tt.want {
				t.Fatalf("display=%q, want%q", f.Spec.Graphics, tt.want)
			}
		})
	}
}
func TestCreationDisplayReloadAndSavedChoicesRemainExplicit(t *testing.T) {
	base := creationFormFixture()
	options := base.Options
	options.Graphics = []string{"none", "vnc-unix", "spice-unix"}
	for _, explicit := range []string{"none", "vnc-unix"} {
		t.Run(explicit, func(t *testing.T) {
			original := base
			original.Spec.Graphics = explicit
			saved := saveCreationValues(original)
			f := NewCreationForm(base.OperationID, base.Source, options, base.Pools, base.Networks)
			if f.Spec.Graphics != "spice-unix" {
				t.Fatal("test setup did not suggestSPICE")
			}
			saved.restore(&f)
			f.SetOptions(options)
			if f.Spec.Graphics != explicit {
				t.Fatal("explicit resumed setting replaced")
			}
			options.Graphics = []string{"none", "spice-unix"}
			f.SetOptions(options)
			if f.Spec.Graphics != explicit {
				t.Fatal("removed observed option silently replaced instead of left for review")
			}
		})
	}
}
func TestCreationDisplayAdvancedChoicesAndHelpAreAccurate(t *testing.T) {
	base := creationFormFixture()
	base.Options.Graphics = []string{"none", "vnc-unix", "spice-unix"}
	base.Page = 3
	for _, value := range base.Options.Graphics {
		f := base
		f.Spec.Graphics = value
		f = creationFocus(t, f, "graphics")
		control := f.controls()[f.Focus]
		if !reflect.DeepEqual(control.choices, base.Options.Graphics) {
			t.Fatal("advanced options hidden")
		}
		view := f.View(80, 17)
		switch value {
		case "spice-unix":
			if !strings.Contains(view, "Local display (SPICE)") || !strings.Contains(view, "host's desktop") || !strings.Contains(view, "SSH terminal") {
				t.Fatal(view)
			}
		case "vnc-unix":
			if !strings.Contains(view, "VNC launch is unavailable") || !strings.Contains(view, "Choose SPICE") {
				t.Fatal(view)
			}
		case "none":
			if !strings.Contains(view, "No graphical display") || !strings.Contains(view, "configured separately") {
				t.Fatal(view)
			}
		}
		before := f.Spec.Graphics
		f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRight})
		if f.Spec.Graphics != base.Options.Graphics[(slices.Index(base.Options.Graphics, before)+1)%len(base.Options.Graphics)] {
			t.Fatal("explicit observed option cycling broke")
		}
	}
}
func TestCreationDisplayBasicAndFinalReviewStayVisibleAt80Columns(t *testing.T) {
	base := creationFormFixture()
	options := base.Options
	options.Graphics = []string{"none", "vnc-unix", "spice-unix"}
	for _, page := range []int{0, 2} {
		f := NewCreationForm(base.OperationID, base.Source, options, base.Pools, base.Networks)
		f.Page = page
		view := f.View(80, 17)
		if !strings.Contains(view, "Display: Local display (SPICE)") || !strings.Contains(view, "host's desktop") {
			t.Fatal("basic display caveat missing", view)
		}
		primary := "Continue to disks"
		if page == 2 {
			primary = "Preview VM creation"
		}
		if !strings.Contains(view, primary) || !strings.Contains(view, "Tab next") {
			t.Fatal("primary action/navigation lost", view)
		}
		if len(strings.Split(view, "\n")) > 17 {
			t.Fatal("too many rows")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > 80 {
				t.Fatal("wide display message")
			}
		}
	}
	fallback := base.View(80, 17)
	if !strings.Contains(fallback, "VNC launch is unavailable in Virmill") {
		t.Fatal("legacy fallback claims usable viewer", fallback)
	}
}
