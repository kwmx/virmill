package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// A network has its own plan/job. The VM's source, preparation identity and
// editing draft must never acquire that job's identity or submission state.
type creationNetworkHandoff struct {
	Creation   CreationForm
	Import     *ImportForm
	Connection string
	Section    int
	Binding    string
	JobID      string
	PlanID     string
}
type creationNetworkRefresh struct{ Connection, Binding string }

func creationNetworkBinding(f *CreationForm, imported *ImportForm, connection string) string {
	if f == nil {
		return ""
	}
	value := struct {
		Connection, OperationID, Source, Import string
		Before                                  bool
	}{Connection: connection, OperationID: f.OperationID, Source: setupBinding(f.Source), Before: f.BeforePreparation}
	if imported != nil {
		value.Import = creationDraftBinding(imported.Draft)
	}
	return setupBinding(value)
}
func (m *Workspace) openCreationNetwork() tea.Cmd {
	if m.Creation == nil || m.Busy || m.Pending["apply"] != 0 || m.CreationNetwork != nil {
		return nil
	}
	if m.Connection != "qemu:///system" {
		m.Creation.Error = "Create network requires the local system connection. Your VM choices were kept."
		return nil
	}
	f := *m.Creation
	f.Spec = saveCreationValues(f).Spec
	f.Networks = slices.Clone(f.Networks)
	var imported *ImportForm
	if m.Import != nil {
		i := *m.Import
		imported = &i
	}
	m.CreationNetwork = &creationNetworkHandoff{Creation: f, Import: imported, Connection: m.Connection, Section: m.Section, Binding: creationNetworkBinding(&f, imported, m.Connection)}
	m.Creation, m.Import = nil, nil
	m.CreationPicking = false
	m.draftSubmitted = false
	m.Pending = maps.Clone(m.Pending)
	for _, kind := range []string{"creation-load", "creation-sources", "creation-networks", "import-inspect"} {
		delete(m.Pending, kind)
	}
	m.CreationNetworkRefresh = nil
	m.openNetworkForm()
	m.Notice = "Create this network separately. Your VM setup is kept; Esc returns to it."
	return nil
}
func (m *Workspace) refreshCreationNetworks() tea.Cmd {
	if m.Creation == nil || m.Pending["apply"] != 0 || m.Pending["creation-networks"] != 0 {
		return nil
	}
	m.CreationNetworkRefresh = &creationNetworkRefresh{Connection: m.Connection, Binding: creationNetworkBinding(m.Creation, m.Import, m.Connection)}
	m.Creation.Error = ""
	m.Busy = true
	m.Notice = "Refreshing network choices. Your VM settings are kept..."
	return m.request("creation-networks", "network.list", app.Request{})
}
func (m *Workspace) receiveCreationNetworks(data any) {
	expected := m.CreationNetworkRefresh
	m.CreationNetworkRefresh = nil
	m.Busy, m.Notice = false, ""
	if expected == nil || m.Creation == nil {
		return
	}
	if expected.Connection != m.Connection || expected.Binding != creationNetworkBinding(m.Creation, m.Import, m.Connection) {
		m.Creation.Error = "VM setup changed during refresh. Refresh networks again."
		return
	}
	var networks []domain.VirtualNetwork
	raw, err := json.Marshal(data)
	if err != nil || json.Unmarshal(raw, &networks) != nil || len(networks) > 4096 {
		m.Creation.Error = "Could not read network choices. Your settings are kept; try Refresh networks."
		return
	}
	ids, names := map[string]bool{}, map[string]bool{}
	active := 0
	for _, n := range networks {
		if n.Key.ProviderID != "libvirt" || n.Key.Kind != "network" || n.Key.ConnectionID != m.Connection || !guidedUUID.MatchString(n.Key.UUID) || n.Key.UUID == "00000000-0000-0000-0000-000000000000" || n.Name == "" || ids[n.Key.UUID] || names[n.Name] {
			m.Creation.Error = "Network identities could not be verified. Existing choices were kept; refresh again."
			return
		}
		ids[n.Key.UUID], names[n.Name] = true, true
		if n.Active {
			active++
		}
	}
	m.Creation.Networks = networks
	m.Creation.Error = ""
	m.Notice = fmt.Sprintf("%d active network(s). Choose a network for each adapter; cable settings are unchanged.", active)
	if active == 0 {
		m.Creation.Error = "No active networks yet. Choose Create network, or start an existing one and Refresh networks."
	}
	for _, nic := range m.Creation.Spec.NICs {
		if nic.NetworkID == "" {
			continue
		}
		found := false
		for _, n := range networks {
			if n.Key.UUID == nic.NetworkID && n.Active {
				found = true
				break
			}
		}
		if !found {
			m.Creation.Error = "A selected network is unavailable. Choose an active network for that adapter; your other settings are kept."
			break
		}
	}
}
func (m *Workspace) returnCreationNetwork(refresh bool) tea.Cmd {
	h := m.CreationNetwork
	if h == nil {
		return nil
	}
	if m.Pending["apply"] != 0 {
		m.Notice = "Submission is pending. Check its result before returning to VM setup."
		return nil
	}
	if h.Connection != m.Connection || h.Binding != creationNetworkBinding(&h.Creation, h.Import, h.Connection) {
		m.Error = "The saved VM setup no longer matches this connection or source. Resume your saved setup."
		return nil
	}
	m.resetNetworkForm()
	m.Plan = nil
	m.Reviewing = false
	m.ActionForm = nil
	m.SavedActionForm = nil
	m.Form = nil
	m.SavedForm = nil
	m.Creation = &h.Creation
	m.Import = h.Import
	m.CreationNetwork = nil
	m.CreationPicking = false
	m.Section, m.NavIndex = h.Section, h.Section
	m.Detail = nil
	m.DetailTitle = ""
	m.Creation.Page = 2
	m.Busy = false
	m.Error = ""
	m.Offset = 0
	m.Notice = "VM setup restored. Choose the network explicitly; adapter settings are unchanged."
	if refresh {
		return m.refreshCreationNetworks()
	}
	return nil
}
func (m *Workspace) acceptCreationNetwork(data any) (bool, tea.Cmd) {
	h := m.CreationNetwork
	if h == nil {
		return false, nil
	}
	var job domain.Job
	raw, err := json.Marshal(data)
	if err != nil || json.Unmarshal(raw, &job) != nil || !guidedUUID.MatchString(job.ID) || m.Plan == nil || m.Plan.Operation != "network.create" || job.PlanID != m.Plan.ID || m.Plan.ConnectionID != h.Connection {
		m.Busy = false
		m.Error = "Network submission could not be matched. Check Jobs before another attempt; VM choices are kept."
		return true, nil
	}
	hcopy := *h
	hcopy.JobID, hcopy.PlanID = job.ID, job.PlanID
	m.CreationNetwork = &hcopy
	m.resetNetworkForm()
	m.resetJobOutcome()
	m.Plan = nil
	m.Reviewing = false
	m.Form = nil
	m.ActionForm = nil
	m.SavedForm = nil
	m.SavedActionForm = nil
	m.Section, m.NavIndex = 8, 8
	m.Detail = generic(job)
	m.DetailTitle = "Network job"
	m.Offset = 0
	m.Busy = false
	m.Notice = "Creating the network. VM setup will return when it completes; you can also use Back to VM setup."
	return true, m.request("jobs", "operation.list", app.Request{})
}
func (m Workspace) creationNetworkJobMatches() bool {
	h := m.CreationNetwork
	return h != nil && h.Connection == m.Connection && h.JobID != "" && h.JobID == resourceID(m.Detail) && h.PlanID == field(m.Detail, "planID") && m.Section == 8
}
func (m Workspace) canAutoReturnCreationNetwork() bool {
	return m.creationNetworkJobMatches() && !m.Busy && m.Pending["apply"] == 0 &&
		m.Plan == nil && !m.Reviewing && !m.Advanced && !m.Help && m.draftModal == "" &&
		m.ActionForm == nil && m.Form == nil && m.Picker == nil && m.ExportForm == nil &&
		m.NetworkForm == nil && m.Creation == nil && m.Import == nil && !m.CreationPicking &&
		m.AutostartTarget == nil && m.RemovalTarget == nil && m.Resources == nil &&
		m.BackupRecovery == nil && !m.BackupRecoveryLoading && m.GuestAgent == nil &&
		m.Protection == nil && m.Console == nil && !m.ConsoleLoading && m.Boot == nil && !m.BootLoading
}
func (m *Workspace) cancelCreationNetworkRefresh() {
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "creation-networks")
	m.CreationNetworkRefresh = nil
}
