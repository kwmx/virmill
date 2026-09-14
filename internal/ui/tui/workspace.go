package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
)

// Workspace presents observed resources and guided workflows. The command
// browser is retained as an explicit advanced tool, not the default product UI.
type Workspace struct {
	Activity               *jobActivity
	ActivityReading        *jobActivityRead
	AutostartTarget        *domain.VM
	RemovalTarget          *domain.VM
	coordinator            coordinatorConnection
	NetworkForm            *NetworkForm
	CreationNetwork        *creationNetworkHandoff
	CreationNetworkRefresh *creationNetworkRefresh
	JobOutcome             *jobOutcome
	Resources              *resourceSetup
	ResourceSummary        *domain.VMResourceView
	ResourceSummaryVM      domain.VM
	ResourceSummaryError   string

	draftWriter                               *setupWriter
	draftSaved, draftResume                   *SavedSetupDocument
	draftSequence                             uint64
	draftLoading, draftBypass, draftSubmitted bool
	draftModal, draftError                    string
	draftChoice                               int

	BackupRecovery        *BackupRecoveryForm
	BackupRecoveryLoading bool
	PickerBackupRecovery  bool
	PickerBackupReceipt   bool
	GuestAgent            *guestAgentSetup
	Protection            *ProtectionForm
	PickerProtection      bool
	Console               *domain.ConsoleInfo
	ConsoleIndex          int
	ConsoleLoading        bool
	ConsoleVM             domain.VM
	Boot                  *BootForm
	BootVM                domain.VM
	BootLoading           bool
	Creation              *CreationForm
	SavedCreation         *CreationForm
	SavedForm             *GuidedForm
	PreparedBasics        map[string]ImportDraft
	SavedImport           *ImportForm
	CreationPicking       bool
	CreationChoices       []creationChoice
	CreationIndex         int
	PendingPreparation    string
	Import                *ImportForm
	ExportForm            *GuidedForm
	ImportPickerTarget    string
	ImportCancel          context.CancelFunc
	ImportStarted         time.Time
	ImportElapsed         time.Duration
	ButtonFocus           bool
	ButtonIndex           int

	CatalogSection, CatalogIndex int
	CatalogSearch                string
	CatalogSearching             bool
	CatalogMode                  string
	CatalogExpert                bool
	ImportFromCatalog            bool
	Picker                       *FilePicker
	PickerField                  int
	PickerAction                 bool
	ActionForm                   *ActionForm
	SavedActionForm              *ActionForm
	ActionTitle                  string

	Client                                                    ui.Client
	Connection                                                string
	Width, Height                                             int
	Section, Selected, NavIndex                               int
	NavFocus, Searching, Help, Advanced, Quit, NoColor, ASCII bool
	Search, Notice, Error                                     string
	Data                                                      map[string]any
	Errors                                                    map[string]string
	Pending                                                   map[string]uint64
	sequence                                                  uint64
	Detail                                                    any
	DetailTitle                                               string
	Offset                                                    int
	Raw                                                       bool
	Form                                                      *GuidedForm
	Plan                                                      *domain.Plan
	Approved                                                  []bool
	AckIndex                                                  int
	ApplyKey                                                  string
	Reviewing                                                 bool
	Busy                                                      bool
	legacy                                                    Model
}
type workspaceReply struct {
	Kind     string
	Token    uint64
	Response app.Response
	Err      error
}
type workspaceTick struct{}

var workspaceKinds = []string{"vms", "vms", "networks", "pools", "", "", "captures", "usb", "jobs", "plugins", "health"}
var workspaceMethods = map[string]string{"vms": "inventory.list", "networks": "network.list", "pools": "storage.pool.list", "captures": "snapshot.list", "usb": "device.usb.list", "jobs": "operation.list", "plugins": "plugin.list", "health": "host.inspect"}

func NewWorkspace(c ui.Client, connection string) Workspace {
	return Workspace{Client: c, Connection: connection, Width: 80, Height: 24, NoColor: os.Getenv("NO_COLOR") != "" || os.Getenv("VIRMILL_NO_COLOR") == "1" || os.Getenv("TERM") == "dumb", ASCII: os.Getenv("VIRMILL_ASCII") == "1" || os.Getenv("TERM") == "dumb", Data: map[string]any{}, Errors: map[string]string{}, Pending: map[string]uint64{}, legacy: New(c, connection)}
}
func (m Workspace) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg { return workspaceTick{} }, jobRefreshTick(), m.loadSetup())
}
func (m *Workspace) request(kind, method string, r app.Request) tea.Cmd {
	m.sequence++
	token := m.sequence
	m.Pending = maps.Clone(m.Pending)
	m.Pending[kind] = token
	client := m.Client
	r.Connection = m.Connection
	wait := 30 * time.Second
	importRead := ui.ImportRead(method)
	if importRead || kind == "apply" {
		wait = ui.ImportWait
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	if importRead && m.Import != nil {
		if m.ImportCancel != nil {
			m.ImportCancel()
		}
		m.ImportCancel = cancel
	}
	return func() tea.Msg {
		defer cancel()
		normalized, err := ui.NormalizeRequest(method, r)
		if err != nil {
			return workspaceReply{Kind: kind, Token: token, Err: err}
		}
		if client == nil {
			return workspaceReply{Kind: kind, Token: token, Err: fmt.Errorf("coordinator unavailable")}
		}
		response, err := client.Call(ctx, method, normalized)
		return workspaceReply{Kind: kind, Token: token, Response: response, Err: err}
	}
}
func (m *Workspace) refresh() tea.Cmd {
	if m.Section == 0 {
		return tea.Batch(m.request("vms", "inventory.list", app.Request{}), m.request("jobs", "operation.list", app.Request{}), m.request("pools", "storage.pool.list", app.Request{}))
	}
	kind := workspaceKinds[m.Section]
	if kind == "" {
		return nil
	}
	return m.request(kind, workspaceMethods[kind], app.Request{})
}
func (m *Workspace) page(section int) tea.Cmd {
	m.closeJobActivity()
	m.resetAutostartLoad()
	m.resetRemovalLoad()
	m.resetNetworkForm()
	m.CreationNetwork = nil
	if m.CreationNetworkRefresh != nil {
		m.Creation, m.Import = nil, nil
	}
	m.cancelCreationNetworkRefresh()
	m.resetJobOutcome()
	m.resetBackupRecovery()
	m.resetGuestAgent()
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	delete(m.Pending, "boot-load")
	delete(m.Pending, "protection-pools")
	delete(m.Pending, "console-load")
	m.Protection, m.Console, m.ConsoleLoading = nil, nil, false
	m.Boot, m.BootLoading = nil, false
	delete(m.Pending, "plan")
	if m.Pending["apply"] == 0 {
		m.Busy = false
		m.Notice = ""
	}
	m.Section = section
	m.NavIndex = section
	m.Selected = 0
	m.Offset = 0
	m.Detail = nil
	m.DetailTitle = ""
	m.Search = ""
	m.Searching = false
	m.Raw = false
	m.NavFocus = false
	m.ButtonFocus = false
	m.Error = ""
	return m.refresh()
}
func generic(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if decoder.Decode(&out) != nil {
		return nil
	}
	return out
}
func object(v any) map[string]any { x, _ := v.(map[string]any); return x }
func field(v any, key string) string {
	x := object(v)
	if x == nil {
		return ""
	}
	s, ok := x[key].(string)
	if ok {
		return s
	}
	if x[key] == nil {
		return ""
	}
	return fmt.Sprint(x[key])
}
func resourceID(v any) string {
	if id, ok := v.(string); ok {
		return id
	}
	if id := field(object(v)["key"], "resourceUUID"); id != "" {
		return id
	}
	for _, key := range []string{"operationID", "captureID", "snapshotID", "id", "stableID"} {
		if id := field(v, key); id != "" {
			return id
		}
	}
	return ""
}
func rowName(v any) string {
	for _, key := range []string{"name", "displayName", "product", "operation", "id"} {
		if name := field(v, key); name != "" {
			return name
		}
	}
	if id := resourceID(v); id != "" {
		return id
	}
	return "Unnamed resource"
}
func rowState(v any) string {
	if s := field(v, "state"); s != "" {
		return s
	}
	if active, ok := object(v)["active"].(bool); ok {
		if active {
			return "active"
		}
		return "inactive"
	}
	if s := field(v, "status"); s != "" {
		return s
	}
	return "observed"
}
func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	for _, key := range []string{"items", "plugins", "installations", "captures", "devices"} {
		if a, ok := object(v)[key].([]any); ok {
			return a
		}
	}
	return nil
}
func (m Workspace) rows() []any {
	out := []any{}
	for _, row := range array(m.Data[workspaceKinds[m.Section]]) {
		if m.Search == "" || strings.Contains(strings.ToLower(validation.SafeText(m.nameCell(row)+" "+resourceID(row)+" "+rowState(row))), strings.ToLower(m.Search)) {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if m.Section == 8 {
			return field(out[i], "createdAt") > field(out[j], "createdAt")
		}
		return strings.ToLower(rowName(out[i])) < strings.ToLower(rowName(out[j]))
	})
	return out
}
func (m Workspace) selectedVM() domain.VM {
	var vm domain.VM
	if m.Section != 1 && m.Section != 0 {
		return vm
	}
	v := m.Detail
	if field(object(v)["key"], "resourceUUID") == "" {
		v = nil
	}
	if v == nil {
		rows := m.rows()
		if m.Selected >= 0 && m.Selected < len(rows) {
			v = rows[m.Selected]
		}
	}
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &vm)
	return vm
}
func (m *Workspace) guided(kind string) {
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	f, err := NewGuidedForm(kind, m.selectedVM())
	if err != nil {
		m.Error = validation.SafeText(err.Error())
		return
	}
	m.Form = &f
	m.Error = ""
	m.Offset = 0
}
func (m *Workspace) preview(action string) tea.Cmd {
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	vm := m.selectedVM()
	if vm.Key.UUID == "" {
		m.Error = "Select a virtual machine first."
		return nil
	}
	m.Busy = true
	m.Error = ""
	m.Notice = "Preparing a review for " + validation.SafeText(vm.Name) + "..."
	return m.request("plan", "vm.plan", app.Request{ID: vm.Key.UUID, Action: action})
}
func (m *Workspace) showRow() tea.Cmd {
	rows := m.rows()
	if len(rows) == 0 {
		return nil
	}
	m.resetJobOutcome()
	m.Selected = max(0, min(m.Selected, len(rows)-1))
	row := rows[m.Selected]
	m.Detail = row
	m.Raw = false
	m.Offset = 0
	m.Error = ""
	id := resourceID(row)
	switch m.Section {
	case 0, 1:
		m.Section = 1
		m.NavIndex = 1
		m.DetailTitle = "VM details"
		return m.request("detail", "inventory.get", app.Request{ID: id})
	case 2:
		m.DetailTitle = "Network details"
		return m.request("detail", "network.get", app.Request{ID: id})
	case 3:
		m.DetailTitle = "Storage pool details"
		return m.request("detail", "storage.pool.get", app.Request{ID: id})
	case 8:
		m.DetailTitle = "Job details"
		return m.request("detail", "operation.get", app.Request{ID: id})
	default:
		m.DetailTitle = sections[m.Section] + " details"
	}
	return nil
}
func (m *Workspace) advanced() {
	m.resetJobOutcome()
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	m.Advanced = true
	m.CatalogSection = -1
	m.CatalogIndex = 0
	m.CatalogSearch = ""
	m.CatalogSearching = false
	m.CatalogMode = ""
	m.CatalogExpert = false
}
func (m *Workspace) openAction(a ui.Action) tea.Cmd {
	if m.Busy || m.Pending["apply"] != 0 {
		m.Error = "Wait for the current request before selecting another action."
		return nil
	}
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "detail")
	m.Error = ""
	m.ActionTitle = actionLabel(a)
	vm := m.selectedVM()
	switch a.Command {
	case "network create":
		m.openNetworkForm()
		return nil
	case "vm create", "vm create-devices":
		return m.openCreationSources()
	case "import prepare", "import source describe":
		return m.openImport("auto")
	case "import prepare-install":
		return m.openImport("iso")
	case "import prepare-disks":
		return m.openImport("disks")
	case "vm start", "vm stop", "vm reboot", "vm pause", "vm resume":
		if vm.Key.UUID != "" {
			m.Advanced = false
			return m.preview(a.Mutation)
		}
	case "vm remove":
		return m.openRemoval()
	case "vm autostart":
		return m.openAutostart()
	case "vm console show":
		return m.openConsole()
	case "backup restore", "backup receipts":
		return m.openBackupRecovery()
	case "snapshot restore", "backup create":
		return m.openProtection(a.Command)
	case "vm boot set":
		return m.openBootForm()
	case "vm set", "vm resources show":
		return m.openResources()
	case "snapshot create":
		if vm.Key.UUID != "" {
			m.guided("capture")
			m.Advanced = false
			return nil
		}
	case "guest tools install", "vm guest-agent enable", "vm guest-agent show":
		return m.openGuestTools()
	case "guest recipe run":
		if vm.Key.UUID != "" {
			m.guided("guest-recipe")
			m.Advanced = false
			return nil
		}
	case "backup repository init":
		m.guided("repository-init")
		m.Advanced = false
		return nil
	case "backup repository check":
		m.guided("repository-check")
		m.Advanced = false
		return nil
	}
	if a.Argument == "" {
		m.Advanced = false
		m.DetailTitle = m.ActionTitle
		m.Detail = nil
		return m.request("detail", a.Method, app.Request{})
	}
	selectedID := ""
	if vm.Key.UUID != "" && (a.Method == "vm.plan" || a.Method == "inventory.get" || a.Method == "vm.readiness.show" || a.Method == "vm.boot.get" || strings.HasPrefix(a.Method, "vm.recovery.")) {
		selectedID = vm.Key.UUID
	}
	if selectedID == "" {
		row := m.Detail
		if row == nil {
			rows := m.rows()
			if m.Selected >= 0 && m.Selected < len(rows) {
				row = rows[m.Selected]
			}
		}
		id := resourceID(row)
		if m.Section == 2 && a.Method == "network.get" || m.Section == 3 && a.Method == "storage.pool.get" || m.Section == 6 && (a.Method == "snapshot.show" || a.Method == "snapshot.restore" || a.Method == "backup.create") || m.Section == 8 && (strings.HasPrefix(a.Method, "operation.") || strings.HasSuffix(a.Method, ".result")) {
			selectedID = id
		}
	}
	f, err := NewActionForm(a, selectedID)
	if err != nil {
		m.Error = validation.SafeText(err.Error())
		return nil
	}
	m.ActionForm = &f
	if a.Method == "import.prepare" || a.Method == "import.prepare-install" || a.Method == "import.prepare-disks" || a.Method == "import.inspect" || a.Method == "import.describe" {
		return m.browseField()
	}
	return nil
}

