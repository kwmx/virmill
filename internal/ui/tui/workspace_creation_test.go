package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
)

type creationWorkspaceClient struct {
	Calls    []string
	Requests []app.Request
	Plan     domain.Plan
	Fail     string
}

func (c *creationWorkspaceClient) Call(_ context.Context, method string, request app.Request) (app.Response, error) {
	c.Calls = append(c.Calls, method)
	c.Requests = append(c.Requests, request)
	if c.Fail == method {
		return app.Response{Error: domain.Fail("UNAVAILABLE", "Could not read hardware. Reconnect and retry.")}, nil
	}
	f := creationFormFixture()
	var data any
	switch method {
	case "import.sources":
		data = []creationChoice{{OperationID: creationFormOperation, Name: "appliance", Destination: "/prepared/appliance"}}
	case "import.result":
		data = map[string]any{"artifact": f.Source, "directory": "/prepared/appliance"}
	case "vm.creation.options":
		data = f.Options
	case "storage.pool.list":
		data = f.Pools
	case "network.list":
		data = f.Networks
	case "vm.create", "import.prepare-install":
		data = c.Plan
	default:
		return app.Response{Error: domain.Fail("UNEXPECTED", "unexpected call "+method)}, nil
	}
	return app.Response{Data: data}, nil
}

func TestCreationHardwareBeforePreparationBindsToAcceptedImport(t *testing.T) {
	m := importWorkspace(t)
	c := &creationWorkspaceClient{Plan: testWorkspacePlan(t)}
	m.Client = c
	cmd := m.configureImportHardware()
	n, _ := m.Update(cmd())
	m = n.(Workspace)
	if m.Creation == nil || !m.Creation.BeforePreparation || !strings.Contains(m.View(), "CPU cores") {
		t.Fatal("hardware unavailable before copying", m.View())
	}
	for _, method := range c.Calls {
		if method == "import.result" {
			t.Fatal("unprepared source treated as completed")
		}
	}
	f := creationComplete(*m.Creation)
	f.CPUText = "5"
	f.MemoryText = "1536"
	f.Page = 2
	f = creationFocus(t, f, "preview")
	m.Creation = &f
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal(m.View())
	}
	n, _ = m.Update(cmd())
	m = n.(Workspace)
	if m.Creation != nil || m.Import.VM == nil || m.Import.VM.CPUText != "5" || c.Calls[len(c.Calls)-1] != "import.prepare-install" {
		t.Fatal("hardware preview skipped image review", c.Calls, m.View())
	}
	m.Pending["apply"] = 600
	n, _ = m.Update(workspaceReply{Kind: "apply", Token: 600, Response: app.Response{Data: domain.Job{ID: creationFormOperation, State: "running"}}})
	m = n.(Workspace)
	if m.SavedCreation == nil || m.SavedCreation.BeforePreparation || m.SavedCreation.OperationID != creationFormOperation || m.SavedCreation.MemoryText != "1536" {
		t.Fatal("hardware selections lost at durable handoff")
	}
}

func TestCreationWorkspaceSourceChooserAndRealRequestBinding(t *testing.T) {
	m := fixtureWorkspace()
	c := &creationWorkspaceClient{Plan: testWorkspacePlan(t)}
	m.Client = c
	cmd := m.openAction(ui.Action{Command: "vm create", Method: "vm.create", Argument: "id", Mutation: "create"})
	n, _ := m.Update(cmd())
	m = n.(Workspace)
	if !m.CreationPicking || m.ActionForm != nil || !strings.Contains(m.View(), "prepared images") {
		t.Fatal(m.View())
	}
	m, cmd = wk(m, "enter")
	n, _ = m.Update(cmd())
	m = n.(Workspace)
	if m.Creation == nil || m.Creation.CPUText != "4" || m.Creation.MemoryText != "4096" || m.Creation.OperationID != creationFormOperation {
		t.Fatal("prepared source defaults lost", m.View())
	}
	f := creationComplete(*m.Creation)
	f.CPUText = "6"
	f.MemoryText = "8192"
	f.Page = 2
	f = creationFocus(t, f, "preview")
	m.Creation = &f
	m, cmd = wk(m, "enter")
	n, _ = m.Update(cmd())
	m = n.(Workspace)
	if m.Plan == nil || m.Creation == nil {
		t.Fatal("missing review", m.View())
	}
	r := c.Requests[len(c.Requests)-1]
	h := r.Input["hardware"].(map[string]any)
	if r.ID != creationFormOperation || h["vcpus"] != float64(6) || h["memoryMiB"] != float64(8192) {
		t.Fatal("visible settings did not reach shared service", r)
	}
	for _, method := range c.Calls {
		if method == "operation.apply" {
			t.Fatal("unreviewed mutation")
		}
	}
	m, _ = wk(m, "esc")
	if m.Plan != nil || m.Creation == nil || m.Creation.CPUText != "6" {
		t.Fatal("review back lost edits", m.View())
	}
}

func TestCreationWorkspaceErrorsCancellationAndExport(t *testing.T) {
	m := fixtureWorkspace()
	c := &creationWorkspaceClient{Fail: "vm.creation.options"}
	m.Client = c
	cmd := m.loadCreation(creationFormOperation, "")
	n, _ := m.Update(cmd())
	m = n.(Workspace)
	if m.Busy || !strings.Contains(m.Error, "Reconnect") || m.Creation != nil {
		t.Fatal("hidden capability failure", m.View())
	}
	c.Fail = ""
	cmd = m.loadCreation(creationFormOperation, "")
	m, _ = wk(m, "esc")
	n, _ = m.Update(cmd())
	m = n.(Workspace)
	if m.Creation != nil || m.CreationPicking {
		t.Fatal("canceled read reopened wizard")
	}
	f := creationComplete(creationFormFixture())
	m.Creation = &f
	m.openSettingsExport("settings.json")
	m.ExportForm.Fields[0].Value = t.TempDir()
	path := filepath.Join(m.ExportForm.Fields[0].Value, "settings.json")
	m, cmd = wk(m, "enter")
	n, _ = m.Update(cmd())
	m = n.(Workspace)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if json.Unmarshal(raw, &input) != nil || input["identityMode"] != "clone" || input["hardware"] == nil {
		t.Fatal("bad reusable settings", string(raw))
	}
	if m.ExportForm != nil || m.Creation == nil {
		t.Fatal("export lost form")
	}
}

func TestCreationPreparationCompletionOnlyOpensWhenStillViewingJob(t *testing.T) {
	for _, away := range []bool{false, true} {
		m := fixtureWorkspace()
		m.Section = 8
		m.Detail = generic(domain.Job{ID: creationFormOperation, State: "running"})
		m.PendingPreparation = creationFormOperation
		if away {
			m.Section = 1
		}
		m.Pending["preparation-job"] = 10
		n, cmd := m.Update(workspaceReply{Kind: "preparation-job", Token: 10, Response: app.Response{Data: domain.Job{ID: creationFormOperation, State: "succeeded"}}})
		m = n.(Workspace)
		if m.PendingPreparation != "" {
			t.Fatal("terminal import still polled")
		}
		if away && (cmd != nil || m.CreationPicking) {
			t.Fatal("completion stole another page")
		}
		if !away && (cmd == nil || !m.CreationPicking) {
			t.Fatal("missing guided handoff")
		}
	}
}
