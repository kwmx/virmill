package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
)

// These tests exercise the workspace/service boundary with synthetic replies.
// They do not validate image conversion, guest creation or hardware behavior.
type summaryWorkspaceClient struct {
	creationWorkspaceClient
	source CreationSource
	report importer.Report
}

func (c *summaryWorkspaceClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	switch method {
	case "import.prepare", "import.describe", "import.result":
		c.Calls = append(c.Calls, method)
		c.Requests = append(c.Requests, r)
		switch method {
		case "import.prepare":
			return app.Response{Data: c.Plan}, nil
		case "import.describe":
			return app.Response{Data: c.report}, nil
		default:
			return app.Response{Data: map[string]any{"artifact": c.source, "directory": "/prepared/appliance"}}, nil
		}
	default:
		return c.creationWorkspaceClient.Call(ctx, method, r)
	}
}
func summaryWorkspace(t *testing.T) (Workspace, *summaryWorkspaceClient) {
	t.Helper()
	m := fixtureWorkspace()
	f := NewImportForm("ova")
	f.Draft = importTestDraft(t, "ova")
	f.Draft.Report.Systems[0].Name = "Original appliance"
	f.Draft.Report.Systems[0].Items = []importer.Item{{ResourceType: "3", Quantity: "4"}, {ResourceType: "4", MemoryMiB: 4096}, {ResourceType: "10"}, {ResourceType: "10"}}
	f.Page = 3
	m.Import = &f
	c := &summaryWorkspaceClient{creationWorkspaceClient: creationWorkspaceClient{Plan: testWorkspacePlan(t)}, source: importCreationSource(f.Draft), report: *f.Draft.Report}
	m.Client = c
	return m, c
}
func summaryEdit(t *testing.T, m Workspace, id, value string) Workspace {
	t.Helper()
	f := importFocus(t, *m.Import, id)
	m.Import = &f
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = n.(Workspace)
	m, _ = wk(m, value)
	return m
}
func summaryRun(t *testing.T, m Workspace, cmd tea.Cmd) Workspace {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing service request", m.View())
	}
	n, _ := m.Update(cmd())
	return n.(Workspace)
}
func summaryOpenAdvanced(t *testing.T, m Workspace) Workspace {
	t.Helper()
	f := importFocus(t, *m.Import, "hardware")
	m.Import = &f
	m, cmd := wk(m, "enter")
	m = summaryRun(t, m, cmd)
	if m.Creation == nil || m.Creation.Page != 3 || !m.Creation.BeforePreparation {
		t.Fatal("advanced settings did not open", m.View())
	}
	return m
}
func summaryNoApply(t *testing.T, c *summaryWorkspaceClient) {
	t.Helper()
	for _, method := range c.Calls {
		if method == "operation.apply" {
			t.Fatal("read or preview submitted an operation")
		}
	}
}

func TestWorkspaceSummaryBasicsSurvivePreparationIntoCreationRequest(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		name := "without advanced"
		if advanced {
			name = "with advanced"
		}
		t.Run(name, func(t *testing.T) {
			m, c := summaryWorkspace(t)
			m = summaryEdit(t, m, "vmName", "Reviewed appliance")
			m = summaryEdit(t, m, "vcpus", "7")
			m = summaryEdit(t, m, "memoryMiB", "6144")
			if advanced {
				m = summaryOpenAdvanced(t, m)
				m.Creation.Spec.CPU = domain.CreationCPU{Mode: "custom", Model: "test-model"}
				m, _ = wk(m, "esc")
			}
			// Summary -> destination -> disk review uses the same visible draft.
			f := importFocus(t, *m.Import, "next")
			m.Import = &f
			m, _ = wk(m, "enter")
			f = importFocus(t, *m.Import, "next")
			m.Import = &f
			m, _ = wk(m, "enter")
			f = importFocus(t, *m.Import, "preview")
			m.Import = &f
			m, cmd := wk(m, "enter")
			m = summaryRun(t, m, cmd)
			if m.Plan == nil || c.Calls[len(c.Calls)-1] != "import.prepare" {
				t.Fatal("preparation preview missing", m.View())
			}
			// A coordinator reply represents an already-approved durable preparation;
			// this test never executes operation.apply or fabricates a backend outcome.
			m.Pending["apply"] = 700
			n, _ := m.Update(workspaceReply{Kind: "apply", Token: 700, Response: app.Response{Data: domain.Job{ID: creationFormOperation, State: "running"}}})
			m = n.(Workspace)
			m.Pending["preparation-job"] = 701
			n, cmd = m.Update(workspaceReply{Kind: "preparation-job", Token: 701, Response: app.Response{Data: domain.Job{ID: creationFormOperation, State: "succeeded"}}})
			m = summaryRun(t, n.(Workspace), cmd)
			if m.Creation == nil || m.Creation.Spec.Name != "Reviewed appliance" || m.Creation.CPUText != "7" || m.Creation.MemoryText != "6144" {
				t.Fatal("summary edits lost at durable handoff", m.View())
			}
			if advanced && m.Creation.Spec.CPU.Model != "test-model" {
				t.Fatal("advanced CPU choice lost")
			}
			fvm := creationComplete(*m.Creation)
			fvm.Page = 2
			fvm = creationFocus(t, fvm, "preview")
			m.Creation = &fvm
			m, cmd = wk(m, "enter")
			m = summaryRun(t, m, cmd)
			r := c.Requests[len(c.Requests)-1]
			h := r.Input["hardware"].(map[string]any)
			if c.Calls[len(c.Calls)-1] != "vm.create" || r.ID != creationFormOperation || h["name"] != "Reviewed appliance" || h["vcpus"] != float64(7) || h["memoryMiB"] != float64(6144) {
				t.Fatal("visible summary missing from actual shared request", r)
			}
			summaryNoApply(t, c)
		})
	}
}