func (m Workspace) updateWorkspace(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.Picker != nil {
		switch msg.(type) {
		case workspaceReply, workspaceTick, jobRefreshPulse, creationPulse, importPulse, importExportReply, consolePrepared, consoleClosed:
		default:
			return m.updatePicker(msg)
		}
	}
	switch v := msg.(type) {
	case consolePrepared:
		return m.receiveConsolePrepared(v)
	case consoleClosed:
		m.Busy = false
		m.Notice = "Console closed. Virmill did not stop the VM."
		if v.Err != nil {
			m.Error = "Console ended: " + validation.SafeText(v.Err.Error()) + ". Refresh access and try again."
		}
		return m, nil
	case importPulse:
		if m.Import != nil && m.Pending["import-inspect"] == v.Token {
			m.ImportElapsed = time.Since(m.ImportStarted)
			return m, importPulseCommand(v.Token)
		}
		return m, nil
	case importExportReply:
		if m.Pending["import-export"] != v.Token {
			return m, nil
		}
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, "import-export")
		m.Busy = false
		m.Notice = ""
		if v.Err != nil {
			if m.ExportForm != nil {
				m.ExportForm.Error = validation.SafeText(v.Err.Error())
			}
			return m, nil
		}
		m.ExportForm = nil
		m.Notice = "Settings exported: " + validation.SafeText(v.Path)
		if m.BackupRecovery != nil {
			m.Notice = "Recovery receipt saved: " + validation.SafeText(v.Path)
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.Width = v.Width
		m.Height = v.Height
		m.legacy.Width = v.Width
		m.legacy.Height = max(1, v.Height-1)
		return m, nil
	case resultMsg:
		next, cmd := m.legacy.Update(v)
		m.legacy = next.(Model)
		return m, cmd
	case creationPulse:
		if v.OperationID != m.PendingPreparation {
			return m, nil
		}
		return m, m.request("preparation-job", "operation.get", app.Request{ID: v.OperationID})
	case workspaceTick:
		return m, m.refresh()
	case jobRefreshPulse:
		return m, m.refreshJobs()
	case workspaceReply:
		if m.Pending[v.Kind] != v.Token {
			return m, nil
		}
		m.Pending = maps.Clone(m.Pending)
		delete(m.Pending, v.Kind)
		m.observeCoordinatorConnection(v)
		err := v.Err
		if err == nil && v.Response.Error != nil {
			err = v.Response.Error
		}
		m.Errors = maps.Clone(m.Errors)
		if err != nil {
			text := validation.SafeText(err.Error())
			m.Errors[v.Kind] = text
			if v.Kind == "job-activity" {
				m.failJobActivity("Could not read job activity: " + text)
			}
			if v.Kind == "apply" {
				m.Offset = 0
			}
			if v.Kind == "plan" || v.Kind == "apply" || v.Kind == "detail" || v.Kind == "boot-load" || v.Kind == "console-load" || v.Kind == "protection-pools" || v.Kind == "guest-agent-load" || v.Kind == "backup-receipts" || v.Kind == "backup-receipt-read" || v.Kind == "import-inspect" {
				m.Error = text
				m.Busy = m.Pending["plan"] != 0 || m.Pending["apply"] != 0
				m.Notice = ""
			}
			if v.Kind == "draft-preparation-job" {
				m.Busy = false
				m.draftResume = nil
				m.draftModal = "creation"
				m.draftChoice = 0
				m.Error = "Could not check the saved preparation: " + importError(err) + ". Choose Continue prepared setup to retry."
				m.Notice = "Saved choices were kept. No operation was repeated."
			}
			if v.Kind == "removal-load" {
				m.resetRemovalLoad()
				m.Busy, m.Notice, m.Error = false, "", text
			}
			if v.Kind == "autostart-load" {
				m.resetAutostartLoad()
				m.Busy, m.Notice, m.Error = false, "", text
			}
			if v.Kind == "creation-networks" {
				m.Busy, m.Notice = false, ""
				m.CreationNetworkRefresh = nil
				if m.Creation != nil {
					m.Creation.Error = "Could not refresh networks: " + text
					m.Error = ""
				}
			}
			if v.Kind == "resources-load" {
				m.Busy = false
				if m.Resources != nil {
					m.Resources.Loading = false
					m.Resources.Error = text
					m.Error = ""
				}
			}
			if v.Kind == "resources-summary" {
				m.ResourceSummaryError = text
			}
			if v.Kind == "plan" && m.Form != nil && m.Form.Kind == "remove-definition" {
				m.Form.Error = text
				m.Error = ""
			}
			if v.Kind == "plan" && m.NetworkForm != nil && m.ActionForm == nil {
				m.NetworkForm.Error = importError(err)
				m.Error, m.Notice = "", ""
			}
			if v.Kind == "plan" && m.Resources != nil {
				m.Resources.Error = importError(err)
				m.Error = ""
			}
			if v.Kind == "guest-agent-load" {
				m.failGuestAgent(text)
			}
			if v.Kind == "backup-receipts" || v.Kind == "backup-receipt-read" || v.Kind == "plan" && m.BackupRecovery != nil {
				m.Busy = false
				m.BackupRecoveryLoading = false
				if m.BackupRecovery != nil {
					m.BackupRecovery.Error = text
					m.Error = ""
				}
			}
			if v.Kind == "job-open-vm" {
				m.Busy = false
				m.Error = "Could not open this job's VM: " + text
			}
			if v.Kind == "console-load" {
				m.ConsoleLoading = false
			}
			if v.Kind == "boot-load" {
				m.BootLoading = false
			}
			if v.Kind == "import-inspect" && m.Import != nil {
				m.Import.Error = importError(err)
				m.ImportCancel = nil
				m.Error = ""
				delete(m.Errors, v.Kind)
			}
			if v.Kind == "plan" && m.Import != nil {
				m.Import.Error = importError(err)
				m.ImportCancel = nil
				m.Error = ""
				delete(m.Errors, v.Kind)
			}
			if strings.HasPrefix(v.Kind, "creation-") {
				m.Busy = false
				m.Error = importError(err)
				if m.Creation != nil {
					m.Creation.Error = m.Error
					m.Error = ""
				}
			}
			if v.Kind == "preparation-job" {
				m.PendingPreparation = ""
				m.Notice = "Could not refresh the import. Open Jobs to check its status."
			}
			if v.Kind == "plan" && m.Creation != nil {
				m.Creation.FocusError(fmt.Errorf("%s", importError(err)))
				m.Error = ""
				delete(m.Errors, v.Kind)
			}
			if v.Kind == "apply" {
				// Preserve the exact review and request identity when the reply
				// is uncertain. A deliberate retry cannot become a second job.
				m.Reviewing = false
				m.Notice = "Submission was not confirmed. Check Jobs; retrying this review reuses the same request."

			}
			return m, nil
		}
		delete(m.Errors, v.Kind)
		data := generic(v.Response.Data)
		switch v.Kind {
		case "job-activity":
			m.receiveJobActivity(v.Response.Data)
		case "draft-preparation-job":
			return m, m.resumePreparedJob(v.Response.Data)
		case "creation-sources":
			m.Busy = false
			b, _ := json.Marshal(v.Response.Data)
			if err := json.Unmarshal(b, &m.CreationChoices); err != nil {
				m.Error = "Could not read the prepared-image list. Try Create VM again."
			}
		case "creation-networks":
			m.receiveCreationNetworks(v.Response.Data)
		case "creation-load":
			m.Busy = false
			b, _ := json.Marshal(v.Response.Data)
			var bundle creationBundle
			if err := json.Unmarshal(b, &bundle); err != nil {
				m.Error = "Could not read hardware choices. Try Create VM again."
				return m, nil
			}
			if m.draftResume != nil && m.draftResume.State == "submitted" && m.draftResume.Import != nil && m.draftResume.OperationID == bundle.OperationID {
				saved := m.draftResume
				f := NewCreationForm(bundle.OperationID, bundle.Source, bundle.Options, bundle.Pools, bundle.Networks)
				if saved.Creation != nil {
					saved.Creation.restore(&f)
				} else {
					applyImportBasics(&f, saved.Import.form().Draft)
				}
				m.Creation = &f
				m.draftResume = nil
			}
			if m.draftResume != nil && m.draftResume.Creation != nil && m.draftResume.Import == nil {
				saved := m.draftResume
				if saved.Creation.OperationID != bundle.OperationID || saved.SourceBinding == "" || saved.SourceBinding != setupBinding(bundle.Source) {
					m.Error = "Prepared images changed or could not be matched. Saved choices were kept. Go back and start a new setup."
					return m, nil
				}
				f := NewCreationForm(bundle.OperationID, bundle.Source, bundle.Options, bundle.Pools, bundle.Networks)
				saved.Creation.restore(&f)
				m.Creation = &f
				m.draftResume = nil
			}
			if m.Creation == nil && m.SavedCreation != nil && m.SavedCreation.OperationID == bundle.OperationID {
				m.Creation = m.SavedCreation
				m.SavedCreation = nil
			}
			if m.Creation != nil && m.Creation.OperationID == bundle.OperationID {
				m.Creation.SetOptions(bundle.Options)
				m.Creation.Source = bundle.Source
				m.Creation.Pools = bundle.Pools
				m.Creation.Networks = bundle.Networks
			} else {
				f := NewCreationForm(bundle.OperationID, bundle.Source, bundle.Options, bundle.Pools, bundle.Networks)
				f.BeforePreparation = bundle.OperationID == ""
				m.Creation = &f
			}

			if bundle.OperationID == "" && m.Import != nil {
				applyImportBasics(m.Creation, m.Import.Draft)
				if m.Import.Page == 3 {
					m.Creation.Page, m.Creation.Focus = 3, 0
				}
			} else if d, ok := m.PreparedBasics[bundle.OperationID]; ok {
				applyImportBasics(m.Creation, d)
				delete(m.PreparedBasics, bundle.OperationID)
			}
			m.CreationPicking = false
			m.draftSubmitted = false
			m.Notice = ""
			m.Error = ""
		case "preparation-job":
			var job domain.Job
			b, _ := json.Marshal(v.Response.Data)
			if json.Unmarshal(b, &job) != nil || job.ID != m.PendingPreparation {
				return m, nil
			}
			if !domain.Terminal(job.State) {
				return m, creationTick(job.ID)
			}
			m.PendingPreparation = ""
			if m.Section == 8 && resourceID(m.Detail) == job.ID {
				m.Detail = data
			}
			if job.State == "succeeded" {
				if m.Section == 8 && resourceID(m.Detail) == job.ID && m.Activity == nil && m.Plan == nil && !m.Busy && m.AutostartTarget == nil && m.RemovalTarget == nil && m.CreationNetwork == nil && m.NetworkForm == nil && m.Import == nil && m.Creation == nil && !m.Advanced && m.Form == nil && m.ActionForm == nil && !m.CreationPicking && m.Picker == nil && m.ExportForm == nil && !m.Help {
					return m, m.loadCreation(job.ID, "")
				}
				m.Notice = "Images are ready. Choose Create VM to set CPU, RAM and networks."
			} else {
				m.Notice = "Import needs attention. Open its job for the error and recovery options."
			}
		case "import-inspect":
			m.importInspection(v.Response.Data)
		case "plan":
			m.Busy = false
			m.Notice = ""
			var p domain.Plan
			b, _ := json.Marshal(v.Response.Data)
			decoder := json.NewDecoder(bytes.NewReader(b))
			decoder.UseNumber()
			if decoder.Decode(&p) != nil || p.ID == "" || p.Digest == "" {
				m.Error = "The coordinator did not return a complete review plan."
				return m, nil
			}
			if digest, err := operations.PlanDigest(p); err != nil || digest != p.Digest {
				m.Error = "The returned plan digest does not match its review. No approval is available."
				return m, nil
			}
			if m.CreationNetwork != nil && m.NetworkForm != nil && (p.Operation != "network.create" || p.ConnectionID != m.CreationNetwork.Connection) {
				m.Error = "The review does not match network creation. No approval is available; VM choices are kept."
				return m, nil
			}
			m.Plan = &p
			m.ApplyKey = ""
			m.Approved = make([]bool, len(p.Acknowledgements))
			m.AckIndex = 0
			m.Offset = 0
			m.Reviewing = false
			m.SavedForm = m.Form
			m.SavedActionForm = m.ActionForm
			m.Form = nil
			m.ActionForm = nil
			m.Advanced = false
		case "resources-load":
			m.receiveResources(v.Response.Data)
		case "resources-summary":
			m.receiveResourceSummary(v.Response.Data)
		case "guest-agent-load":
			m.receiveGuestAgent(v.Response.Data)
		case "backup-receipts":
			m.receiveBackupReceipts(v.Response.Data)
		case "backup-receipt-read":
			m.receiveBackupReceipt(v.Response.Data)
		case "console-load":
			m.receiveConsole(v.Response.Data)
		case "protection-pools":
			m.receiveProtectionPools(v.Response.Data)
		case "removal-load":
			m.receiveRemoval(v.Response.Data)
		case "autostart-load":
			m.receiveAutostart(v.Response.Data)
		case "boot-load":
			m.receiveBoot(v.Response.Data)
		case "job-outcome":
			m.receiveJobOutcome(v.Response.Data)
		case "job-open-vm":
			return m, m.receiveJobVM(v.Response.Data)
		case "job-update":
			if m.Section == 8 && resourceID(m.Detail) != "" && resourceID(m.Detail) == resourceID(data) {
				m.Detail = data
				if m.canAutoReturnCreationNetwork() && field(data, "state") == "succeeded" {
					return m, m.returnCreationNetwork(true)
				}
				return m, m.loadJobOutcome(false)
			}
		case "apply":
			if handled, cmd := m.acceptCreationNetwork(v.Response.Data); handled {
				return m, cmd
			}
			m.resetNetworkForm()
			m.resetResources()
			draftCommand := m.setupAccepted(resourceID(data))
			m.resetBackupRecovery()
			m.resetGuestAgent()
			m.Protection = nil
			m.Boot = nil
			m.SavedForm, m.SavedActionForm = nil, nil
			prepared := m.Import != nil
			if prepared {
				m.PreparedBasics = maps.Clone(m.PreparedBasics)
				if m.PreparedBasics == nil {
					m.PreparedBasics = map[string]ImportDraft{}
				}
				m.PreparedBasics[resourceID(data)] = m.Import.Draft
			}
			if prepared && m.Import.VM != nil {
				if m.Import.VMBinding == creationDraftBinding(m.Import.Draft) {
					f := *m.Import.VM
					f.OperationID = resourceID(data)
					f.BeforePreparation = false
					m.SavedCreation = &f
				}
			}
			m.Creation = nil
			m.CreationPicking = false
			m.Import = nil
			m.Busy = false
			m.Plan = nil
			m.Reviewing = false
			m.Form = nil
			m.Section = 8
			m.NavIndex = 8
			m.Detail = data
			m.DetailTitle = "Job details"
			m.Offset = 0
			m.Notice = "Operation accepted. Jobs continue when you leave this page."
			if prepared {
				m.PendingPreparation = resourceID(data)
				m.Notice = "Preparing images. CPU, RAM and network setup opens when ready."
				return m, tea.Batch(m.request("jobs", "operation.list", app.Request{}), creationTick(m.PendingPreparation), draftCommand)
			}
			return m, tea.Batch(m.request("jobs", "operation.list", app.Request{}), draftCommand)
		case "detail":
			m.Detail = data
			m.Busy = m.Pending["plan"] != 0 || m.Pending["apply"] != 0
			m.ActionForm = nil
			m.Advanced = false
			m.Offset = 0
			return m, tea.Batch(m.requestResourceSummary(), m.loadJobOutcome(false))
		default:
			selectedID := ""
			rows := m.rows()
			if m.Selected >= 0 && m.Selected < len(rows) {
				selectedID = resourceID(rows[m.Selected])
			}
			m.Data = maps.Clone(m.Data)
			m.Data[v.Kind] = data
			if selectedID != "" {
				found := -1
				matches := 0
				for i, row := range m.rows() {
					if resourceID(row) == selectedID {
						found = i
						matches++
					}
				}
				if matches == 1 {
					m.Selected = found
				} else {
					m.Selected = -1
					m.Notice = "The selected resource changed or disappeared. Select a row before acting."
				}
			} else if m.Selected >= 0 {
				m.Selected = max(0, min(m.Selected, len(m.rows())-1))
			}
		}
		if len(v.Response.Warnings) > 0 {
			m.Notice = validation.SafeText(strings.Join(v.Response.Warnings, "; "))
		}
		return m, nil
	case tea.KeyMsg:
		if v.Type == tea.KeyCtrlC {
			if m.ImportCancel != nil {
				m.ImportCancel()
			}
			m.Quit = true
			return m, tea.Quit
		}
		if m.Width < 60 || m.Height < 18 {
			if v.Type == tea.KeyEsc {
				if m.ImportCancel != nil {
					m.ImportCancel()
					m.ImportCancel = nil
				}
				m.resetBackupRecovery()
				m.resetGuestAgent()
				m.resetResources()
				m.resetAutostartLoad()
				m.resetRemovalLoad()
				m.resetNetworkForm()
				m.Form = nil
				m.ActionForm = nil
				m.Import = nil
				m.ExportForm = nil
				m.Creation = nil
				m.CreationPicking = false
				m.Plan = nil
				m.Advanced = false
				m.Help = false
				m.Pending = maps.Clone(m.Pending)
				delete(m.Pending, "plan")
				delete(m.Pending, "import-inspect")
				delete(m.Pending, "creation-load")
				delete(m.Pending, "creation-sources")
				m.Busy = m.Pending["apply"] != 0 || m.Pending["import-export"] != 0
			}
			return m, nil
		}
		if m.Help {
			if v.String() == "?" || v.Type == tea.KeyEsc {
				m.Help = false
			}
			return m, nil
		}
		if m.Activity != nil {
			return m.updateJobActivity(v)
		}
		if m.RemovalTarget != nil {
			if v.Type == tea.KeyEsc {
				m.resetRemovalLoad()
				m.Busy, m.Notice = false, ""
			}
			return m, nil
		}
		if m.AutostartTarget != nil {
			if v.Type == tea.KeyEsc {
				m.resetAutostartLoad()
				m.resetRemovalLoad()
				m.Busy, m.Notice = false, ""
			}
			return m, nil
		}
		if m.Resources != nil && m.Plan == nil {
			return m.updateResources(v)
		}
		if (m.Console != nil || m.ConsoleLoading) && m.Plan == nil {
			return m.updateConsole(v)
		}
		if m.BackupRecovery != nil && m.Plan == nil && m.ExportForm == nil {
			return m.updateBackupRecovery(v)
		}
		if m.GuestAgent != nil && m.Plan == nil && m.Picker == nil {
			return m.updateGuestAgent(v)
		}
		if m.Protection != nil && m.Plan == nil {
			return m.updateProtection(v)
		}
		if (m.Boot != nil || m.BootLoading) && m.Plan == nil {
			return m.updateBoot(v)
		}
		if m.ExportForm != nil {
			return m.updateImportExport(v)
		}
		if m.NetworkForm != nil && m.Plan == nil && m.ActionForm == nil {
			return m.updateNetworkForm(v)
		}
		if (m.Creation != nil || m.CreationPicking) && m.Plan == nil {
			return m.updateCreation(v)
		}
		if m.Import != nil && m.Plan == nil {
			return m.updateImport(v)
		}
		if (m.ActionForm != nil || m.Form != nil) && v.Type == tea.KeyCtrlO {
			return m, m.browseField()
		}
		if m.ActionForm != nil {
			f, submit, cancel := m.ActionForm.Update(v)
			m.ActionForm = &f
			if cancel {
				m.ActionForm = nil
				m.Pending = maps.Clone(m.Pending)
				delete(m.Pending, "plan")
				delete(m.Pending, "detail")
				m.Busy = false
				m.Notice = ""
				return m, nil
			}
			if submit && !m.Busy {
				method, r, err := f.Request(m.Connection)
				if err != nil {
					m.Error = validation.SafeText(err.Error())
					return m, nil
				}
				m.Busy = true
				m.DetailTitle = m.ActionTitle
				kind := "detail"
				if r.Action != "" || method == "plan.show" {
					kind = "plan"
				}
				return m, m.request(kind, method, r)
			}
			return m, nil
		}
		if m.Advanced {
			if m.CatalogSearching {
				switch v.Type {
				case tea.KeyEsc:
					m.CatalogSearching = false
					m.CatalogSearch = ""
					m.CatalogIndex = 0
				case tea.KeyEnter:
					m.CatalogSearching = false
				case tea.KeyBackspace, tea.KeyCtrlH:
					r := []rune(m.CatalogSearch)
					if len(r) > 0 {
						m.CatalogSearch = string(r[:len(r)-1])
					}
					m.CatalogIndex = 0
				case tea.KeyRunes, tea.KeySpace:
					text := string(v.Runes)
					if v.Type == tea.KeySpace {
						text = " "
					}
					if len(m.CatalogSearch+text) <= 256 && validation.SafeText(text) == text && !strings.ContainsAny(text, "\r\n\t") {
						m.CatalogSearch += text
						m.CatalogIndex = 0
					}
				}
				return m, nil
			}
			if v.Type == tea.KeyRunes && (v.Paste || len(v.Runes) != 1) {
				return m, nil
			}
			switch v.String() {
			case "esc":
				if m.CatalogSearch != "" {
					m.CatalogSearch = ""
					m.CatalogIndex = 0
				} else if m.CatalogExpert {
					m.CatalogExpert = false
					m.CatalogIndex = 0
				} else {
					m.Advanced = false
				}
			case "A":
				if m.catalogHasAdvanced() {
					m.CatalogExpert = true
					m.CatalogIndex = 0
				}
			case "end":
				m.CatalogIndex = max(0, m.catalogCount()-1)
			case "home":
				m.CatalogIndex = 0
			case "left", "right":
				if m.CatalogMode == "all" {
					delta := 1
					if v.String() == "left" {
						delta = -1
					}
					m.CatalogSection = (m.CatalogSection+1+delta+len(sections)+1)%(len(sections)+1) - 1
					m.CatalogIndex = 0
				}
			case "/":
				m.CatalogSearching = true
			case "up", "k":
				m.CatalogIndex = max(0, m.CatalogIndex-1)
			case "down", "j":
				m.CatalogIndex = max(0, min(m.catalogCount()-1, m.CatalogIndex+1))
			case "pgdown":
				m.CatalogIndex = max(0, min(m.catalogCount()-1, m.CatalogIndex+max(1, m.Height-13)))
			case "pgup":
				m.CatalogIndex = max(0, m.CatalogIndex-max(1, m.Height-13))
			case "enter":
				rows := m.catalog()
				if m.catalogHasAdvanced() && m.CatalogIndex == len(rows) {
					m.CatalogExpert = true
					m.CatalogIndex = 0
					return m, nil
				}
				if len(rows) > 0 {
					return m, m.openAction(rows[max(0, min(m.CatalogIndex, len(rows)-1))])
				}
			case "q":
				m.Quit = true
				return m, tea.Quit
			}
			return m, nil
		}
		if m.Form != nil {
			if m.Pending["apply"] != 0 {
				m.Busy = true
				m.Notice = "Submission is pending. Check Jobs before trying again."
				return m, nil
			}
			if m.Busy && v.Type != tea.KeyEsc {
				return m, nil
			}
			m.Form.SetViewport(m.Width, m.Height-7)
			f, submit, cancel := m.Form.Update(v)
			m.Form = &f
			if cancel {
				m.Pending = maps.Clone(m.Pending)
				delete(m.Pending, "plan")
				m.Busy = false
				m.Notice = ""
				m.Form = nil
				return m, nil
			}
			if submit && !m.Busy {
				method, r, err := f.Request(m.Connection)
				if err != nil {
					m.Error = validation.SafeText(err.Error())
					return m, nil
				}
				m.Busy = true
				m.Error = ""
				m.Notice = "Preparing reviewed changes..."
				return m, m.request("plan", method, r)
			}
			return m, nil
		}
		key := v.String()
		if m.Searching {
			switch v.Type {
			case tea.KeyEsc:
				m.Searching = false
				m.Search = ""
				m.Selected = 0
			case tea.KeyEnter:
				m.Searching = false
			case tea.KeyBackspace, tea.KeyCtrlH:
				r := []rune(m.Search)
				if len(r) > 0 {
					m.Search = string(r[:len(r)-1])
				}
				m.Selected = 0
			case tea.KeyRunes, tea.KeySpace:
				s := string(v.Runes)
				if v.Type == tea.KeySpace {
					s = " "
				}
				if len([]rune(m.Search+s)) <= 128 && validation.SafeText(s) == s && !strings.ContainsAny(s, "\n\r\t") {
					m.Search += s
					m.Selected = 0
				}
			}
			return m, nil
		}
		if v.Type == tea.KeyRunes && (v.Paste || len(v.Runes) != 1) {
			return m, nil
		}
		if m.Plan != nil {
			switch key {
			case "esc":
				if m.Reviewing {
					m.Reviewing = false
				} else {
					m.Plan = nil
					m.ActionForm, m.SavedActionForm = m.SavedActionForm, nil
					if m.SavedForm != nil {
						m.Form = m.SavedForm
						m.SavedForm = nil
					}
				}
				m.Offset = 0
			case "pgdown":
				m.Offset += max(1, m.Height-11)
			case "pgup":
				m.Offset = max(0, m.Offset-max(1, m.Height-11))
			case "down", "j", "tab":
				if m.Reviewing {
					m.AckIndex = min(len(m.Approved), m.AckIndex+1)
				} else {
					m.Offset++
				}
			case "up", "k", "shift+tab":
				if m.Reviewing {
					m.AckIndex = max(0, m.AckIndex-1)
				} else {
					m.Offset = max(0, m.Offset-1)
				}
			case "a":
				m.Reviewing = true
				m.AckIndex = 0
				m.Offset = 0
			case " ":
				if m.Reviewing && m.AckIndex < len(m.Approved) {
					m.Approved = append([]bool{}, m.Approved...)
					m.Approved[m.AckIndex] = !m.Approved[m.AckIndex]
				}
			case "enter":
				if !m.Reviewing {
					m.Reviewing = true
					m.Offset = 0
					return m, nil
				}
				if m.AckIndex < len(m.Approved) {
					m.Approved = append([]bool{}, m.Approved...)
					m.Approved[m.AckIndex] = !m.Approved[m.AckIndex]
					m.AckIndex++
					return m, nil
				}
				all := true
				for _, yes := range m.Approved {
					all = all && yes
				}
				if !all {
					m.Error = "Acknowledge each listed consequence before applying."
					return m, nil
				}
				if !m.Busy && m.Pending["apply"] == 0 {
					m.Busy = true
					m.Error = ""
					if m.ApplyKey == "" {
						m.ApplyKey = domain.ID()
					}
					m.resetJobOutcome()
					apply := m.request("apply", "operation.apply", app.Request{Apply: &operations.ApplyRequest{PlanID: m.Plan.ID, PlanDigest: m.Plan.Digest, IdempotencyKey: m.ApplyKey, Acknowledgements: append([]string{}, m.Plan.Acknowledgements...)}})
					guarded := m.guardSetupApply(apply)
					return m, guarded
				}
			}
			return m, nil
		}
		if key == "esc" {
			if m.Help {
				m.Help = false
			} else if m.Detail != nil {
				m.resetJobOutcome()
				m.Pending = maps.Clone(m.Pending)
				delete(m.Pending, "detail")
				m.Detail = nil
				m.DetailTitle = ""
				m.Offset = 0
				m.Raw = false
			} else if m.Search != "" {
				m.Search = ""
				m.Selected = 0
			} else {
				m.NavFocus = false
				m.ButtonFocus = false
			}
			m.Error = ""
			return m, nil
		}
		if m.ButtonFocus {
			switch key {
			case "left", "up":
				m.ButtonIndex = max(0, m.ButtonIndex-1)
				return m, nil
			case "right", "down":
				m.ButtonIndex = min(len(m.buttons())-1, m.ButtonIndex+1)
				return m, nil
			case "enter":
				buttons := m.buttons()
				if len(buttons) == 0 {
					return m, nil
				}
				key = buttons[max(0, min(m.ButtonIndex, len(buttons)-1))].key
				m.ButtonFocus = false
				if key == "esc" {
					return m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				}
			}
		}
		if strings.HasPrefix(key, "action:") {
			for _, a := range ui.Actions {
				if a.Command == strings.TrimPrefix(key, "action:") {
					return m, m.openAction(a)
				}
			}
		}
		switch key {
		case "connection-help":
			if m.connectionRecoveryVisible() {
				m.coordinator.Expanded = !m.coordinator.Expanded
				m.Offset = 0
			}
			return m, nil
		case "job-open-vm":
			return m, m.openJobVM()
		case "job-activity":
			return m, m.openJobActivity()
		case "creation-network-return":
			return m, m.returnCreationNetwork(true)
		case "job-refresh-result":
			return m, m.loadJobOutcome(true)
		case "q":
			m.Quit = true
			return m, tea.Quit
		case "?":
			m.Help = !m.Help
		case ":":
			m.advanced()
			m.CatalogMode = "all"
		case "i":
			if m.Section == 0 || m.Section == 1 {
				return m, m.openImport("auto")
			}
		case "a":
			m.advanced()
			m.CatalogSection = m.Section
		case "tab", "shift+tab":
			focus := 0
			if m.ButtonFocus {
				focus = 1
			}
			if m.NavFocus {
				focus = 2
			}
			if key == "shift+tab" {
				focus = (focus + 2) % 3
			} else {
				focus = (focus + 1) % 3
			}
			m.ButtonFocus = focus == 1
			m.NavFocus = focus == 2
			m.ButtonIndex = 0
			m.NavIndex = m.Section

		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			return m, m.page(int(key[0] - '1'))
		case "0":
			return m, m.page(9)
		case ",":
			return m, m.page(10)
		case "/":
			if m.Detail == nil {
				m.Searching = true
			}
		case "r":
			if m.Detail != nil {
				switch m.Section {
				case 1:
					return m, m.request("detail", "inventory.get", app.Request{ID: resourceID(m.Detail)})
				case 8:
					return m, m.request("detail", "operation.get", app.Request{ID: resourceID(m.Detail)})
				}
			}
			return m, m.refresh()
		case "up", "k":
			if m.NavFocus {
				m.NavIndex = max(0, m.NavIndex-1)
			} else if m.Detail != nil {
				m.Offset = max(0, m.Offset-1)
			} else {
				m.Selected = max(0, m.Selected-1)
			}
		case "down", "j":
			if m.NavFocus {
				m.NavIndex = min(len(sections)-1, m.NavIndex+1)
			} else if m.Detail != nil {
				m.Offset++
			} else {
				m.Selected = max(0, min(len(m.rows())-1, m.Selected+1))
			}
		case "pgup":
			m.Offset = max(0, m.Offset-max(1, m.Height-10))
			m.Selected = max(0, m.Selected-max(1, m.Height-10))
		case "pgdown":
			m.Offset += max(1, m.Height-10)
			m.Selected = max(0, min(len(m.rows())-1, m.Selected+max(1, m.Height-10)))
		case "enter":
			if m.NavFocus {
				return m, m.page(m.NavIndex)
			}
			if m.Detail == nil {
				return m, m.showRow()
			}
		case "x":
			if m.Detail != nil {
				m.Raw = !m.Raw
				m.Offset = 0
			}
		case "s", "t", "b", "p", "u":
			if !m.Busy && (m.Section == 0 || m.Section == 1) {
				return m, m.preview(map[string]string{"s": "start", "t": "stop", "b": "reboot", "p": "pause", "u": "resume"}[key])
			}
		case "e":
			if !m.Busy && (m.Section == 0 || m.Section == 1) {
				return m, m.openResources()
			}
		case "c":
			if !m.Busy && (m.Section == 0 || m.Section == 1) {
				m.guided("capture")
			}
		case "g":
			if !m.Busy && (m.Section == 0 || m.Section == 1) {
				return m, m.openGuestTools()
			}
		case "n":
			if !m.Busy && m.Section == 6 {
				m.guided("repository-init")
			}
		case "v":
			if !m.Busy && m.Section == 6 {
				m.guided("repository-check")
			}
		}
	}
	return m, nil
}

