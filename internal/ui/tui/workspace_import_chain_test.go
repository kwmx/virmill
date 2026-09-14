package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// These tests cover the TUI chain of ADR 0057: which plans may be applied under
// one approval. Durable job behavior is covered by each service's own tests.
const chainPrepJob = "99999999-9999-4999-8999-999999999991"
const chainCreateJob = "99999999-9999-4999-8999-999999999992"
const chainPrepPlan = "99999999-9999-4999-8999-999999999993"
const chainCreatePlan = "99999999-9999-4999-8999-999999999994"
const chainStartPlan = "99999999-9999-4999-8999-999999999995"

func chainPlan(t *testing.T, id, operation, connection string, acks []string, review map[string]any, resources []string) domain.Plan {
	t.Helper()
	if resources == nil {
		resources = []string{}
	}
	p := domain.Plan{APIVersion: domain.APIVersion, ID: id, ConnectionID: connection, Operation: operation, ResourceIDs: resources, Before: map[string]string{}, RequiredGrants: []domain.Grant{}, Acknowledgements: acks, Risks: []string{"fixture"}, Steps: []domain.Step{{ID: "effect"}}, Review: review}
	digest, err := operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Digest = digest
	return p
}

// deliver answers the pending request of kind with data.
func deliver(t *testing.T, m Workspace, kind string, data any) (Workspace, tea.Cmd) {
	t.Helper()
	token := m.Pending[kind]
	if token == 0 {
		t.Fatalf("no pending %s request", kind)
	}
	next, cmd := m.Update(workspaceReply{Kind: kind, Token: token, Response: app.Response{Data: data}})
	return next.(Workspace), cmd
}

func chainImportWorkspace(t *testing.T) Workspace {
	t.Helper()
	m := creationNetworkWorkspace(t, true)
	m.Creation.Spec.NICs[0].NetworkID = creationFormNetwork
	m.Creation.Spec.NICs[0].Link = "up"
	f := *m.Creation
	m.Import.VM = &f
	m.Creation = nil
	return m
}

func TestExpectedCreationAcksFollowSettings(t *testing.T) {
	f := creationComplete(creationFormFixture())
	f.Spec.GuestAgent = true
	f.Spec.Firmware = domain.CreationFirmware{Mode: "uefi"}
	f.Spec.DevicePolicy.WatchdogAction = "reset"
	want := []string{"host-mutation", "copy-managed-volumes", "new-vm-identity", "guest-agent-channel", "new-firmware-state", "network-attachment", "attach-readonly-media", "creation-device-policy", "watchdog-reset"}
	if got := expectedCreationAcks(f); !slices.Equal(got, want) {
		t.Fatal(got)
	}
	f.Spec.NICs, f.Spec.Media, f.Spec.GuestAgent = nil, nil, false
	f.Spec.Firmware = domain.CreationFirmware{Mode: "bios"}
	f.Spec.DevicePolicy.WatchdogAction = "none"
	if got := expectedCreationAcks(f); !slices.Equal(got, []string{"host-mutation", "copy-managed-volumes", "new-vm-identity", "creation-device-policy"}) {
		t.Fatal(got)
	}
}