func TestWorkspaceSummaryAdvancedNeedsNoDestinationAndSavesOnBack(t *testing.T) {
	m, c := summaryWorkspace(t)
	m.Import.Draft.DestinationParent = ""
	m.Import.Draft.DestinationName = ""
	m = summaryEdit(t, m, "vmName", "Edited name")
	m = summaryEdit(t, m, "vcpus", "6")
	m = summaryEdit(t, m, "memoryMiB", "5120")
	m = summaryOpenAdvanced(t, m)
	if m.Creation.Spec.Name != "Edited name" || m.Creation.CPUText != "6" || m.Creation.MemoryText != "5120" {
		t.Fatal("advanced ignored common fields")
	}
	m.Creation.Spec.CPU = domain.CreationCPU{Mode: "custom", Model: "test-model"}
	m.Creation.Spec.Firmware = m.Creation.Options.Firmware[1].Firmware
	m, _ = wk(m, "esc")
	if m.Creation != nil || m.Import.Page != 3 || m.Import.VM == nil || m.Import.VM.Spec.CPU.Model != "test-model" || m.Import.VM.Spec.Firmware.Mode != "uefi" {
		t.Fatal("Back lost advanced choices", m.View())
	}
	for _, method := range c.Calls {
		if method != "vm.creation.options" && method != "storage.pool.list" && method != "network.list" {
			t.Fatal("advanced should only read host choices", c.Calls)
		}
	}
}

func TestWorkspaceSummaryDestinationRetainsAdvancedButChangedSourceClearsIt(t *testing.T) {
	m, c := summaryWorkspace(t)
	m = summaryOpenAdvanced(t, m)
	m.Creation.Spec.CPU = domain.CreationCPU{Mode: "custom", Model: "test-model"}
	m, _ = wk(m, "esc")
	binding := m.Import.VMBinding
	m.ImportPickerTarget = "destination"
	m.importPicked(t.TempDir())
	m.Import.Draft.DestinationName = "renamed-copy"
	m = summaryOpenAdvanced(t, m)
	if m.Creation.Spec.CPU.Model != "test-model" || m.Import.VMBinding != binding {
		t.Fatal("destination invalidated unrelated hardware choices")
	}
	m, _ = wk(m, "esc")
	m.ImportPickerTarget = "source"
	m.importPicked(filepath.Join(t.TempDir(), "different.ova"))
	if m.Import.VM != nil || m.Import.VMBinding != "" || m.Import.Draft.Report != nil || m.Import.Draft.VMName != "" || m.Import.Draft.VCPUs != "" || m.Import.Draft.MemoryMiB != "" {
		t.Fatal("source switch retained previous settings")
	}
	report := c.report
	report.Source = m.Import.Draft.Source
	m.importInspection(report)
	m = summaryOpenAdvanced(t, m)
	if m.Creation.Spec.CPU.Model == "test-model" {
		t.Fatal("new source reused previous advanced settings")
	}
	summaryNoApply(t, c)
}

func TestWorkspaceSummaryPickingApplianceAutomaticallyDescribesWithoutApply(t *testing.T) {
	m, c := summaryWorkspace(t)
	source := filepath.Join(t.TempDir(), "picked.ova")
	c.report.Source = source
	c.report.Integrity = "not-verified"
	c.report.Readiness = "metadata-only"
	m.ImportPickerTarget = "source"
	m.Picker = &FilePicker{kind: "file", loading: true, token: 998}
	n, cmd := m.Update(pickerRead{token: 998, path: source, selected: true})
	m = n.(Workspace)
	if cmd == nil || !m.Busy || m.Picker != nil {
		t.Fatal("selection did not start metadata read", m.View())
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatal("metadata request and elapsed display missing")
	}
	m = summaryRun(t, m, batch[0])
	if len(c.Calls) != 1 || c.Calls[0] != "import.describe" || c.Requests[0].Path != source || c.Requests[0].Action != "" {
		t.Fatal("selection did not use read-only metadata service", c.Calls, c.Requests)
	}
	if m.Import.Page != 3 || m.Import.Draft.Report.Readiness != "metadata-only" || m.Plan != nil || m.Creation != nil || m.Busy {
		t.Fatal("metadata read skipped source review", m.View())
	}
	summaryNoApply(t, c)
}
