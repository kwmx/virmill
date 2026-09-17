package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type resourceSetup struct {
	VM      domain.VM
	Report  *domain.VMResourceView
	Form    GuidedForm
	Loading bool
	Focus   int
	Error   string
	Details bool
	Offset  int
}

func resourceValue(v *uint64) string {
	if v == nil {
		return "Unknown"
	}
	return strconv.FormatUint(*v, 10)
}
func resourceMemory(v *uint64) string {
	if v == nil {
		return "Unknown"
	}
	if *v%(1<<20) == 0 {
		return strconv.FormatUint(*v>>20, 10) + " MiB"
	}
	return strconv.FormatUint(*v, 10) + " bytes"
}
func resourceTable(r domain.VMResourceView) []string {
	liveCPU, liveMemory := "Unavailable", "Unavailable"
	if r.State == "stopped" {
		liveCPU, liveMemory = "Not running", "Not running"
	}
	if r.Live != nil {
		liveCPU = resourceValue(r.Live.VCPUs)
		liveMemory = resourceMemory(r.Live.MemoryBytes)
	}
	return []string{fmt.Sprintf("%-10s %-22s %s", "Resources", "Live", "Next boot"), fmt.Sprintf("%-10s %-22s %s", "CPU cores", liveCPU, resourceValue(r.Persistent.VCPUs)), fmt.Sprintf("%-10s %-22s %s", "RAM", liveMemory, resourceMemory(r.Persistent.MemoryBytes))}
}
func decodeResources(data any, vm domain.VM) (domain.VMResourceView, error) {
	var r domain.VMResourceView
	raw, err := json.Marshal(data)
	if err == nil {
		err = validation.Schema("vm-resources-view", raw)
	}
	if err == nil {
		err = wire.Decode(raw, &r)
	}
	if err != nil || r.Resource != vm.Key || r.Resource.ConnectionID == "" || !guidedUUID.MatchString(r.Resource.UUID) || r.Resource.ProviderID != "libvirt" || r.Resource.Kind != "vm" || len(r.Fingerprint) != 64 || r.Name == "" || r.State == "" {
		return r, fmt.Errorf("Could not match these resource settings to the selected VM. Refresh to try again")
	}
	if r.CanEditCPU && (r.Persistent.VCPUs == nil || *r.Persistent.VCPUs == 0) || r.CanEditMemory && (r.Persistent.MemoryBytes == nil || *r.Persistent.MemoryBytes == 0) {
		return r, fmt.Errorf("Editable resource values are incomplete. Refresh to try again")
	}
	if (r.CanEditCPU || r.CanEditMemory) && (r.HasManagedSave || !slices.Contains(r.ApplyModes, "next-boot")) {
		return r, fmt.Errorf("Resource edit availability is inconsistent. Refresh before making changes")
	}
	return r, nil
}
func (m *Workspace) resetResources() {
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "resources-load")
	m.Resources = nil
}
func (m *Workspace) openResources() tea.Cmd {
	vm := m.selectedVM()
	if vm.Key.UUID == "" {
		m.Error = "Choose a VM before editing CPU and RAM."
		return nil
	}
	m.resetResources()
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	m.Resources = &resourceSetup{VM: vm, Loading: true}
	m.Form = nil
	m.Advanced = false
	m.Busy = true
	m.Error, m.Notice = "", ""
	return m.request("resources-load", "vm.resources.show", app.Request{ID: vm.Key.UUID})
}
func (m *Workspace) receiveResources(data any) {
	if m.Resources == nil {
		return
	}
	g := *m.Resources
	m.Resources = &g
	g.Loading = false
	m.Busy = false
	r, err := decodeResources(data, g.VM)
	if err != nil {
		g.Error = validation.SafeText(err.Error())
		return
	}
	previous := g.Report
	g.Report = &r
	g.Error = ""
	f, _ := NewGuidedForm("resources", g.VM)
	f.Fields[0].Limit = 20
	f.Fields[1].Limit = 20
	f.Fields[0].Hint = "CPU cores for the next boot. Enter a whole number; blank is invalid."
	f.Fields[1].Hint = "RAM for the next boot in MiB (1024 MiB = 1 GiB)."
	if r.Persistent.VCPUs != nil {
		f.Fields[0].Value = resourceValue(r.Persistent.VCPUs)
	}
	f.Fields[1].Value = resourceMemoryInput(r.Persistent.MemoryBytes)
	if previous != nil {
		previousCPU := resourceValue(previous.Persistent.VCPUs)
		previousMemory := resourceMemoryInput(previous.Persistent.MemoryBytes)
		if previous.Fingerprint == r.Fingerprint || g.Form.Fields[0].Value != previousCPU {
			f.Fields[0].Value = g.Form.Fields[0].Value
		}
		if previous.Fingerprint == r.Fingerprint || g.Form.Fields[1].Value != previousMemory {
			f.Fields[1].Value = g.Form.Fields[1].Value
		}
		if previous.Fingerprint != r.Fingerprint {
			g.Error = "The VM configuration changed. Your edits were kept; compare them with the refreshed next-boot values."
		}
	}
	for i := range f.Fields {
		f.Fields[i].Cursor = len([]rune(f.Fields[i].Value))
	}
	g.Form = f
	g.Focus = g.rows()[0]
}
func (g resourceSetup) rows() []int {
	if g.Report == nil {
		return []int{4, 5}
	}
	r := g.Report
	rows := []int{}
	if r.CanEditCPU {
		rows = append(rows, 0)
	}
	if r.CanEditMemory {
		rows = append(rows, 1)
	}
	if r.CanEditCPU || r.CanEditMemory {
		rows = append(rows, 2)
	}
	// Changing what the VM is running with is its own action (ADR 0068).
	if r.CanChangeLiveCPU || r.CanChangeLiveMemory {
		rows = append(rows, 7)
	}
	if r.RequiresShutdown && !r.HasManagedSave && r.State == "running" {
		rows = append(rows, 3)
	}
	if resourceHasDetails(*r) {
		rows = append(rows, 6)
	}
	return append(rows, 4, 5)
}
func (g resourceSetup) request(connection string) (app.Request, error) {
	r := g.Report
	if r == nil || r.Resource.ConnectionID != connection {
		return app.Request{}, fmt.Errorf("Refresh resource settings for this connection first")
	}
	input := map[string]any{}
	if r.CanEditCPU && g.Form.Fields[0].Value != resourceValue(r.Persistent.VCPUs) {
		n, ok := guidedNumber(g.Form.Fields[0].Value, 512)
		if !ok {
			return app.Request{}, fmt.Errorf("CPU cores must be a whole number from 1 to 512; blank is not a value")
		}
		input["vcpus"] = float64(n)
	}
	if r.CanEditMemory && g.Form.Fields[1].Value != resourceMemoryInput(r.Persistent.MemoryBytes) {
		n, ok := guidedNumber(g.Form.Fields[1].Value, 1048576)
		if !ok {
			return app.Request{}, fmt.Errorf("RAM must be a whole number from 1 to 1048576 MiB; blank is not a value")
		}
		input["memoryMiB"] = float64(n)
	}
	if len(input) == 0 {
		return app.Request{}, fmt.Errorf("No changes yet. Adjust CPU cores or RAM before previewing")
	}
	input["applyMode"] = "next-boot"
	return app.Request{Connection: connection, ID: r.Resource.UUID, Action: "set", Input: input}, nil
}

