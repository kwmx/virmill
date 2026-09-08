package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
)

type workspaceClient struct {
	calls    []string
	requests []app.Request
	response app.Response
	err      error
}

func (c *workspaceClient) Call(_ context.Context, method string, r app.Request) (app.Response, error) {
	c.calls = append(c.calls, method)
	c.requests = append(c.requests, r)
	return c.response, c.err
}

const workspaceVMID = "12345678-1234-4234-8234-123456789abc"

func workspaceVM(id, name string) domain.VM {
	return domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: id}, Name: name, State: "stopped", Ownership: "external", PersistentXML: "<domain><vcpu>4</vcpu><memory unit='MiB'>4096</memory></domain>"}
}
func fixtureWorkspace() Workspace {
	m := NewWorkspace(&workspaceClient{}, "qemu:///system")
	m.NoColor = true
	m.Data["vms"] = generic([]domain.VM{workspaceVM(workspaceVMID, "Recovery workstation"), workspaceVM("22345678-1234-4234-8234-123456789abc", "Build guest")})
	m.Data["jobs"] = []any{}
	m.Data["pools"] = []any{}
	return m
}
func wk(m Workspace, key string) (Workspace, tea.Cmd) {
	var v tea.KeyMsg
	switch key {
	case "enter":
		v.Type = tea.KeyEnter
	case "esc":
		v.Type = tea.KeyEsc
	case "tab":
		v.Type = tea.KeyTab
	case "down":
		v.Type = tea.KeyDown
	case "up":
		v.Type = tea.KeyUp
	case "space":
		v.Type = tea.KeySpace
	default:
		v = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(v)
	return next.(Workspace), cmd
}
func testWorkspacePlan(t *testing.T) domain.Plan {
	t.Helper()
	p := domain.Plan{APIVersion: domain.APIVersion, ID: "33345678-1234-4234-8234-123456789abc", ActorUID: 1000, ConnectionID: "qemu:///system", Operation: "vm.start", ResourceIDs: []string{"libvirt|qemu:///system|vm|" + workspaceVMID}, CreatedAt: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), InputDigest: strings.Repeat("a", 64), Acknowledgements: []string{"start-selected-vm", "review-network-exposure"}, Risks: []string{"Selected VM may access its configured networks"}, Steps: []domain.Step{{ID: "start", Action: "Start exact VM"}}}
	var err error
	p.Digest, err = operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkspaceResourceFirstDefaultAndNativeUUID(t *testing.T) {
	m := fixtureWorkspace()
	view := m.View()
	for _, text := range []string{"Overview", "Virtual machines", "Recovery workstation", "Build guest", "[ a More ]"} {
		if !strings.Contains(view, text) {
			t.Fatal("missing", text, view)
		}
	}
	if strings.Contains(view, "persistentXML") || strings.Contains(view, "Input: path/ID") {
		t.Fatal("command dump remains default")
	}
	m, _ = wk(m, "2")
	m.Selected = 1
	var cmd tea.Cmd
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("details missing")
	}
	cmd()
	c := m.Client.(*workspaceClient)
	if c.calls[len(c.calls)-1] != "inventory.get" || c.requests[len(c.requests)-1].ID != workspaceVMID {
		t.Fatal(c.calls, c.requests)
	}
	if !strings.Contains(m.View(), workspaceVMID) {
		t.Fatal("full stable ID missing", m.View())
	}
}
func TestWorkspaceCanceledDetailCannotReopenAndRefreshRetainsIdentity(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	m.Selected = 1
	m, _ = m.showForTest()
	token := m.Pending["detail"]
	m, _ = wk(m, "esc")
	next, _ := m.Update(workspaceReply{Kind: "detail", Token: token, Response: app.Response{Data: workspaceVM(workspaceVMID, "Late response")}})
	m = next.(Workspace)
	if m.Detail != nil {
		t.Fatal("late dismissed detail reopened")
	}
	cmd := m.request("vms", "inventory.list", app.Request{})
	_ = cmd
	token = m.Pending["vms"]
	next, _ = m.Update(workspaceReply{Kind: "vms", Token: token, Response: app.Response{Data: []domain.VM{workspaceVM(workspaceVMID, "A renamed workstation"), workspaceVM("22345678-1234-4234-8234-123456789abc", "Build guest")}}})
	m = next.(Workspace)
	if m.selectedVM().Key.UUID != workspaceVMID {
		t.Fatal("refresh changed selection")
	}
	_ = m.request("vms", "inventory.list", app.Request{})
	token = m.Pending["vms"]
	next, _ = m.Update(workspaceReply{Kind: "vms", Token: token, Response: app.Response{Data: []domain.VM{workspaceVM("22345678-1234-4234-8234-123456789abc", "Build guest")}}})
	m = next.(Workspace)
	m, cmd = wk(m, "s")
	if cmd != nil || m.Selected != -1 {
		t.Fatal("removed identity silently retargeted")
	}
}
func (m Workspace) showForTest() (Workspace, tea.Cmd) { cmd := m.showRow(); return m, cmd }
func TestWorkspaceApprovalRequiresVisibleExplicitAcknowledgements(t *testing.T) {
	m := fixtureWorkspace()
	p := testWorkspacePlan(t)
	m.Plan = &p
	m.Approved = make([]bool, len(p.Acknowledgements))
	m, _ = wk(m, "enter")
	if !m.Reviewing {
		t.Fatal("missing review confirmation")
	}
	m.AckIndex = len(m.Approved)
	var cmd tea.Cmd
	m, cmd = wk(m, "enter")
	if cmd != nil {
		t.Fatal("unchecked risks accepted")
	}
	m.AckIndex = 0
	m, _ = wk(m, "space")
	m, _ = wk(m, "down")
	m, _ = wk(m, "space")
	m, _ = wk(m, "down")
	m, cmd = wk(m, "enter")
	if cmd == nil {
		t.Fatal("button did not submit")
	}
	cmd()
	c := m.Client.(*workspaceClient)
	q := c.requests[len(c.requests)-1]
	if q.Apply == nil || q.Apply.PlanID != p.ID || q.Apply.PlanDigest != p.Digest || len(q.Apply.Acknowledgements) != 2 {
		t.Fatal(q)
	}
	// A late unrelated detail response cannot unlock duplicate Apply.
	next, _ := m.Update(workspaceReply{Kind: "detail", Token: 99, Response: app.Response{Data: map[string]any{}}})
	m = next.(Workspace)
	_, again := wk(m, "enter")
	if again != nil {
		t.Fatal("double apply accepted")
	}
}
func TestWorkspaceHelpAndSmallTerminalNeverSubmitHiddenActions(t *testing.T) {
	for _, mode := range []string{"help", "small"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureWorkspace()
			p := testWorkspacePlan(t)
			m.Plan = &p
			m.Approved = []bool{true, true}
			m.Reviewing = true
			m.AckIndex = 2
			if mode == "help" {
				m.Help = true
			} else {
				m.Width = 40
				m.Height = 12
			}
			for _, key := range []string{"enter", "s", "a", "space"} {
				var cmd tea.Cmd
				m, cmd = wk(m, key)
				if cmd != nil {
					t.Fatal("hidden action", key)
				}
			}
		})
	}
}
func TestWorkspaceEveryRegisteredActionIsSelectable(t *testing.T) {
	m := fixtureWorkspace()
	m, _ = wk(m, ":")
	if len(m.catalog()) != len(ui.Actions) {
		t.Fatal("missing registered action")
	}
	for _, a := range ui.Actions {
		found := false
		for _, got := range m.catalog() {
			found = found || got.Command == a.Command
		}
		if !found {
			t.Fatal(a.Command)
		}
	}
	if strings.Contains(m.View(), "Advanced commands") || !strings.Contains(m.View(), "All tools") {
		t.Fatal("old command UI remains default")
	}
	m, _ = wk(m, "esc")
	if m.Advanced || m.Section != 0 {
		t.Fatal("catalog back lost location")
	}
}
func TestWorkspaceBadPlanDigestAndCanceledFormRefuseLateAuthority(t *testing.T) {
	m := fixtureWorkspace()
	m.Section = 1
	m.Selected = 1
	m.guided("resources")
	_ = m.request("plan", "vm.plan", app.Request{})
	token := m.Pending["plan"]
	m, _ = wk(m, "esc")
	p := testWorkspacePlan(t)
	next, _ := m.Update(workspaceReply{Kind: "plan", Token: token, Response: app.Response{Data: p}})
	m = next.(Workspace)
	if m.Plan != nil || m.Form != nil {
		t.Fatal("canceled form reopened")
	}
	_ = m.request("plan", "vm.plan", app.Request{})
	p.Digest = strings.Repeat("0", 64)
	next, _ = m.Update(workspaceReply{Kind: "plan", Token: m.Pending["plan"], Response: app.Response{Data: p}})
	m = next.(Workspace)
	if m.Plan != nil || !strings.Contains(m.Error, "digest") {
		t.Fatal("changed plan approvable")
	}
}
func TestWorkspaceSearchTextCannotExecuteAndScalarCaptureIDs(t *testing.T) {
	m := fixtureWorkspace()
	m, _ = wk(m, "/")
	m, _ = wk(m, "s")
	if !m.Searching || m.Search != "s" || len(m.Client.(*workspaceClient).calls) != 0 {
		t.Fatal("search executed lifecycle")
	}
	m, _ = wk(m, "esc")
	m.Section = 6
	m.Data["captures"] = []any{workspaceVMID}
	if rowName(m.rows()[0]) != workspaceVMID || resourceID(m.rows()[0]) != workspaceVMID {
		t.Fatal("capture identity hidden")
	}
}
func TestWorkspaceRenderingFitsAndSanitizes(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 36}, {60, 18}, {40, 10}} {
		m := fixtureWorkspace()
		m.Width, m.Height = size[0], size[1]
		m.ASCII = true
		for _, mode := range []string{"overview", "details", "catalog", "plan", "approval"} {
			t.Run(mode+string(rune(size[0])), func(t *testing.T) {
				n := m
				switch mode {
				case "details":
					n.Section = 1
					n.Detail = generic(workspaceVM(workspaceVMID, "Name\x1b[2J\u202e injected"))
					n.DetailTitle = "VM details"
				case "catalog":
					n.advanced()
				case "plan", "approval":
					p := testWorkspacePlan(t)
					n.Plan = &p
					n.Approved = []bool{false, false}
					n.Reviewing = mode == "approval"
				}
				view := n.View()
				if strings.Contains(view, "\x1b") || strings.ContainsRune(view, '\u202e') {
					t.Fatal("untrusted controls or color in no-color view")
				}
				lines := strings.Split(view, "\n")
				if len(lines) > n.Height {
					t.Fatal("height overflow", len(lines), n.Height)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > n.Width {
						t.Fatal("width overflow", line)
					}
				}
			})
		}
	}
}

