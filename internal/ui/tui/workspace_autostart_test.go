package tui

import (
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
)

func autostartWorkspace(t *testing.T) Workspace {
	t.Helper()
	m := fixtureWorkspace()
	m.Section = 1
	vm := m.selectedVM()
	vm.Autostart = true // fresh state differs from the cached list
	vm.Fingerprint = strings.Repeat("a", 64)
	c := &workspaceClient{response: app.Response{Data: vm}}
	m.Client = c
	var action ui.Action
	for _, a := range ui.Actions {
		if a.Command == "vm autostart" {
			action = a
		}
	}
	cmd := m.openAction(action)
	if cmd == nil || m.Form != nil || m.AutostartTarget == nil {
		t.Fatal("did not read before editing")
	}
	if !strings.Contains(m.View(), "Reading the VM's current setting") {
		t.Fatal(m.View())
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Form == nil || m.Form.Kind != "autostart" || m.Form.Fields[0].Value != "true" || m.AutostartTarget != nil || m.Busy {
		t.Fatal("fresh defaults not loaded", m.Error)
	}
	if len(c.calls) != 1 || c.calls[0] != "inventory.get" || c.requests[0].ID != vm.Key.UUID || len(c.requests[0].Input) != 0 {
		t.Fatal("read changed scope", c.requests)
	}
	return m
}
func TestAutostartWorkspaceFreshDefaultsPreviewBack(t *testing.T) {
	m := autostartWorkspace(t)
	m, _ = wk(m, "space")
	m, _ = wk(m, "tab")
	p := testWorkspacePlan(t)
	p.Operation = "vm.autostart"
	p.Review = map[string]any{"beforeAutostart": true, "afterAutostart": false}
	p.Digest, _ = operations.PlanDigest(p)
	c := m.Client.(*workspaceClient)
	c.response = app.Response{Data: p}
	m, cmd := wk(m, "enter")
	if cmd == nil {
		t.Fatal("preview missing", m.Error, m.Form.Error)
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	r := c.requests[len(c.requests)-1]
	if c.calls[len(c.calls)-1] != "vm.plan" || r.Action != "autostart" || r.Input["enabled"] != false || len(r.Input) != 1 {
		t.Fatal(r)
	}
	if !strings.Contains(m.View(), "Automatic startup: On → Off") {
		t.Fatal(m.View())
	}
	m, _ = wk(m, "esc")
	if m.Form == nil || m.Form.Fields[0].Value != "false" || m.Plan != nil {
		t.Fatal("Back lost edits")
	}
	m, _ = wk(m, "esc")
	if m.Form != nil || m.Pending["plan"] != 0 {
		t.Fatal("cancel retained form/preview")
	}
	for _, call := range c.calls {
		if call == "operation.apply" {
			t.Fatal("preview applied")
		}
	}
}
func TestAutostartWorkspaceCanceledAndMismatchedLoads(t *testing.T) {
	for _, mode := range []string{"cancel", "page", "wrong-vm", "error"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureWorkspace()
			m.Section = 1
			vm := m.selectedVM()
			vm.Fingerprint = strings.Repeat("a", 64)
			c := &workspaceClient{response: app.Response{Data: vm}}
			m.Client = c
			cmd := m.openAutostart()
			switch mode {
			case "cancel":
				m, _ = wk(m, "esc")
			case "page":
				m.page(2)
			case "wrong-vm":
				vm.Key.UUID = "33345678-1234-4234-8234-123456789abc"
				c.response.Data = vm
			case "error":
				c.err = domain.Fail("PERMISSION_DENIED", "access refused")
			}
			next, _ := m.Update(cmd())
			m = next.(Workspace)
			if m.Form != nil || m.AutostartTarget != nil || m.Busy {
				t.Fatal("late or invalid observation opened edit", mode, m.Error)
			}
			if (mode == "error" || mode == "wrong-vm") && m.Error == "" {
				t.Fatal("missing actionable load error")
			}
		})
	}
}
func TestAutostartWorkspacePendingApplyCannotDiscardSubmission(t *testing.T) {
	m := autostartWorkspace(t)
	m.Pending["apply"] = 999
	m.Busy = true
	next, cmd := wk(m, "esc")
	if cmd != nil || !next.Busy || next.Form == nil || next.Pending["apply"] != 999 {
		t.Fatal("Esc lost pending apply")
	}
}
