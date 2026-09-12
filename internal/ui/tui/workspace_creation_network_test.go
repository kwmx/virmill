package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// These tests cover frontend state transitions and shared-service requests.
// They do not qualify native network creation, guest routing or isolation.
func creationNetworkWorkspace(t *testing.T, beforePreparation bool) Workspace {
	t.Helper()
	m := fixtureWorkspace()
	if beforePreparation {
		m = importWorkspace(t)
	}
	f := creationComplete(creationFormFixture())
	f.BeforePreparation = beforePreparation
	if beforePreparation {
		f.OperationID = ""
	}
	f.CPUText, f.MemoryText = "7", "6144"
	f.Spec.Name = "keep-exact-vm-choices"
	f.Page, f.NIC = 2, 1
	f.Spec.NICs[0].NetworkID = ""
	f.Spec.NICs[0].Link = "down"
	f.Spec.NICs[1].NetworkID = creationFormNetwork
	m.Creation = &f
	if m.Import != nil {
		m.Import.VM = &f
		m.Import.VMBinding = creationDraftBinding(m.Import.Draft)
	}
	m.Client = &workspaceClient{}
	return m
}

func TestCreationNetworkCancelPreservesPreparedAndUnpreparedSettings(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "before-preparation"}[before], func(t *testing.T) {
			m := creationNetworkWorkspace(t, before)
			creation, imported, snapshot := m.Creation, m.Import, m.setupDocument()
			_ = m.openCreationNetwork()
			if m.CreationNetwork == nil || m.NetworkForm == nil || m.Creation != nil || m.Import != nil {
				t.Fatal("network creation did not suspend the exact VM setup")
			}
			if !reflect.DeepEqual(m.setupDocument(), snapshot) {
				t.Fatal("suspended draft lost source binding or editable settings")
			}
			m.NetworkForm.Name = "new-isolated-lab"
			m, cmd := wk(m, "esc")
			if cmd != nil || m.CreationNetwork != nil || m.NetworkForm != nil || !reflect.DeepEqual(m.Creation, creation) || !reflect.DeepEqual(m.Import, imported) || !reflect.DeepEqual(m.setupDocument(), snapshot) {
				t.Fatal("cancel lost original prepared/import choices")
			}
			if len(m.Client.(*workspaceClient).calls) != 0 {
				t.Fatal("opening/canceling network setup invoked a service")
			}
		})
	}
}

func TestCreationNetworkRefreshReadsOnlyNetworksAndDoesNotSelectOrRebuildSource(t *testing.T) {
	m := creationNetworkWorkspace(t, true)
	before, source, options, pools := m.setupDocument(), m.Creation.Source, m.Creation.Options, m.Creation.Pools
	newNetwork := domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "network", UUID: "44444444-4444-4444-8444-444444444444"}, Name: "new-network", Active: true}
	c := &workspaceClient{response: app.Response{Data: []domain.VirtualNetwork{newNetwork}}}
	m.Client = c
	cmd := m.refreshCreationNetworks()
	if cmd == nil {
		t.Fatal("network refresh has no read command")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if !reflect.DeepEqual(c.calls, []string{"network.list"}) || len(c.requests) != 1 || c.requests[0].ID != "" || c.requests[0].Action != "" || c.requests[0].Path != "" || c.requests[0].Apply != nil || len(c.requests[0].Input) != 0 {
		t.Fatal("refresh gained source reads or mutation authority", c.calls, c.requests)
	}
	if m.Creation == nil || !reflect.DeepEqual(m.setupDocument(), before) || !reflect.DeepEqual(m.Creation.Source, source) || !reflect.DeepEqual(m.Creation.Options, options) || !reflect.DeepEqual(m.Creation.Pools, pools) {
		t.Fatal("refresh replaced original source, hardware or selected NIC mappings")
	}
	if len(m.Creation.Networks) != 1 || m.Creation.Networks[0].Key.UUID != newNetwork.Key.UUID || m.Creation.Spec.NICs[0].NetworkID != "" || m.Creation.Spec.NICs[1].NetworkID != creationFormNetwork {
		t.Fatal("fresh networks missing or implicitly selected")
	}
}

