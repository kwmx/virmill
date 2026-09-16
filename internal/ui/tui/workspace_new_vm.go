package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
)

// newVMFlow is New VM (ADR 0065): choose a file, one settings page, one
// confirmation, then progress on the same screen until the VM runs. It drives
// the existing import form, VM settings and one-approval chain; it adds no
// decision of its own. The workspace keeps working underneath, so hiding this
// view never stops what was confirmed.
type newVMFlow struct {
	Focus     int
	Discard   bool // Esc was pressed once on the settings page
	Loading   bool // VM settings are being read for the chosen file
	LoadError bool // reading them failed; Esc leaves, and nothing retries by itself
	Error     string
	RealPools []domain.StoragePool
	DiskNote  string // why the installer disk is smaller than usual

	// A storage pool created or started in the same confirmation.
	PoolPending bool
	PoolPlan    *domain.Plan
	PoolName    string
	PoolVerb    string

	Running   bool
	Name      string
	Chain     importChain
	Started   bool
	Failure   string
	Display   string
	EndFocus  int
	PoolSetUp bool
}

const newVMGiB = 1024

// newVMFreeBytes is replaced in tests.
var newVMFreeBytes = freeBytes

// installerDiskMiB is the usual 32 GiB installer disk, or the largest common
// size whose preparation fits the free space. Preparation reserves the disk's
// full size plus a margin (importing.installationBudget), so a size that does
// not fit would only be refused after Create VM.
func installerDiskMiB(free uint64, isoBytes int64) (string, string) {
	for _, gib := range []uint64{32, 24, 16, 12, 8, 4} {
		disk := gib << 30
		need := uint64(64<<20) + uint64(max(0, isoBytes)) + disk + disk/4 + (16 << 20) + (512 << 20)
		if need <= free {
			if gib == 32 {
				return "32768", ""
			}
			return strconv.FormatUint(gib*newVMGiB, 10), fmt.Sprintf("Suggested: %d GiB, so preparing the installer fits the free space here.", gib)
		}
	}
	return "32768", ""
}

func (m *Workspace) openNewVM() tea.Cmd {
	if m.Busy || m.Pending["apply"] != 0 {
		m.Error = "Wait for the current request to finish, then choose New VM."
		return nil
	}
	if m.NewVM != nil && m.NewVM.Running {
		m.Error = "A new VM is still being set up. Wait for it to finish."
		return nil
	}
	// New VM always starts fresh; saved setups stay available from More.
	m.draftBypass = true
	m.SavedImport = nil
	m.Advanced = false
	m.Error, m.Notice = "", ""
	m.NewVM = &newVMFlow{}
	return m.openImport("auto")
}

// newVMReady reports a described source whose VM settings can be read.
func newVMReady(f *ImportForm) bool {
	if f == nil || f.Error != "" {
		return false
	}
	d := f.Draft
	if d.Kind == "ova" {
		return d.Report != nil && d.Report.Source == d.Source && d.SystemID != ""
	}
	return d.HasSourceDescription()
}

// newVMSettingsActive is the settings page, shown once VM settings are read.
func (m Workspace) newVMSettingsActive() bool {
	return m.NewVM != nil && !m.NewVM.Running && m.Import != nil && m.Import.VM != nil && m.Plan == nil && m.Creation == nil && !m.CreationPicking && m.Picker == nil && m.ExportForm == nil
}

// newVMProgressActive covers every automatic step. A step that needs a
// decision the confirmation did not cover shows its own confirmation instead.
func (m Workspace) newVMProgressActive() bool {
	return m.NewVM != nil && m.NewVM.Running && !(m.Plan != nil && m.Chain == nil && !m.Busy) && m.Picker == nil
}

type newVMControl struct{ id, label, value, help string }