func TestOneReviewPreparesCreatesAndStarts(t *testing.T) {
	m := chainImportWorkspace(t)
	m.Width, m.Height = 120, 40
	m.Client = &workspaceClient{}
	settings := *m.Import.VM
	m.Busy = true
	m.Pending = map[string]uint64{"plan": 7}
	// Preparation plans are host-local ("local"), unlike creation and start.
	prep := chainPlan(t, chainPrepPlan, "import.prepare-install", "local", []string{"write-import-artifacts", "offline-source-files"}, map[string]any{}, nil)
	m, _ = deliver(t, m, "plan", prep)
	if m.ChainOffer == nil || !m.ChainOffer.Start {
		t.Fatal("preparation review does not cover creation and start")
	}
	acks := m.reviewAcks()
	for _, want := range []string{"write-import-artifacts", "host-mutation", "copy-managed-volumes", "network-attachment", "attach-readonly-media", "creation-device-policy", startVMAck} {
		if !slices.Contains(acks, want) {
			t.Fatalf("combined review lacks %s: %v", want, acks)
		}
	}
	if len(m.Approved) != len(acks) {
		t.Fatal("not every consequence needs a check")
	}
	view := m.View()
	if !strings.Contains(view, "creates VM keep-exact-vm-choices") || !strings.Contains(view, "starts the VM once it is created") {
		t.Fatal("review does not describe the later steps", view)
	}
	m.Reviewing = true
	if lines := strings.Join(m.confirmationLines(120, 60), "\n"); !strings.Contains(lines, "Start the VM as soon as it is created") || !strings.Contains(lines, "Next steps") {
		t.Fatal("confirmation lacks later steps", lines)
	}

	// Accepting preparation binds the approval to its job.
	c := &workspaceClient{}
	m.Client = c
	apply := m.request("apply", "operation.apply", app.Request{})
	c.response = app.Response{Data: domain.Job{ID: chainPrepJob, PlanID: chainPrepPlan, State: "queued"}}
	next, _ := m.Update(apply())
	m = next.(Workspace)
	if m.Chain == nil || m.Chain.PrepJob != chainPrepJob || m.Chain.Connection != m.Connection || !m.Chain.Start || m.ChainOffer != nil || slices.Contains(m.Chain.Approved, startVMAck) {
		t.Fatal("approval not bound to the preparation job", m.Chain)
	}

	// After preparation the same settings are requested and applied unasked.
	prepared := settings
	prepared.OperationID, prepared.BeforePreparation = chainPrepJob, false
	m.SavedCreation, m.Creation, m.Import = &prepared, nil, nil
	m.CreationAutoReview = chainPrepJob
	m.Pending = map[string]uint64{"creation-load": 11}
	m, cmd := deliver(t, m, "creation-load", creationBundle{OperationID: chainPrepJob, Source: settings.Source, Options: settings.Options, Pools: settings.Pools, Networks: settings.Networks})
	if cmd == nil || m.Chain == nil || m.Chain.Sent != m.Chain.Settings {
		t.Fatal("prepared images did not request the approved VM", m.Notice, m.Creation.Error)
	}
	cmd()
	if c.calls[len(c.calls)-1] != "vm.create" || c.requests[len(c.requests)-1].ID != chainPrepJob {
		t.Fatal("creation request", c.calls)
	}
	create := chainPlan(t, chainCreatePlan, "vm.create.devices-v1", m.Connection, expectedCreationAcks(prepared), map[string]any{"sourceOperationID": chainPrepJob}, nil)
	m, cmd = deliver(t, m, "plan", create)
	if cmd == nil {
		t.Fatal("matching creation plan was not applied", m.Notice)
	}
	cmd()
	last := c.requests[len(c.requests)-1]
	if c.calls[len(c.calls)-1] != "operation.apply" || last.Apply == nil || last.Apply.PlanID != chainCreatePlan || !slices.Equal(last.Apply.Acknowledgements, create.Acknowledgements) {
		t.Fatal("creation apply", c.calls, last.Apply)
	}
	m, _ = deliver(t, m, "apply", domain.Job{ID: chainCreateJob, PlanID: chainCreatePlan, State: "queued"})
	if m.Chain == nil || m.Chain.CreateJob != chainCreateJob {
		t.Fatal("creation job not followed")
	}

	// A verified created VM is started under the same approval.
	m.Section, m.NavIndex = 8, 8
	m.Detail = generic(domain.Job{ID: chainCreateJob, PlanID: chainCreatePlan, State: "succeeded"})
	m.JobOutcome = &jobOutcome{JobID: chainCreateJob, PlanID: chainCreatePlan, State: "succeeded", Connection: m.Connection, VMID: workspaceVMID}
	if cmd = m.chainAfterCreation(); cmd == nil || !m.JobStartVM || m.Chain.VMID != workspaceVMID {
		t.Fatal("created VM was not opened to start")
	}
	vm := workspaceVM(workspaceVMID, "keep-exact-vm-choices")
	vm.Fingerprint = "fixture"
	m.receiveJobVM(generic(vm))
	key := (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "vm", UUID: workspaceVMID}).String()
	start := chainPlan(t, chainStartPlan, "vm.start", m.Connection, []string{"host-mutation"}, map[string]any{}, []string{key})
	m, cmd = deliver(t, m, "plan", start)
	if cmd == nil {
		t.Fatal("start plan was not applied", m.Notice)
	}
	cmd()
	if last = c.requests[len(c.requests)-1]; last.Apply == nil || last.Apply.PlanID != chainStartPlan {
		t.Fatal("start apply", last)
	}
	m, _ = deliver(t, m, "apply", domain.Job{ID: "99999999-9999-4999-8999-999999999996", PlanID: chainStartPlan, State: "queued"})
	if m.Chain != nil {
		t.Fatal("chain kept after its last step")
	}
}

