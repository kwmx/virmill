package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
)

// These tests cover frontend state transitions and shared-service requests.
// Native pool creation is covered by the backend and service tests.
const creationPoolID = "55555555-5555-4555-8555-555555555555"
const creationPoolJobID = "66666666-6666-4666-8666-666666666666"
const creationPoolPlanID = "77777777-7777-4777-8777-777777777777"

func creationPoolPlan(t *testing.T, connection string) domain.Plan {
	t.Helper()
	p := domain.Plan{APIVersion: domain.APIVersion, ID: creationPoolPlanID, ConnectionID: connection, Operation: "storage.pool.create", ResourceIDs: []string{}, Before: map[string]string{}, RequiredGrants: []domain.Grant{}, Acknowledgements: []string{"host-mutation"}, Risks: []string{"Files already in the folder are listed as volumes"}, Steps: []domain.Step{{ID: "define"}}, Review: map[string]any{"definition": map[string]any{"uuid": creationPoolID, "name": "default", "path": "/var/lib/libvirt/images", "autostart": true}}}
	digest, err := operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Digest = digest
	return p
}
func creationPoolReply(t *testing.T, m Workspace, c *workspaceClient, cmd tea.Cmd, data any) (Workspace, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no service request")
	}
	c.response = app.Response{Data: data}
	next, out := m.Update(cmd())
	return next.(Workspace), out
}
func creationPoolPulseReply(t *testing.T, m Workspace, c *workspaceClient, job domain.Job) (Workspace, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(creationPoolPulse{JobID: creationPoolJobID})
	return creationPoolReply(t, next.(Workspace), c, cmd, job)
}

func TestCreationPoolIsCreatedInsideVMSetupAndSelected(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "before-preparation"}[before], func(t *testing.T) {
			m := creationNetworkWorkspace(t, before)
			m.Creation.Pools, m.Creation.Spec.PoolID = nil, ""
			c := &workspaceClient{}
			m.Client = c
			imported := m.Import
			m, _ = creationPoolReply(t, m, c, m.openPoolCreation(), creationPoolPlan(t, m.Connection))
			if m.Plan == nil || m.Plan.Operation != "storage.pool.create" || m.Creation == nil {
				t.Fatal("pool review not shown over VM setup", m.Error)
			}
			if !reflect.DeepEqual(c.calls, []string{"storage.pool.create"}) || c.requests[0].Action != "create" || len(c.requests[0].Input) != 0 || c.requests[0].ID != "" || c.requests[0].Path != "" {
				t.Fatal("pool plan request gained parameters", c.requests)
			}
			snapshot := m.setupDocument()
			apply := m.request("apply", "operation.apply", app.Request{})
			m, _ = creationPoolReply(t, m, c, apply, domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "queued"})
			if m.Creation == nil || m.Plan != nil || m.CreationPool == nil || m.Section == 8 || m.draftSubmitted || !reflect.DeepEqual(m.setupDocument(), snapshot) || (before && m.Import != imported) {
				t.Fatal("pool submission left or changed VM setup")
			}
			if !strings.Contains(m.Notice, "Keep setting up your VM") {
				t.Fatal("pool progress not explained", m.Notice)
			}
			m, cmd := creationPoolPulseReply(t, m, c, domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "running"})
			if m.CreationPool == nil || cmd == nil || m.Creation.Spec.PoolID != "" {
				t.Fatal("running pool job stopped polling or selected early")
			}
			m, cmd = creationPoolPulseReply(t, m, c, domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "succeeded"})
			other := domain.StoragePool{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "storage-pool", UUID: "88888888-8888-4888-8888-888888888888"}, Name: "iso", Type: "dir", Active: true}
			created := other
			created.Key.UUID, created.Name = creationPoolID, "default"
			m, _ = creationPoolReply(t, m, c, cmd, []domain.StoragePool{other, created})
			if m.Creation.Spec.PoolID != creationPoolID || m.CreationPool != nil || len(m.Creation.Pools) != 2 || !strings.Contains(m.Notice, "ready and selected") {
				t.Fatal("new pool not selected", m.Creation.Spec.PoolID, m.Notice, m.Creation.Error)
			}
			if m.Creation.Spec.Name != "keep-exact-vm-choices" || m.Creation.CPUText != "7" || m.Creation.MemoryText != "6144" {
				t.Fatal("VM choices changed")
			}
			want := []string{"storage.pool.create", "operation.apply", "operation.get", "operation.get", "storage.pool.list"}
			if !reflect.DeepEqual(c.calls, want) {
				t.Fatal("unexpected service calls", c.calls)
			}
		})
	}
}

