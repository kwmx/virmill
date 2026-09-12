package tui

import (
	"encoding/json"
	"maps"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

type jobActivityRead struct {
	Connection, OperationID string
	After, PreviousAfter    int64
	History                 []int64
}

func (m *Workspace) openJobActivity() tea.Cmd {
	if m.Section == 8 && m.Detail == nil {
		rows := m.rows()
		if m.Selected >= 0 && m.Selected < len(rows) {
			m.Detail = rows[m.Selected]
			m.DetailTitle = "Job details"
		}
	}
	id := resourceID(m.Detail)
	if m.Section != 8 || !guidedLocal(m.Connection) || !guidedUUID.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" || field(m.Detail, "state") == "" || m.Plan != nil || m.Pending["apply"] != 0 {
		m.Error = "Open a job first to read its recorded activity."
		return nil
	}
	m.closeJobActivity()
	m.Activity = &jobActivity{OperationID: id, Connection: m.Connection}
	m.Error, m.Notice = "", ""
	return m.refreshJobActivity()
}

func (m Workspace) activityMatches() bool {
	return m.Activity != nil && m.Activity.Connection == m.Connection && m.Section == 8 && m.Activity.OperationID == resourceID(m.Detail)
}

func (m *Workspace) requestActivityPage(after int64, history []int64) tea.Cmd {
	if !m.activityMatches() || m.Pending["job-activity"] != 0 || after < 0 {
		return nil
	}
	a := *m.Activity
	a.Loading, a.Error = true, ""
	m.Activity = &a
	m.ActivityReading = &jobActivityRead{Connection: a.Connection, OperationID: a.OperationID, After: after, PreviousAfter: a.After, History: slices.Clone(history)}
	return m.request("job-activity", "operation.watch", app.Request{ID: a.OperationID, After: after})
}

func (m *Workspace) refreshJobActivity() tea.Cmd {
	if m.Activity == nil {
		return nil
	}
	return m.requestActivityPage(m.Activity.After, m.Activity.History)
}

func (m *Workspace) failJobActivity(message string) {
	m.ActivityReading = nil
	if m.Activity != nil {
		a := *m.Activity
		a.Loading, a.Error = false, message
		a.Offset = 0
		m.Activity = &a
	}
}

func (m *Workspace) receiveJobActivity(data any) {
	r := m.ActivityReading
	if r == nil || !m.activityMatches() || r.OperationID != m.Activity.OperationID || r.Connection != m.Connection || r.PreviousAfter != m.Activity.After {
		m.failJobActivity("This activity reply no longer matches the open job. Refresh to read it again.")
		return
	}
	raw, err := json.Marshal(data)
	var events []domain.Event
	if err != nil || len(raw) > 4<<20 || json.Unmarshal(raw, &events) != nil || len(events) > 1000 {
		m.failJobActivity("Could not read this event page. Previously loaded activity is kept; choose Refresh.")
		return
	}
	previous := r.After
	for _, event := range events {
		if event.APIVersion != domain.APIVersion || event.OperationID != r.OperationID || event.Seq <= previous || event.At.IsZero() {
			m.failJobActivity("Event identities or ordering could not be verified. Previously loaded activity is kept; choose Refresh.")
			return
		}
		previous = event.Seq
	}
	a := *m.Activity
	if a.After != r.After {
		a.Offset, a.Focus = 0, 0
	}
	a.After, a.History = r.After, slices.Clone(r.History)
	a.Events, a.Loading, a.Error = events, false, ""
	a.CheckedAt = time.Now().UTC()
	m.Activity, m.ActivityReading = &a, nil
}

func (m *Workspace) closeJobActivity() {
	m.Activity, m.ActivityReading = nil, nil
	m.Pending = maps.Clone(m.Pending)
	delete(m.Pending, "job-activity")
}

func (m Workspace) updateJobActivity(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.activityMatches() {
		m.closeJobActivity()
		m.Error = "The selected job changed. Open its activity again."
		return m, nil
	}
	if key.String() == "?" {
		m.Help = true
		return m, nil
	}
	a := *m.Activity
	a.SetViewport(m.Width, max(1, m.Height-7))
	a, intent := a.Update(key)
	m.Activity = &a
	switch intent {
	case "back":
		m.closeJobActivity()
	case "refresh":
		return m, m.refreshJobActivity()
	case "older":
		if len(a.History) > 0 {
			return m, m.requestActivityPage(a.History[len(a.History)-1], a.History[:len(a.History)-1])
		}
	case "newer":
		if len(a.Events) == 1000 {
			return m, m.requestActivityPage(a.Events[len(a.Events)-1].Seq, append(slices.Clone(a.History), a.After))
		}
	}
	return m, nil
}