func (m Workspace) newVMControls() []newVMControl {
	f, d := m.Import.VM, m.Import.Draft
	c := []newVMControl{
		{"name", "Name", f.Spec.Name, "The VM's name on this host."},
		{"cpu", "CPU cores", f.CPUText, "How many processor cores the VM can use."},
		{"memory", "Memory (MiB)", f.MemoryText, "1024 MiB is 1 GiB."},
	}
	if d.Kind == "iso" && len(d.Disks) > 0 {
		help := "The new, empty disk the installer uses. Space is used only as the guest writes."
		if m.NewVM.DiskNote != "" {
			help = m.NewVM.DiskNote
		}
		c = append(c, newVMControl{"disk", "Disk size (GiB)", newVMDiskGiB(d.Disks[0].SizeMiB), help})
	}
	start := " "
	if f.StartAfter {
		start = "x"
	}
	c = append(c,
		newVMControl{"start", "[" + start + "] Start it and open its display", "", "Space turns this on or off."},
		newVMControl{"advanced", "[ Advanced settings ]", "", "Firmware, storage, network, disks, cloud-init and other hardware."},
		newVMControl{"create", "[ Create VM ]", "", "Shows everything that will happen, once, before anything changes."},
	)
	return c
}

func newVMDiskGiB(mib string) string {
	n, err := strconv.ParseUint(mib, 10, 64)
	if err != nil || n == 0 {
		return mib
	}
	if n%newVMGiB == 0 {
		return strconv.FormatUint(n/newVMGiB, 10)
	}
	return strconv.FormatFloat(float64(n)/newVMGiB, 'f', 1, 64)
}

func (m Workspace) newVMSourceLine() string {
	d := m.Import.Draft
	kind := map[string]string{"iso": "installer", "disks": "disk image", "ova": "appliance"}[d.Kind]
	name := filepath.Base(d.SelectedSource)
	if name == "." || name == "" {
		name = filepath.Base(d.Source)
	}
	return "From " + kind + ": " + name
}

// newVMStorageLine says where the disks go, including a pool this
// confirmation would set up.
func (m Workspace) newVMStorageLine() string {
	f := m.Import.VM
	for _, p := range f.Pools {
		if p.Key.UUID == f.Spec.PoolID && creationUsablePool(p) {
			return "Storage: pool " + p.Name
		}
	}
	if pool, ok := f.startablePool(); ok {
		return "Storage: pool " + pool.Name + " is stopped; Virmill starts it first"
	}
	if creationAnyUsable(f.Pools) {
		return "Storage: choose a pool in Advanced settings"
	}
	return "Storage: Virmill sets up libvirt's standard pool first"
}

func (m Workspace) newVMNetworkLine() string {
	f := m.Import.VM
	if len(f.Spec.NICs) == 0 {
		return "Network: none"
	}
	nic := f.Spec.NICs[0]
	name := "a network"
	for _, n := range f.Networks {
		if n.Key.UUID == nic.NetworkID && n.Name != "" {
			name = n.Name
		}
	}
	if nic.NetworkID == f.SuggestedNetworkID && name == "default" {
		name = "libvirt's default NAT network"
	}
	if nic.Link != "up" {
		return "Network: " + name + ", cable disconnected until you connect it"
	}
	if name == "libvirt's default NAT network" {
		return "Network: " + name + " (internet access)"
	}
	return "Network: " + name
}