func TestCreationPoolFailureAndClosedSetupKeepChoices(t *testing.T) {
	m := creationNetworkWorkspace(t, false)
	m.Creation.Pools, m.Creation.Spec.PoolID = nil, ""
	c := &workspaceClient{}
	m.Client = c
	job := &creationPoolJob{Connection: m.Connection, JobID: creationPoolJobID, PlanID: creationPoolPlanID, Pool: domain.StoragePoolDefinition{UUID: creationPoolID, Name: "default", Path: "/var/lib/libvirt/images"}}
	m.CreationPool = job
	if m.openPoolCreation() != nil || !strings.Contains(m.Creation.Error, "still being created") {
		t.Fatal("second pool plan while one runs")
	}
	m, _ = creationPoolPulseReply(t, m, c, domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "recovery-required", Error: &domain.Error{Code: "RECOVERY_REQUIRED", Message: "storage pool creation could not prepare the folder: Permission denied"}})
	if m.CreationPool != nil || !strings.Contains(m.Creation.Error, "Permission denied") || m.Creation.Spec.Name != "keep-exact-vm-choices" || m.Creation.Spec.PoolID != "" {
		t.Fatal("failed pool job not explained or VM choices changed", m.Creation.Error)
	}
	m.CreationPool = job
	m, _ = creationPoolPulseReply(t, m, c, domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "succeeded"})
	c.calls = nil
	m, _ = creationPoolReply(t, m, c, m.request("creation-pools", "storage.pool.list", app.Request{}), []domain.StoragePool{{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "storage-pool", UUID: creationPoolID}, Name: "default", Type: "dir"}})
	if m.Creation.Spec.PoolID != "" {
		t.Fatal("inactive pool selected")
	}
	m.CreationPool = job
	m.Creation = nil
	next, cmd := m.Update(creationPoolPulse{JobID: creationPoolJobID})
	if cmd != nil || next.(Workspace).CreationPool != nil {
		t.Fatal("closed VM setup kept polling the pool job")
	}
}

func TestStoragePageCreatesPoolWithoutOtherTools(t *testing.T) {
	text := strings.Join(emptyState(3), " ")
	if strings.Contains(text, "virt-manager") || !strings.Contains(text, "Create pool") {
		t.Fatal("empty Storage page points to another tool", text)
	}
	m := fixtureWorkspace()
	m.Section, m.NavIndex = 3, 3
	c := &workspaceClient{}
	m.Client = c
	for _, a := range ui.Actions {
		if a.Command != "storage pool create" {
			continue
		}
		if !m.commonAction(a) || actionLabel(a) != "Create storage pool" {
			t.Fatal("pool creation is not a common Storage action")
		}
		cmd := m.openAction(a)
		if cmd == nil {
			t.Fatal("no pool plan request")
		}
		cmd()
		if !reflect.DeepEqual(c.calls, []string{"storage.pool.create"}) || c.requests[0].Action != "create" || len(c.requests[0].Input) != 0 {
			t.Fatal("pool plan request", c.calls, c.requests)
		}
		c.response = app.Response{Data: creationPoolPlan(t, m.Connection)}
		o := readJobOutcome(context.Background(), c, jobOutcome{JobID: creationPoolJobID, PlanID: creationPoolPlanID, State: "succeeded", Connection: m.Connection})
		if o.Error != "" || o.Title != "Storage pool ready" || !strings.Contains(o.Summary, "/var/lib/libvirt/images") {
			t.Fatal("pool completion card", o)
		}
		return
	}
	t.Fatal("storage pool create is not registered")
}

func creationPoolStartPlan(t *testing.T, connection string) domain.Plan {
	t.Helper()
	p := domain.Plan{APIVersion: domain.APIVersion, ID: creationPoolPlanID, ConnectionID: connection, Operation: "storage.pool.start", ResourceIDs: []string{}, Before: map[string]string{}, RequiredGrants: []domain.Grant{}, Acknowledgements: []string{"host-mutation"}, Risks: []string{"Starts the existing pool exactly as it is defined"}, Steps: []domain.Step{{ID: "start"}}, Review: map[string]any{"pool": map[string]any{"uuid": creationPoolID, "name": "default", "type": "dir", "path": "/var/lib/libvirt/images"}, "enableAutostart": true}}
	digest, err := operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Digest = digest
	return p
}