func TestChainStopsAtReviewForAnythingNotApproved(t *testing.T) {
	base := func() Workspace {
		m := fixtureWorkspace()
		m.Client = &workspaceClient{}
		m.Chain = &importChain{Connection: m.Connection, Settings: "{}", Sent: "{}", Approved: []string{"host-mutation", "copy-managed-volumes", "new-vm-identity", "creation-device-policy"}, Start: true, PrepJob: chainPrepJob}
		m.Pending = map[string]uint64{"plan": 3}
		return m
	}
	for name, plan := range map[string]func(t *testing.T, m Workspace) domain.Plan{
		"new consequence": func(t *testing.T, m Workspace) domain.Plan {
			return chainPlan(t, chainCreatePlan, "vm.create.devices-v1", m.Connection, []string{"host-mutation", "watchdog-reset"}, map[string]any{"sourceOperationID": chainPrepJob}, nil)
		},
		"other images": func(t *testing.T, m Workspace) domain.Plan {
			return chainPlan(t, chainCreatePlan, "vm.create.devices-v1", m.Connection, []string{"host-mutation"}, map[string]any{"sourceOperationID": chainCreateJob}, nil)
		},
		"other connection": func(t *testing.T, m Workspace) domain.Plan {
			return chainPlan(t, chainCreatePlan, "vm.create.devices-v1", "qemu:///session", []string{"host-mutation"}, map[string]any{"sourceOperationID": chainPrepJob}, nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := base()
			m, cmd := deliver(t, m, "plan", plan(t, m))
			if cmd != nil || m.Chain != nil || m.Plan == nil || !strings.Contains(m.Notice, "needs your review") {
				t.Fatal("unapproved step applied or review hidden", m.Notice)
			}
		})
	}
	m := base()
	m.Chain.Sent = `{"changed":true}`
	m, cmd := deliver(t, m, "plan", chainPlan(t, chainCreatePlan, "vm.create.devices-v1", m.Connection, []string{"host-mutation"}, map[string]any{"sourceOperationID": chainPrepJob}, nil))
	if cmd != nil || m.Chain != nil {
		t.Fatal("changed settings applied")
	}
	m = base()
	m.Chain.PrepJob, m.Chain.CreateJob, m.Chain.VMID = "", chainCreateJob, workspaceVMID
	other := (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "vm", UUID: "22345678-1234-4234-8234-123456789abc"}).String()
	if m, cmd = deliver(t, m, "plan", chainPlan(t, chainStartPlan, "vm.start", m.Connection, []string{"host-mutation"}, map[string]any{}, []string{other})); cmd != nil || m.Chain != nil {
		t.Fatal("another VM was started")
	}
	m = base()
	if m, cmd = deliver(t, m, "plan", chainPlan(t, chainStartPlan, "network.create", m.Connection, []string{"host-mutation"}, map[string]any{}, nil)); cmd != nil || m.Chain == nil {
		t.Fatal("unrelated review changed the chain")
	}
}

func TestCreationWithoutStartHasNoLaterSteps(t *testing.T) {
	m := fixtureWorkspace()
	f := creationComplete(creationFormFixture())
	f.StartAfter = false
	m.Creation = &f
	m.Pending = map[string]uint64{"plan": 5}
	m, _ = deliver(t, m, "plan", chainPlan(t, chainCreatePlan, "vm.create.devices-v1", m.Connection, expectedCreationAcks(f), map[string]any{"sourceOperationID": creationFormOperation}, nil))
	if m.ChainOffer != nil || len(m.Approved) != len(m.Plan.Acknowledgements) {
		t.Fatal("later steps offered without start")
	}
	f.StartAfter = true
	m.Creation, m.Plan = &f, nil
	m.Pending = map[string]uint64{"plan": 6}
	m, _ = deliver(t, m, "plan", chainPlan(t, chainCreatePlan, "vm.create.devices-v1", m.Connection, expectedCreationAcks(f), map[string]any{"sourceOperationID": creationFormOperation}, nil))
	if m.ChainOffer == nil || !slices.Equal(m.ChainOffer.Extras, []string{startVMAck}) {
		t.Fatal("start after creation not offered", m.ChainOffer)
	}
}

func TestImportPreviewLoadsVMSettingsFirst(t *testing.T) {
	m := importWorkspace(t)
	f := importFocus(t, *m.Import, "preview")
	m.Import = &f
	m, cmd := wk(m, "enter")
	if cmd == nil || !m.ImportAutoPreview || m.Pending["creation-load"] == 0 {
		t.Fatal("preview did not load VM settings first")
	}
}