func (m Workspace) newVMView(width, height int) []string {
	flow := m.NewVM
	lines := []string{"New VM", m.newVMSourceLine(), ""}
	controls := m.newVMControls()
	focus := max(0, min(flow.Focus, len(controls)-1))
	for i, c := range controls {
		mark := "  "
		if i == focus {
			mark = "> "
		}
		row := c.label
		if c.value != "" || !strings.HasPrefix(c.label, "[") {
			value := validation.SafeText(c.value)
			if i == focus {
				value += "|"
			}
			row = c.label + ": [" + value + "]"
		}
		if c.id == "advanced" {
			lines = append(lines, "", "  "+m.newVMStorageLine(), "  "+m.newVMNetworkLine(), "  Display: "+creationFriendlyValue("graphics", m.Import.VM.Spec.Graphics), "")
		}
		lines = append(lines, mark+row)
		if c.id == "disk" && flow.DiskNote != "" && i != focus {
			lines = append(lines, "    "+flow.DiskNote)
		}
	}
	lines = append(lines, "")
	switch {
	case flow.Error != "":
		lines = append(lines, wrap(validation.SafeText(flow.Error), width)...)
	case m.Import.Error != "":
		lines = append(lines, wrap(newVMRefusal(m.Import.Error, m.Import.Draft.Kind), width)...)
	case flow.Discard:
		lines = append(lines, "Press Esc again to discard this new VM. Any other key keeps your settings.")
	default:
		lines = append(lines, wrap(controls[focus].help, width)...)
	}
	return pageLines(lines, width, height, 0)
}

// newVMRefusal words a refused preparation for the settings page.
func newVMRefusal(text, kind string) string {
	text = validation.SafeText(text)
	if rest, ok := strings.CutPrefix(text, "INSUFFICIENT_SPACE:"); ok {
		text = "Not enough free space. " + strings.TrimSpace(rest)
		if kind == "iso" {
			text += " A smaller Disk size needs less."
		}
	}
	return text
}

func (m Workspace) updateNewVMSettings(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	flow := *m.NewVM
	m.NewVM = &flow
	if m.Busy {
		if key.Type == tea.KeyEsc {
			m.Pending = maps.Clone(m.Pending)
			delete(m.Pending, "plan")
			m.Busy, m.Notice = false, ""
			flow.PoolPending = false
		}
		return m, nil
	}
	if key.Type == tea.KeyEsc {
		if !flow.Discard {
			flow.Discard = true
			return m, nil
		}
		m.NewVM, m.Import = nil, nil
		m.Notice = "The new VM was discarded. Nothing was changed."
		return m, nil
	}
	flow.Discard = false
	controls := m.newVMControls()
	flow.Focus = max(0, min(flow.Focus, len(controls)-1))
	c := controls[flow.Focus]
	vm := *m.Import.VM
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		flow.Focus = (flow.Focus + 1) % len(controls)
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		flow.Focus = (flow.Focus + len(controls) - 1) % len(controls)
		return m, nil
	case tea.KeyEnter, tea.KeySpace:
		switch c.id {
		case "start":
			vm.StartAfter = !vm.StartAfter
			m.Import.VM = &vm
			return m, nil
		case "advanced":
			if key.Type != tea.KeyEnter {
				return m, nil
			}
			vm.Page, vm.Focus = 0, 0
			m.Creation = &vm
			flow.Error = ""
			return m, nil
		case "create":
			if key.Type != tea.KeyEnter {
				return m, nil
			}
			return m, m.createNewVM()
		}
		if key.Type == tea.KeyEnter {
			flow.Focus = (flow.Focus + 1) % len(controls)
			return m, nil
		}
	}
	if key.Type != tea.KeyRunes && key.Type != tea.KeySpace && key.Type != tea.KeyBackspace && key.Type != tea.KeyCtrlU {
		return m, nil
	}
	value := c.value
	switch key.Type {
	case tea.KeyBackspace:
		if r := []rune(value); len(r) > 0 {
			value = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		value = ""
	default:
		text := string(key.Runes)
		if key.Type == tea.KeySpace {
			text = " "
		}
		if key.Alt || !guidedPrintable(text) || utf8.RuneCountInString(value+text) > 255 {
			return m, nil
		}
		value += text
	}
	switch c.id {
	case "name":
		vm.Spec.Name, vm.NameOrigin = value, ""
	case "cpu":
		vm.CPUText = value
	case "memory":
		vm.MemoryText = value
	case "disk":
		draft := *m.Import
		draft.Draft.Disks = append([]ImportDisk{}, draft.Draft.Disks...)
		if gib, err := strconv.ParseFloat(value, 64); err == nil && gib > 0 && gib <= 512*1024 {
			draft.Draft.Disks[0].SizeMiB = strconv.FormatUint(uint64(gib*newVMGiB+0.5), 10)
		} else {
			draft.Draft.Disks[0].SizeMiB = value
		}
		m.Import = &draft
		flow.DiskNote = ""
	default:
		return m, nil
	}
	m.Import.VM = &vm
	flow.Error = ""
	return m, nil
}