// Optional generated renderer artifacts are UI layout fixtures, not host evidence.
func TestWorkspaceRenderFixtures(t *testing.T) {
	dir := os.Getenv("VIRMILL_TEST_WORKSPACE_RENDER")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{80, 24}, {120, 36}} {
		m := fixtureWorkspace()
		m.Width, m.Height = size[0], size[1]
		m.NoColor = false
		for _, page := range []string{"overview", "details", "actions", "form"} {
			n := m
			switch page {
			case "details":
				n.Section = 1
				n.Detail = generic(workspaceVM(workspaceVMID, "Recovery workstation"))
				n.DetailTitle = "VM details"
			case "actions":
				n.advanced()
			case "form":
				n.Section = 1
				n.Selected = 1
				n.guided("resources")
			}
			b, _ := json.Marshal(map[string]any{"width": n.Width, "height": n.Height, "ansi": n.View()})
			if err := os.WriteFile(filepath.Join(dir, page+"-"+string(rune('0'+size[0]/40))+".json"), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestWorkspaceActionButtonsAreKeyboardSelectable(t *testing.T) {
	m := fixtureWorkspace()
	m, _ = wk(m, "tab")
	if !m.ButtonFocus || m.NavFocus {
		t.Fatal("buttons cannot receive focus")
	}
	for m.buttons()[m.ButtonIndex].key != "a" {
		m, _ = wk(m, "down")
	}
	m, cmd := wk(m, "enter")
	if cmd != nil || !m.Advanced || m.CatalogSection != 0 {
		t.Fatal("Actions button did not open catalog")
	}
}
