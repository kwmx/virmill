package tui

import (
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

// A storage pool has its own plan and job. VM setup stays open and editable
// while it runs; only the pool choice changes, and only to the exact new pool.
type creationPoolJob struct {
	Connection, JobID, PlanID string
	Pool                      domain.StoragePoolDefinition
}
type creationPoolPulse struct{ JobID string }

func creationPoolTick(id string) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return creationPoolPulse{JobID: id} })
}

// openPoolCreation plans libvirt's standard pool without questions; the review
// shows its name and folder before anything changes.
func (m *Workspace) openPoolCreation() tea.Cmd {
	if m.Busy || m.Pending["apply"] != 0 {
		m.Error = "Wait for the current request before creating a storage pool."
		return nil
	}
	if m.CreationPool != nil {
		if m.Creation != nil {
			m.Creation.Error = "The storage pool is still being created; it is selected when ready."
		}
		return nil
	}
	m.Busy = true
	m.Error = ""
	m.Notice = "Preparing the storage pool review…"
	return m.request("plan", "storage.pool.create", app.Request{Action: "create", Input: map[string]any{}})
}

func poolPlanOperation(operation string) bool {
	return operation == "storage.pool.create" || operation == "storage.pool.start"
}

// openPoolStart reviews starting an existing stopped pool.
func (m *Workspace) openPoolStart(id string) tea.Cmd {
	if !guidedUUID.MatchString(id) {
		m.Error = "Select a stopped storage pool first."
		return nil
	}
	if m.Busy || m.Pending["apply"] != 0 {
		m.Error = "Wait for the current request before starting a storage pool."
		return nil
	}
	if m.CreationPool != nil {
		if m.Creation != nil {
			m.Creation.Error = "A storage pool is still being prepared; it is selected when ready."
		}
		return nil
	}
	m.Busy = true
	m.Error = ""
	m.Notice = "Preparing the storage pool review…"
	return m.request("plan", "storage.pool.start", app.Request{ID: id, Action: "start", Input: map[string]any{}})
}

func planPoolDefinition(p *domain.Plan) (domain.StoragePoolDefinition, bool) {
	var d domain.StoragePoolDefinition
	if p == nil || !poolPlanOperation(p.Operation) {
		return d, false
	}
	key := "definition"
	if p.Operation == "storage.pool.start" {
		key = "pool"
	}
	raw, err := json.Marshal(p.Review[key])
	if err != nil || json.Unmarshal(raw, &d) != nil || !guidedUUID.MatchString(d.UUID) || d.Name == "" || d.Path == "" {
		return d, false
	}
	return d, true
}

// acceptCreationPool keeps the VM form open instead of switching to Jobs.
func (m *Workspace) acceptCreationPool(data any) (bool, tea.Cmd) {
	if m.Creation == nil || m.Plan == nil || !poolPlanOperation(m.Plan.Operation) {
		return false, nil
	}
	verb := "Creating"
	if m.Plan.Operation == "storage.pool.start" {
		verb = "Starting"
	}
	var job domain.Job
	raw, err := json.Marshal(data)
	pool, ok := planPoolDefinition(m.Plan)
	if err != nil || json.Unmarshal(raw, &job) != nil || !guidedUUID.MatchString(job.ID) || job.PlanID != m.Plan.ID || m.Plan.ConnectionID != m.Connection || !ok {
		m.Busy = false
		m.Error = "Storage pool submission could not be matched. Check Jobs before another attempt; VM choices are kept."
		return true, nil
	}
	m.CreationPool = &creationPoolJob{Connection: m.Connection, JobID: job.ID, PlanID: job.PlanID, Pool: pool}
	m.Plan = nil
	m.Reviewing = false
	m.Busy = false
	m.Error = ""
	m.Creation.Error = ""
	m.Notice = verb + " storage pool " + pool.Name + "… Keep setting up your VM; the pool is selected when ready."
	return true, tea.Batch(creationPoolTick(job.ID), m.request("jobs", "operation.list", app.Request{}))
}

func (m *Workspace) pollCreationPool(id string) tea.Cmd {
	h := m.CreationPool
	if h == nil || h.JobID != id {
		return nil
	}
	if m.Creation == nil || h.Connection != m.Connection {
		m.CreationPool = nil
		return nil
	}
	return m.request("creation-pool-job", "operation.get", app.Request{ID: id})
}

func (m *Workspace) receiveCreationPoolJob(data any) tea.Cmd {
	h := m.CreationPool
	var job domain.Job
	raw, err := json.Marshal(data)
	if h == nil || err != nil || json.Unmarshal(raw, &job) != nil || job.ID != h.JobID || job.PlanID != h.PlanID {
		return nil
	}
	if m.Creation == nil {
		m.CreationPool = nil
		return nil
	}
	if !domain.Terminal(job.State) {
		return creationPoolTick(job.ID)
	}
	if job.State != "succeeded" {
		m.CreationPool = nil
		reason := "it needs attention"
		if job.Error != nil && job.Error.Message != "" {
			reason = validation.SafeText(job.Error.Message)
		}
		m.Notice = ""
		m.Creation.Error = "The storage pool was not created: " + reason + ". Open Jobs for details; your VM settings are kept."
		return nil
	}
	return m.request("creation-pools", "storage.pool.list", app.Request{})
}

func (m *Workspace) receiveCreationPools(data any) {
	h := m.CreationPool
	m.CreationPool = nil
	if h == nil || m.Creation == nil || h.Connection != m.Connection {
		return
	}
	var pools []domain.StoragePool
	raw, err := json.Marshal(data)
	if err != nil || json.Unmarshal(raw, &pools) != nil || len(pools) > 4096 {
		m.Creation.Error = "Could not read storage pools. Your settings are kept; reopen Create VM to refresh."
		return
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, p := range pools {
		if p.Key.ProviderID != "libvirt" || p.Key.Kind != "storage-pool" || p.Key.ConnectionID != m.Connection || !guidedUUID.MatchString(p.Key.UUID) || p.Name == "" || ids[p.Key.UUID] || names[p.Name] {
			m.Creation.Error = "Storage pool identities could not be verified. Your settings are kept; reopen Create VM to refresh."
			return
		}
		ids[p.Key.UUID], names[p.Name] = true, true
	}
	m.Creation.Pools = pools
	for _, p := range pools {
		if p.Key.UUID == h.Pool.UUID && p.Name == h.Pool.Name && creationUsablePool(p) {
			m.Creation.Spec.PoolID = p.Key.UUID
			m.Creation.Error = ""
			m.Notice = "Storage pool " + p.Name + " is ready and selected."
			return
		}
	}
	m.Notice = ""
	m.Creation.Error = "The new storage pool is not active. Open Storage to check it; your settings are kept."
}
