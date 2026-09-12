package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

const outcomeJobID = "44345678-1234-4234-8234-123456789abc"
const outcomeOtherVMID = "55345678-1234-4234-8234-123456789abc"

type outcomeTestClient struct {
	plan, receipt any
	failMethod    string
	err           error
	serviceError  *domain.Error
	calls         []string
	requests      []app.Request
}

func (c *outcomeTestClient) Call(_ context.Context, method string, r app.Request) (app.Response, error) {
	c.calls = append(c.calls, method)
	c.requests = append(c.requests, r)
	if method == c.failMethod {
		return app.Response{Error: c.serviceError}, c.err
	}
	switch method {
	case "plan.show":
		return app.Response{Data: c.plan}, nil
	case "vm.creation.result":
		return app.Response{Data: c.receipt}, nil
	default:
		return app.Response{}, errors.New("unexpected service method: " + method)
	}
}

func outcomeFixture(t *testing.T, operation string) (jobOutcome, domain.Plan, *outcomeTestClient) {
	t.Helper()
	p := testWorkspacePlan(t)
	p.Operation = operation
	sealOutcomePlan(t, &p)
	o := jobOutcome{JobID: outcomeJobID, PlanID: p.ID, State: "succeeded", Connection: p.ConnectionID, Loading: true}
	return o, p, &outcomeTestClient{plan: p}
}

func sealOutcomePlan(t *testing.T, p *domain.Plan) {
	t.Helper()
	var err error
	p.Digest, err = operations.PlanDigest(*p)
	if err != nil {
		t.Fatal(err)
	}
}

func outcomeCreationResult(o jobOutcome) map[string]any {
	return map[string]any{
		"complete":  true,
		"operation": map[string]any{"operationID": o.JobID, "planID": o.PlanID, "state": "succeeded"},
		"receipt":   map[string]any{"planID": o.PlanID, "operationID": o.JobID, "vmID": workspaceVMID, "connection": o.Connection, "defined": true, "volumesVerified": true},
	}
}

func assertNoOutcomeShortcut(t *testing.T, got jobOutcome) {
	t.Helper()
	if got.Loading || got.VMID != "" || got.Error == "" {
		t.Fatalf("unverified result offered a shortcut: %+v", got)
	}
	m := fixtureWorkspace()
	m.Section = 8
	m.Detail = generic(domain.Job{ID: got.JobID, PlanID: got.PlanID, State: got.State})
	m.JobOutcome = &got
	if m.openJobVM() != nil {
		t.Fatal("failed result can open a VM")
	}
}

func TestJobOutcomeExistingVMUsesExactVerifiedPlanTarget(t *testing.T) {
	o, p, c := outcomeFixture(t, "vm.start")
	// Non-VM resources do not make a single VM ambiguous.
	p.ResourceIDs = append(p.ResourceIDs, "libvirt|qemu:///system|network|"+outcomeOtherVMID)
	sealOutcomePlan(t, &p)
	c.plan = p
	got := readJobOutcome(context.Background(), c, o)
	if got.Error != "" || got.Loading || got.VMID != workspaceVMID || got.Title != "VM started" || !strings.Contains(got.Summary, "does not yet prove") {
		t.Fatalf("valid completed start not recognized: %+v", got)
	}
	if len(c.calls) != 1 || c.calls[0] != "plan.show" || c.requests[0].ID != o.PlanID || c.requests[0].Connection != o.Connection || c.requests[0].Apply != nil {
		t.Fatal("outcome did not read the exact bound plan", c.calls, c.requests)
	}
}

func TestJobOutcomeCreationRequiresBoundDurableReceipt(t *testing.T) {
	for _, operation := range []string{"vm.create", "vm.create.devices-v1"} {
		t.Run(operation, func(t *testing.T) {
			o, _, c := outcomeFixture(t, operation)
			c.receipt = outcomeCreationResult(o)
			got := readJobOutcome(context.Background(), c, o)
			if got.Error != "" || got.VMID != workspaceVMID || got.Title != "VM created" || !strings.Contains(got.Summary, "separate step") {
				t.Fatalf("bound creation receipt not recognized: %+v", got)
			}
			if len(c.calls) != 2 || c.calls[1] != "vm.creation.result" || c.requests[1].ID != o.JobID || c.requests[1].Connection != o.Connection || c.requests[1].Apply != nil {
				t.Fatal("creation result read the wrong job", c.calls, c.requests)
			}
		})
	}
}