// createNewVM checks the settings and asks for the one confirmation. With no
// usable storage pool, the pool's plan is requested first and shown in the
// same confirmation.
func (m *Workspace) createNewVM() tea.Cmd {
	flow := m.NewVM
	imp := *m.Import
	imp.Error = ""
	imp.Draft.Disks = append([]ImportDisk{}, imp.Draft.Disks...)
	f := *m.Import.VM
	f.Pools = append([]domain.StoragePool{}, flow.RealPools...)
	if !creationPoolUsable(f.Pools, f.Spec.PoolID) {
		f.Spec.PoolID = defaultCreationPool(f.Pools)
	}
	if imp.Draft.Kind == "iso" && len(imp.Draft.Disks) > 0 {
		if _, ok := guidedNumber(imp.Draft.Disks[0].SizeMiB, 512<<20); !ok {
			flow.Error = "Disk size: enter a size in GiB, such as 32."
			return nil
		}
	}
	imp.Draft.VMName, imp.Draft.VCPUs, imp.Draft.MemoryMiB = f.Spec.Name, f.CPUText, f.MemoryText
	// The confirmation states that the source files are not open elsewhere.
	imp.Draft.Offline = true
	if imp.Draft.DestinationParent == "" && imp.StagingRoot != "" {
		imp.Draft.DestinationParent, imp.Draft.DestinationName = imp.StagingRoot, defaultImportFolder(imp.Draft)
	}
	m.Import = &imp
	if _, _, err := imp.Draft.Request(m.Connection); err != nil {
		flow.Error = err.Error()
		return nil
	}
	m.Import.VM = &f
	m.Import.VMBinding = creationDraftBinding(imp.Draft)
	if _, err := f.Request(m.Connection); err != nil {
		if strings.HasPrefix(err.Error(), "Storage pool:") && !creationAnyUsable(f.Pools) {
			flow.PoolPending, flow.Error = true, ""
			m.Busy, m.Error = true, ""
			m.Notice = "Checking storage…"
			if pool, ok := f.startablePool(); ok {
				return m.request("plan", "storage.pool.start", app.Request{ID: pool.Key.UUID, Action: "start", Input: map[string]any{}})
			}
			return m.request("plan", "storage.pool.create", app.Request{Action: "create", Input: map[string]any{}})
		}
		flow.Error = err.Error() + " Open Advanced settings to change it."
		return nil
	}
	flow.Error, flow.PoolPlan = "", nil
	return m.previewPreparationWith(f)
}

func creationPoolUsable(pools []domain.StoragePool, id string) bool {
	for _, p := range pools {
		if p.Key.UUID == id && creationUsablePool(p) {
			return true
		}
	}
	return false
}

func creationAnyUsable(pools []domain.StoragePool) bool {
	for _, p := range pools {
		if creationUsablePool(p) {
			return true
		}
	}
	return false
}