// liveRequest asks for what the VM should be running with, comparing the typed
// values with the running ones rather than the next-boot ones (ADR 0068).
func (g resourceSetup) liveRequest(connection string) (app.Request, error) {
	r := g.Report
	if r == nil || r.Resource.ConnectionID != connection || r.Live == nil {
		return app.Request{}, fmt.Errorf("Refresh resource settings for this connection first")
	}
	input := map[string]any{}
	if r.CanChangeLiveCPU && g.Form.Fields[0].Value != resourceValue(r.Live.VCPUs) {
		n, ok := guidedNumber(g.Form.Fields[0].Value, 512)
		if !ok {
			return app.Request{}, fmt.Errorf("CPU cores must be a whole number from 1 to 512; blank is not a value")
		}
		input["vcpus"] = float64(n)
	}
	if r.CanChangeLiveMemory && g.Form.Fields[1].Value != resourceMemoryInput(r.Live.MemoryBytes) {
		n, ok := guidedNumber(g.Form.Fields[1].Value, 1048576)
		if !ok {
			return app.Request{}, fmt.Errorf("RAM must be a whole number from 1 to 1048576 MiB; blank is not a value")
		}
		input["memoryMiB"] = float64(n)
	}
	if len(input) == 0 {
		return app.Request{}, fmt.Errorf("No change yet. Set CPU cores or RAM to something else than this VM is running with")
	}
	input["applyMode"] = "now"
	return app.Request{Connection: connection, ID: r.Resource.UUID, Action: "set", Input: input}, nil
}
func (m Workspace) updateResources(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Resources == nil {
		return m, nil
	}
	if g := m.Resources; g.Details {
		copy := *g
		m.Resources = &copy
		switch key.Type {
		case tea.KeyEsc:
			copy.Details = false
			copy.Offset = 0
		case tea.KeyDown:
			copy.Offset++
		case tea.KeyUp:
			copy.Offset = max(0, copy.Offset-1)
		case tea.KeyPgDown:
			copy.Offset += max(1, m.Height-10)
		case tea.KeyPgUp:
			copy.Offset = max(0, copy.Offset-max(1, m.Height-10))
		}
		return m, nil
	}
	if key.Type == tea.KeyEsc {
		m.resetResources()
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "plan")
		m.Busy = false
		m.Error, m.Notice = "", ""
		return m, nil
	}
	if m.Busy || m.Resources.Loading {
		return m, nil
	}
	g := *m.Resources
	m.Resources = &g
	rows := g.rows()
	at := max(0, slices.Index(rows, g.Focus))
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		g.Focus = rows[(at+1)%len(rows)]
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		g.Focus = rows[(at+len(rows)-1)%len(rows)]
		return m, nil
	case tea.KeyEnter:
		switch g.Focus {
		case 0, 1:
			g.Focus = rows[(at+1)%len(rows)]
		case 2:
			req, err := g.request(m.Connection)
			if err != nil {
				g.Error = err.Error()
				return m, nil
			}
			g.Error = ""
			m.Busy = true
			m.Error = ""
			return m, m.request("plan", "vm.plan", req)
		case 7:
			req, err := g.liveRequest(m.Connection)
			if err != nil {
				g.Error = err.Error()
				return m, nil
			}
			g.Error = ""
			m.Busy = true
			m.Error = ""
			return m, m.request("plan", "vm.plan", req)
		case 3:
			m.Busy = true
			g.Error = ""
			m.Error = ""
			return m, m.request("plan", "vm.plan", app.Request{ID: g.VM.Key.UUID, Action: "stop"})
		case 4:
			g.Loading = true
			g.Error = ""
			m.Busy = true
			return m, m.request("resources-load", "vm.resources.show", app.Request{ID: g.VM.Key.UUID})
		case 6:
			g.Details = true
			g.Offset = 0
		case 5:
			m.resetResources()
			m.Error, m.Notice = "", ""
		}
		return m, nil
	}
	if g.Report != nil && (g.Focus == 0 && g.Report.CanEditCPU || g.Focus == 1 && g.Report.CanEditMemory) {
		g.Form.Focus = g.Focus
		if g.Focus == 1 && g.Form.Fields[1].Value == "Keep current" && key.Type == tea.KeyRunes && !key.Alt && len(key.Runes) > 0 {
			digits := true
			for _, r := range key.Runes {
				digits = digits && r >= '0' && r <= '9'
			}
			if digits {
				g.Form.Fields = slices.Clone(g.Form.Fields)
				g.Form.Fields[1].Value = ""
				g.Form.Fields[1].Cursor = 0
			}
		}
		if key.Type == tea.KeyCtrlU {
			g.Form.Fields = slices.Clone(g.Form.Fields)
			g.Form.Fields[g.Focus].Value = ""
			g.Form.Fields[g.Focus].Cursor = 0
			g.Error = ""
		} else {
			g.Form, _, _ = g.Form.Update(key)
			g.Error = g.Form.Error
		}
	}
	return m, nil
}
func (m Workspace) resourcesView(width, height int) []string {
	g := m.Resources
	if g == nil {
		return nil
	}
	if g.Details {
		return resourceDetails(*g, width, height)
	}
	if g.Loading {
		return pageLines([]string{"CPU and RAM", "Reading current and next-boot settings...", "", "Esc Back"}, width, height, 0)
	}
	lines := []string{"CPU and RAM · " + validation.SafeText(g.VM.Name), "Changes below apply at the next boot.", ""}
	if g.Report != nil {
		r := g.Report
		lines = append(lines, resourceTable(*r)...)
		lines = append(lines, "")
		for i, label := range []string{"CPU cores", "RAM (MiB)"} {
			editable := r.CanEditCPU
			if i == 1 {
				editable = r.CanEditMemory
			}
			value := "Not editable"
			if editable {
				value = "[" + g.Form.Fields[i].Value + "]"
			}
			prefix := "  "
			if g.Focus == i {
				prefix = "> "
			}
			lines = append(lines, prefix+"Requested "+label+": "+value)
		}
		if r.CanChangeLiveCPU || r.CanChangeLiveMemory {
			what := "CPU cores and RAM"
			switch {
			case !r.CanChangeLiveCPU:
				what = "RAM"
			case !r.CanChangeLiveMemory:
				what = "CPU cores"
			}
			lines = append(lines, "Change while it runs applies "+what+" to the running VM now and leaves the next boot as it is.")
		}
		if r.CanEditMemory && resourceMemoryInput(r.Persistent.MemoryBytes) == "Keep current" {
			lines = append(lines, "Keep current preserves exact bytes. Type a number to replace it.")
		}
		if r.HasManagedSave {
			lines = append(lines, "Saved runtime exists. Resume it, then shut down before editing.")
		} else if r.RequiresShutdown && (r.CanEditCPU || r.CanEditMemory) {
			lines = append(lines, "The VM keeps its current values until it shuts down; a restart inside the guest is not enough.")
		} else if r.RequiresShutdown {
			lines = append(lines, "Shut down first; then return here to change these values.")
		}
	}
	labels := map[int]string{2: "Preview changes", 3: "Preview graceful shutdown", 4: "Refresh values", 5: "Back to VM", 6: "Advanced details", 7: "Change while it runs"}
	for _, row := range g.rows() {
		if row < 2 {
			continue
		}
		prefix := "  "
		if g.Focus == row {
			prefix = "> "
		}
		lines = append(lines, prefix+"[ "+labels[row]+" ]")
	}
	if g.Report != nil {
		r := g.Report
		reasons := []string{}
		if r.CPUReason != "" {
			reasons = append(reasons, "CPU: "+validation.SafeText(r.CPUReason))
		}
		if r.MemoryReason != "" {
			reasons = append(reasons, "RAM: "+validation.SafeText(r.MemoryReason))
		}
		if r.State == "running" && r.LiveCPUReason != "" && !r.CanChangeLiveCPU {
			reasons = append(reasons, "While running, CPU: "+validation.SafeText(r.LiveCPUReason))
		}
		if r.State == "running" && r.LiveMemoryReason != "" && !r.CanChangeLiveMemory {
			reasons = append(reasons, "While running, RAM: "+validation.SafeText(r.LiveMemoryReason))
		}
		for _, reason := range reasons {
			lines = append(lines, clipCell(reason, width-1))
		}
	}
	if g.Error != "" {
		lines = append(lines, wrap("Issue: "+validation.SafeText(g.Error), width)...)
	}
	return pageLines(lines, width, height, 0)
}
func (m *Workspace) requestResourceSummary() tea.Cmd {
	vm := m.selectedVM()
	if m.Section != 1 || m.DetailTitle != "VM details" || vm.Key.UUID == "" {
		return nil
	}
	m.ResourceSummary = nil
	m.ResourceSummaryVM = vm
	m.ResourceSummaryError = ""
	return m.request("resources-summary", "vm.resources.show", app.Request{ID: vm.Key.UUID})
}
func (m *Workspace) receiveResourceSummary(data any) {
	if m.Section != 1 || m.DetailTitle != "VM details" || resourceID(m.Detail) != m.ResourceSummaryVM.Key.UUID {
		return
	}
	r, err := decodeResources(data, m.ResourceSummaryVM)
	if err != nil {
		m.ResourceSummaryError = validation.SafeText(err.Error())
		return
	}
	m.ResourceSummary = &r
	m.ResourceSummaryError = ""
}
func (m Workspace) resourceSummaryLines(vm domain.VM) []string {
	if m.ResourceSummary != nil && m.ResourceSummary.Resource == vm.Key {
		return resourceTable(*m.ResourceSummary)
	}
	if m.ResourceSummaryError != "" {
		return []string{"CPU/RAM: unavailable; open CPU / RAM to refresh."}
	}
	if m.Pending["resources-summary"] != 0 {
		return []string{"CPU/RAM: reading current and next-boot values..."}
	}
	return []string{"Open CPU / RAM to view current and next-boot values."}
}