func TestJobOutcomeRejectsPlanMismatchOrAmbiguousVM(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*domain.Plan)
		seal bool
	}{
		{"modified digest", func(p *domain.Plan) { p.Operation = "vm.stop" }, false},
		{"other plan", func(p *domain.Plan) { p.ID = outcomeOtherVMID }, true},
		{"other connection", func(p *domain.Plan) { p.ConnectionID = "qemu:///session" }, true},
		{"two VMs", func(p *domain.Plan) {
			p.ResourceIDs = append(p.ResourceIDs, "libvirt|qemu:///system|vm|"+outcomeOtherVMID)
		}, true},
		{"duplicate VM", func(p *domain.Plan) { p.ResourceIDs = append(p.ResourceIDs, p.ResourceIDs[0]) }, true},
		{"missing VM", func(p *domain.Plan) { p.ResourceIDs = []string{"libvirt|qemu:///system|network|" + workspaceVMID} }, true},
		{"other provider", func(p *domain.Plan) { p.ResourceIDs = []string{"plugin|qemu:///system|vm|" + workspaceVMID} }, true},
		{"other target connection", func(p *domain.Plan) { p.ResourceIDs = []string{"libvirt|qemu:///session|vm|" + workspaceVMID} }, true},
		{"invalid VM ID", func(p *domain.Plan) { p.ResourceIDs = []string{"libvirt|qemu:///system|vm|not-a-uuid"} }, true},
		{"zero VM ID", func(p *domain.Plan) {
			p.ResourceIDs = []string{"libvirt|qemu:///system|vm|00000000-0000-0000-0000-000000000000"}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, p, c := outcomeFixture(t, "vm.start")
			tc.edit(&p)
			if tc.seal {
				sealOutcomePlan(t, &p)
			}
			c.plan = p
			assertNoOutcomeShortcut(t, readJobOutcome(context.Background(), c, o))
		})
	}
}

func TestJobOutcomeCreationRejectsIncompleteOrMismatchedReceipt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		group string
		field string
		value any
	}{
		{"incomplete", "", "complete", false},
		{"missing receipt", "", "receipt", nil},
		{"wrong job", "operation", "operationID", outcomeOtherVMID},
		{"wrong operation plan", "operation", "planID", outcomeOtherVMID},
		{"not succeeded", "operation", "state", "running"},
		{"wrong receipt plan", "receipt", "planID", outcomeOtherVMID},
		{"wrong receipt job", "receipt", "operationID", outcomeOtherVMID},
		{"wrong receipt connection", "receipt", "connection", "qemu:///session"},
		{"wrong receipt VM", "receipt", "vmID", outcomeOtherVMID},
		{"empty receipt VM", "receipt", "vmID", ""},
		{"invalid receipt VM", "receipt", "vmID", "unverified"},
		{"undefined VM", "receipt", "defined", false},
		{"unverified volumes", "receipt", "volumesVerified", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, _, c := outcomeFixture(t, "vm.create")
			r := outcomeCreationResult(o)
			dst := r
			if tc.group != "" {
				dst = r[tc.group].(map[string]any)
			}
			dst[tc.field] = tc.value
			c.receipt = r
			assertNoOutcomeShortcut(t, readJobOutcome(context.Background(), c, o))
		})
	}
}

func TestJobOutcomeServiceFailuresNeverOfferShortcut(t *testing.T) {
	for _, method := range []string{"plan.show", "vm.creation.result"} {
		for _, failure := range []string{"transport", "service", "decode", "marshal"} {
			t.Run(method+"/"+failure, func(t *testing.T) {
				o, _, c := outcomeFixture(t, "vm.create")
				c.receipt = outcomeCreationResult(o)
				switch failure {
				case "transport":
					c.failMethod, c.err = method, errors.New("service disconnected")
				case "service":
					c.failMethod, c.serviceError = method, domain.Fail("NOT_FOUND", "record unavailable")
				default:
					var invalid any = "not an object"
					if failure == "marshal" {
						invalid = make(chan int)
					}
					if method == "plan.show" {
						c.plan = invalid
					} else {
						c.receipt = invalid
					}
				}
				assertNoOutcomeShortcut(t, readJobOutcome(context.Background(), c, o))
			})
		}
	}
	o, _, _ := outcomeFixture(t, "vm.start")
	assertNoOutcomeShortcut(t, readJobOutcome(context.Background(), nil, o))
}

func TestJobOutcomeUnknownOperationDoesNotInventSuccess(t *testing.T) {
	o, _, c := outcomeFixture(t, "plugin.example.run")
	got := readJobOutcome(context.Background(), c, o)
	if got.VMID != "" || got.Title != "" || got.Summary != "" || got.Loading {
		t.Fatalf("unknown operation invented an outcome: %+v", got)
	}
}

func TestJobOutcomeLateReplyAfterPageChangeIgnored(t *testing.T) {
	o, _, c := outcomeFixture(t, "vm.start")
	m := fixtureWorkspace()
	m.Client = c
	m.Section = 8
	m.Detail = generic(domain.Job{ID: o.JobID, PlanID: o.PlanID, State: o.State})
	cmd := m.loadJobOutcome(false)
	if cmd == nil || m.JobOutcome == nil || !m.JobOutcome.Loading {
		t.Fatal("missing outcome read")
	}
	reply := cmd()
	m.page(1)
	next, _ := m.Update(reply)
	m = next.(Workspace)
	if m.Section != 1 || m.currentJobOutcome() != nil || m.openJobVM() != nil || m.Detail != nil {
		t.Fatal("late outcome reopened another page")
	}
	// Returning to the same job must trigger a fresh read, not revive the old
	// pending result that was canceled by navigation.
	m.page(8)
	m.Detail = generic(domain.Job{ID: o.JobID, PlanID: o.PlanID, State: o.State})
	next, _ = m.Update(reply)
	m = next.(Workspace)
	if current := m.currentJobOutcome(); current != nil && !current.Loading {
		t.Fatal("old result reappeared after returning to Jobs")
	}
	if m.loadJobOutcome(false) == nil {
		t.Fatal("canceled outcome prevented a fresh result read")
	}
}

