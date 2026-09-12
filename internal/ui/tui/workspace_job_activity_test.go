package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

const activityJobID = "88888888-8888-4888-8888-888888888888"

func activityEvent(id string, seq int64) domain.Event {
	return domain.Event{APIVersion: "virmill/v1", OperationID: id, Seq: seq, At: time.Unix(1700000000+seq, 0).UTC(), Phase: "running", Severity: "info", Message: fmt.Sprintf("Recorded event %d", seq)}
}
func activityWorkspace() Workspace {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Section, m.NavIndex = 8, 8
	m.Data["jobs"] = generic([]domain.Job{{ID: workspaceVMID, State: "failed"}, {ID: activityJobID, State: "running"}})
	m.Selected = 1
	return m
}
func loadActivity(t *testing.T, events []domain.Event) Workspace {
	t.Helper()
	m := activityWorkspace()
	m.Client = &workspaceClient{response: app.Response{Data: events}}
	cmd := m.openJobActivity()
	if cmd == nil {
		t.Fatal("Activity has no initial request")
	}
	next, _ := m.Update(cmd())
	return next.(Workspace)
}

func TestJobActivitySelectedJobOpensAtZeroWithoutFormOrMutation(t *testing.T) {
	m := activityWorkspace()
	events := []domain.Event{activityEvent(activityJobID, 3), activityEvent(activityJobID, 97)}
	c := &workspaceClient{response: app.Response{Data: events}}
	m.Client = c
	cmd := m.openJobActivity()
	if cmd == nil || m.Activity == nil || m.Activity.OperationID != activityJobID || m.Activity.After != 0 || !m.Activity.Loading || m.ActionForm != nil || m.Form != nil || m.Plan != nil {
		t.Fatal("Activity prompted for an already selected job or lost identity")
	}
	if duplicate := m.refreshJobActivity(); duplicate != nil {
		t.Fatal("duplicate activity read while loading")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if !reflect.DeepEqual(c.calls, []string{"operation.watch"}) || len(c.requests) != 1 {
		t.Fatal("unexpected Activity service calls", c.calls)
	}
	r := c.requests[0]
	if r.ID != activityJobID || r.Connection != m.Connection || r.After != 0 || r.Apply != nil || r.Action != "" || r.Path != "" || len(r.Input) != 0 {
		t.Fatal("Activity request gained mutation/cursor inputs", r)
	}
	if m.Activity == nil || m.Activity.Loading || m.Activity.After != 0 || m.Activity.Error != "" || !reflect.DeepEqual(m.Activity.Events, events) {
		t.Fatal("valid globally noncontiguous sequences were rejected")
	}
}

func TestJobActivityInvalidPageAndReadErrorsKeepPriorEventsAndCursor(t *testing.T) {
	for _, fault := range []string{"foreign-job", "reversed", "duplicate", "cursor", "version", "time", "oversized", "transport", "service"} {
		t.Run(fault, func(t *testing.T) {
			prior := []domain.Event{activityEvent(activityJobID, 103), activityEvent(activityJobID, 190)}
			m := loadActivity(t, prior)
			m.Activity.After = 100
			m.Activity.History = []int64{0}
			c := m.Client.(*workspaceClient)
			bad := []domain.Event{activityEvent(activityJobID, 107), activityEvent(activityJobID, 203)}
			switch fault {
			case "foreign-job":
				bad[0].OperationID = workspaceVMID
			case "reversed":
				bad[0], bad[1] = bad[1], bad[0]
			case "duplicate":
				bad[1].Seq = bad[0].Seq
			case "cursor":
				bad[0].Seq = 100
			case "version":
				bad[0].APIVersion = "virmill/v2"
			case "time":
				bad[0].At = time.Time{}
			case "oversized":
				bad = make([]domain.Event, 1001)
				for i := range bad {
					bad[i] = activityEvent(activityJobID, int64(i+101))
				}
			case "transport":
				c.err = fmt.Errorf("fixture coordinator unavailable")
			case "service":
				c.response.Error = domain.Fail("UNAVAILABLE", "fixture events unavailable")
			}
			c.response.Data = bad
			cmd := m.refreshJobActivity()
			if cmd == nil {
				t.Fatal("no refresh")
			}
			next, _ := m.Update(cmd())
			m = next.(Workspace)
			if m.Activity == nil || m.Activity.Loading || m.Activity.Error == "" || !reflect.DeepEqual(m.Activity.Events, prior) || m.Activity.After != 100 || !reflect.DeepEqual(m.Activity.History, []int64{0}) {
				t.Fatal("invalid page replaced prior evidence or cursor", fault)
			}
			for _, method := range c.calls {
				if method != "operation.watch" {
					t.Fatal("Activity performed mutation", method)
				}
			}
		})
	}
}

func TestJobActivityOlderNewerUsePageCursorsWithoutAssumingContiguousSequences(t *testing.T) {
	page := make([]domain.Event, 1000)
	for i := range page {
		page[i] = activityEvent(activityJobID, int64((i+1)*3))
	}
	m := loadActivity(t, page)
	c := m.Client.(*workspaceClient)
	c.err = fmt.Errorf("fixture next page temporarily unavailable")
	m.Activity.Focus = 1
	m, cmd := wk(m, "enter")
	if cmd == nil {
		t.Fatal("no next-page attempt")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Activity.Error == "" || m.Activity.After != 0 || len(m.Activity.History) != 0 || !reflect.DeepEqual(m.Activity.Events, page) {
		t.Fatal("failed next page committed cursor/history before valid response")
	}
	c.err = nil
	c.response.Data = []domain.Event{activityEvent(activityJobID, 3017), activityEvent(activityJobID, 3099)}
	m.Activity.Focus = 1 // Refresh, Newer events, Back on the first full page.
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("Newer events did not read the next page")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if c.requests[len(c.requests)-1].After != 3000 || m.Activity.After != 3000 || !reflect.DeepEqual(m.Activity.History, []int64{0}) || len(m.Activity.Events) != 2 {
		t.Fatal("newer page lost global sequence cursor")
	}
	c.response.Data = page
	m.Activity.Focus = 1 // Refresh, Older events, Back on the final short page.
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("Older events did not read the prior page")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	if c.requests[len(c.requests)-1].After != 0 || m.Activity.After != 0 || len(m.Activity.History) != 0 || !reflect.DeepEqual(m.Activity.Events, page) {
		t.Fatal("older navigation did not restore its original query")
	}
}

func TestJobActivityInvalidTargetsDoNotRequestEvents(t *testing.T) {
	for _, fault := range []string{"uuid", "remote", "section", "apply", "plan"} {
		t.Run(fault, func(t *testing.T) {
			m := activityWorkspace()
			m.Detail = generic(domain.Job{ID: activityJobID, State: "running"})
			switch fault {
			case "uuid":
				m.Detail = generic(domain.Job{ID: "invalid-job", State: "running"})
			case "remote":
				m.Connection = "qemu+ssh://other/system"
			case "section":
				m.Section = 1
			case "apply":
				m.Pending["apply"] = 4
			case "plan":
				m.Plan = &domain.Plan{}
			}
			if cmd := m.openJobActivity(); cmd != nil || m.Activity != nil || len(m.Client.(*workspaceClient).calls) != 0 {
				t.Fatal("invalid Activity target requested events", fault)
			}
		})
	}
}

func TestJobActivityCloseDuringReadRejectsLateReplyAndKeepsJob(t *testing.T) {
	m := activityWorkspace()
	m.Detail = generic(domain.Job{ID: activityJobID, State: "running"})
	selected := m.Detail
	m.Client = &workspaceClient{response: app.Response{Data: []domain.Event{activityEvent(activityJobID, 8)}}}
	cmd := m.openJobActivity()
	if cmd == nil {
		t.Fatal("no initial read")
	}
	m, closeCmd := wk(m, "esc")
	if closeCmd != nil || m.Activity != nil || m.Pending["job-activity"] != 0 || !reflect.DeepEqual(m.Detail, selected) {
		t.Fatal("Back while loading did not retain the selected job")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.Activity != nil || !reflect.DeepEqual(m.Detail, selected) {
		t.Fatal("late page reopened closed Activity")
	}
}

func TestJobActivityRepeatedOldReplyCannotReplaceNewerPage(t *testing.T) {
	m := activityWorkspace()
	c := &workspaceClient{response: app.Response{Data: []domain.Event{activityEvent(activityJobID, 8)}}}
	m.Client = c
	cmd := m.openJobActivity()
	if cmd == nil {
		t.Fatal("no initial read")
	}
	oldReply := cmd()
	next, _ := m.Update(oldReply)
	m = next.(Workspace)
	fresh := []domain.Event{activityEvent(activityJobID, 8), activityEvent(activityJobID, 50)}
	c.response.Data = fresh
	cmd = m.refreshJobActivity()
	if cmd == nil {
		t.Fatal("no fresh read")
	}
	next, _ = m.Update(cmd())
	m = next.(Workspace)
	next, followup := m.Update(oldReply)
	m = next.(Workspace)
	if followup != nil || m.Activity == nil || !reflect.DeepEqual(m.Activity.Events, fresh) || m.Activity.Loading {
		t.Fatal("old token replaced newer event evidence")
	}
}

func TestJobActivityBlocksNetworkAndPreparationAutomaticNavigation(t *testing.T) {
	t.Run("network", func(t *testing.T) {
		m := acceptCreationNetworkJob(t, creationNetworkWorkspace(t, false))
		handoff := m.CreationNetwork
		_ = m.openJobActivity()
		activity := m.Activity
		if activity == nil {
			t.Fatal("network job Activity missing")
		}
		m.Pending["job-update"] = 1201
		next, _ := m.Update(workspaceReply{Kind: "job-update", Token: 1201, Response: app.Response{Data: domain.Job{ID: creationNetworkJob, PlanID: handoff.PlanID, State: "succeeded"}}})
		m = next.(Workspace)
		if m.Activity != activity || m.CreationNetwork != handoff || m.Creation != nil || m.Pending["creation-networks"] != 0 {
			t.Fatal("network completion interrupted Activity")
		}
	})
	t.Run("preparation", func(t *testing.T) {
		m := activityWorkspace()
		m.Detail = generic(domain.Job{ID: creationFormOperation, State: "running"})
		m.PendingPreparation = creationFormOperation
		_ = m.openJobActivity()
		activity := m.Activity
		if activity == nil {
			t.Fatal("preparation Activity missing")
		}
		m.Pending["preparation-job"] = 1202
		next, cmd := m.Update(workspaceReply{Kind: "preparation-job", Token: 1202, Response: app.Response{Data: domain.Job{ID: creationFormOperation, State: "succeeded"}}})
		m = next.(Workspace)
		if cmd != nil || m.Activity != activity || m.Creation != nil || m.CreationPicking || m.PendingPreparation != "" {
			t.Fatal("preparation completion interrupted Activity")
		}
	})
}

func TestJobActivityPresentationSanitizesAndScrollsLongEvents(t *testing.T) {
	event := activityEvent(activityJobID, 42)
	event.Message = "unsafe\x1b[31m text\r\n" + strings.Repeat("long event detail ", 90) + "LAST-EVENT-LINE"
	a := jobActivity{OperationID: activityJobID, Connection: "qemu:///system", Events: []domain.Event{event}}
	view := strings.Join(a.View(80, 17), "\n")
	if strings.ContainsAny(view, "\x1b\r") || strings.Contains(view, "LAST-EVENT-LINE") {
		t.Fatal("unsafe terminal controls or unbounded initial event", view)
	}
	a.SetViewport(80, 17)
	a, intent := a.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if intent != "" {
		t.Fatal("scrolling submitted an action")
	}
	view = strings.Join(a.View(80, 17), "\n")
	if !strings.Contains(view, "LAST-EVENT-LINE") {
		t.Fatal("long event tail not reachable", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatal("event exceeds terminal columns")
		}
	}
	if len(strings.Split(view, "\n")) > 17 {
		t.Fatal("event exceeds body rows")
	}
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyHome})
	if a.Offset != 0 || a.After != 0 || len(a.History) != 0 || !reflect.DeepEqual(a.Events, []domain.Event{event}) {
		t.Fatal("reading event changed underlying evidence or cursor")
	}
}
