package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func resourceNumber(n uint64) *uint64 { return &n }
func resourceReport(vm domain.VM) domain.VMResourceView {
	return domain.VMResourceView{Resource: vm.Key, Name: vm.Name, State: "stopped", Fingerprint: strings.Repeat("a", 64), Persistent: domain.ResourceValues{VCPUs: resourceNumber(4), MaximumVCPUs: resourceNumber(4), MemoryBytes: resourceNumber(4096 << 20), MaximumMemoryBytes: resourceNumber(4096 << 20)}, CanEditCPU: true, CanEditMemory: true, ApplyModes: []string{"next-boot"}}
}
func resourceWorkspace(t *testing.T, alter func(*domain.VMResourceView)) Workspace {
	t.Helper()
	m := fixtureWorkspace()
	m.Section = 1
	m.Selected = 1
	report := resourceReport(m.selectedVM())
	if alter != nil {
		alter(&report)
	}
	c := &workspaceClient{response: app.Response{Data: report}}
	m.Client = c
	cmd := m.openResources()
	if cmd == nil {
		t.Fatal("no resource read")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Resources == nil || m.Resources.Report == nil {
		t.Fatal("resource read rejected", m.Error, m.Resources)
	}
	if !reflect.DeepEqual(c.calls, []string{"vm.resources.show"}) || c.requests[0].ID != workspaceVMID || c.requests[0].Action != "" || len(c.requests[0].Input) != 0 {
		t.Fatal("not a read-only bound load", c.requests)
	}
	return m
}
func TestResourceEditorFreshPrefillAndOnlyChangedFields(t *testing.T) {
	m := resourceWorkspace(t, nil)
	g := m.Resources
	if g.Form.Fields[0].Value != "4" || g.Form.Fields[1].Value != "4096" {
		t.Fatal("did not prefill exact observed values")
	}
	if _, err := g.request(m.Connection); err == nil || !strings.Contains(err.Error(), "No changes") {
		t.Fatal("unchanged values submitted")
	}
	g.Form.Fields[1].Value = "8192"
	req, err := g.request(m.Connection)
	if err != nil || len(req.Input) != 2 || req.Input["memoryMiB"] != float64(8192) || req.Input["vcpus"] != nil || req.Input["applyMode"] != "next-boot" {
		t.Fatal("did not isolate changed field", req, err)
	}
}
func TestResourceEditorRejectsBlankAndInvalidWithoutPlan(t *testing.T) {
	for _, value := range []string{"", "0", "2.5", "-1", "9999999"} {
		m := resourceWorkspace(t, nil)
		m.Resources.Form.Fields[0].Value = value
		m.Resources.Focus = 2
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Workspace)
		if cmd != nil || m.Plan != nil || m.Resources.Error == "" || m.Resources.Form.Fields[0].Value != value {
			t.Fatal("invalid edit dispatched or discarded", value)
		}
	}
}
func TestResourceEditorDisabledCPUAllowsMemoryAndShowsReason(t *testing.T) {
	reason := "CPU topology needs advanced configuration; keep its sockets and pins."
	m := resourceWorkspace(t, func(r *domain.VMResourceView) { r.CanEditCPU = false; r.CPUReason = reason })
	if m.Resources.Focus != 1 || slicesContainInt(m.Resources.rows(), 0) {
		t.Fatal("disabled CPU takes input focus")
	}
	m.Resources.Form.Fields[0].Value = "99"
	m.Resources.Form.Fields[1].Value = "8192"
	req, err := m.Resources.request(m.Connection)
	if err != nil || req.Input["vcpus"] != nil || req.Input["memoryMiB"] != float64(8192) {
		t.Fatal("disabled field submitted", req, err)
	}
	view := m.View()
	if !strings.Contains(view, "Requested CPU cores: Not editable") || !strings.Contains(view, "Advanced details") || !strings.Contains(view, "CPU topology") {
		t.Fatal(view)
	}
	m.Resources.Focus = 6
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if !strings.Contains(m.View(), reason) || !strings.Contains(m.View(), "read-only") {
		t.Fatal("full advanced reason inaccessible", m.View())
	}
}
func slicesContainInt(values []int, value int) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func TestResourceEditorRunningShowsLiveNextBootAndReviewedShutdown(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) {
		r.State = "running"
		r.CanEditCPU = false
		r.CanEditMemory = false
		r.RequiresShutdown = true
		r.CPUReason = "Shut down first"
		r.MemoryReason = "Shut down first"
		r.Live = &domain.ResourceValues{VCPUs: resourceNumber(2), MemoryBytes: resourceNumber(2048 << 20)}
	})
	view := m.View()
	for _, text := range []string{"Live", "Next boot", "2048 MiB", "4096 MiB", "Preview graceful shutdown", "Not editable"} {
		if !strings.Contains(view, text) {
			t.Fatal(text, view)
		}
	}
	if m.Resources.Focus != 3 {
		t.Fatal("shutdown is not the clear next step")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	if cmd == nil || m.Pending["plan"] == 0 {
		t.Fatal("shutdown skipped review")
	}
	cmd()
	c := m.Client.(*workspaceClient)
	last := c.requests[len(c.requests)-1]
	if c.calls[len(c.calls)-1] != "vm.plan" || last.Action != "stop" || last.ID != workspaceVMID {
		t.Fatal("not a reviewed graceful stop", last)
	}
}
func TestResourceEditorManagedSaveDoesNotOfferShutdownShortcut(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) {
		r.HasManagedSave = true
		r.CanEditCPU = false
		r.CanEditMemory = false
		r.RequiresShutdown = true
		r.CPUReason = "Restore saved state first"
		r.MemoryReason = "Restore saved state first"
	})
	if strings.Contains(m.View(), "Preview graceful shutdown") || !strings.Contains(m.View(), "Resume it, then shut down") {
		t.Fatal(m.View())
	}
}
func TestResourceEditorCancelIgnoresLateLoadAndPlan(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	report := resourceReport(m.selectedVM())
	m.openResources()
	token := m.Pending["resources-load"]
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Workspace)
	next, _ = m.Update(workspaceReply{Kind: "resources-load", Token: token, Response: app.Response{Data: report}})
	m = next.(Workspace)
	if m.Resources != nil || m.Busy {
		t.Fatal("late load reopened canceled editor")
	}
	m = resourceWorkspace(t, nil)
	m.Resources.Form.Fields[0].Value = "8"
	m.Resources.Focus = 2
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	token = m.Pending["plan"]
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Workspace)
	next, _ = m.Update(workspaceReply{Kind: "plan", Token: token, Response: app.Response{Data: testWorkspacePlan(t)}})
	m = next.(Workspace)
	if m.Plan != nil || m.Resources != nil {
		t.Fatal("canceled editor reopened approval")
	}
}
func TestResourceEditorPlanBackAndFailureRetainEdits(t *testing.T) {
	m := resourceWorkspace(t, nil)
	m.Resources.Form.Fields[0].Value = "8"
	m.Resources.Focus = 2
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	token := m.Pending["plan"]
	next, _ = m.Update(workspaceReply{Kind: "plan", Token: token, Response: app.Response{Data: testWorkspacePlan(t)}})
	m = next.(Workspace)
	if m.Plan == nil {
		t.Fatal("plan missing")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Workspace)
	if m.Resources == nil || m.Resources.Form.Fields[0].Value != "8" {
		t.Fatal("plan back discarded input")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Workspace)
	token = m.Pending["plan"]
	next, _ = m.Update(workspaceReply{Kind: "plan", Token: token, Err: errors.New("configuration changed")})
	m = next.(Workspace)
	if m.Resources.Form.Fields[0].Value != "8" || !strings.Contains(m.Resources.Error, "configuration changed") || m.Busy {
		t.Fatal("failed review lost form")
	}
}
func TestResourceEditorUnknownValuesReadOnlyWithoutRounding(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) {
		r.CanEditCPU = false
		r.CanEditMemory = false
		r.Persistent.VCPUs = nil
		r.Persistent.MemoryBytes = resourceNumber(1025)
		r.CPUReason = "CPU could not be read"
		r.MemoryReason = "Not whole MiB"
	})
	if !strings.Contains(m.View(), "Unknown") || !strings.Contains(m.View(), "1025 bytes") || slicesContainInt(m.Resources.rows(), 2) {
		t.Fatal("unreadable configuration was guessed", m.View())
	}
}
func TestResourceEditorRefreshRetainsUnfinishedText(t *testing.T) {
	m := resourceWorkspace(t, nil)
	m.Resources.Form.Fields[0].Value = "unfinished"
	r := *m.Resources.Report
	r.Fingerprint = strings.Repeat("b", 64)
	r.Persistent.MemoryBytes = resourceNumber(8192 << 20)
	m.receiveResources(r)
	if m.Resources.Form.Fields[0].Value != "unfinished" || m.Resources.Form.Fields[1].Value != "8192" || !strings.Contains(m.Resources.Error, "edits were kept") {
		t.Fatal("refresh silently discarded choices")
	}
}
func TestResourceEditorCompactBoundsAndLateSummaryIdentity(t *testing.T) {
	m := resourceWorkspace(t, nil)
	m.Width, m.Height = 80, 24
	lines := strings.Split(m.View(), "\n")
	if len(lines) > 24 {
		t.Fatal("too many rows")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 80 {
			t.Fatal("overwide line")
		}
	}
	vm := m.selectedVM()
	m.ResourceSummaryVM = vm
	m.Section = 1
	m.DetailTitle = "VM details"
	m.Detail = generic(workspaceVM("22345678-1234-4234-8234-123456789abc", "Other"))
	m.receiveResourceSummary(resourceReport(vm))
	if m.ResourceSummary != nil {
		t.Fatal("late summary crossed VM selection")
	}
}

