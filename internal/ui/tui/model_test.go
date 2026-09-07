package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

type recorder struct {
	method  string
	request app.Request
}

func (r *recorder) Call(ctx context.Context, method string, p app.Request) (app.Response, error) {
	r.method = method
	r.request = p
	return app.Response{APIVersion: domain.APIVersion, Data: map[string]string{"label": "fixture\x1b[2J"}, Warnings: []string{}}, nil
}
func TestKeyboardNavigationSharedServiceAndResize(t *testing.T) {
	r := &recorder{}
	m := New(r, "qemu:///system")
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = model.(Model)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter did not dispatch")
	}
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	if r.method != "host.inspect" {
		t.Fatal("different service method")
	}
	if strings.Contains(m.View(), "\x1b") {
		t.Fatal("untrusted terminal escape rendered")
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(Model)
	if sections[m.Section] != "VMs" {
		t.Fatal("navigation failed")
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !model.(Model).Quit {
		t.Fatal("cannot detach")
	}
}
func TestEscDiscardsApprovalWithoutMutation(t *testing.T) {
	r := &recorder{}
	m := New(r, "qemu:///system")
	m.Plan = &domain.Plan{ID: "plan", Digest: "digest"}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = model.(Model)
	if !m.Confirm {
		t.Fatal("no approval dialog")
	}
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || model.(Model).Confirm || r.method != "" {
		t.Fatal("canceled dialog executed")
	}
}

func TestPluginFormDispatchAndScrollableActionMenu(t *testing.T) {
	r := &recorder{}
	m := New(r, "qemu:///system")
	m.Section = 9
	for i, a := range m.actions() {
		if a.Command == "plugin install" {
			m.Selected = i
		}
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	m.Input = `{"path":"/tmp/signed.tar","input":{"keyID":"reviewed","publicKey":"fixture-public"}}`
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("plugin form did not dispatch")
	}
	m = model.(Model)
	model, _ = m.Update(cmd())
	m = model.(Model)
	if r.method != "plugin.plan" || r.request.Action != "install" || r.request.Path != "/tmp/signed.tar" || r.request.Input["keyID"] != "reviewed" {
		t.Fatal("TUI plugin form differs from CLI", r)
	}
	if strings.Count(m.View(), "\n") > 24 {
		t.Fatal("plugin action menu overflowed 80x24 terminal")
	}
}

func TestImportFormDispatchesCompleteMappingToSharedService(t *testing.T) {
	r := &recorder{}
	m := New(r, "qemu:///system")
	for i, name := range sections {
		if name == "VMs" {
			m.Section = i
		}
	}
	found := false
	for i, a := range m.actions() {
		if a.Command == "import prepare" {
			m.Selected = i
			found = true
		}
	}
	if !found {
		t.Fatal("import preparation is inaccessible in TUI")
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	m.Input = `{"path":"/tmp/source.ova","input":{"destination":"/tmp/private/prepared","systemID":"appliance","disks":[{"id":"boot","format":"vmdk","maximumVirtualBytes":16777216},{"id":"data","format":"vmdk","maximumVirtualBytes":16777216}]}}`
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("form not submitted")
	}
	cmd()
	if r.method != "import.prepare" || r.request.Path != "/tmp/source.ova" || r.request.Input["systemID"] != "appliance" {
		t.Fatal("TUI import mapping lost", r)
	}
}

func TestStorageAndNetworkInventoryAreReachableWithoutApproval(t *testing.T) {
	for _, test := range []struct{ section, method string }{{"Storage", "storage.pool.list"}, {"Networks", "network.list"}} {
		r := &recorder{}
		m := New(r, "qemu:///session")
		for i, name := range sections {
			if name == test.section {
				m.Section = i
			}
		}
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("inventory not reachable", test.section)
		}
		cmd()
		if r.method != test.method || r.request.Connection != "qemu:///session" {
			t.Fatal("TUI inventory changed connection or method", r)
		}
	}
}
