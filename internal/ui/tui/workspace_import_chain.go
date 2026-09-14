package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
)

// One approval covers preparation, creation and start (ADR 0057). The approval
// lives only in this session. A later plan is applied automatically only when
// it is the expected step for the approved images or VM and asks for nothing
// that was not shown and approved; anything else stops at its own review.
const startVMAck = "start-vm"

type chainOffer struct {
	Settings string
	Extras   []string
	Start    bool
	Summary  []string
}

type importChain struct {
	Connection, Settings, Sent string
	Approved                   []string
	Start                      bool
	PrepJob, CreateJob, VMID   string
}

func canonicalInput(v map[string]any) string {
	b, err := operations.Canonical(v)
	if err != nil {
		return ""
	}
	return string(b)
}
func preparationOperation(op string) bool {
	return op == "import.prepare" || op == "import.prepare-disks" || op == "import.prepare-install"
}
func creationOperation(op string) bool { return op == "vm.create" || op == "vm.create.devices-v1" }

// expectedCreationAcks mirrors the consequences creation asks for with these
// settings. A difference only stops the chain at creation's own review.
func expectedCreationAcks(f CreationForm) []string {
	acks := []string{"host-mutation", "copy-managed-volumes", "new-vm-identity"}
	if f.Spec.GuestAgent {
		acks = append(acks, "guest-agent-channel")
	}
	if f.Spec.Firmware.Mode == "uefi" {
		acks = append(acks, "new-firmware-state")
	}
	if len(f.Spec.NICs) > 0 {
		acks = append(acks, "network-attachment")
	}
	if len(f.Spec.Media) > 0 {
		acks = append(acks, "attach-readonly-media")
	}
	acks = append(acks, "creation-device-policy")
	if f.Spec.DevicePolicy != nil && f.Spec.DevicePolicy.WatchdogAction == "reset" {
		acks = append(acks, "watchdog-reset")
	}
	return acks
}

// chainOfferFor adds the later steps to the review of a preparation plan with
// VM settings, or of a creation plan that starts the VM afterwards.
func (m Workspace) chainOfferFor(p domain.Plan) *chainOffer {
	if m.Chain != nil {
		return nil
	}
	var f CreationForm
	switch {
	case preparationOperation(p.Operation) && m.Import != nil && m.Import.VM != nil && m.Import.VMBinding == creationDraftBinding(m.Import.Draft):
		f = *m.Import.VM
	case creationOperation(p.Operation) && m.Creation != nil && m.Creation.StartAfter:
		f = *m.Creation
	default:
		return nil
	}
	r, err := f.Request(m.Connection)
	if err != nil {
		return nil
	}
	prepare := preparationOperation(p.Operation)
	o := &chainOffer{Settings: canonicalInput(r.Input), Start: f.StartAfter}
	if prepare {
		for _, ack := range expectedCreationAcks(f) {
			if !slices.Contains(p.Acknowledgements, ack) {
				o.Extras = append(o.Extras, ack)
			}
		}
	}
	if f.StartAfter {
		o.Extras = append(o.Extras, startVMAck)
	}
	o.Summary = chainSummary(f, prepare)
	return o
}

func chainSummary(f CreationForm, create bool) []string {
	lines := []string{"After approval, Virmill also:"}
	if create {
		pool := f.Spec.PoolID
		for _, p := range f.Pools {
			if p.Key.UUID == f.Spec.PoolID {
				pool = p.Name
			}
		}
		firmware := "no"
		if i := f.firmwareIndex(); i >= 0 {
			firmware = f.Options.Firmware[i].Label
		}
		networks := []string{}
		for _, nic := range f.Spec.NICs {
			name := nic.NetworkID
			for _, n := range f.Networks {
				if n.Key.UUID == nic.NetworkID && n.Name != "" {
					name = n.Name
				}
			}
			cable := "connected"
			if nic.Link != "up" {
				cable = "disconnected"
			}
			networks = append(networks, name+" ("+cable+")")
		}
		network := "no network"
		if len(networks) > 0 {
			network = "network " + strings.Join(networks, ", ")
		}
		lines = append(lines, fmt.Sprintf("- creates VM %s: %s CPU, %s MiB memory, pool %s, %s firmware, %s", f.Spec.Name, f.CPUText, f.MemoryText, pool, firmware, network))
	}
	if f.StartAfter {
		lines = append(lines, "- starts the VM once it is created")
	}
	return append(lines, "Each later step runs only if it asks for nothing beyond the items you check; otherwise it stops at its own review.")
}

func (m Workspace) chainOfferLines(width int) []string {
	if m.ChainOffer == nil {
		return nil
	}
	lines := []string{}
	for _, line := range m.ChainOffer.Summary {
		lines = append(lines, wrap(validation.SafeText(line), max(1, width))...)
	}
	return append(lines, "")
}

// reviewAcks is the plan's own acknowledgements plus the later steps' items.
func (m Workspace) reviewAcks() []string {
	if m.Plan == nil {
		return nil
	}
	out := slices.Clone(m.Plan.Acknowledgements)
	if m.ChainOffer != nil {
		out = append(out, m.ChainOffer.Extras...)
	}
	return out
}

