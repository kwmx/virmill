package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func allocationTUIExample(t *testing.T) (string, network.AllocationConfig) {
	t.Helper()
	raw, err := os.ReadFile(protectedNetworkExample(t, "auto-lab.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "automatic lab declaration.yaml")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../../examples/network-allocation.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg network.AllocationConfig
	if err = wire.Decode(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if err = network.ValidateAllocationConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func allocationTUIEmptyErrorData[T any](t *testing.T, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var got, empty T
	if err = wire.Decode(raw, &got); err != nil || !reflect.DeepEqual(got, empty) {
		t.Fatal("error exposed a nonzero success payload", string(raw), err)
	}
}

// Open the registered path form and type the real filename, including physical
// space keys. Navigation and editing must not dispatch a service request.
func allocationTUIPath(t *testing.T, m Model, path string, cancel bool) Model {
	t.Helper()
	found := false
	for section := range sections {
		m.Section = section
		for index, action := range m.actions() {
			if action.Command == "network create" {
				m.Selected, found = index, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("network create is absent")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || !m.Editing {
		t.Fatal("path navigation executed work")
	}
	for _, r := range path {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			key = tea.KeyMsg{Type: tea.KeySpace}
		}
		next, cmd = m.Update(key)
		m = next.(Model)
		if cmd != nil {
			t.Fatal("typing submitted a request")
		}
	}
	if m.Input != path {
		t.Fatal("path was changed", m.Input, path)
	}
	key := tea.KeyEnter
	if cancel {
		key = tea.KeyEsc
	}
	next, cmd = m.Update(tea.KeyMsg{Type: key})
	m = next.(Model)
	if cancel {
		if cmd != nil || m.Editing || m.Busy {
			t.Fatal("canceling the path submitted work")
		}
		return m
	}
	if cmd == nil || !m.Busy {
		t.Fatal("path was not submitted")
	}
	next, _ = m.Update(cmd())
	return next.(Model)
}

func TestNetworkAllocationTUIExactReviewApprovalAndPathCancellation(t *testing.T) {
	c := newProtectedNetworkClient(t)
	path, cfg := allocationTUIExample(t)
	c.service.NetworkAllocation = func(ctx context.Context) (network.AllocationConfig, error) { return cfg, ctx.Err() }
	m := allocationTUIPath(t, New(c, "qemu:///system"), path, true)
	if len(c.requests) != 0 {
		t.Fatal("path cancellation reached the service")
	}
	assertProtectedNetworkNoMutation(t, c)
	m = allocationTUIPath(t, m, path, false)
	p := protectedNetworkTUIPlan(t, m)
	assertProtectedNetworkReview(t, p, "lab", "services-only", "10.0.0.0/24", true, 2)
	if len(c.requests) != 1 || c.requests[0].Path != path || c.requests[0].Action != "create" || c.requests[0].Apply != nil || c.firewall.checks != 0 {
		t.Fatal("automatic preview changed request or acquired authority", c.requests)
	}
	raw, _ := json.Marshal(p.Review["allocation"])
	var allocation struct {
		Version        int                      `json:"version"`
		RequestedCIDR  string                   `json:"requestedCIDR"`
		Config         network.AllocationConfig `json:"config"`
		ObservedDigest string                   `json:"observedDigest"`
	}
	if err := wire.Decode(raw, &allocation); err != nil || allocation.Version != 1 || allocation.RequestedCIDR != "auto" || len(allocation.ObservedDigest) != 64 || !reflect.DeepEqual(allocation.Config, cfg) {
		t.Fatal("automatic review changed", string(raw), err)
	}
	// All allocation and qualification fields must remain accessible in the
	// existing 80x24 result view. Paging is not approval.
	pages := ""
	for i := 0; m.Offset <= len(wrap(m.Output, m.Width)); i++ {
		if i == 200 {
			t.Fatal("automatic review exceeded bounded paging")
		}
		pages += m.View()
		old := m.Offset
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(Model)
		if cmd != nil {
			t.Fatal("paging dispatched work")
		}
		if m.Offset == old {
			break
		}
	}
	for _, visible := range []string{"allocation", "requestedCIDR", "auto", "10.0.0.0/24", "observedDigest", "external-lab", "packetVerification", "not-run", "guestRoutingVerified"} {
		if !strings.Contains(strings.ReplaceAll(pages, "\n", ""), visible) {
			t.Fatal("automatic review is not visible", visible)
		}
	}
	if len(c.requests) != 1 {
		t.Fatal("review navigation executed work")
	}
	assertProtectedNetworkNoMutation(t, c)
	// A later settings change cannot rewrite a stored plan or its digest.
	if err := wire.Decode([]byte(`{"version":1,"ranges":[{"cidr":"172.16.0.0/12","prefixLength":24}],"planned":[]}`), &cfg); err != nil {
		t.Fatal(err)
	}
	m = protectedNetworkTUIAction(t, m, "plan show", p.ID)
	q := protectedNetworkTUIPlan(t, m)
	if q.Digest != p.Digest || !reflect.DeepEqual(q.Review, p.Review) {
		t.Fatal("readback reallocated the reviewed subnet")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if cmd != nil || !m.Confirm {
		t.Fatal("automatic plan bypassed approval")
	}
	m.Input = "wrong-digest"
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || len(c.requests) != 2 {
		t.Fatal("wrong digest submitted the automatic plan")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil || m.Confirm || len(c.requests) != 2 {
		t.Fatal("approval cancellation submitted work")
	}
	assertProtectedNetworkNoMutation(t, c)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = next.(Model)
	if cmd != nil || !m.Confirm {
		t.Fatal("approval could not reopen")
	}
	m.Input = p.Digest
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("exact digest did not submit")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	apply := c.requests[len(c.requests)-1].Apply
	acks := append([]string{}, p.Acknowledgements...)
	sort.Strings(acks)
	if apply == nil || apply.PlanID != p.ID || apply.PlanDigest != p.Digest || !reflect.DeepEqual(apply.Acknowledgements, acks) || c.firewall.checks != 1 {
		t.Fatal("automatic plan approval changed", apply)
	}
	var denied app.Response
	if err := wire.Decode([]byte(m.Output), &denied); err != nil || denied.Error == nil || denied.Error.Code != "PERMISSION_DENIED" || m.Plan != nil || m.Confirm {
		t.Fatal("TUI hid the shared grant refusal or retained success", m.Output, err)
	}
	allocationTUIEmptyErrorData[domain.Job](t, denied.Data)
	if !reflect.DeepEqual(c.methods, []string{"network.create", "plan.show", "operation.apply"}) {
		t.Fatal(c.methods)
	}
	assertProtectedNetworkNoMutation(t, c)
}

func TestNetworkAllocationTUIFailedSelectionClearsPriorApproval(t *testing.T) {
	for _, fault := range []string{"canceled", "settings-unavailable"} {
		t.Run(fault, func(t *testing.T) {
			c := newProtectedNetworkClient(t)
			path, cfg := allocationTUIExample(t)
			c.service.NetworkAllocation = func(ctx context.Context) (network.AllocationConfig, error) { return cfg, ctx.Err() }
			m := allocationTUIPath(t, New(c, "qemu:///system"), path, false)
			p := protectedNetworkTUIPlan(t, m)
			wantCode := "OPERATION_FAILED"
			if fault == "canceled" {
				c.canceled = true
			} else {
				wantCode = "INVALID_INPUT"
				cfg.Version = 0
			}
			m = allocationTUIPath(t, m, path, false)
			var r app.Response
			if err := wire.Decode([]byte(m.Output), &r); err != nil || r.Error == nil || r.Error.Code != wantCode || m.Plan != nil || m.Confirm || strings.Contains(m.Output, p.Digest) || strings.Contains(m.Output, "networkXML") {
				t.Fatal("failed selection retained old approval or hid error", m.Output, err)
			}
			allocationTUIEmptyErrorData[domain.Plan](t, r.Data)
			for _, request := range c.requests {
				if request.Apply != nil {
					t.Fatal("failure submitted authority")
				}
			}
			assertProtectedNetworkNoMutation(t, c)
		})
	}
}