func (m Workspace) color(s, code string) string {
	if m.NoColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
func clipCell(s string, n int) string { return ansi.Truncate(validation.SafeText(s), max(1, n), "...") }
func padCell(s string, n int) string {
	s = clipCell(s, n)
	return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s)))
}
func sizeNumber(v any) (float64, bool) {
	if n, ok := v.(json.Number); ok {
		f, err := n.Float64()
		return f, err == nil
	}
	n, ok := v.(float64)
	return n, ok
}
func sizeText(v any) string {
	n, ok := sizeNumber(v)
	if !ok {
		return "unknown"
	}
	for _, unit := range []string{"B", "KiB", "MiB", "GiB", "TiB"} {
		if n < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", n, unit)
		}
		n /= 1024
	}
	return "unknown"
}
func (m Workspace) status() string {
	if _, ok := m.Data["jobs"]; !ok {
		return "Loading jobs..."
	}
	if badge := m.jobBadge(); badge != "" {
		return strings.ToUpper(badge[:1]) + badge[1:]
	}
	return "No jobs running"
}

// jobBadge summarizes jobs for the header; empty when nothing is running or failing.
func (m Workspace) jobBadge() string {
	if m.Errors["jobs"] != "" {
		return "jobs unavailable"
	}
	active, attention := 0, 0
	for _, j := range array(m.Data["jobs"]) {
		state := rowState(j)
		if state == "failed" || state == "partial" || state == "recovery-required" || state == "interrupted" {
			attention++
		} else if !domain.Terminal(state) {
			active++
		}
	}
	parts := []string{}
	if active > 0 {
		parts = append(parts, countLabel(active, "job running", "jobs running"))
	}
	if attention > 0 {
		parts = append(parts, countLabel(attention, "job needs attention", "jobs need attention"))
	}
	return strings.Join(parts, ", ")
}