func resourceDetails(g resourceSetup, width, height int) []string {
	if g.Report == nil {
		return nil
	}
	r := g.Report
	lines := []string{"CPU and RAM · configuration details", ""}
	lines = append(lines, resourceTable(*r)...)
	lines = append(lines, "Next-boot CPU maximum: "+resourceValue(r.Persistent.MaximumVCPUs), "Next-boot RAM maximum: "+resourceMemory(r.Persistent.MaximumMemoryBytes), "")
	reasons := []string{"CPU: " + r.CPUReason, "RAM: " + r.MemoryReason}
	if r.Live != nil {
		lines = append(lines, "Live CPU maximum: "+resourceValue(r.Live.MaximumVCPUs), "Live RAM maximum: "+resourceMemory(r.Live.MaximumMemoryBytes))
		if r.Live.CPUError != "" {
			reasons = append(reasons, "Live CPU: "+r.Live.CPUError)
		}
		if r.Live.MemoryError != "" {
			reasons = append(reasons, "Live RAM: "+r.Live.MemoryError)
		}
	}
	for _, reason := range reasons {
		if strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(reason, "CPU:"), "RAM:")) != "" {
			lines = append(lines, wrap(validation.SafeText(reason), width)...)
			lines = append(lines, "")
		}
	}
	lines = append(lines, "Advanced CPU layouts and memory policies are shown read-only.", "This basic editor does not rewrite dependent configuration.")
	offset := min(g.Offset, max(0, len(lines)-max(1, height-1)))
	out := pageLines(lines, width, max(1, height-1), offset)
	return append(out, "Up/Down Scroll   Esc Back to CPU and RAM")
}

func resourceHasDetails(r domain.VMResourceView) bool {
	different := func(a, b *uint64) bool { return a != nil && b != nil && *a != *b }
	return r.CPUReason != "" || r.MemoryReason != "" || r.Persistent.CPUError != "" || r.Persistent.MemoryError != "" || different(r.Persistent.VCPUs, r.Persistent.MaximumVCPUs) || different(r.Persistent.MemoryBytes, r.Persistent.MaximumMemoryBytes) || r.Live != nil && (r.Live.CPUError != "" || r.Live.MemoryError != "" || different(r.Live.VCPUs, r.Live.MaximumVCPUs) || different(r.Live.MemoryBytes, r.Live.MaximumMemoryBytes))
}

func resourceMemoryInput(v *uint64) string {
	if v == nil {
		return ""
	}
	if *v%(1<<20) != 0 {
		return "Keep current"
	}
	return strconv.FormatUint(*v>>20, 10)
}
