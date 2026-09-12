package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func connectionReply(t *testing.T, m Workspace, kind string, response app.Response, err error) Workspace {
	t.Helper()
	_ = m.request(kind, workspaceMethods[kind], app.Request{})
	next, cmd := m.Update(workspaceReply{Kind: kind, Token: m.Pending[kind], Response: response, Err: err})
	if cmd != nil {
		t.Fatal("connection observation caused another action")
	}
	return next.(Workspace)
}

func TestCoordinatorRecoveryCardUsesTypedFailureOnOverviewAndSettings(t *testing.T) {
	for _, section := range []int{0, 10} {
		for _, envelope := range []bool{false, true} {
			m := NewWorkspace(nil, "qemu:///system")
			m.NoColor, m.Section = true, section
			failure := domain.Fail("COORDINATOR_UNAVAILABLE", "fixture socket unavailable")
			response, err := app.Response{}, error(failure)
			if envelope {
				response.Error, err = failure, nil
			}
			m = connectionReply(t, m, workspaceKinds[section], response, err)
			view := m.View()
			for _, want := range []string{"Background service unavailable", "VMs may still be running", "systemctl --user start virmilld.service", "Retry connection", "More help"} {
				if !strings.Contains(view, want) {
					t.Fatalf("section %d missing %q: %s", section, want, view)
				}
			}
			if strings.Contains(view, "Loading your virtual machines") || strings.Contains(view, "0 virtual machines") || strings.Contains(view, "start automatically") {
				t.Fatal("offline view invented inventory or startup state", view)
			}
			if len(strings.Split(view, "\n")) > 24 {
				t.Fatal("recovery card exceeds 24 rows")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > 80 {
					t.Fatalf("recovery card exceeds 80 columns: %q", line)
				}
			}
		}
	}
	for _, err := range []error{errors.New("COORDINATOR_UNAVAILABLE appears in untyped text"), domain.Fail("PERMISSION_DENIED", "libvirt permission denied")} {
		m := connectionReply(t, NewWorkspace(nil, "qemu:///system"), "vms", app.Response{}, err)
		if m.connectionRecoveryVisible() {
			t.Fatal("unrelated failure became coordinator outage", err)
		}
	}
}

func TestCoordinatorHelpAndRetryOnlyReadObservations(t *testing.T) {
	m := fixtureWorkspace()
	client := m.Client.(*workspaceClient)
	m = connectionReply(t, m, "vms", app.Response{}, fmt.Errorf("wrapped: %w", domain.Fail("COORDINATOR_UNAVAILABLE", "fixture")))
	saved := m.Data["vms"]
	m, _ = wk(m, "tab")
	m, _ = wk(m, "down")
	m, cmd := wk(m, "enter")
	if cmd != nil || !m.coordinator.Expanded || len(client.calls) != 0 {
		t.Fatal("opening startup help invoked a command")
	}
	view := m.View()
	for _, want := range []string{"systemctl --user enable virmilld.service", "future sign-ins", "does not enable guest autostart", "systemctl --user status virmilld.service", "Less help"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expanded help missing %q: %s", want, view)
		}
	}
	if !reflect.DeepEqual(m.Data["vms"], saved) {
		t.Fatal("offline help changed cached observations")
	}
	m, cmd = wk(m, "r")
	if cmd == nil || !m.coordinator.Unavailable {
		t.Fatal("retry missing or success claimed before reply")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 3 {
		t.Fatal("Overview retry should refresh its three existing inventories")
	}
	for _, request := range batch {
		next, followup := m.Update(request())
		m = next.(Workspace)
		if followup != nil {
			t.Fatal("inventory retry caused another action")
		}
	}
	if m.coordinator.Unavailable || m.coordinator.Expanded || !reflect.DeepEqual(client.calls, []string{"inventory.list", "operation.list", "storage.pool.list"}) {
		t.Fatal("retry failed to restore normal observation flow", client.calls)
	}
	for _, request := range client.requests {
		if request.Action != "" || request.Apply != nil || request.Path != "" || len(request.Input) != 0 {
			t.Fatal("retry gained mutation input", request)
		}
	}
	// Settings also works offline and reuses its existing host observation.
	m.Section = 10
	m = connectionReply(t, m, "health", app.Response{}, domain.Fail("COORDINATOR_UNAVAILABLE", "fixture"))
	m, _ = wk(m, "tab")
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("Retry connection button has no command")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.coordinator.Unavailable || client.calls[len(client.calls)-1] != "host.inspect" {
		t.Fatal("Settings retry did not use host inspection")
	}
}

