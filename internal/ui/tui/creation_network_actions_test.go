package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
)

func TestCreationNetworkActionsPreserveIncompleteAndPreparedChoices(t *testing.T) {
	for _, before := range []bool{false, true} {
		for _, action := range []string{"create-network", "refresh-networks"} {
			for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeySpace} {
				f := creationComplete(creationFormFixture())
				f.BeforePreparation = before
				if before {
					f.OperationID = ""
				}
				f.Page, f.NIC, f.Disk = 2, 1, 1
				f.Networks = nil
				f.CPUText, f.MemoryText = "unfinished", "8192"
				f.Spec.CPU = domain.CreationCPU{Mode: "custom", Model: "test-model"}
				f.Spec.GuestAgent = true
				f.Spec.DevicePolicy.USBController = "qemu-xhci"
				f.Spec.NICs[0].Link = "up"
				f.Error = "Previous preview issue"
				f = creationFocus(t, f, action)
				got, intent := creationPress(f, key)
				if intent.Kind != action {
					t.Fatalf("before=%v action=%s key=%v: intent=%#v", before, action, key, intent)
				}
				if !reflect.DeepEqual(got, f) {
					t.Fatalf("%s changed saved VM choices or binding", action)
				}
			}
		}
	}
}

func TestCreationNetworkActionsReachableByTabAndBounded(t *testing.T) {
	for _, noNICs := range []bool{false, true} {
		f := creationFormFixture()
		f.Page, f.Focus, f.Networks = 2, 0, nil
		if noNICs {
			f.Spec.NICs = nil
		}
		seen := map[string]bool{}
		for range f.controls() {
			c := f.controls()[f.Focus]
			view := f.View(80, 17)
			if len(strings.Split(view, "\n")) > 17 {
				t.Fatal("network form exceeds the 80x24 modal body")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > 80 {
					t.Fatalf("line exceeds terminal width: %q", line)
				}
			}
			if !strings.Contains(view, "Preview VM creation") || !strings.Contains(view, "Tab next") {
				t.Fatalf("primary action or keyboard help hidden:\n%s", view)
			}
			if c.id == "create-network" || c.id == "refresh-networks" {
				seen[c.id] = true
				if !strings.Contains(view, "> [ "+c.label+" ]") || !strings.Contains(view, "choices") {
					t.Fatalf("focused network action or preservation help hidden:\n%s", view)
				}
			}
			var intent ImportIntent
			f, intent = creationPress(f, tea.KeyTab)
			if intent.Kind != "" {
				t.Fatal("navigation emitted action", intent)
			}
		}
		if !seen["create-network"] || !seen["refresh-networks"] {
			t.Fatal("network actions unreachable", seen)
		}
	}
}

func TestCreationNetworkActionCancelAndReturnKeepsNICs(t *testing.T) {
	for _, before := range []bool{false, true} {
		f := creationComplete(creationFormFixture())
		f.BeforePreparation = before
		if before {
			f.OperationID = ""
		}
		f.Page = 2
		f = creationFocus(t, f, "create-network")
		original := f
		f, intent := creationPress(f, tea.KeyEsc)
		if intent.Kind != "" || f.Page != 1 || !reflect.DeepEqual(f.Spec, original.Spec) {
			t.Fatal("back changed VM choices or submitted an action")
		}
		f = creationFocus(t, f, "next")
		f, intent = creationPress(f, tea.KeyEnter)
		if intent.Kind != "" || f.Page != 2 || !reflect.DeepEqual(f.Spec, original.Spec) || f.OperationID != original.OperationID {
			t.Fatal("return to networks lost choices", f.Error)
		}
	}
}

func TestCreationNetworkUnavailableChoicesStayVisibleAndRefused(t *testing.T) {
	for _, id := range []string{"", "malformed", "00000000-0000-0000-0000-000000000000", "44444444-4444-4444-8444-444444444444"} {
		f := creationComplete(creationFormFixture())
		f.Page, f.NIC = 2, 0
		f.Spec.NICs[0].NetworkID = id
		f.Networks = []domain.VirtualNetwork{{Key: domain.ResourceKey{UUID: "malformed"}, Active: true}, {Key: domain.ResourceKey{UUID: "00000000-0000-0000-0000-000000000000"}, Active: true}}
		f = creationFocus(t, f, "network")
		control := f.controls()[f.Focus]
		if len(control.choices) != 0 || (id == "" && control.value != "Choose…") || (id != "" && control.value != "Unavailable: "+id) {
			t.Fatalf("invalid/unavailable selection hidden or offered: %#v", control)
		}
		view := f.View(80, 17)
		if id != "" && !strings.Contains(view, "Unavailable:") {
			t.Fatal("stale selection not visible", view)
		}
		if _, err := f.Request("qemu:///system"); err == nil || !strings.Contains(err.Error(), "choose an active network") {
			t.Fatal("invalid selection did not produce actionable refusal", err)
		}
		got, intent := creationPress(f, tea.KeyRight)
		if intent.Kind != "" || !reflect.DeepEqual(got.Spec, f.Spec) || got.Error == "" {
			t.Fatal("empty picker silently changed adapter")
		}
	}
}

func TestCreationNetworkRefreshDoesNotChooseOrConnect(t *testing.T) {
	f := creationFormFixture()
	f.Page = 2
	f.Networks = nil
	f = creationFocus(t, f, "refresh-networks")
	f, intent := creationPress(f, tea.KeyEnter)
	if intent.Kind != "refresh-networks" {
		t.Fatal(intent)
	}
	// The workspace replaces only the observed inventory after the read.
	f.Networks = []domain.VirtualNetwork{{Key: domain.ResourceKey{UUID: creationFormNetwork}, Active: true}}
	f = creationFocus(t, f, "network")
	if f.controls()[f.Focus].value != "Choose…" {
		t.Fatal("refresh selected a network")
	}
	f, _ = creationPress(f, tea.KeyRight)
	if f.Spec.NICs[0].NetworkID != creationFormNetwork || f.Spec.NICs[0].Link != "down" || f.Spec.NICs[1].NetworkID != "" {
		t.Fatal("explicit selection altered cable or another original adapter")
	}
	if f.controls()[f.Focus].value != creationFormNetwork {
		t.Fatal("unnamed observed network has no readable fallback")
	}
}