// newVMPoolPlan keeps a pool plan for the confirmation and requests the
// preparation plan with the planned pool selected.
func (m *Workspace) newVMPoolPlan(p domain.Plan) tea.Cmd {
	flow := m.NewVM
	flow.PoolPending = false
	pool, ok := planPoolDefinition(&p)
	if !ok || p.ConnectionID != m.Connection || m.Import == nil || m.Import.VM == nil {
		m.Busy, m.Notice = false, ""
		flow.Error = "Virmill could not plan the storage pool. Open Storage to create one, then choose Create VM again."
		return nil
	}
	f := *m.Import.VM
	f.Pools = append([]domain.StoragePool{}, flow.RealPools...)
	planned := domain.StoragePool{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "storage-pool", UUID: pool.UUID}, Name: pool.Name, Type: "dir", Active: true, Persistent: true}
	replaced := false
	for i := range f.Pools {
		if f.Pools[i].Key.UUID == pool.UUID {
			f.Pools[i].Active, replaced = true, true
		}
	}
	if !replaced {
		f.Pools = append(f.Pools, planned)
	}
	f.Spec.PoolID = pool.UUID
	flow.PoolPlan, flow.PoolName = &p, pool.Name
	flow.PoolVerb = "creates storage pool " + pool.Name + " in " + pool.Path
	if p.Operation == "storage.pool.start" {
		flow.PoolVerb = "starts storage pool " + pool.Name
	}
	return m.previewPreparationWith(f)
}

// newVMPoolLines put the pool step into the confirmation it belongs to.
func (m Workspace) newVMPoolLines(width int) ([]string, []string) {
	flow := m.NewVM
	if flow == nil || flow.PoolPlan == nil || m.Plan == nil || !preparationOperation(m.Plan.Operation) {
		return nil, nil
	}
	return wrap("First, Virmill "+validation.SafeText(flow.PoolVerb)+" for the VM's disks.", max(1, width)), flow.PoolPlan.Acknowledgements
}