func TestCreationNetworkRefreshIgnoresStaleReplyAndCanceledWizard(t *testing.T) {
	m := creationNetworkWorkspace(t, false)
	observed := domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "network", UUID: creationFormNetwork}, Name: "old", Active: true}
	c := &workspaceClient{response: app.Response{Data: []domain.VirtualNetwork{observed}}}
	m.Client = c
	old := m.refreshCreationNetworks()
	if old == nil {
		t.Fatal("missing old refresh")
	}
	oldReply := old()
	if duplicate := m.refreshCreationNetworks(); duplicate != nil {
		t.Fatal("duplicate pending refresh was issued")
	}
	m.cancelCreationNetworkRefresh()
	observed.Name = "fresh"
	c.response = app.Response{Data: []domain.VirtualNetwork{observed}}
	fresh := m.refreshCreationNetworks()
	next, _ := m.Update(fresh())
	m = next.(Workspace)
	next, _ = m.Update(oldReply)
	m = next.(Workspace)
	if m.Creation == nil || len(m.Creation.Networks) != 1 || m.Creation.Networks[0].Name != "fresh" {
		t.Fatal("old network reply replaced a newer observation")
	}
	late := m.refreshCreationNetworks()
	m, _ = wk(m, "esc") // Cancel the pending read without discarding settings.
	for i := 0; i < 4 && m.Creation != nil; i++ {
		m, _ = wk(m, "esc") // Back through wizard pages, then leave the setup.
	}
	if m.Creation != nil {
		t.Fatal("cannot leave canceled creation wizard")
	}
	m.page(1)
	next, _ = m.Update(late())
	m = next.(Workspace)
	if m.Creation != nil || m.CreationNetwork != nil || m.NetworkForm != nil || m.Section != 1 {
		t.Fatal("late network reply reopened a canceled VM wizard")
	}
}

const creationNetworkJob = "55555555-5555-4555-8555-555555555555"

func acceptCreationNetworkJob(t *testing.T, m Workspace) Workspace {
	t.Helper()
	_ = m.openCreationNetwork()
	plan := testWorkspacePlan(t)
	plan.Operation = "network.create"
	m.Plan = &plan
	m.Pending["apply"] = 600
	m.Busy = true
	next, _ := m.Update(workspaceReply{Kind: "apply", Token: 600, Response: app.Response{Data: domain.Job{ID: creationNetworkJob, PlanID: plan.ID, State: "running"}}})
	return next.(Workspace)
}

func TestCreationNetworkAcceptedJobDoesNotSubmitImagePreparation(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "before-preparation"}[before], func(t *testing.T) {
			m := creationNetworkWorkspace(t, before)
			original := m.setupDocument()
			m.draftSaved = original
			m.PendingPreparation = "66666666-6666-4666-8666-666666666666"
			pending := m.PendingPreparation
			m = acceptCreationNetworkJob(t, m)
			if m.CreationNetwork == nil || m.Creation != nil || m.Import != nil || m.NetworkForm != nil || m.Plan != nil || m.Busy || m.Section != 8 || resourceID(m.Detail) != creationNetworkJob {
				t.Fatal("accepted network operation did not show its own job")
			}
			if m.PendingPreparation != pending || m.SavedCreation != nil || m.draftSubmitted || !reflect.DeepEqual(m.draftSaved, original) || !reflect.DeepEqual(m.setupDocument(), original) || len(m.PreparedBasics) != 0 {
				t.Fatal("network job contaminated preparation identity or durable setup state")
			}
		})
	}
}

func TestCreationNetworkPendingApplyCannotReturnToVMSetup(t *testing.T) {
	m := creationNetworkWorkspace(t, true)
	original := m.setupDocument()
	_ = m.openCreationNetwork()
	m.Pending["apply"] = 100
	m.Busy = true
	if cmd := m.returnCreationNetwork(false); cmd != nil || m.CreationNetwork == nil || m.Creation != nil {
		t.Fatal("direct return bypassed pending submission")
	}
	for _, key := range []string{"esc", "enter", "tab"} {
		var cmd tea.Cmd
		m, cmd = wk(m, key)
		if cmd != nil || m.CreationNetwork == nil || m.NetworkForm == nil || m.Creation != nil || m.Import != nil || m.Pending["apply"] != 100 || !m.Busy || !strings.Contains(m.Notice, "Submission is pending") || !reflect.DeepEqual(m.setupDocument(), original) {
			t.Fatal("pending network submission escaped as an unsubmitted draft", key)
		}
	}
}

func TestCreationNetworkRefreshBindsOriginalSourceAndConnection(t *testing.T) {
	for _, changed := range []string{"source", "connection"} {
		t.Run(changed, func(t *testing.T) {
			m := creationNetworkWorkspace(t, true)
			networks := m.Creation.Networks
			m.Client = &workspaceClient{response: app.Response{Data: []domain.VirtualNetwork{}}}
			cmd := m.refreshCreationNetworks()
			if changed == "source" {
				m.Creation.Source.System.Name = "different-source"
			} else {
				m.Connection = "qemu:///session"
			}
			next, _ := m.Update(cmd())
			m = next.(Workspace)
			if m.Creation == nil || !reflect.DeepEqual(m.Creation.Networks, networks) || !strings.Contains(m.Creation.Error, "changed") || m.Busy {
				t.Fatal("late read for another source/connection replaced choices")
			}
		})
	}
}