func TestCoordinatorConnectionIgnoresStaleAndSyntheticReplies(t *testing.T) {
	m := fixtureWorkspace()
	_ = m.request("vms", "inventory.list", app.Request{})
	older := m.Pending["vms"]
	_ = m.request("health", "host.inspect", app.Request{})
	newer := m.Pending["health"]
	m.ButtonFocus, m.ButtonIndex = true, 1
	next, _ := m.Update(workspaceReply{Kind: "health", Token: newer, Err: domain.Fail("COORDINATOR_UNAVAILABLE", "fixture")})
	m = next.(Workspace)
	if m.ButtonFocus {
		t.Fatal("asynchronous help card retained focus on a different action")
	}
	next, _ = m.Update(workspaceReply{Kind: "vms", Token: older, Response: app.Response{Data: []domain.VM{}}})
	m = next.(Workspace)
	if !m.coordinator.Unavailable {
		t.Fatal("older concurrent success hid newer failure")
	}
	_ = m.request("job-outcome", "plan.show", app.Request{})
	next, _ = m.Update(workspaceReply{Kind: "job-outcome", Token: m.Pending["job-outcome"], Response: app.Response{Data: jobOutcome{}}})
	m = next.(Workspace)
	if !m.coordinator.Unavailable {
		t.Fatal("synthetic outcome envelope claimed connectivity")
	}
	_ = m.request("health", "host.inspect", app.Request{})
	stale := m.Pending["health"]
	_ = m.request("health", "host.inspect", app.Request{})
	next, _ = m.Update(workspaceReply{Kind: "health", Token: stale, Response: app.Response{Data: map[string]any{}}})
	m = next.(Workspace)
	if !m.coordinator.Unavailable {
		t.Fatal("superseded same-kind reply hid failure")
	}
	next, _ = m.Update(workspaceReply{Kind: "health", Token: m.Pending["health"], Response: app.Response{Error: domain.Fail("PERMISSION_DENIED", "host inspection refused")}})
	m = next.(Workspace)
	if m.coordinator.Unavailable {
		t.Fatal("fresh service refusal did not distinguish reachable coordinator")
	}
}

func TestCoordinatorRecoveryDoesNotReplaceActiveWorkflows(t *testing.T) {
	m := connectionReply(t, fixtureWorkspace(), "jobs", app.Response{}, domain.Fail("COORDINATOR_UNAVAILABLE", "fixture"))
	m.openNetworkForm()
	if strings.Contains(m.View(), "Background service unavailable") || m.NetworkForm == nil {
		t.Fatal("background failure replaced active form")
	}
	m.resetNetworkForm()
	m.Section = 1
	if m.connectionRecoveryVisible() {
		t.Fatal("offline card replaced unrelated section")
	}
	m.Section = 0
	m.Detail = generic(workspaceVM(workspaceVMID, "Existing VM"))
	if m.connectionRecoveryVisible() {
		t.Fatal("offline card hid selected VM details")
	}
	m.Detail = nil
	p := testWorkspacePlan(t)
	m.Plan, m.Offset = &p, 7
	m = connectionReply(t, m, "jobs", app.Response{}, nil)
	m = connectionReply(t, m, "jobs", app.Response{}, domain.Fail("COORDINATOR_UNAVAILABLE", "fixture"))
	if m.Plan != &p || m.Offset != 7 || strings.Contains(m.View(), "Background service unavailable") {
		t.Fatal("background connectivity changed an active review or its scroll position")
	}
}
