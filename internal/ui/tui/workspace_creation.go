package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

type creationChoice struct {
	OperationID string `json:"operationID"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Destination string `json:"destination"`
}
type creationBundle struct {
	Source      CreationSource
	Options     domain.CreationOptions
	Pools       []domain.StoragePool
	Networks    []domain.VirtualNetwork
	OperationID string
}
type creationPulse struct{ OperationID string }

func creationTick(id string) tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return creationPulse{OperationID: id} })
}

func (m *Workspace) openCreationSources() tea.Cmd {
	if m.offerSetup("creation") {
		return nil
	}
	m.draftSubmitted = false
	if m.Creation != nil {
		m.SavedCreation = m.Creation
	}
	m.Creation = nil
	m.CreationPicking = true
	m.CreationChoices = nil
	m.CreationIndex = 0
	m.Advanced = false
	m.ActionForm = nil
	m.Error = ""
	m.Busy = true
	return m.request("creation-sources", "import.sources", app.Request{})
}

func (m *Workspace) loadCreation(id, machine string) tea.Cmd {
	m.sequence++
	token := m.sequence
	m.Pending = maps.Clone(m.Pending)
	m.Pending["creation-load"] = token
	m.Busy = true
	m.CreationPicking = true
	m.Error = ""
	client, connection := m.Client, m.Connection
	draftSource := CreationSource{}
	if id == "" && m.Import != nil {
		draftSource = importCreationSource(m.Import.Draft)
	}
	return func() tea.Msg {
		b := creationBundle{OperationID: id}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		call := func(method string, request app.Request, target any) error {
			if client == nil {
				return fmt.Errorf("The coordinator is unavailable. Reconnect and try again.")
			}
			request.Connection = connection
			r, err := client.Call(ctx, method, request)
			if err != nil {
				return err
			}
			if r.Error != nil {
				return r.Error
			}
			raw, err := json.Marshal(r.Data)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, target)
		}
		var result struct {
			Artifact CreationSource `json:"artifact"`
		}
		var err error
		if id != "" {
			err = call("import.result", app.Request{ID: id}, &result)
			b.Source = result.Artifact
		} else {
			b.Source = draftSource
		}
		if err == nil {
			err = call("vm.creation.options", app.Request{Input: map[string]any{"machine": machine}}, &b.Options)
		}
		if err == nil {
			err = call("storage.pool.list", app.Request{}, &b.Pools)
		}
		if err == nil {
			err = call("network.list", app.Request{}, &b.Networks)
		}
		return workspaceReply{Kind: "creation-load", Token: token, Response: app.Response{Data: b}, Err: err}
	}
}

func (m Workspace) updateCreation(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Busy {
		if key.Type == tea.KeyEsc {
			m.Pending = maps.Clone(m.Pending)
			m.cancelCreationNetworkRefresh()
			delete(m.Pending, "creation-load")
			delete(m.Pending, "creation-sources")
			delete(m.Pending, "plan")
			m.Busy = false
			m.CreationPicking = false
			m.Notice = ""
		}
		return m, nil
	}
	if m.Creation == nil {
		switch key.Type {
		case tea.KeyEsc:
			m.CreationPicking = false
			m.Error = ""
		case tea.KeyUp:
			m.CreationIndex = max(0, m.CreationIndex-1)
		case tea.KeyDown, tea.KeyTab:
			m.CreationIndex = min(len(m.CreationChoices), m.CreationIndex+1)
		case tea.KeyEnter:
			if m.CreationIndex >= len(m.CreationChoices) {
				m.CreationPicking = false
				m.Error = ""
				return m, m.openImport("auto")
			}
			return m, m.loadCreation(m.CreationChoices[m.CreationIndex].OperationID, "")
		}
		return m, nil
	}
	previousPage := m.Creation.Page
	f, intent := m.Creation.Update(key)
	m.Creation = &f
	if f.BeforePreparation && m.Import != nil && m.Import.Page == 3 && previousPage == 3 && f.Page != 3 {
		m.saveImportHardware(f)
		return m, nil
	}
	switch intent.Kind {
	case "create-network":
		return m, m.openCreationNetwork()
	case "refresh-networks":
		return m, m.refreshCreationNetworks()
	case "cancel":
		m.Creation = nil
		m.CreationPicking = false
		m.Error = ""
	case "reload":
		return m, m.loadCreation(f.OperationID, f.Spec.Machine)
	case "preview":
		r, err := f.Request(m.Connection)
		if err != nil {
			m.Creation.Error = err.Error()
			return m, nil
		}
		m.Creation.Error = ""
		m.Error = ""
		if f.BeforePreparation {
			if m.Import == nil {
				m.Creation.Error = "Return to import and choose the source again."
				return m, nil
			}
			method, prep, err := m.Import.Draft.Request(m.Connection)
			if err != nil {
				m.Creation.Error = err.Error()
				return m, nil
			}
			m.Import.VM = &f
			m.Import.VMBinding = creationDraftBinding(m.Import.Draft)
			m.Creation = nil
			m.CreationPicking = false
			m.Busy = true
			return m, m.request("plan", method, prep)
		}
		m.Busy = true
		return m, m.request("plan", "vm.create", r)
	case "export":
		if _, err := f.Request(m.Connection); err != nil {
			m.Creation.Error = err.Error()
			return m, nil
		}
		m.openSettingsExport("virmill-vm-settings.json")
	}
	return m, nil
}

func (m Workspace) creationView(width, height int) []string {
	if m.Busy && m.Pending["creation-load"] != 0 {
		return pageLines([]string{"Set up your VM", "", "Reading supported hardware and prepared images…", "", "Esc Back"}, width, height, 0)
	}
	if m.Creation != nil {
		return strings.Split(m.Creation.View(width, height), "\n")
	}
	lines := []string{"Create a virtual machine", "", "Choose prepared images, or import a new source.", "CPU, RAM, firmware and networks are configured next.", ""}
	if m.Busy {
		return append(lines, "Loading prepared images…")
	}
	for i, choice := range m.CreationChoices {
		mark := "  "
		if i == m.CreationIndex {
			mark = "> "
		}
		lines = append(lines, mark+"[ "+choice.Name+" ]")
		if i == m.CreationIndex {
			lines = append(lines, "  "+choice.Destination)
		}
	}
	mark := "  "
	if m.CreationIndex >= len(m.CreationChoices) {
		mark = "> "
	}
	lines = append(lines, mark+"[ Import new images ]")
	return pageLines(lines, width, height, max(0, m.CreationIndex-height+9))
}

func importCreationSource(d ImportDraft) CreationSource {
	source := CreationSource{Kind: map[string]string{"ova": "PreparedImport", "iso": "PreparedInstallation", "disks": "PreparedDiskSet"}[d.Kind]}
	if d.Report != nil {
		for _, s := range d.Report.Systems {
			if s.ID == d.SystemID {
				source.System = s
				break
			}
		}
	}
	for _, disk := range d.Disks {
		source.Disks = append(source.Disks, CreationSourceDisk{SourceID: disk.ID})
	}
	if d.Kind == "iso" {
		source.Media = []CreationSourceMedia{{SourceID: d.MediaID}}
	}
	return source
}

// Bind choices to the selected appliance and disk mappings, not its destination.
func creationDraftBinding(d ImportDraft) string {
	d.DestinationParent, d.DestinationName = "", ""
	d.VMName, d.VCPUs, d.MemoryMiB = "", "", ""
	d.Offline = false
	raw, _ := json.Marshal(d)
	return string(raw)
}
func (m *Workspace) configureImportHardware() tea.Cmd {
	if m.Import == nil {
		return nil
	}
	d := m.Import.Draft
	if d.Source == "" || (d.Kind == "ova" && (d.Report == nil || d.SystemID == "")) {
		m.Import.Error = "Choose an appliance first."
		return nil
	}
	m.Creation = nil
	if m.Import.VM != nil && m.Import.VMBinding == creationDraftBinding(d) {
		f := *m.Import.VM
		m.Creation = &f
	}
	return m.loadCreation("", "")
}
func applyImportBasics(f *CreationForm, d ImportDraft) {
	if d.VMName != "" {
		f.Spec.Name = d.VMName
	}
	if d.VCPUs != "" {
		f.CPUText = d.VCPUs
	}
	if d.MemoryMiB != "" {
		f.MemoryText = d.MemoryMiB
	}
}
func (m *Workspace) saveImportHardware(f CreationForm) {
	m.Import.Focus = 0
	m.Import.VM = &f
	m.Import.VMBinding = creationDraftBinding(m.Import.Draft)
	m.Import.Draft.VMName, m.Import.Draft.VCPUs, m.Import.Draft.MemoryMiB = f.Spec.Name, f.CPUText, f.MemoryText
	m.Creation = nil
	m.CreationPicking = false
}