func TestCreationNetworkSubmissionPersistsEditingSetupBeforeSending(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "before-preparation"}[before], func(t *testing.T) {
			m := creationNetworkWorkspace(t, before)
			original := m.setupDocument()
			store, _ := newTestDraftStore(t)
			m.draftWriter = &setupWriter{store: store}
			_ = m.openCreationNetwork()
			plan := testWorkspacePlan(t)
			plan.Operation = "network.create"
			m.Plan = &plan
			m.Pending["apply"] = 950
			m.Busy = true
			sent := false
			cmd := m.guardSetupApply(func() tea.Msg {
				var persisted SavedSetupDocument
				if _, err := store.Load("import", &persisted); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(&persisted, original) || persisted.State != "editing" || persisted.OperationID != "" || persisted.Creation.OperationID != original.Creation.OperationID {
					t.Fatal("network submission persisted preparation as submitted or changed its identity", persisted)
				}
				sent = true
				return workspaceReply{Kind: "apply", Token: 950, Response: app.Response{Data: domain.Job{ID: creationNetworkJob, PlanID: plan.ID, State: "running"}}}
			})
			if cmd == nil || sent || m.draftSubmitted {
				t.Fatal("network guard bypassed editing-state save barrier")
			}
			next, _ := m.Update(cmd())
			m = next.(Workspace)
			if !sent || m.draftSubmitted || !reflect.DeepEqual(m.setupDocument(), original) || m.PendingPreparation != "" {
				t.Fatal("accepted network job altered persisted VM setup identity")
			}
		})
	}
}

func TestCreationNetworkOnlyMatchingSuccessfulJobReturnsAndRefreshes(t *testing.T) {
	m := creationNetworkWorkspace(t, false)
	original := m.setupDocument()
	c := &workspaceClient{response: app.Response{Data: []domain.VirtualNetwork{{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "network", UUID: creationFormNetwork}, Name: "fresh-network", Active: true}}}}
	m.Client = c
	m = acceptCreationNetworkJob(t, m)
	m.Pending["job-update"] = 701
	next, cmd := m.Update(workspaceReply{Kind: "job-update", Token: 701, Response: app.Response{Data: domain.Job{ID: "77777777-7777-4777-8777-777777777777", State: "succeeded"}}})
	m = next.(Workspace)
	if cmd != nil || m.CreationNetwork == nil || m.Creation != nil {
		t.Fatal("another network job stole the suspended wizard")
	}
	m.Pending["job-update"] = 702
	next, cmd = m.Update(workspaceReply{Kind: "job-update", Token: 702, Response: app.Response{Data: domain.Job{ID: creationNetworkJob, PlanID: m.CreationNetwork.PlanID, State: "succeeded"}}})
	m = next.(Workspace)
	if cmd == nil || m.CreationNetwork != nil || m.Creation == nil || m.NetworkForm != nil || !reflect.DeepEqual(m.setupDocument(), original) {
		t.Fatal("matching success did not restore original VM setup before refreshing networks")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if !reflect.DeepEqual(c.calls, []string{"network.list"}) || !reflect.DeepEqual(m.setupDocument(), original) {
		t.Fatal("network success reread source, submitted VM or selected a network", c.calls)
	}
}

func TestCreationNetworkFailedJobOffersBackWithoutLosingChoices(t *testing.T) {
	m := creationNetworkWorkspace(t, true)
	original := m.setupDocument()
	m = acceptCreationNetworkJob(t, m)
	m.Pending["job-update"] = 801
	next, _ := m.Update(workspaceReply{Kind: "job-update", Token: 801, Response: app.Response{Data: domain.Job{ID: creationNetworkJob, PlanID: m.CreationNetwork.PlanID, State: "failed", Error: domain.Fail("UNSUPPORTED_CAPABILITY", "fixture network policy unavailable")}}})
	m = next.(Workspace)
	if m.CreationNetwork == nil || m.Creation != nil || resourceID(m.Detail) != creationNetworkJob || !reflect.DeepEqual(m.setupDocument(), original) {
		t.Fatal("network failure lost its job or suspended setup")
	}
	index := -1
	for i, button := range m.buttons() {
		if button.label == "Back to VM setup" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("failed network job has no Back to VM setup action")
	}
	m.ButtonFocus, m.ButtonIndex = true, index
	m, _ = wk(m, "enter")
	if m.CreationNetwork != nil || m.Creation == nil || m.Import == nil || m.NetworkForm != nil || !reflect.DeepEqual(m.setupDocument(), original) {
		t.Fatal("Back after failed network job lost original source/settings")
	}
}