func TestResourceMissingLiveAndMaximumValuesRemainHonest(t *testing.T) {
	r := resourceReport(workspaceVM(workspaceVMID, "vm"))
	r.State = "running"
	lines := strings.Join(resourceTable(r), "\n")
	if strings.Contains(lines, "Not running") || !strings.Contains(lines, "Unavailable") {
		t.Fatal("missing live observation inferred stopped", lines)
	}
	r.State = "stopped"
	r.Persistent.MaximumVCPUs = resourceNumber(8)
	r.Persistent.MaximumMemoryBytes = resourceNumber(8192 << 20)
	if !resourceHasDetails(r) {
		t.Fatal("maximum configuration is inaccessible")
	}
	m := resourceWorkspace(t, func(report *domain.VMResourceView) { *report = r })
	m.Resources.Details = true
	view := m.View()
	if !strings.Contains(view, "Next-boot CPU maximum: 8") || !strings.Contains(view, "Next-boot RAM maximum: 8192 MiB") {
		t.Fatal(view)
	}
}

func TestResourceEditorCanReduceLargeObservedValuesWithoutChangingOtherField(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) {
		r.Persistent.VCPUs = resourceNumber(1024)
		r.Persistent.MemoryBytes = resourceNumber(2 << 40)
	})
	if m.Resources.Form.Fields[0].Value != "1024" || m.Resources.Form.Fields[1].Value != "2097152" {
		t.Fatal("large original values lost")
	}
	m.Resources.Form.Fields[0].Value = "128"
	req, err := m.Resources.request(m.Connection)
	if err != nil || req.Input["vcpus"] != float64(128) || req.Input["memoryMiB"] != nil {
		t.Fatal("unchanged large memory blocked CPU reduction", req, err)
	}
	m.Resources.Form.Fields[0].Value = "1024"
	m.Resources.Form.Fields[1].Value = "8192"
	req, err = m.Resources.request(m.Connection)
	if err != nil || req.Input["vcpus"] != nil || req.Input["memoryMiB"] != float64(8192) {
		t.Fatal("unchanged large CPU blocked RAM reduction", req, err)
	}
}
func TestResourceEditorKeepsExactFractionalMiBUnlessExplicitlyChanged(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) { r.Persistent.MemoryBytes = resourceNumber(1000000) })
	if m.Resources.Form.Fields[1].Value != "Keep current" || !strings.Contains(m.View(), "1000000 bytes") || !strings.Contains(m.View(), "preserves exact bytes") {
		t.Fatal(m.View())
	}
	m.Resources.Form.Fields[0].Value = "8"
	req, err := m.Resources.request(m.Connection)
	if err != nil || req.Input["memoryMiB"] != nil {
		t.Fatal("fractional MiB rounded implicitly", req, err)
	}
	m.Resources.Form.Fields[1].Value = "125"
	req, err = m.Resources.request(m.Connection)
	if err != nil || req.Input["memoryMiB"] != float64(125) {
		t.Fatal("explicit MiB replacement refused", req, err)
	}
	m.Resources.Form.Fields[1].Value = ""
	if _, err = m.Resources.request(m.Connection); err == nil {
		t.Fatal("blank mistaken for keep current")
	}
}

