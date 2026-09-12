package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
)

// A completion card is bound to the selected durable job, not the last submitted
// form. Reopening Jobs after a TUI restart reads the same service records.
type jobOutcome struct {
	JobID, PlanID, State, Connection string
	Title, Summary, VMID, Error      string
	Loading                          bool
}

func (m Workspace) currentJobOutcome() *jobOutcome {
	o := m.JobOutcome
	if o == nil || m.Section != 8 || o.JobID != resourceID(m.Detail) || o.PlanID != field(m.Detail, "planID") || o.State != field(m.Detail, "state") || o.Connection != m.Connection {
		return nil
	}
	return o
}
func (m *Workspace) loadJobOutcome(force bool) tea.Cmd {
	if m.Section != 8 || field(m.Detail, "state") != "succeeded" {
		return nil
	}
	id, plan := resourceID(m.Detail), field(m.Detail, "planID")
	if !guidedUUID.MatchString(id) || !guidedUUID.MatchString(plan) {
		return nil
	}
	if current := m.currentJobOutcome(); current != nil && (current.Loading || !force) {
		return nil
	}
	m.sequence++
	token := m.sequence
	m.Pending = maps.Clone(m.Pending)
	m.Pending["job-outcome"] = token
	o := jobOutcome{JobID: id, PlanID: plan, State: "succeeded", Connection: m.Connection, Loading: true}
	m.setJobOutcome(o)
	client := m.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result := readJobOutcome(ctx, client, o)
		return workspaceReply{Kind: "job-outcome", Token: token, Response: app.Response{Data: result}}
	}
}
func readJobOutcome(ctx context.Context, client ui.Client, o jobOutcome) jobOutcome {
	o.Loading = false
	fail := func(err error) jobOutcome { o.Error = validation.SafeText(err.Error()); o.VMID = ""; return o }
	call := func(method, id string, target any) error {
		if client == nil {
			return fmt.Errorf("The service is unavailable. Refresh the result to try again.")
		}
		r, err := client.Call(ctx, method, app.Request{Connection: o.Connection, ID: id})
		if err != nil {
			return err
		}
		if r.Error != nil {
			return r.Error
		}
		b, err := json.Marshal(r.Data)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.UseNumber()
		return decoder.Decode(target)
	}
	var p domain.Plan
	if err := call("plan.show", o.PlanID, &p); err != nil {
		return fail(err)
	}
	digest, err := operations.PlanDigest(p)
	if err != nil || digest != p.Digest || p.ID != o.PlanID {
		return fail(fmt.Errorf("Could not verify this job's plan. Refresh its result."))
	}
	// These are local, known VM workflows. An unknown/plugin operation receives
	// no invented outcome or target; its normal job/events view stays available.
	switch p.Operation {
	case "vm.create", "vm.create.devices-v1":
		o.Title = "VM created"
		o.Summary = "The VM definition and disk copies were confirmed. Open the VM for its current state, Start and Console. Guest setup is a separate step."
		var result struct {
			Complete  bool       `json:"complete"`
			Operation domain.Job `json:"operation"`
			Receipt   *struct {
				PlanID          string `json:"planID"`
				OperationID     string `json:"operationID"`
				VMID            string `json:"vmID"`
				Connection      string `json:"connection"`
				Defined         bool   `json:"defined"`
				VolumesVerified bool   `json:"volumesVerified"`
			} `json:"receipt"`
		}
		if err := call("vm.creation.result", o.JobID, &result); err != nil {
			return fail(err)
		}
		r := result.Receipt
		if !result.Complete || result.Operation.ID != o.JobID || result.Operation.PlanID != o.PlanID || result.Operation.State != "succeeded" || r == nil || r.PlanID != o.PlanID || r.OperationID != o.JobID || r.Connection != o.Connection || !r.Defined || !r.VolumesVerified || !guidedUUID.MatchString(r.VMID) {
			return fail(fmt.Errorf("Creation completion could not be verified. Inspect the job and its creation result."))
		}
		o.VMID = r.VMID
	case "vm.remove-definition-v1":
		if p.ConnectionID != o.Connection || p.Review["diskDeletion"] != false || p.Review["backupsDeleted"] != false || p.Review["configurationRemoved"] != true || p.Review["backupCreated"] != false {
			return fail(fmt.Errorf("Removal result does not match the retained-storage plan. Inspect the job."))
		}
		o.Title = "VM removed; disks kept"
		o.Summary = "The VM definition was removed. Its disks and backups were kept; no disk space was freed and no configuration backup was created. Use VMs to create or restore a definition when needed."
		o.VMID = ""
		return o
	case "vm.start":
		o.Title, o.Summary = "VM started", "Open the VM to use its console or set up guest tools. A running VM does not yet prove the guest is ready."
	case "vm.stop", "vm.hard-stop":
		o.Title, o.Summary = "VM stopped", "Open the VM to change its settings or start it again."
	case "vm.configure-resources", "vm.configure-hardware":
		o.Title, o.Summary = "VM settings saved", "Open the VM to see its current and next-boot settings."
	case "vm.configure-guest-agent":
		o.Title, o.Summary = "Guest-agent connection enabled", "Open the VM, start it, then choose Guest tools to install the guest software."
	case "guest.recipe.run":
		o.Title, o.Summary = "Guest setup completed", "The recipe's verification steps completed. Open the VM to continue."
	default:
		return o
	}
	if p.ConnectionID != o.Connection {
		return fail(fmt.Errorf("This job belongs to another connection. Switch to that connection to open its VM."))
	}
	// Exactly one local VM must be part of the immutable plan. Creation's durable
	// receipt must name that same VM before a shortcut is offered.
	prefix := "libvirt|" + o.Connection + "|vm|"
	target := ""
	for _, resource := range p.ResourceIDs {
		if !strings.HasPrefix(resource, prefix) {
			continue
		}
		id := strings.TrimPrefix(resource, prefix)
		if !guidedUUID.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" || target != "" {
			return fail(fmt.Errorf("This result has no unambiguous VM target. Inspect the complete plan."))
		}
		target = id
	}
	if target == "" || o.VMID != "" && o.VMID != target {
		return fail(fmt.Errorf("The result and plan do not identify the same VM. Inspect the complete plan."))
	}
	o.VMID = target
	return o
}
func (m *Workspace) receiveJobOutcome(data any) {
	o, ok := data.(jobOutcome)
	if !ok || m.Section != 8 || o.JobID != resourceID(m.Detail) || o.PlanID != field(m.Detail, "planID") || o.State != field(m.Detail, "state") || o.Connection != m.Connection {
		return
	}
	m.setJobOutcome(o)
}
func (m *Workspace) openJobVM() tea.Cmd {
	o := m.currentJobOutcome()
	if o == nil || o.Loading || o.Error != "" || o.VMID == "" {
		return nil
	}
	m.Busy = true
	m.Error = ""
	return m.request("job-open-vm", "inventory.get", app.Request{ID: o.VMID})
}
func (m *Workspace) receiveJobVM(data any) tea.Cmd {
	m.Busy = false
	o := m.currentJobOutcome()
	if o == nil || o.VMID == "" {
		return nil
	}
	var vm domain.VM
	raw, err := json.Marshal(data)
	if err != nil || json.Unmarshal(raw, &vm) != nil || vm.Key != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "vm", UUID: o.VMID}) || vm.Fingerprint == "" {
		m.Error = "Could not verify the VM returned for this job. Refresh and try again."
		return nil
	}
	refresh := m.page(1)
	m.Detail = generic(vm)
	m.DetailTitle = "VM details"
	m.Notice = ""
	m.Error = ""
	return tea.Batch(refresh, m.requestResourceSummary())
}

func (m *Workspace) resetJobOutcome() {
	m.Pending = maps.Clone(m.Pending)
	if m.Pending["job-open-vm"] != 0 && m.Pending["apply"] == 0 {
		m.Busy = false
	}
	delete(m.Pending, "job-outcome")
	delete(m.Pending, "job-open-vm")
	m.JobOutcome = nil
}

// Async result buttons must never change the action under keyboard focus.
func (m *Workspace) setJobOutcome(o jobOutcome) {
	key := ""
	if m.ButtonFocus {
		buttons := m.jobButtons()
		if m.ButtonIndex >= 0 && m.ButtonIndex < len(buttons) {
			key = buttons[m.ButtonIndex].key
		}
	}
	m.JobOutcome = &o
	if !m.ButtonFocus {
		return
	}
	for i, button := range m.jobButtons() {
		if key != "" && button.key == key {
			m.ButtonIndex = i
			return
		}
	}
	m.ButtonFocus = false
	m.ButtonIndex = 0
}