// applyNewVMPool applies the pool first; the preparation follows when the
// pool's job is accepted.
func (m *Workspace) applyNewVMPool() tea.Cmd {
	p := m.NewVM.PoolPlan
	m.Busy, m.Error = true, ""
	m.Notice = "Setting up storage pool " + m.NewVM.PoolName + "…"
	return m.request("pool-apply", "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: append([]string{}, p.Acknowledgements...)}})
}

func (m *Workspace) newVMPoolAccepted(data any) tea.Cmd {
	flow := m.NewVM
	var job domain.Job
	raw, err := json.Marshal(data)
	if flow == nil || flow.PoolPlan == nil || m.Plan == nil || err != nil || json.Unmarshal(raw, &job) != nil || job.PlanID != flow.PoolPlan.ID {
		m.Busy = false
		m.Error = "The storage pool submission could not be matched. Check Jobs before trying again."
		return nil
	}
	flow.PoolPlan, flow.PoolSetUp = nil, true
	m.Notice = "Preparing the images…"
	if m.ApplyKey == "" {
		m.ApplyKey = domain.ID()
	}
	m.resetJobOutcome()
	apply := m.request("apply", "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: m.Plan.ID, PlanDigest: m.Plan.Digest, IdempotencyKey: m.ApplyKey, Acknowledgements: append([]string{}, m.Plan.Acknowledgements...)}})
	return m.guardSetupApply(apply)
}

// interceptNewVM handles what only New VM decides: its pages, the pool in the
// confirmation, and its progress. Everything else goes to the workspace.
func (m Workspace) interceptNewVM(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.NewVM == nil {
		return m, nil, false
	}
	switch v := msg.(type) {
	case workspaceReply:
		if v.Kind == "pool-apply" && m.Pending["pool-apply"] == v.Token {
			m.Pending = maps.Clone(m.Pending)
			delete(m.Pending, "pool-apply")
			err := v.Err
			if err == nil && v.Response.Error != nil {
				err = v.Response.Error
			}
			if err != nil {
				m.Busy, m.Notice = false, ""
				m.Error = "The storage pool was not set up: " + validation.SafeText(err.Error()) + ". Nothing else was changed."
				return m, nil, true
			}
			return m, m.newVMPoolAccepted(v.Response.Data), true
		}
		if v.Kind == "plan" && m.NewVM.PoolPending && m.Pending["plan"] == v.Token && v.Err == nil && v.Response.Error == nil {
			var p domain.Plan
			b, _ := json.Marshal(v.Response.Data)
			decoder := json.NewDecoder(bytes.NewReader(b))
			decoder.UseNumber()
			if decoder.Decode(&p) == nil && poolPlanOperation(p.Operation) {
				m.Pending = maps.Clone(m.Pending)
				delete(m.Pending, "plan")
				if digest, err := operations.PlanDigest(p); err != nil || digest != p.Digest {
					m.NewVM.PoolPending, m.Busy, m.Notice = false, false, ""
					m.NewVM.Error = "The storage pool plan could not be verified. Nothing was changed."
					return m, nil, true
				}
				flow := *m.NewVM
				m.NewVM = &flow
				return m, m.newVMPoolPlan(p), true
			}
		}
	case tea.KeyMsg:
		if v.Type == tea.KeyCtrlC || m.Width < 60 || m.Height < 18 || m.Help {
			return m, nil, false
		}
		if m.newVMProgressActive() {
			next, cmd := m.updateNewVMProgress(v)
			return next, cmd, true
		}
		if m.NewVM.PoolPlan != nil && m.Plan != nil && !m.PlanDetails && v.Type == tea.KeyEnter && !m.Busy && m.Pending["apply"] == 0 && m.Pending["pool-apply"] == 0 {
			flow := *m.NewVM
			m.NewVM = &flow
			return m, m.applyNewVMPool(), true
		}
		if m.Plan != nil && v.Type == tea.KeyEsc && !m.PlanDetails {
			// Back to the settings page; a new confirmation plans the pool again.
			flow := *m.NewVM
			flow.PoolPlan = nil
			m.NewVM = &flow
			return m, nil, false
		}
		if m.Creation != nil && m.Plan == nil && m.Picker == nil && !m.Busy && m.Import != nil && m.Import.VM != nil && !m.NewVM.Running && v.Type == tea.KeyEsc {
			// Advanced settings keep every change and return to New VM.
			m.saveImportHardware(*m.Creation)
			return m, nil, true
		}
		if m.newVMSettingsActive() {
			next, cmd := m.updateNewVMSettings(v)
			return next, cmd, true
		}
		if m.Import != nil && m.Import.VM == nil && m.Plan == nil && m.Picker == nil && !m.Busy && !m.NewVM.Loading && v.Type == tea.KeyEsc {
			m.NewVM, m.Import = nil, nil
			m.Notice = ""
			return m, nil, true
		}
	}
	return m, nil, false
}

// observeNewVM follows the workspace after each message: it reads VM settings
// once the file is described, and tracks the confirmed steps.
func (m Workspace) observeNewVM(before Workspace, msg tea.Msg) (Workspace, tea.Cmd) {
	if m.NewVM == nil {
		return m, nil
	}
	flow := *m.NewVM
	m.NewVM = &flow
	if !flow.Running {
		if reply, ok := msg.(workspaceReply); ok && reply.Kind == "apply" && before.Plan != nil && preparationOperation(before.Plan.Operation) && before.Pending["apply"] == reply.Token && m.Pending["apply"] == 0 {
			if m.Chain != nil && m.Chain.PrepJob != "" {
				flow.Running, flow.Chain = true, *m.Chain
				flow.Name = before.Import.VM.Spec.Name
				return m, nil
			}
			if reply.Err == nil && reply.Response.Error == nil {
				// No approved chain: the import runs as its own job.
				m.NewVM = nil
			}
			return m, nil
		}
		if m.Import == nil && m.Plan == nil && m.Pending["apply"] == 0 && m.Pending["pool-apply"] == 0 {
			m.NewVM = nil
			return m, nil
		}
		if flow.Loading && m.Pending["creation-load"] == 0 && !m.Busy {
			flow.Loading = false
			if m.Creation == nil {
				flow.LoadError = true
			}
			if m.Creation != nil && m.Import != nil {
				f := *m.Creation
				f.Page, f.Focus = 0, 0
				flow.RealPools = append([]domain.StoragePool{}, f.Pools...)
				m.saveImportHardware(f)
				if d := m.Import.Draft; d.Kind == "iso" && len(d.Disks) == 1 && d.Disks[0].SizeMiB == "32768" && m.Import.StagingRoot != "" {
					if free, ok := newVMFreeBytes(m.Import.StagingRoot); ok {
						iso := int64(0)
						if d.Description != nil {
							iso = d.Description.PhysicalBytes
						}
						imp := *m.Import
						imp.Draft.Disks = []ImportDisk{d.Disks[0]}
						imp.Draft.Disks[0].SizeMiB, flow.DiskNote = installerDiskMiB(free, iso)
						m.Import = &imp
					}
				}
				flow.Focus = len(m.newVMControls()) - 1
			}
			return m, nil
		}
		if m.Import != nil && m.Import.VM == nil && !flow.Loading && !flow.LoadError && !m.Busy && m.Plan == nil && m.Picker == nil && m.Creation == nil && newVMReady(m.Import) {
			flow.Loading = true
			m.Import.Page, m.Import.Focus = 3, 0
			return m, m.configureImportHardware()
		}
		if m.Creation == nil && before.Creation != nil && m.Import != nil && m.Import.VM != nil {
			flow.RealPools = append([]domain.StoragePool{}, m.Import.VM.Pools...)
		}
		return m, nil
	}
	if m.Chain != nil {
		flow.Chain = *m.Chain
	}
	if o := m.currentJobOutcome(); o != nil && !o.Loading {
		switch o.JobID {
		case flow.Chain.StartJob:
			if o.Error == "" && o.State == "succeeded" {
				flow.Started = true
			}
		}
	}
	state := field(m.Detail, "state")
	if m.Section == 8 && domain.Terminal(state) && state != "succeeded" {
		id := resourceID(m.Detail)
		if id != "" && (id == flow.Chain.PrepJob || id == flow.Chain.CreateJob || id == flow.Chain.StartJob) && flow.Failure == "" {
			flow.Failure = field(object(m.Detail)["error"], "message")
			if flow.Failure == "" {
				flow.Failure = "a step " + state + "."
			}
		}
	}
	switch v := msg.(type) {
	case displayOpened:
		if v.Err != nil {
			flow.Display = validation.SafeText(v.Err.Error())
		} else {
			flow.Display = "open"
		}
	}
	if flow.Started && flow.Display == "" && m.DisplayAutoVM == "" && m.Pending["console-launch"] == 0 && m.Pending["display-auto"] == 0 && strings.Contains(m.Notice, "is running.") {
		flow.Display = m.Notice
	}
	if m.Chain == nil && !flow.Started && flow.Failure == "" && before.Chain != nil {
		flow.Failure = strings.TrimSpace(m.Error + " " + m.Notice)
		if flow.Failure == "" {
			flow.Failure = "a step stopped before the VM started."
		}
	}
	// The steps above say what is happening; job and review notices meant
	// for other pages would contradict them.
	if flow.Failure == "" && m.newVMProgressActive() {
		m.Notice = ""
	}
	return m, nil
}

type newVMStep struct {
	label, state string // state: done, now, wait, skip
}

func (m Workspace) newVMSteps() []newVMStep {
	flow := m.NewVM
	c := flow.Chain
	steps := []newVMStep{}
	if flow.PoolSetUp {
		steps = append(steps, newVMStep{"Set up storage pool " + flow.PoolName, "done"})
	}
	state := func(done, now bool) string {
		switch {
		case done:
			return "done"
		case now:
			return "now"
		}
		return "wait"
	}
	created := c.VMID != "" || c.StartJob != "" || flow.Started
	steps = append(steps,
		newVMStep{"Copy and check the images", state(c.CreateJob != "" || created, true)},
		newVMStep{"Create the VM", state(created, c.CreateJob != "")},
	)
	if c.Start {
		steps = append(steps, newVMStep{"Start the VM", state(flow.Started, c.StartJob != "")})
		display := state(flow.Display == "open", flow.Started)
		if flow.Display != "" && flow.Display != "open" {
			display = "skip"
		}
		steps = append(steps, newVMStep{"Open its display", display})
	}
	if c.Cleanup {
		cleaned := (flow.Started || !c.Start) && m.Chain == nil
		steps = append(steps, newVMStep{"Remove the prepared copy", state(cleaned, c.Discarding)})
	}
	if flow.Failure != "" {
		for i := range steps {
			if steps[i].state == "now" {
				steps[i].state = "failed"
			}
		}
	}
	return steps
}

func (m Workspace) newVMFinished() bool {
	flow := m.NewVM
	return flow.Failure == "" && flow.Started && m.Chain == nil
}

func (m Workspace) newVMProgressView(width, height int) []string {
	flow := m.NewVM
	name := validation.SafeText(flow.Name)
	lines := []string{}
	switch {
	case flow.Failure != "":
		lines = append(lines, "The new VM needs attention", "")
	case m.newVMFinished():
		lines = append(lines, name+" is running", "")
	default:
		lines = append(lines, "Creating "+name, "")
	}
	marks := map[string]string{"done": "[x]", "now": "[>]", "wait": "[ ]", "skip": "[-]", "failed": "[!]"}
	for _, s := range m.newVMSteps() {
		lines = append(lines, "  "+marks[s.state]+" "+s.label)
	}
	lines = append(lines, "")
	switch {
	case flow.Failure != "":
		lines = append(lines, wrap("What happened: "+validation.SafeText(flow.Failure), width)...)
		lines = append(lines, "", "> [ Show details ]", "", "Esc Show details")
	case m.newVMFinished():
		switch flow.Display {
		case "open":
			lines = append(lines, "Its display is open in its own window. Closing that window does not stop the VM.")
		case "":
			lines = append(lines, "Opening its display…")
		default:
			lines = append(lines, wrap(flow.Display, width)...)
		}
		buttons := []string{"Open display", "Done"}
		row := ""
		for i, b := range buttons {
			mark := "  "
			if i == flow.EndFocus {
				mark = "> "
			}
			row += mark + "[ " + b + " ]  "
		}
		lines = append(lines, "", strings.TrimRight(row, " "))
	default:
		lines = append(lines, "This takes a few minutes for large images. You can keep using Virmill;", "Esc hides this, and the VM keeps being set up.")
	}
	return pageLines(lines, width, height, 0)
}

func (m Workspace) updateNewVMProgress(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	flow := *m.NewVM
	m.NewVM = &flow
	switch {
	case flow.Failure != "":
		if key.Type == tea.KeyEsc || key.Type == tea.KeyEnter {
			m.NewVM = nil
		}
	case m.newVMFinished():
		switch key.Type {
		case tea.KeyLeft, tea.KeyShiftTab, tea.KeyUp:
			flow.EndFocus = 0
		case tea.KeyRight, tea.KeyTab, tea.KeyDown:
			flow.EndFocus = 1
		case tea.KeyEsc:
			m.NewVM = nil
			return m, m.page(1)
		case tea.KeyEnter:
			if flow.EndFocus == 0 {
				flow.Display = ""
				return m, m.openDisplayAfterStart(flow.Chain.VMID)
			}
			m.NewVM = nil
			return m, m.page(1)
		}
	default:
		if key.Type == tea.KeyEsc {
			m.NewVM = nil
			m.Notice = fmt.Sprintf("%s is still being set up. Its progress is in Jobs.", validation.SafeText(flow.Name))
		}
	}
	return m, nil
}