func TestCreationNetworkSuccessDoesNotInterruptActiveOverlayOrSubmission(t *testing.T) {
	for _, overlay := range []string{"help", "plan", "action-form", "form", "picker", "export", "advanced", "busy", "pending-apply", "network-form", "import", "creation", "creation-picker", "draft-modal", "boot", "protection", "guest-agent", "resources"} {
		t.Run(overlay, func(t *testing.T) {
			m := acceptCreationNetworkJob(t, creationNetworkWorkspace(t, false))
			handoff := m.CreationNetwork
			var stillActive func(Workspace) bool
			switch overlay {
			case "help":
				m.Help = true
				stillActive = func(m Workspace) bool { return m.Help }
			case "plan":
				m.Plan = &domain.Plan{ID: "another-plan"}
				stillActive = func(m Workspace) bool { return m.Plan != nil && m.Plan.ID == "another-plan" }
			case "action-form":
				m.ActionForm = &ActionForm{}
				stillActive = func(m Workspace) bool { return m.ActionForm != nil }
			case "form":
				m.Form = &GuidedForm{}
				stillActive = func(m Workspace) bool { return m.Form != nil }
			case "picker":
				m.Picker = &FilePicker{}
				stillActive = func(m Workspace) bool { return m.Picker != nil }
			case "export":
				m.ExportForm = &GuidedForm{}
				stillActive = func(m Workspace) bool { return m.ExportForm != nil }
			case "advanced":
				m.Advanced = true
				stillActive = func(m Workspace) bool { return m.Advanced }
			case "busy":
				m.Busy = true
				stillActive = func(m Workspace) bool { return m.Busy }
			case "pending-apply":
				m.Pending["apply"] = 123
				stillActive = func(m Workspace) bool { return m.Pending["apply"] == 123 }
			case "network-form":
				m.NetworkForm = &NetworkForm{}
				stillActive = func(m Workspace) bool { return m.NetworkForm != nil }
			case "import":
				m.Import = &ImportForm{}
				stillActive = func(m Workspace) bool { return m.Import != nil }
			case "creation":
				m.Creation = &CreationForm{CPUText: "other-setup"}
				stillActive = func(m Workspace) bool { return m.Creation != nil && m.Creation.CPUText == "other-setup" }
			case "creation-picker":
				m.CreationPicking = true
				stillActive = func(m Workspace) bool { return m.CreationPicking }
			case "draft-modal":
				m.draftModal = "import"
				stillActive = func(m Workspace) bool { return m.draftModal == "import" }
			case "boot":
				m.Boot = &BootForm{}
				stillActive = func(m Workspace) bool { return m.Boot != nil }
			case "protection":
				m.Protection = &ProtectionForm{}
				stillActive = func(m Workspace) bool { return m.Protection != nil }
			case "guest-agent":
				m.GuestAgent = &guestAgentSetup{}
				stillActive = func(m Workspace) bool { return m.GuestAgent != nil }
			case "resources":
				m.Resources = &resourceSetup{}
				stillActive = func(m Workspace) bool { return m.Resources != nil }
			}
			m.Pending["job-update"] = 1001
			next, _ := m.Update(workspaceReply{Kind: "job-update", Token: 1001, Response: app.Response{Data: domain.Job{ID: creationNetworkJob, PlanID: handoff.PlanID, State: "succeeded"}}})
			m = next.(Workspace)
			if m.CreationNetwork != handoff || !stillActive(m) || m.Section != 8 || resourceID(m.Detail) != creationNetworkJob || field(m.Detail, "state") != "succeeded" || m.Pending["creation-networks"] != 0 {
				t.Fatal("completed network job interrupted another interaction", overlay)
			}
		})
	}
}

func TestCreationNetworkExplicitBackWorksAfterDeferredAutomaticReturn(t *testing.T) {
	m := creationNetworkWorkspace(t, false)
	original := m.setupDocument()
	m = acceptCreationNetworkJob(t, m)
	m.Help = true
	m.Pending["job-update"] = 1101
	next, _ := m.Update(workspaceReply{Kind: "job-update", Token: 1101, Response: app.Response{Data: domain.Job{ID: creationNetworkJob, PlanID: m.CreationNetwork.PlanID, State: "succeeded"}}})
	m = next.(Workspace)
	if !m.Help || m.CreationNetwork == nil {
		t.Fatal("completion interrupted Help")
	}
	m, _ = wk(m, "esc")
	if m.Help || m.CreationNetwork == nil {
		t.Fatal("Help dismissal lost suspended VM setup")
	}
	index := -1
	for i, button := range m.buttons() {
		if button.label == "Back to VM setup" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("deferred return has no explicit Back action")
	}
	m.ButtonFocus, m.ButtonIndex = true, index
	m, cmd := wk(m, "enter")
	if cmd == nil || m.CreationNetwork != nil || m.Creation == nil || !reflect.DeepEqual(m.setupDocument(), original) {
		t.Fatal("explicit return could not restore choices and refresh after Help")
	}
}