func TestJobOutcomeLateReplyForAnotherJobIgnored(t *testing.T) {
	o, _, c := outcomeFixture(t, "vm.start")
	m := fixtureWorkspace()
	m.Client = c
	m.Section = 8
	m.Detail = generic(domain.Job{ID: o.JobID, PlanID: o.PlanID, State: o.State})
	cmd := m.loadJobOutcome(false)
	m.Detail = generic(domain.Job{ID: outcomeOtherVMID, PlanID: o.PlanID, State: o.State})
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.currentJobOutcome() != nil || m.openJobVM() != nil {
		t.Fatal("previous job's result became another job's shortcut")
	}
}

func TestJobOutcomeAdvancedCatalogCancelsPendingOpenVM(t *testing.T) {
	o, _, c := outcomeFixture(t, "vm.start")
	o = readJobOutcome(context.Background(), c, o)
	m := fixtureWorkspace()
	m.Section = 8
	m.Detail = generic(domain.Job{ID: o.JobID, PlanID: o.PlanID, State: o.State})
	m.JobOutcome = &o
	if m.openJobVM() == nil || !m.Busy {
		t.Fatal("Open VM did not begin its observed inventory read")
	}
	token := m.Pending["job-open-vm"]
	if token == 0 {
		t.Fatal("missing inventory read token")
	}
	m.advanced()
	if !m.Advanced || m.Busy || m.JobOutcome != nil || m.Pending["job-open-vm"] != 0 {
		t.Fatal("advanced catalog retained pending Open VM")
	}
	vm := workspaceVM(workspaceVMID, "Completed VM")
	vm.Fingerprint = strings.Repeat("c", 64)
	next, cmd := m.Update(workspaceReply{Kind: "job-open-vm", Token: token, Response: app.Response{Data: vm}})
	m = next.(Workspace)
	if cmd != nil || !m.Advanced || m.Section != 8 || resourceID(m.Detail) != o.JobID || m.Busy {
		t.Fatal("late inventory reply replaced the advanced catalog or job details")
	}
}

func TestJobOutcomeAsyncResultKeepsEventsFocused(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "success"
		if failed {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			o, _, c := outcomeFixture(t, "vm.start")
			m := fixtureWorkspace()
			m.Client = c
			m.Section = 8
			m.Detail = generic(domain.Job{ID: o.JobID, PlanID: o.PlanID, State: o.State})
			cmd := m.loadJobOutcome(false)
			if cmd == nil {
				t.Fatal("missing asynchronous outcome read")
			}
			m.ButtonFocus, m.ButtonIndex = true, 0
			if m.jobButtons()[m.ButtonIndex].key != "action:operation watch" {
				t.Fatal("fixture did not focus Events")
			}
			if failed {
				c.failMethod, c.err = "plan.show", errors.New("service disconnected")
			}
			next, _ := m.Update(cmd())
			m = next.(Workspace)
			wantFirst := "job-open-vm"
			if failed {
				wantFirst = "job-refresh-result"
			}
			buttons := m.jobButtons()
			if buttons[0].key != wantFirst || !m.ButtonFocus || m.ButtonIndex != 1 || buttons[m.ButtonIndex].key != "action:operation watch" {
				t.Fatal("new result action stole Events focus", buttons, m.ButtonFocus, m.ButtonIndex)
			}
		})
	}
}

func TestJobOutcomeRefreshClearsFocusWhenResultButtonDisappears(t *testing.T) {
	o, _, c := outcomeFixture(t, "vm.start")
	o.Loading, o.Error = false, "service disconnected"
	m := fixtureWorkspace()
	m.Client = c
	m.Section = 8
	m.Detail = generic(domain.Job{ID: o.JobID, PlanID: o.PlanID, State: o.State})
	m.JobOutcome = &o
	m.ButtonFocus, m.ButtonIndex = true, 0
	if m.jobButtons()[0].key != "job-refresh-result" {
		t.Fatal("fixture did not focus Refresh result")
	}
	cmd := m.loadJobOutcome(true)
	if cmd == nil || m.JobOutcome == nil || !m.JobOutcome.Loading {
		t.Fatal("explicit refresh did not begin")
	}
	if m.ButtonFocus || m.ButtonIndex != 0 || m.jobButtons()[0].key != "action:operation watch" {
		t.Fatal("disappearing Refresh result moved Enter focus onto Events")
	}
	next, _ := m.Update(cmd())
	m = next.(Workspace)
	if m.ButtonFocus || m.jobButtons()[0].key != "job-open-vm" {
		t.Fatal("completed refresh unexpectedly refocused an action")
	}
}
