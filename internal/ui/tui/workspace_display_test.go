package tui

import (
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func displayInfo(m Workspace, choices ...domain.ConsoleChoice) domain.ConsoleInfo {
	return domain.ConsoleInfo{Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "vm", UUID: workspaceVMID}, Name: "lab", State: "running", ConfigFingerprint: strings.Repeat("a", 64), Choices: choices}
}

var spiceDisplay = domain.ConsoleChoice{ID: "graphics-0", Kind: "graphical", Protocol: "spice", Label: "Graphical display", Available: true}
var serialConsole = domain.ConsoleChoice{ID: "serial-0", Kind: "serial", Protocol: "serial", Label: "Serial", Device: "serial0", Available: true}

// A VM New VM started opens its only display by itself on a desktop, and
// says where to open it otherwise, without blocking the screen.
func TestStartedVMOpensItsDisplay(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "")
	m := fixtureWorkspace()
	m.Client = &workspaceClient{}
	if cmd := m.openDisplayAfterStart(workspaceVMID); cmd == nil || m.Busy {
		t.Fatal("display not requested, or the screen was blocked")
	}
	m, cmd := deliver(t, m, "display-auto", displayInfo(m, spiceDisplay, serialConsole))
	if cmd == nil || m.Pending["console-launch"] == 0 || m.Busy || !strings.Contains(m.Notice, "Opening its display") {
		t.Fatal("the only display was not opened", m.Notice)
	}

	t.Setenv("DISPLAY", "")
	m = fixtureWorkspace()
	m.Client = &workspaceClient{}
	m.openDisplayAfterStart(workspaceVMID)
	m, cmd = deliver(t, m, "display-auto", displayInfo(m, spiceDisplay))
	if cmd != nil || !strings.Contains(m.Notice, "host's desktop") {
		t.Fatal("no desktop: the notice must say where the display opens", m.Notice)
	}
}

func TestDisplayButtonOpensTheOnlyDisplayWithoutAChoice(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	m := fixtureWorkspace()
	m.Client = &workspaceClient{}
	m.ConsoleVM = workspaceVM(workspaceVMID, "lab")
	m.Pending = map[string]uint64{"console-load": 1}
	next, cmd := m.Update(workspaceReply{Kind: "console-load", Token: 1, Response: app.Response{Data: displayInfo(m, spiceDisplay)}})
	if m = next.(Workspace); cmd == nil || m.Pending["console-launch"] == 0 {
		t.Fatal("one display still asked for a choice")
	}
	m.Pending = map[string]uint64{"console-load": 2}
	next, cmd = m.Update(workspaceReply{Kind: "console-load", Token: 2, Response: app.Response{Data: displayInfo(m, spiceDisplay, domain.ConsoleChoice{ID: "graphics-1", Kind: "graphical", Protocol: "spice", GraphicsIndex: 1, Available: true})}})
	if m = next.(Workspace); cmd != nil || m.Console == nil {
		t.Fatal("several displays must keep the chooser")
	}
}

func TestDisplayOpenedKeepsVirmillUsable(t *testing.T) {
	m := fixtureWorkspace()
	m.Pending = map[string]uint64{"console-launch": 4}
	m.Busy, m.Console = true, &domain.ConsoleInfo{}
	next, _ := m.Update(displayOpened{Token: 4, Err: errors.New("The display window closed right away (Unable to connect).")})
	if m = next.(Workspace); m.Busy || !strings.Contains(m.Error, "Unable to connect") {
		t.Fatal("viewer failure hidden", m.Error)
	}
	m.Pending = map[string]uint64{"console-launch": 5}
	m.Busy = true
	done := make(chan error, 1)
	next, cmd := m.Update(displayOpened{Token: 5, Done: done})
	if m = next.(Workspace); m.Busy || m.Console != nil || cmd == nil || !strings.Contains(m.Notice, "own window") {
		t.Fatal("opened display left Virmill waiting")
	}
	done <- errors.New("exit status 1")
	next, _ = m.Update(cmd())
	if m = next.(Workspace); !strings.Contains(m.Notice, "was not stopped") {
		t.Fatal(m.Notice)
	}
}