func stoppedCreationPool(connection string) domain.StoragePool {
	return domain.StoragePool{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: connection, Kind: "storage-pool", UUID: creationPoolID}, Name: "default", Type: "dir", Persistent: true}
}

// A stopped pool is started from VM setup instead of a second pool being made.
func TestCreationStartsAStoppedPoolInsideVMSetup(t *testing.T) {
	m := creationNetworkWorkspace(t, false)
	stopped := stoppedCreationPool(m.Connection)
	m.Creation.Pools, m.Creation.Spec.PoolID = []domain.StoragePool{stopped}, ""
	m.Creation.Page = 0
	f := creationFocus(t, *m.Creation, "start-pool")
	if f.controls()[f.Focus].label != "Start pool default" {
		t.Fatal("stopped pool not offered", f.controls()[f.Focus].label)
	}
	f, intent := creationPress(f, tea.KeyEnter)
	if intent.Kind != "start-pool" || intent.Target != creationPoolID {
		t.Fatal("start intent", intent)
	}
	c := &workspaceClient{}
	m.Client = c
	m, _ = creationPoolReply(t, m, c, m.openPoolStart(intent.Target), creationPoolStartPlan(t, m.Connection))
	if m.Plan == nil || m.Plan.Operation != "storage.pool.start" || c.requests[0].ID != creationPoolID || c.requests[0].Action != "start" || len(c.requests[0].Input) != 0 {
		t.Fatal("pool start review not requested", c.requests)
	}
	apply := m.request("apply", "operation.apply", app.Request{})
	m, _ = creationPoolReply(t, m, c, apply, domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "queued"})
	if m.Creation == nil || m.CreationPool == nil || m.Section == 8 || m.draftSubmitted || !strings.Contains(m.Notice, "Starting storage pool default") {
		t.Fatal("pool start left VM setup", m.Notice)
	}
	m, cmd := creationPoolPulseReply(t, m, c, domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "succeeded"})
	running := stopped
	running.Active = true
	m, _ = creationPoolReply(t, m, c, cmd, []domain.StoragePool{running})
	if m.Creation.Spec.PoolID != creationPoolID || m.CreationPool != nil {
		t.Fatal("started pool not selected", m.Creation.Error)
	}
	for _, pool := range []domain.StoragePool{{Key: stopped.Key, Name: "lvm", Type: "logical", Persistent: true}, {Key: stopped.Key, Name: "temp", Type: "dir"}} {
		g := NewCreationForm(creationFormOperation, m.Creation.Source, m.Creation.Options, []domain.StoragePool{pool}, nil)
		for _, control := range g.controls() {
			if control.id == "start-pool" {
				t.Fatal("unstartable pool offered", pool.Name)
			}
		}
	}
}

func TestStoppedPoolDetailsOfferStartAndReview(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 120, 36
	m.Section, m.NavIndex = 3, 3
	m.Detail = generic(stoppedCreationPool(m.Connection))
	has := func() bool {
		for _, b := range m.buttons() {
			if b.label == "Start pool" {
				return true
			}
		}
		return false
	}
	if !has() {
		t.Fatal("stopped pool details lack Start pool")
	}
	running := stoppedCreationPool(m.Connection)
	running.Active = true
	m.Detail = generic(running)
	if has() {
		t.Fatal("running pool offers Start pool")
	}
	p := creationPoolStartPlan(t, m.Connection)
	m.Detail = nil
	m.Plan = &p
	view := m.View()
	for _, want := range []string{"Pool name: default", "Folder: /var/lib/libvirt/images", "Starts: Now and whenever the host starts"} {
		if !strings.Contains(view, want) {
			t.Fatalf("start review lacks %q", want)
		}
	}
}

func TestPoolReviewNamesPoolFolderAndStartup(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 120, 36
	p := creationPoolPlan(t, m.Connection)
	m.Plan = &p
	view := m.View()
	for _, want := range []string{"Pool name: default", "Folder: /var/lib/libvirt/images", "Starts: Now and whenever the host starts"} {
		if !strings.Contains(view, want) {
			t.Fatalf("review lacks %q: %s", want, view)
		}
	}
}