// header puts the page title on the left and jobs plus the connection on the right.
func (m Workspace) header(title string, width int, modal bool) string {
	left := m.color(" Virmill ", "1;35") + m.color(" / "+title, "1")
	connection := validation.SafeText(m.Connection)
	right := connection
	if badge := m.jobBadge(); badge != "" && !modal {
		right = badge + "   " + connection
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	if gap < 3 && right != connection {
		right = connection
		gap = width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	}
	return ansi.Truncate(left+strings.Repeat(" ", max(3, gap))+right, width, "")
}

// navLine lists the main sections in narrow terminals; the sidebar and Help list all.
func (m Workspace) navLine() string {
	parts := []string{}
	for _, i := range []int{0, 1, 2, 3, 6, 8, 10} {
		key := fmt.Sprint(i + 1)
		if i == 10 {
			key = ","
		}
		item := key + " " + sections[i]
		if i == m.Section {
			item = m.color(item, "1;35")
		}
		parts = append(parts, item)
	}
	return strings.Join(parts, "  ")
}

// screenHints lists the keys that matter on the current list or details page.
func (m Workspace) screenHints() string {
	switch {
	case m.Detail != nil:
		return "x Technical details   PgUp/PgDn Scroll   Esc Back   ? Help"
	case m.Section == 10:
		return "PgUp/PgDn Scroll   : All tools   ? Help"
	case workspaceKinds[m.Section] == "" || len(m.rows()) == 0:
		return ": All tools   ? Help"
	}
	arrows := "↑/↓"
	if m.ASCII {
		arrows = "Up/Down"
	}
	return arrows + " Select   Enter Open   / Search   : All tools   ? Help"
}

func (m Workspace) separator() string {
	if m.ASCII {
		return " - "
	}
	return " · "
}

func countLabel(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

var rowNouns = map[int][2]string{0: {"VM", "VMs"}, 1: {"VM", "VMs"}, 2: {"network", "networks"}, 3: {"storage pool", "storage pools"},
	6: {"recovery point", "recovery points"}, 7: {"USB device", "USB devices"}, 8: {"job", "jobs"}, 9: {"plugin", "plugins"}}

func (m Workspace) rowCounter(selected, total int) string {
	noun := rowNouns[m.Section]
	count := countLabel(total, noun[0], noun[1])
	if m.Search != "" {
		count = countLabel(total, "match", "matches")
	}
	return count + m.separator() + fmt.Sprintf("row %d of %d", selected+1, total)
}

// sectionMarks flag sections whose workflows are incomplete in this beta.
var sectionMarks = map[int]string{4: "not ready", 5: "validate only", 7: "discovery only", 9: "preview"}

var sectionPlaceholders = map[int][]string{
	4: {"Templates aren't available in this beta yet.", "", "Planned: turn a stopped VM into a reusable template and create clones from it."},
	5: {"Only checking is available for labs so far.", "", "Validate lab checks a lab file for mistakes without creating anything.",
		"Creating a lab's VMs and networks isn't available yet."},
}

// emptyState explains an empty list and the step that fills it.
func emptyState(section int) []string {
	switch section {
	case 0, 1:
		return []string{"No VMs yet.", "", "Create VM builds one from an installer ISO or prepared disks.", "Import brings in an existing disk image or appliance (OVA)."}
	case 2:
		return []string{"No networks yet.", "", "Create network sets up a private or NAT network for your VMs."}
	case 3:
		return []string{"No storage pools found.", "", "VM disks live in libvirt storage pools. Create one with your distribution's",
			"virtualization tools (for example virt-manager), then press r."}
	case 6:
		return []string{"No recovery points yet.", "", "Open a stopped VM and choose Capture to save one."}
	case 7:
		return []string{"No USB devices found.", "", "Plug a device in and press r. Attaching devices to VMs isn't available yet."}
	case 8:
		return []string{"No jobs yet.", "", "Changes you apply show up here while they run and after they finish."}
	case 9:
		return []string{"No plugins installed.", "", "Install plugin adds a signed plugin package."}
	}
	return []string{"Nothing to show here yet."}
}

// withoutEmptyDetails drops Details when an empty list has nothing to open.
func (m Workspace) withoutEmptyDetails(buttons []workspaceButton) []workspaceButton {
	if m.Detail != nil || len(m.rows()) > 0 {
		return buttons
	}
	out := []workspaceButton{}
	for _, b := range buttons {
		if b.key != "enter" {
			out = append(out, b)
		}
	}
	return out
}

func (m Workspace) hints() string {
	if m.Help {
		return "? or Esc Close help"
	}
	if m.Activity != nil {
		return "PgUp/PgDn Read   Tab Actions   Enter Choose   Esc Back to job"
	}
	if m.RemovalTarget != nil {
		return "Esc Back"
	}
	if m.Form != nil && m.Form.Kind == "remove-definition" && m.Plan == nil {
		if m.Form.removalIssueOpen {
			return "Up/Down Scroll   PgUp/PgDn Page   Esc Back"
		}
		return "Tab Next   Enter Choose   Ctrl-U Clear   F1 Full issue   Esc Back"
	}
	if m.AutostartTarget != nil {
		return "Esc Back"
	}
	if m.Form != nil && m.Form.Kind == "autostart" {
		return "Space Toggle   Tab Next   Enter Choose   Esc Back"
	}
	if m.NetworkForm != nil && m.Plan == nil && m.ExportForm == nil && m.Picker == nil && m.ActionForm == nil {
		if m.NetworkForm.issueOpen {
			return "Up/Down Scroll   PgUp/PgDn Page   Esc Back"
		}
		return "Tab/Arrows Select   Enter Choose   Ctrl-U Clear   Esc Back"
	}
	if m.Resources != nil && m.Plan == nil && m.Resources.Details {
		return "Up/Down Scroll   PgUp/PgDn Page   Esc Back"
	}
	if m.Resources != nil && m.Plan == nil {
		return "Tab/Arrows Select   Enter Next/Choose   Ctrl-U Clear   Esc Back"
	}
	if m.draftModal != "" {
		return "Enter Choose   Tab Next option   Esc Back"
	}
	if m.Picker == nil && m.Plan == nil && m.BackupRecovery != nil && m.ExportForm == nil {
		return "Tab Next   Enter Choose   Ctrl+O Browse   Esc Back"
	}
	if m.Picker == nil && m.Plan == nil && m.GuestAgent != nil {
		return "Tab/Arrows Select   Enter Choose   Esc Back"
	}
	if m.Picker == nil && m.Plan == nil && (m.Console != nil || m.ConsoleLoading) {
		return "Up/Down Select   Enter Open   Esc Back"
	}
	if m.Picker == nil && m.Plan == nil && m.Protection != nil {
		return "Tab Next   Left/Right Choose   Ctrl+O Browse   Esc Back"
	}
	if (m.Boot != nil || m.BootLoading) && m.Plan == nil {
		return "Tab Next option   Space Select   Left/Right Reorder   Esc Back"
	}
	if m.Picker != nil {
		if m.Picker.mode == "mkdir" {
			return "Enter Create folder   Esc Cancel"
		}
		// The file browser lists its own keys.
		return ""
	}
	if (m.Creation != nil || m.CreationPicking) && m.Plan == nil && m.ExportForm == nil {
		if m.Busy {
			return "Esc Back"
		}
		if m.Creation != nil {
			controls := m.Creation.controls()
			if len(controls) > 0 {
				switch controls[max(0, min(m.Creation.Focus, len(controls)-1))].kind {
				case "text":
					return "Type to edit   Ctrl+U Clear   Tab Next option   Esc Back"
				case "choice":
					return "Left/Right Choose   Tab Next option   Esc Back"
				}
			}
		}
		return "Tab Next option   Enter Select   Esc Back"
	}
	if m.ExportForm != nil {
		return "Ctrl+O Choose folder   Enter Export   Esc Back"
	}
	if m.Import != nil && m.Plan == nil {
		if m.Busy && m.Pending["import-inspect"] != 0 {
			return "Enter / Esc Cancel inspection"
		}
		if m.Busy {
			return "Esc Cancel preview"
		}
		controls := m.Import.controls()
		if len(controls) > 0 {
			switch controls[max(0, min(m.Import.Focus, len(controls)-1))].kind {
			case "text":
				return "Type to edit   Tab Next option   Esc Back"
			case "path":
				return "Enter Browse   Tab Next option   Esc Back"
			case "toggle":
				return "Space Toggle   Tab Next option   Esc Back"
			case "choice":
				return "Left/Right Choose   Tab Next option   Esc Back"
			}
		}
		return "Enter Select   Tab Next option   Esc Back"
	}
	if m.ActionForm != nil {
		return m.formHints()
	}
	if m.Advanced && m.CatalogSearching {
		return "Type to find an action   Enter Keep matches   Esc Clear"
	}
	if m.Advanced {
		return "[ Enter Select ]   [ / Find action ]   [ Esc Back ]"
	}
	if m.Form != nil {
		if m.Form.Kind == "guest-tools" {
			return "Tab/Arrows Select   Enter Next/Choose   Ctrl+O Browse   Esc Back"
		}
		return m.formHints()
	}
	if m.Plan != nil {
		if m.Reviewing {
			return "Space Check   Tab Next   Enter Continue   Esc Review"
		}
		return "PgUp/PgDn Read plan   Enter Review & apply   Esc Cancel"
	}
	if m.ButtonFocus {
		return "Left/Right selects a button. Enter activates it; Tab moves focus."
	}
	if m.NavFocus {
		return "Up/Down Choose section   Enter Open   Tab Back to content   q Quit"
	}
	if m.Searching {
		return "Type a name, state or ID   Enter Keep filter   Esc Clear"
	}
	return m.screenHints()
}
func (m Workspace) content(width, height int) []string {
	if m.RemovalTarget != nil {
		return pageLines([]string{"Remove VM, keep disks", "Reading the selected VM...", "", "Esc Back"}, width, height, 0)
	}
	if m.AutostartTarget != nil {
		return pageLines([]string{"Automatic startup", "Reading the VM's current setting...", "", "Esc Back"}, width, height, 0)
	}
	if m.NetworkForm != nil && m.Plan == nil && m.ExportForm == nil && m.Picker == nil && m.ActionForm == nil {
		return m.NetworkForm.View(width, height)
	}
	if m.Resources != nil && m.Plan == nil {
		return m.resourcesView(width, height)
	}
	if m.draftModal != "" {
		return m.setupView(width, height)
	}
	if m.BackupRecovery != nil && m.Plan == nil && m.Picker == nil && m.ExportForm == nil {
		return m.backupRecoveryView(width, height)
	}
	if m.GuestAgent != nil && m.Plan == nil && m.Picker == nil {
		return m.guestAgentView(width, height)
	}
	if m.Console != nil || m.ConsoleLoading {
		return m.consoleView(width, height)
	}
	if m.Protection != nil && m.Plan == nil && m.Picker == nil {
		return strings.Split(m.Protection.View(width, height), "\n")
	}
	if (m.Boot != nil || m.BootLoading) && m.Plan == nil {
		if m.BootLoading {
			return []string{"Reading boot devices...", "Esc cancels this view."}
		}
		return strings.Split(m.Boot.View(width, height), "\n")
	}
	if m.Picker != nil {
		return strings.Split(m.Picker.View(width, height), "\n")
	}
	if m.Help {
		return []string{"Keyboard guide", "", "1 Overview  2 VMs  3 Networks  4 Storage  5 Templates", "6 Labs  7 Protection  8 Devices  9 Jobs  0 Plugins  , Settings", "", "Tab cycles content, action buttons and section navigation.", "Arrow keys select rows. Enter opens full resource details.", "/ searches names, states and complete resource IDs.", "r refreshes observations; x toggles raw data in details.", "VMs: s start, t graceful stop, b reboot, p pause, u resume.", "VMs: e CPU/RAM, c cold capture, g guest tools.", "Every VM change opens a review before it can be submitted.", ": opens All tools; a groups more tasks for this section.", "Esc goes back. q/Ctrl-C detach; accepted jobs keep running.", "", "? or Esc closes this help."}
	}
	if m.Activity != nil {
		return m.Activity.View(width, height)
	}
	if m.ExportForm != nil {
		return m.importExportView(width, height)
	}
	if (m.Creation != nil || m.CreationPicking) && m.Plan == nil {
		return m.creationView(width, height)
	}
	if m.Import != nil && m.Plan == nil {
		if m.Busy && m.Pending["import-inspect"] != 0 {
			return m.importBusyView(width, height)
		}
		return strings.Split(m.Import.View(width, height), "\n")
	}
	if (m.Creation != nil || m.CreationPicking) && m.Plan == nil {
		return m.creationView(width, height)
	}
	if m.ActionForm != nil {
		return strings.Split(m.ActionForm.View(width, height), "\n")
	}
	if m.Advanced {
		return m.catalogLines(width, height)
	}
	if m.Form != nil {
		target := []string{}
		if m.Form.VM.Key.UUID != "" && m.Form.Kind != "autostart" && m.Form.Kind != "remove-definition" {
			target = pageLines([]string{"VM: " + m.Form.VM.Name, ""}, width, height, 0)
		}
		return append(target, strings.Split(m.Form.View(width, max(6, height-len(target))), "\n")...)
	}
	if m.Plan != nil {
		if m.Reviewing {
			return m.confirmationLines(width, height)
		}
		lines := []string{}
		if m.Error != "" {
			lines = append(lines, "Submission needs attention", "", validation.SafeText(m.Error), "")
			if strings.Contains(m.Error, "helper-key.pem") {
				lines = append(lines, "A host administrator must finish helper setup before this action can run.", "See the installed network-helper.md and managed-volume-access.md guides.")
			}
			lines = append(lines, "Your settings and reviewed plan are kept. Check Jobs before trying again; an accepted job may still be running.", "PgUp/PgDn reads the complete issue and plan. Esc returns to your settings.", "")
		}
		lines = append(lines, PlanDetails(*m.Plan, width)...)
		return pageLines(lines, width, height, m.Offset)
	}
	if m.Detail != nil {
		if m.Section == 8 && field(m.Detail, "state") != "" && !m.Raw {
			return pageLines(m.jobDetails(width), width, height, m.Offset)
		}
		lines := []string{m.DetailTitle, ""}
		if m.Section == 1 && m.DetailTitle == "VM details" && !m.Raw {
			vm := m.selectedVM()
			autostart := "Off"
			if vm.Autostart {
				autostart = "On"
			}
			lines = append(lines, "Name: "+validation.SafeText(vm.Name), "State: "+validation.SafeText(vm.State), "UUID: "+vm.Key.UUID, "", "Automatic startup: "+autostart)
			lines = append(lines, "")
			lines = append(lines, m.resourceSummaryLines(vm)...)
			if vm.HasManagedSave {
				lines = append(lines, "Saved session: available; starting resumes the saved runtime")
			}
			if len(vm.Tags) > 0 {
				lines = append(lines, "Tags: "+validation.SafeText(strings.Join(vm.Tags, ", ")))
			}
			return pageLines(lines, width, height, m.Offset)
		}
		if m.Raw {
			lines[0] += m.separator() + "technical details"
			lines = append(lines, technicalLines(m.Detail, width)...)
		} else {
			lines = append(lines, m.readableDetails(width)...)
		}
		return pageLines(lines, width, height, m.Offset)
	}
	if m.connectionRecoveryVisible() {
		return m.connectionRecoveryView(width, height)
	}
	lines := []string{}
	kind := workspaceKinds[m.Section]
	if m.Section == 0 {
		vms := array(m.Data["vms"])
		running, stopped := 0, 0
		for _, vm := range vms {
			if rowState(vm) == "running" {
				running++
			}
			if rowState(vm) == "stopped" || rowState(vm) == "shut off" {
				stopped++
			}
		}
		if _, ok := m.Data["vms"]; ok {
			lines = append(lines, m.color(fmt.Sprintf("  %d virtual machines     %d running     %d stopped", len(vms), running, stopped), "1;36"))
		} else {
			lines = append(lines, "  Loading your virtual machines...")
		}
		if len(m.Errors["jobs"]) > 0 {
			lines = append(lines, "Jobs unavailable: "+m.Errors["jobs"])
		} else {
			lines = append(lines, "  "+m.status())
		}
		free := float64(0)
		known := false
		for _, pool := range array(m.Data["pools"]) {
			if object(pool)["active"] == true {
				if n, ok := sizeNumber(object(pool)["availableBytes"]); ok {
					free += n
					known = true
				}
			}
		}
		if known {
			lines = append(lines, "  Available pool storage: "+sizeText(free))
		}
		lines = append(lines, "", m.color("Virtual machines", "1"))
	} else {
		lines = append(lines, m.color(sections[m.Section], "1"))
	}
	if kind == "" {
		return append(append(lines, ""), sectionPlaceholders[m.Section]...)
	}
	if err := m.Errors[kind]; err != "" {
		return append(lines, "", "Could not load "+sections[m.Section]+":", err, "", "Press r to retry. The existing selection is retained.")
	}
	if m.Searching || m.Search != "" {
		lines = append(lines, "Search: "+m.Search)
	}
	if m.Section == 10 {
		return pageLines(append(lines, HumanDetails(m.Data[kind], width)...), width, height, m.Offset)
	}
	rows := m.rows()
	if _, loaded := m.Data[kind]; !loaded {
		return append(lines, "", "Loading observations...")
	}
	if len(rows) == 0 {
		if m.Search != "" {
			return append(lines, "", "No resources match this search. Esc clears it.")
		}
		return append(append(lines, ""), emptyState(m.Section)...)
	}
	count := max(1, height-len(lines)-3)
	selected := max(0, min(m.Selected, len(rows)-1))
	lines = append(lines, m.tableLines(rows, selected, width, count)...)
	if m.Selected < 0 {
		lines = append(lines, "", "No resource selected. Use arrows to choose.")
	} else {
		lines = append(lines, "", m.rowCounter(selected, len(rows)))
	}
	return lines
}
func pageLines(lines []string, width, height, offset int) []string {
	out := []string{}
	for _, line := range lines {
		out = append(out, wrap(validation.SafeText(line), width)...)
	}
	offset = max(0, min(offset, max(0, len(out)-max(1, height))))
	return out[offset:min(len(out), offset+max(1, height))]
}
func (m Workspace) View() string {
	if m.Quit || m.Width <= 0 || m.Height <= 0 {
		return ""
	}

	if m.Width < 60 || m.Height < 18 {
		return strings.Join(pageLines([]string{"Virmill", "Resize to at least 60 x 18.", "Your selection and input are retained.", "Ctrl-C detaches."}, m.Width, m.Height, 0), "\n")
	}
	width, height := m.Width, m.Height
	rule := "─"
	if m.ASCII {
		rule = "-"
	}
	importModal := (m.Activity != nil || m.RemovalTarget != nil || m.Form != nil && m.Form.Kind == "remove-definition" || m.AutostartTarget != nil || m.Form != nil && m.Form.Kind == "autostart" || m.NetworkForm != nil || m.Resources != nil || m.draftModal != "" || m.Import != nil || m.Creation != nil || m.CreationPicking || m.Protection != nil || m.BackupRecovery != nil || m.GuestAgent != nil || m.Console != nil || m.ConsoleLoading) && m.Plan == nil
	title := sections[m.Section]
	if importModal {
		title = "Import"
		if m.Activity != nil {
			title = "Job activity"
		}
		if m.RemovalTarget != nil || m.Form != nil && m.Form.Kind == "remove-definition" {
			title = "Remove VM"
		}
		if m.AutostartTarget != nil || m.Form != nil && m.Form.Kind == "autostart" {
			title = "Automatic startup"
		}
		if m.NetworkForm != nil {
			title = "Create network"
		}
		if (m.Creation != nil || m.CreationPicking) && m.NetworkForm == nil {
			title = "Create VM"
		}
		if m.Protection != nil || m.BackupRecovery != nil {
			title = "Protection"
		}
		if m.Resources != nil {
			title = "CPU and RAM"
		}
		if m.GuestAgent != nil {
			title = "Guest tools"
		}
		if m.Console != nil || m.ConsoleLoading {
			title = "Console"
		}
	}
	header := m.header(title, width, importModal)
	lines := []string{ansi.Truncate(header, width, ""), m.color(strings.Repeat(rule, width), "2")}
	sidebar := 0
	if width >= 105 && !importModal {
		sidebar = 30
	}
	bodyWidth := width - sidebar
	if sidebar > 0 {
		bodyWidth--
	}
	footer := m.footerButtons()
	bodyHeight := height - 6 - len(footer)
	content := m.content(bodyWidth, bodyHeight)
	for i := 0; i < bodyHeight; i++ {
		row := ""
		if i < len(content) {
			row = content[i]
		}
		row = ansi.Truncate(row, bodyWidth, "")
		if sidebar > 0 {
			label := ""
			if i < len(sections) {
				shortcut := fmt.Sprint(i + 1)
				if i == 9 {
					shortcut = "0"
				}
				if i == 10 {
					shortcut = ","
				}
				plain := " " + shortcut + "  " + sections[i]
				mark := sectionMarks[i]
				if mark != "" {
					plain = fmt.Sprintf(" %s  %-10s %s", shortcut, sections[i], mark)
				}
				label = plain
				if mark != "" {
					label = strings.TrimSuffix(plain, mark) + m.color(mark, "2")
				}
				if i == m.Section {
					label = m.color(padCell(plain, sidebar), "1;35")
				}
				if m.NavFocus && i == m.NavIndex {
					label = m.color(padCell(">"+strings.TrimPrefix(plain, " "), sidebar), "1;30;46")
				}
			}
			divider := "│"
			if m.ASCII {
				divider = "|"
			}
			row = label + strings.Repeat(" ", max(0, sidebar-ansi.StringWidth(label))) + m.color(divider, "2") + row
		}
		lines = append(lines, row)
	}
	if sidebar == 0 {
		nav := m.navLine()
		if m.Help {
			// The keyboard guide lists every section.
			nav = ""
		}
		if m.NavFocus {
			nav = "Section: " + sections[m.NavIndex] + "   Up/Down to choose, Enter opens"
		}
		if importModal {
			nav = ""
		}
		lines = append(lines, ansi.Truncate(nav, width, ""))
	} else {
		lines = append(lines, "")
	}
	message := m.Notice
	if m.draftError != "" {
		message = m.draftError
	}
	if m.Error != "" {
		message = "Error: " + m.Error
	}
	if m.Busy {
		message = "Working... " + message
	}
	// Menus, forms and Help list their own keys in the footer.
	if message == "" && !m.overlay() {
		message = m.screenHints()
	}
	if importModal && m.Pending["import-inspect"] != 0 {
		message = ""
	}
	lines = append(lines, clipCell(message, width), m.color(strings.Repeat(rule, width), "2"))
	for _, row := range footer {
		lines = append(lines, ansi.Truncate(row, width, ""))
	}
	return strings.Join(lines[:min(height, len(lines))], "\n")
}

type workspaceButton struct{ label, key string }

func (m Workspace) buttons() []workspaceButton {
	if m.connectionRecoveryVisible() {
		return m.connectionRecoveryButtons()
	}
	if m.Section == 8 && m.Detail == nil && m.CreationNetwork != nil && m.CreationNetwork.JobID != "" {
		return []workspaceButton{{"Back to VM setup", "creation-network-return"}, {"Details", "enter"}, {"Refresh", "r"}}
	}
	if m.Section == 8 && m.Detail != nil && field(m.Detail, "state") != "" {
		return m.jobButtons()
	}
	if m.Section == 0 || m.Section == 1 {
		out := []workspaceButton{{"Details", "enter"}}
		vm := m.selectedVM()
		if vm.Key.UUID != "" {
			switch vm.State {
			case "running":
				out = append(out, workspaceButton{"Shut down", "t"})
			case "paused":
				out = append(out, workspaceButton{"Resume", "u"})
			case "stopped", "shut off":
				out = append(out, workspaceButton{"Start", "s"})
			}
		}
		if m.Detail != nil {
			return append(out[1:], workspaceButton{"Console", "action:vm console show"}, workspaceButton{"CPU / RAM", "e"}, workspaceButton{"Boot / installer", "action:vm boot set"}, workspaceButton{"Guest tools", "g"}, workspaceButton{"Capture", "c"}, workspaceButton{"More", "a"}, workspaceButton{"Back", "esc"})
		}
		return m.withoutEmptyDetails(append(out, workspaceButton{"Create VM", "action:vm create"}, workspaceButton{"Import", "i"}, workspaceButton{"More", "a"}))
	}
	// Details pages offer actions on the open resource, not the list.
	if m.Detail != nil {
		out := []workspaceButton{}
		if m.Section == 6 {
			out = append(out, workspaceButton{"Restore", "action:snapshot restore"}, workspaceButton{"Back up", "action:backup create"})
		}
		return append(out, workspaceButton{"More", "a"}, workspaceButton{"Back", "esc"})
	}
	primary := map[int][]workspaceButton{
		2:  {{"Details", "enter"}, {"Create network", "action:network create"}},
		3:  {{"Details", "enter"}, {"Refresh", "r"}},
		4:  {},
		5:  {{"Validate lab", "action:lab validate"}},
		6:  {{"Details", "enter"}, {"Restore", "action:snapshot restore"}, {"Back up", "action:backup create"}, {"New repository", "n"}},
		7:  {{"USB devices", "action:device usb list"}, {"PCI devices", "action:host pci list"}},
		8:  {{"Details", "enter"}, {"Create VM", "action:vm create"}, {"Activity", "job-activity"}, {"Refresh", "r"}},
		9:  {{"Install plugin", "action:plugin install"}, {"Refresh", "r"}},
		10: {{"Host capabilities", "action:host capabilities"}, {"All tools", ":"}},
	}
	return m.withoutEmptyDetails(append(primary[m.Section], workspaceButton{"More", "a"}))
}

// overlay reports a menu, form or modal that shows its own keys instead of buttons.
func (m Workspace) overlay() bool {
	return m.draftModal != "" || m.Activity != nil || m.RemovalTarget != nil || m.AutostartTarget != nil || m.NetworkForm != nil || m.Resources != nil || m.Console != nil || m.ConsoleLoading || m.Protection != nil || m.BackupRecovery != nil || m.GuestAgent != nil || m.Boot != nil || m.BootLoading || m.Creation != nil || m.CreationPicking || m.Import != nil || m.ExportForm != nil || m.Picker != nil || m.Form != nil || m.ActionForm != nil || m.Advanced || m.Plan != nil || m.Searching || m.NavFocus || m.Help
}

func (m Workspace) footerButtons() []string {
	if m.overlay() {
		return []string{m.hints()}
	}
	buttons := m.buttons()
	labels := make([]string, len(buttons))
	for i, b := range buttons {
		key := b.key
		switch {
		case strings.HasPrefix(key, "action:") || strings.HasPrefix(key, "job-") || strings.HasPrefix(key, "connection-"):
			key = ""
		case key == "enter":
			key = "Enter"
		case key == "esc":
			key = "Esc"
		}
		labels[i] = "[ " + strings.TrimSpace(key+" "+b.label) + " ]"
	}
	selected := min(m.ButtonIndex, len(buttons)-1)
	render := func(i int) string {
		if m.ButtonFocus && i == selected {
			return m.color(">"+labels[i], "1;30;46")
		}
		return m.color(labels[i], "36")
	}
	// Wrap onto a second row rather than hiding buttons off screen.
	rows, used := [][]int{{}}, 0
	for i, label := range labels {
		width := ansi.StringWidth(label) + 2
		if len(rows[len(rows)-1]) > 0 && used+width > m.Width {
			rows, used = append(rows, []int{}), 0
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], i)
		used += width
	}
	if len(rows) <= 2 {
		out := make([]string, len(rows))
		for r, row := range rows {
			parts := make([]string, len(row))
			for j, i := range row {
				parts[j] = render(i)
			}
			out[r] = strings.Join(parts, " ")
		}
		return out
	}
	// Very narrow terminals keep one scrolling row with the focused button visible.
	start := 0
	if m.ButtonFocus {
		for start < selected && ansi.StringWidth(strings.Join(labels[start:selected+1], " "))+4 > m.Width {
			start++
		}
	}
	out := ""
	if start > 0 {
		out = "< "
	}
	for i := start; i < len(labels); i++ {
		if ansi.StringWidth(out)+ansi.StringWidth(labels[i])+4 > m.Width {
			out += " >"
			break
		}
		out += render(i) + " "
	}
	return []string{strings.TrimSpace(out)}
}
