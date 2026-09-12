package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func TestWorkspaceApplyFailureShowsCompleteIssueAndPreservesReviewAndSetup(t *testing.T) {
	for _, kind := range []string{"helper-key", "generic"} {
		for _, envelope := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/envelope-%t", kind, envelope), func(t *testing.T) {
				m := creationNetworkWorkspace(t, true)
				m.Width, m.Height, m.ASCII = 80, 24, true
				snapshot := m.setupDocument()
				_ = m.openCreationNetwork()
				handoff := m.CreationNetwork
				plan := testWorkspacePlan(t)
				plan.Operation = "network.create"
				m.Plan = &plan
				m.Reviewing, m.Busy, m.Offset = true, true, 100
				m.ApplyKey = "keep-exact-network-request"
				m.Pending["apply"] = 410
				text := "Coordinator could not confirm the submitted operation."
				if kind == "helper-key" {
					text = "private helper-key.pem unavailable; complete the documented administrator setup."
				}
				// Distinct short words prove all wrapped rows are reachable, not
				// merely the truncated footer's beginning or a repeated sentence.
				for i := 0; i < 160; i++ {
					text += fmt.Sprintf(" detail%03d", i)
				}
				text += " FULL-ISSUE-TAIL"
				failure := domain.Fail("PERMISSION_DENIED", text)
				reply := workspaceReply{Kind: "apply", Token: 410, Err: failure}
				if envelope {
					reply.Err, reply.Response = nil, app.Response{Error: failure}
				}
				next, cmd := m.Update(reply)
				m = next.(Workspace)
				if cmd != nil || m.Offset != 0 || m.Reviewing || m.Busy || m.Pending["apply"] != 0 || m.ApplyKey != "keep-exact-network-request" || !reflect.DeepEqual(m.Plan, &plan) || m.CreationNetwork != handoff || !reflect.DeepEqual(m.setupDocument(), snapshot) {
					t.Fatal("apply failure hid the issue or replaced review/request/source state")
				}
				first := m.View()
				if !strings.Contains(first, "Submission needs attention") || strings.Contains(first, "FULL-ISSUE-TAIL") {
					t.Fatal("issue did not begin at the top or fixture did not exceed the viewport", first)
				}
				pages, previous := []string{}, ""
				for i := 0; i < 30; i++ {
					view := m.View()
					if len(strings.Split(view, "\n")) > 24 {
						t.Fatal("issue exceeds terminal rows")
					}
					for _, line := range strings.Split(view, "\n") {
						if ansi.StringWidth(line) > 80 {
							t.Fatal("issue exceeds terminal columns", line)
						}
					}
					if view == previous {
						break
					}
					pages = append(pages, view)
					previous = view
					next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
					m = next.(Workspace)
					if cmd != nil {
						t.Fatal("reading an issue issued a service command")
					}
				}
				all := strings.Join(pages, "\n")
				for _, word := range strings.Fields(failure.Error()) {
					if !strings.Contains(all, word) {
						t.Fatalf("full error word %q was not reachable", word)
					}
				}
				for _, want := range []string{"FULL-ISSUE-TAIL", "Check Jobs before trying again", "Plan ID:", plan.ID} {
					if !strings.Contains(all, want) {
						t.Fatalf("issue/plan lost %q", want)
					}
				}
				if kind == "helper-key" && !strings.Contains(all, "A host administrator must finish helper setup") {
					t.Fatal("missing helper key has no administrator recovery guidance")
				}
				if kind == "generic" && strings.Contains(all, "A host administrator must finish helper setup") {
					t.Fatal("generic failure was misdiagnosed as helper setup")
				}
				if len(m.Client.(*workspaceClient).calls) != 0 || m.ApplyKey != "keep-exact-network-request" || m.CreationNetwork != handoff || !reflect.DeepEqual(m.Plan, &plan) || !reflect.DeepEqual(m.setupDocument(), snapshot) {
					t.Fatal("reading complete issue resubmitted or changed retained setup")
				}
				m, _ = wk(m, "esc")
				if m.Plan != nil || m.NetworkForm == nil || m.CreationNetwork != handoff {
					t.Fatal("Back did not return to retained network settings")
				}
				m, _ = wk(m, "esc")
				if m.CreationNetwork != nil || m.Creation == nil || m.Import == nil || !reflect.DeepEqual(m.setupDocument(), snapshot) {
					t.Fatal("Back after issue lost VM source/settings")
				}
			})
		}
	}
}
