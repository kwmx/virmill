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
	Settings       string
	Extras         []string
	Start, Cleanup bool
	Summary        []string
}

type importChain struct {
	Connection, Settings, Sent string
	Approved                   []string
	Start, Cleanup, Discarding bool
	PrepJob, CreateJob, VMID   string
	StartJob, Source           string // Source: the preparation to remove
}

func canonicalInput(v map[string]any) string {
	b, err := operations.Canonical(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// chainSettings compares settings without the cloud source digest, which only
// exists after preparation; the chain binds that preparation and the service
// checks the digest against it (ADR 0059).
func chainSettings(input map[string]any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	var copy map[string]any
	if json.Unmarshal(raw, &copy) != nil {
		return ""
	}
	if p, ok := copy["provisioning"].(map[string]any); ok {
		p["sourceSHA256"] = ""
	}
	return canonicalInput(copy)
}
func preparationOperation(op string) bool {
	return op == "import.prepare" || op == "import.prepare-disks" || op == "import.prepare-install"
}
func creationOperation(op string) bool { return op == "vm.create" || op == "vm.create.devices-v1" }

// expectedCreationAcks mirrors the consequences creation asks for with these
// settings. A difference only stops the chain at creation's own review.
func expectedCreationAcks(f CreationForm) []string {
	acks := []string{"host-mutation", "copy-managed-volumes", "new-vm-identity"}
	if f.RemovePrepared {
		acks = append(acks, "hand-over-prepared-copy")
	}
	if f.Spec.GuestAgent {
		acks = append(acks, "guest-agent-channel")
	}
	if f.Spec.Firmware.Mode == "uefi" {
		acks = append(acks, "new-firmware-state")
	}
	if len(f.Spec.NICs) > 0 {
		acks = append(acks, "network-attachment")
	}
	if len(f.Spec.Media) > 0 || f.CloudEnabled {
		acks = append(acks, "attach-readonly-media")
	}
	if f.CloudEnabled {
		acks = append(acks, "guest-root-provisioning", "rotate-guest-host-keys")
		if f.Cloud.Sudo {
			acks = append(acks, "guest-passwordless-sudo")
		}
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
	case creationOperation(p.Operation) && m.Creation != nil && (m.Creation.StartAfter || m.Creation.RemovePrepared):
		f = *m.Creation
	default:
		return nil
	}
	r, err := f.Request(m.Connection)
	if err != nil {
		return nil
	}
	prepare := preparationOperation(p.Operation)
	o := &chainOffer{Settings: chainSettings(r.Input), Start: f.StartAfter}
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
	if f.RemovePrepared {
		o.Extras = append(o.Extras, "delete-prepared-copy")
		o.Cleanup = true
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
	if f.CloudEnabled {
		lines = append(lines, "- sets up user "+f.Cloud.User+" with your SSH key through cloud-init at first boot")
	}
	if f.StartAfter {
		lines = append(lines, "- starts the VM once it is created")
	}
	if f.RemovePrepared {
		lines = append(lines, "- then removes the prepared copy to free disk space; the VM keeps its own disks")
	}
	return append(lines, "If a later step asks for anything not listed here, Virmill stops and asks you first.")
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
		// Preparation plans are host-local; later steps use this session's
		// libvirt connection, which chosen settings were validated against.
		c := &importChain{Connection: m.Connection, Settings: offer.Settings, Approved: approved, Start: offer.Start, Cleanup: offer.Cleanup}
		if preparationOperation(m.Plan.Operation) {
			c.PrepJob, c.Source = job.ID, job.ID
		} else {
			c.CreateJob = job.ID
			if m.Creation != nil {
				c.Source = m.Creation.OperationID
			}
		}
		m.Chain = c
		return
	}
	switch c := m.Chain; {
	case c != nil && creationOperation(m.Plan.Operation) && c.CreateJob == "":
		c.CreateJob = job.ID
	case c != nil && m.Plan.Operation == "vm.start":
		c.StartJob = job.ID
		if !c.Cleanup {
			m.Chain = nil
		}
	case c != nil && m.Plan.Operation == "import.discard":
		m.Chain = nil
	}
}

// chainPlanArrived applies the next approved step, or stops at its review.
func (m *Workspace) chainPlanArrived() (tea.Cmd, bool) {
	c, p := m.Chain, m.Plan
	if c == nil || p == nil {
		return nil, false
	}
	discard := p.Operation == "import.discard" && c.Discarding
	next := creationOperation(p.Operation) && c.PrepJob != "" && c.CreateJob == "" || p.Operation == "vm.start" && c.VMID != "" || discard
	if !next {
		return nil, false
	}
	stop := func(reason string) (tea.Cmd, bool) {
		m.Chain = nil
		m.Notice = "This step needs your review: " + reason + "."
		return nil, false
	}
	// Removing a prepared copy is host-local, like preparation.
	if !discard && p.ConnectionID != c.Connection {
		return stop("the connection changed")
	}
	for _, ack := range p.Acknowledgements {
		if !slices.Contains(c.Approved, ack) {
			return stop("it asks to " + strings.ToLower(strings.TrimSuffix(acknowledgementLabel(ack), " ["+ack+"]")) + ", which was not approved")
		}
	}
	if discard {
		if fmt.Sprint(p.Review["sourceOperationID"]) != c.Source || p.Review["sourceFilesChanged"] != false {
			return stop("it removes a different preparation")
		}
	} else if creationOperation(p.Operation) {
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
	// Stay on the confirmation with a notice; the technical plan would suggest
	// another decision is waiting.
	m.PlanDetails = false
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
		if c.Cleanup {
			return m.requestChainDiscard()
		}
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

// chainAfterStart removes the prepared copy once the VM has started.
func (m *Workspace) chainAfterStart() tea.Cmd {
	c, o := m.Chain, m.currentJobOutcome()
	if c == nil || c.StartJob == "" || c.Discarding || o == nil || o.JobID != c.StartJob || o.Loading {
		return nil
	}
	if o.Error != "" {
		m.Chain = nil
		return nil
	}
	display := m.openDisplayAfterStart(c.VMID)
	if !c.Cleanup {
		m.Chain = nil
		return display
	}
	return tea.Batch(display, m.requestChainDiscard())
}

// requestChainDiscard asks for the removal plan of the approved preparation.
func (m *Workspace) requestChainDiscard() tea.Cmd {
	c := m.Chain
	if c == nil || !guidedUUID.MatchString(c.Source) {
		m.Chain = nil
		return nil
	}
	c.Discarding = true
	m.Busy = true
	m.Notice = "Removing the prepared copy…"
	return m.request("plan", "import.discard", app.Request{ID: c.Source, Action: "discard", Input: map[string]any{}})
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
	m.Import.VM = &f
	m.Import.VMBinding = creationDraftBinding(m.Import.Draft)
	m.Creation = nil
	m.CreationPicking = false
	// An import problem is fixed on the import page; the VM settings are kept.
	fail := func(err error) tea.Cmd {
		m.Import.Error = err.Error() + " Your VM settings are kept."
		m.Notice = ""
		return nil
	}
	if err := m.ensureImportStaging(); err != nil {
		return fail(err)
	}
	method, prep, err := m.Import.Draft.Request(m.Connection)
	if err != nil {
		return fail(err)
	}
	m.Import.Error = ""
	m.Busy = true
	m.Notice = "Preparing the import review. Large sources take a while to check…"
	return m.request("plan", method, prep)
}