func TestResourceEditorSynthetic80x24Layouts(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.VMResourceView)
	}{
		{"ordinary", nil},
		{"running", func(r *domain.VMResourceView) {
			r.State = "running"
			r.CanEditCPU = false
			r.CanEditMemory = false
			r.RequiresShutdown = true
			r.CPUReason = "Shut down the VM before changing next-boot settings"
			r.MemoryReason = r.CPUReason
			r.Live = &domain.ResourceValues{VCPUs: resourceNumber(2), MemoryBytes: resourceNumber(2048 << 20)}
		}},
		{"advanced-read-only", func(r *domain.VMResourceView) {
			r.CanEditCPU = false
			r.CanEditMemory = false
			r.CPUReason = "CPU layout needs advanced configuration; the basic editor cannot safely change it. CPU topology or NUMA nodes must be preserved."
			r.MemoryReason = "Memory layout needs advanced configuration; the basic editor cannot safely change it. Balloon or hotplug limits must be preserved."
		}},
		{"exact-bytes", func(r *domain.VMResourceView) { r.Persistent.MemoryBytes = resourceNumber(1000000) }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := resourceWorkspace(t, tt.change)
			if tt.name == "exact-bytes" {
				m.Resources.Focus = 1
			}
			view := m.View()
			t.Log("Synthetic model render; no native hardware claim:\n" + view)
			if len(strings.Split(view, "\n")) > 24 || !strings.Contains(view, "Tab/Arrows Select") {
				t.Fatal("missing bounded navigation footer")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > 80 {
					t.Fatal("overwide line")
				}
			}
		})
	}
}

func TestResourceKeepCurrentStartsNumericReplacement(t *testing.T) {
	m := resourceWorkspace(t, func(r *domain.VMResourceView) { r.Persistent.MemoryBytes = resourceNumber(1000000) })
	m.Resources.Focus = 1
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("125")})
	m = next.(Workspace)
	if m.Resources.Form.Fields[1].Value != "125" {
		t.Fatal("numeric replacement appended to sentinel")
	}
	req, err := m.Resources.request(m.Connection)
	if err != nil || req.Input["memoryMiB"] != float64(125) {
		t.Fatal(req, err)
	}
}