// startChain binds an accepted job to the approval that was shown for it.
func (m *Workspace) startChain(data any) {
	offer := m.ChainOffer
	m.ChainOffer = nil
	if m.Plan == nil {
		return
	}
	var job domain.Job
	raw, err := json.Marshal(data)
	if err != nil || json.Unmarshal(raw, &job) != nil || !guidedUUID.MatchString(job.ID) || job.PlanID != m.Plan.ID {
		return
	}
	if offer != nil {
		approved := slices.Clone(m.Plan.Acknowledgements)
		for _, ack := range offer.Extras {
			if ack != startVMAck {
				approved = append(approved, ack)
			}
		}
		c := &importChain{Connection: m.Plan.ConnectionID, Settings: offer.Settings, Approved: approved, Start: offer.Start}
		if preparationOperation(m.Plan.Operation) {
			c.PrepJob = job.ID
		} else {
			c.CreateJob = job.ID
		}
		m.Chain = c
		return
	}
	if c := m.Chain; c != nil && creationOperation(m.Plan.Operation) && c.CreateJob == "" {
		c.CreateJob = job.ID
	} else if c != nil && m.Plan.Operation == "vm.start" {
		m.Chain = nil
	}
}

// chainPlanArrived applies the next approved step, or stops at its review.
func (m *Workspace) chainPlanArrived() (tea.Cmd, bool) {
	c, p := m.Chain, m.Plan
	if c == nil || p == nil {
		return nil, false
	}
	next := creationOperation(p.Operation) && c.PrepJob != "" && c.CreateJob == "" || p.Operation == "vm.start" && c.VMID != ""
	if !next {
		return nil, false
	}
	stop := func(reason string) (tea.Cmd, bool) {
		m.Chain = nil
		m.Notice = "This step needs your review: " + reason + "."
		return nil, false
	}
	if p.ConnectionID != c.Connection {
		return stop("the connection changed")
	}
	for _, ack := range p.Acknowledgements {
		if !slices.Contains(c.Approved, ack) {
			return stop("it asks to " + strings.ToLower(strings.TrimSuffix(acknowledgementLabel(ack), " ["+ack+"]")) + ", which was not approved")
		}
	}
	if creationOperation(p.Operation) {
		if fmt.Sprint(p.Review["sourceOperationID"]) != c.PrepJob {
			return stop("it uses different prepared images")
		}
		if c.Sent == "" || c.Sent != c.Settings {
			return stop("the VM settings changed")
		}
	} else if !slices.Equal(p.ResourceIDs, []string{(domain.ResourceKey{ProviderID: "libvirt", ConnectionID: c.Connection, Kind: "vm", UUID: c.VMID}).String()}) {
		return stop("it is for a different VM")
	}
	return m.autoApplyChain(), true
}

func (m *Workspace) autoApplyChain() tea.Cmd {
	p := m.Plan
	m.Approved = make([]bool, len(p.Acknowledgements))
	for i := range m.Approved {
		m.Approved[i] = true
	}
	m.Reviewing = true
	m.Busy = true
	m.Error = ""
	m.ApplyKey = domain.ID()
	m.resetJobOutcome()
	m.Notice = "Continuing the approved setup…"
	apply := m.request("apply", "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: m.ApplyKey, Acknowledgements: append([]string{}, p.Acknowledgements...)}})
	return m.guardSetupApply(apply)
}

// chainAfterCreation starts the verified new VM when that was approved.
func (m *Workspace) chainAfterCreation() tea.Cmd {
	c, o := m.Chain, m.currentJobOutcome()
	if c == nil || c.CreateJob == "" || o == nil || o.JobID != c.CreateJob || o.Loading {
		return nil
	}
	if o.Error != "" || o.VMID == "" {
		m.Chain = nil
		return nil
	}
	if !c.Start {
		m.Chain = nil
		return nil
	}
	c.VMID = o.VMID
	cmd := m.openJobVM()
	m.JobStartVM = cmd != nil
	if cmd == nil {
		m.Chain = nil
	}
	return cmd
}

// previewPreparationWith keeps VM settings f with the import and requests the
// preparation review, which then also covers creation and start.
func (m *Workspace) previewPreparationWith(f CreationForm) tea.Cmd {
	if m.Import == nil {
		if m.Creation != nil {
			m.Creation.Error = "Return to import and choose the source again."
		}
		return nil
	}
	fail := func(err error) tea.Cmd {
		if m.Creation != nil {
			m.Creation.Error = err.Error()
		} else {
			m.Import.Error = err.Error()
		}
		return nil
	}
	if err := m.ensureImportStaging(); err != nil {
		return fail(err)
	}
	method, prep, err := m.Import.Draft.Request(m.Connection)
	if err != nil {
		return fail(err)
	}
	m.Import.VM = &f
	m.Import.VMBinding = creationDraftBinding(m.Import.Draft)
	m.Creation = nil
	m.CreationPicking = false
	m.Busy = true
	return m.request("plan", method, prep)
}
