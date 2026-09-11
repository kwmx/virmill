package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A fresh user can choose the required boot firmware on the ordinary hardware
// page; optional devices remain behind the Advanced hardware button.
func TestCreationFriendlyFirmwareWithoutAdvanced(t *testing.T) {
	f := creationFormFixture()
	f = creationFocus(t, f, "firmware")
	if f.Spec.Firmware.Mode != "" {
		t.Fatal("firmware must not be guessed")
	}
	f, intent := creationPress(f, tea.KeyRight)
	if intent.Kind != "" || f.Page != 0 || f.Spec.Firmware.Mode != "bios" {
		t.Fatal("basic firmware choice failed")
	}
	view := f.View(80, 24)
	for _, text := range []string{"Step 1 of 3", "Firmware: < BIOS >", "Advanced hardware", "Continue to disks"} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing %q: %s", text, view)
		}
	}
	for _, c := range f.controls() {
		if c.id == "watchdog" || c.id == "cpuMode" || c.id == "balloon" {
			t.Fatal("expert control on basic page")
		}
	}
	f = creationFocus(t, f, "advanced")
	f, _ = creationPress(f, tea.KeyEnter)
	f = creationFocus(t, f, "firmware")
	if !strings.Contains(f.View(80, 24), "Firmware: < BIOS >") {
		t.Fatal("advanced view lost basic firmware choice")
	}
}

// Human-readable choices still emit exactly the shared domain values.
func TestCreationFriendlyChoicesPreserveRequest(t *testing.T) {
	f := creationComplete(creationFormFixture())
	f.Page = 3
	f = creationFocus(t, f, "guestAgent")
	if !strings.Contains(f.View(80, 24), "Guest agent channel: < Disabled >") {
		t.Fatal("boolean exposed instead of labeled choice")
	}
	f, _ = creationPress(f, tea.KeySpace)
	if !strings.Contains(f.View(80, 24), "Guest agent channel: < Enabled >") {
		t.Fatal("toggle not displayed")
	}
	f.Page = 2
	f = creationFocus(t, f, "link")
	if !strings.Contains(f.View(80, 24), "Cable: < Disconnected >") {
		t.Fatal("network access not explained")
	}
	f, _ = creationPress(f, tea.KeyRight)
	request, err := f.Request("qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	hardware := request.Input["hardware"].(map[string]any)
	if hardware["guestAgent"] != true || f.Spec.NICs[0].Link != "up" {
		t.Fatal("display mapping changed shared request")
	}
}

func TestCreationFriendlyWrappedHelpAndFocusedResize(t *testing.T) {
	f := creationComplete(creationFormFixture())
	f.Page = 2
	f = creationFocus(t, f, "network")
	view := f.View(80, 24)
	if !strings.Contains(view, "through the guest.") {
		t.Fatalf("network isolation guidance clipped: %s", view)
	}
	f.Page = 3
	f.Error = "The selected machine cannot use this device. Choose another option before continuing."
	for i, c := range f.controls() {
		f.Focus = i
		for _, size := range [][2]int{{80, 24}, {80, 14}, {60, 18}, {40, 10}} {
			view := f.View(size[0], size[1])
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("height overflow")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("width overflow")
				}
			}
			if !strings.Contains(view, "> "+c.label) && !strings.Contains(view, "> [ "+c.label) {
				t.Fatalf("focused control lost at %v: %s", size, view)
			}
		}
	}
	f = creationFocus(t, f, "watchdog")
	view = f.View(80, 14)
	if !strings.Contains(view, "Tab to see more") || !strings.Contains(view, "Left/Right change") {
		t.Fatalf("hidden options/navigation not explained: %s", view)
	}
}

func TestCreationFriendlyContinueValidatesCurrentStep(t *testing.T) {
	f := creationFormFixture()
	f.CPUText = ""
	f = creationFocus(t, f, "next")
	f, intent := creationPress(f, tea.KeyEnter)
	if f.Page != 0 || f.controls()[f.Focus].id != "cpu" || intent.Kind != "" || f.Error == "" {
		t.Fatal("invalid CPU moved user into later wizard steps")
	}
	f = creationComplete(f)
	f.CPUText = "4"
	f.Spec.Firmware.Mode = ""
	f = creationFocus(t, f, "next")
	f, _ = creationPress(f, tea.KeyEnter)
	if f.Page != 0 || f.controls()[f.Focus].id != "firmware" {
		t.Fatal("basic firmware error sent user into expert options")
	}
	f.Spec.Firmware = f.Options.Firmware[0].Firmware
	f.Spec.Disks[1].Bus = ""
	f = creationFocus(t, f, "next")
	f, _ = creationPress(f, tea.KeyEnter)
	if f.Page != 1 {
		t.Fatal("later disk choice blocked finishing basic page")
	}
	f = creationFocus(t, f, "next")
	f, _ = creationPress(f, tea.KeyEnter)
	if f.Page != 1 || f.Disk != 1 || f.controls()[f.Focus].id != "bus" {
		t.Fatal("unfinished disk did not receive focus")
	}
	f.Spec.Disks[1].Bus = "sata"
	f.Spec.NICs[0].NetworkID = ""
	f = creationFocus(t, f, "next")
	f, _ = creationPress(f, tea.KeyEnter)
	if f.Page != 2 {
		t.Fatal("later network choice blocked leaving disk page")
	}
}
