//go:build linux

package tui

import (
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
)

// owner-ova-native-004: planning a VM from a 34 GiB prepared disk hashes it
// twice, about 80 seconds on the test host, longer than the usual 30-second
// wait. VM creation now gets the long wait, without counting as an import read.
func TestVMCreationGetsTheLongWait(t *testing.T) {
	if !ui.LongWait("vm.create") || ui.ImportRead("vm.create") || ui.LongWait("vm.plan") {
		t.Fatal("only disk-reading requests get the long wait")
	}
	for _, method := range []string{"import.prepare", "import.prepare-disks", "import.describe"} {
		if !ui.LongWait(method) {
			t.Fatalf("%s lost its long wait", method)
		}
	}
}

// A creation plan that still times out must say so instead of staying busy.
func TestATimedOutCreationPlanShowsTheErrorInTheChain(t *testing.T) {
	m := creationNetworkWorkspace(t, false)
	m.Chain = &importChain{Connection: m.Connection, PrepJob: chainPrepJob, Start: true, Cleanup: true}
	m.Busy = true
	m.Notice = "Images are ready. Creating the approved VM…"
	m.request("plan", "vm.create", app.Request{})
	m, _ = deliverErr(t, m, "plan", domain.Fail("WAIT_TIMEOUT", "Request timed out; no result was received."))
	view := m.View()
	if m.Busy || m.Chain != nil || strings.Contains(view, "Working...") || !strings.Contains(view, "The VM was not created") || !strings.Contains(view, "Request timed out") {
		t.Fatalf("timed-out creation plan was not reported: busy=%v chain=%v notice=%q error=%q", m.Busy, m.Chain != nil, m.Notice, m.Error)
	}
}

// deliverErr answers the pending request of kind with an error.
func deliverErr(t *testing.T, m Workspace, kind string, err error) (Workspace, any) {
	t.Helper()
	token := m.Pending[kind]
	if token == 0 {
		t.Fatalf("no pending %s request", kind)
	}
	next, cmd := m.Update(workspaceReply{Kind: kind, Token: token, Err: err})
	return next.(Workspace), cmd
}
