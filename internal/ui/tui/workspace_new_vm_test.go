package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
)

const newVMPool = "abcdefab-1234-4234-8234-123456789abc"
const newVMPoolPlan = "99999999-9999-4999-8999-9999999999a1"
const newVMPoolJob = "99999999-9999-4999-8999-9999999999a2"

func newVMKey(t *testing.T, m Workspace, key tea.KeyMsg, presses *int) (Workspace, tea.Cmd) {
	t.Helper()
	*presses++
	next, cmd := m.Update(key)
	return next.(Workspace), cmd
}

// rawIdentity is anything a person should not have to read: UUIDs, digests,
// resource keys and bracketed acknowledgement identifiers.
var rawIdentity = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-|[0-9a-f]{64}|libvirt:|disk-source:|\[[a-z]+(-[a-z]+)+\]`)

// The ADR 0065 path from a host with no storage pool: New VM, choose an
// installer, Create VM, Confirm. One confirmation covers the pool, the images,
// the VM, starting it and removing the prepared copy, and progress stays on
// the same screen.
func TestNewVMFromNothingNeedsOneConfirmation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := fixtureWorkspace()
	m.Width, m.Height = 110, 44
	m.Section = 1
	c := &workspaceClient{}
	m.Client = c
	presses := 0

	m, _ = newVMKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, &presses)
	if m.NewVM == nil || m.Picker == nil || m.draftModal != "" {
		t.Fatal("New VM must open the file chooser straight away, with no saved-setup prompt")
	}

	// Choosing the file is not counted; Virmill reads it by itself.
	iso := filepath.Join(t.TempDir(), "Fedora-Workstation.iso")
	if err := os.WriteFile(iso, []byte("iso"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.importPicked(iso)
	m.describeImport()
	m, cmd := deliver(t, m, "import-inspect", importer.SourceDescription{Source: iso, Kind: "iso", Name: "Fedora-Workstation", PhysicalBytes: 3})
	if cmd == nil || m.Pending["creation-load"] == 0 {
		t.Fatal("reading the file did not go on to read VM settings")
	}
	options := creationFormFixture().Options
	options.Graphics = []string{"spice-unix", "vnc-unix"}
	nat := domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: m.Connection, Kind: "network", UUID: creationFormNetwork}, Name: "default", Active: true, PersistentXML: "<network><name>default</name><forward mode='nat'/></network>"}
	m, _ = deliver(t, m, "creation-load", creationBundle{Source: importCreationSource(m.Import.Draft), Options: options, Networks: []domain.VirtualNetwork{nat}})
	if !m.newVMSettingsActive() {
		t.Fatal("settings page not shown", m.View())
	}
	view := m.View()
	for _, want := range []string{"New VM", "From installer: Fedora-Workstation.iso", "Disk size (GiB): [32]", "[x] Start it and open its display", "> [ Create VM ]", "Virmill sets up libvirt's standard pool first", "default NAT network (internet access)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("settings page lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Step ") || strings.Contains(view, "No storage pool") || rawIdentity.MatchString(view) {
		t.Fatalf("settings page shows steps, a pool dead end or identifiers:\n%s", view)
	}

	m, cmd = newVMKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}, &presses)
	if cmd == nil || m.Pending["plan"] == 0 {
		t.Fatal("Create VM did not plan the storage pool", m.NewVM.Error)
	}
	cmd()
	if c.calls[len(c.calls)-1] != "storage.pool.create" {
		t.Fatal(c.calls)
	}
	pool := chainPlan(t, newVMPoolPlan, "storage.pool.create", m.Connection, []string{"host-mutation", "exclusive-storage-writer"}, map[string]any{"definition": map[string]any{"uuid": newVMPool, "name": "default", "path": "/var/lib/libvirt/images"}}, nil)
	m, cmd = deliver(t, m, "plan", pool)
	if cmd == nil || m.Plan != nil {
		t.Fatal("the pool plan must lead to the preparation plan, not its own confirmation")
	}
	cmd()
	if c.calls[len(c.calls)-1] != "import.prepare-install" || c.requests[len(c.requests)-1].Input["offlineSources"] != true {
		t.Fatal(c.calls)
	}
	prep := chainPlan(t, chainPrepPlan, "import.prepare-install", "local", []string{"write-import-artifacts", "offline-source-files"}, map[string]any{}, nil)
	m, _ = deliver(t, m, "plan", prep)
	if m.Plan == nil || m.ChainOffer == nil {
		t.Fatal("confirmation does not cover the later steps")
	}
	view = strings.Join(m.confirmationLines(110, 400), "\n")
	for _, want := range []string{"First, Virmill creates storage pool default in /var/lib/libvirt/images", "By confirming, you agree that:", "No other administrator or tool is changing this storage", "Start the VM as soon as it is created", "Delete the prepared copy", "[ Confirm ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirmation lacks %q:\n%s", want, view)
		}
	}
	if rawIdentity.MatchString(view) {
		t.Fatalf("confirmation shows identifiers:\n%s", view)
	}

	m, cmd = newVMKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}, &presses)
	if cmd == nil || m.Pending["pool-apply"] == 0 {
		t.Fatal("Confirm did not apply the pool first")
	}
	cmd()
	if last := c.requests[len(c.requests)-1]; last.Apply == nil || last.Apply.PlanID != newVMPoolPlan || !slices.Equal(last.Apply.Acknowledgements, pool.Acknowledgements) {
		t.Fatal("pool apply", last.Apply)
	}
	m, cmd = deliver(t, m, "pool-apply", domain.Job{ID: newVMPoolJob, PlanID: newVMPoolPlan, State: "queued"})
	if cmd == nil || m.Pending["apply"] == 0 {
		t.Fatal("the images were not prepared after the pool was accepted")
	}
	cmd()
	if last := c.requests[len(c.requests)-1]; last.Apply == nil || last.Apply.PlanID != chainPrepPlan || !slices.Equal(last.Apply.Acknowledgements, prep.Acknowledgements) {
		t.Fatal("preparation apply", last.Apply)
	}
	m, _ = deliver(t, m, "apply", domain.Job{ID: chainPrepJob, PlanID: chainPrepPlan, State: "queued"})
	if m.Chain == nil || !m.newVMProgressActive() {
		t.Fatal("progress is not on the same screen")
	}
	view = m.View()
	for _, want := range []string{"New VM", "Creating Fedora-Workstation", "[x] Set up storage pool default", "[>] Copy and check the images", "[ ] Create the VM", "[ ] Start the VM", "[ ] Open its display"} {
		if !strings.Contains(view, want) {
			t.Fatalf("progress lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Job details") || rawIdentity.MatchString(view) {
		t.Fatalf("progress switched to Jobs or shows identifiers:\n%s", view)
	}
	if presses > 6 {
		t.Fatalf("%d keypresses; ADR 0065 allows at most six", presses)
	}
}

func TestNewVMEscAsksBeforeDiscardingAndAdvancedKeepsChanges(t *testing.T) {
	m := chainImportWorkspace(t)
	m.Width, m.Height = 110, 44
	m.Creation = nil
	m.NewVM = &newVMFlow{RealPools: m.Import.VM.Pools}
	if !m.newVMSettingsActive() {
		t.Fatal("fixture is not on the settings page")
	}
	m, _ = wk(m, "esc")
	if m.NewVM == nil || m.Import == nil || !strings.Contains(m.View(), "Press Esc again to discard") {
		t.Fatal("one Esc discarded the settings")
	}
	m, _ = wk(m, "tab")
	if m.NewVM == nil || m.NewVM.Discard {
		t.Fatal("another key must keep the settings")
	}
	for m.newVMControls()[m.NewVM.Focus].id != "advanced" {
		m, _ = wk(m, "tab")
	}
	m, _ = wk(m, "enter")
	if m.Creation == nil {
		t.Fatal("Advanced settings did not open")
	}
	m.Creation.CPUText = "3"
	m, _ = wk(m, "esc")
	if m.Creation != nil || !m.newVMSettingsActive() || m.Import.VM.CPUText != "3" {
		t.Fatal("leaving Advanced settings lost a change")
	}
	m, _ = wk(m, "esc")
	m, _ = wk(m, "esc")
	if m.NewVM != nil || m.Import != nil {
		t.Fatal("two Esc presses should discard")
	}
}

func TestNewVMFinishesOnTheRunningVM(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 110, 44
	m.NewVM = &newVMFlow{Running: true, Name: "lab", Started: true, Display: "open", Chain: importChain{PrepJob: chainPrepJob, CreateJob: chainCreateJob, VMID: workspaceVMID, StartJob: "99999999-9999-4999-8999-999999999996", Start: true, Cleanup: true}}
	view := m.View()
	for _, want := range []string{"lab is running", "[x] Start the VM", "[x] Open its display", "[x] Remove the prepared copy", "Closing that window does not stop the VM", "[ Open display ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("finish lacks %q:\n%s", want, view)
		}
	}
	m.Client = &workspaceClient{response: app.Response{}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m = next.(Workspace); cmd == nil || m.DisplayAutoVM != workspaceVMID {
		t.Fatal("Open display did not open it again")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m = next.(Workspace); m.NewVM != nil || m.Section != 1 {
		t.Fatal("Done should leave on the VM list")
	}

	failed := fixtureWorkspace()
	failed.Width, failed.Height = 110, 44
	failed.NewVM = &newVMFlow{Running: true, Name: "lab", Failure: "Not enough space in pool default.", Chain: importChain{PrepJob: chainPrepJob, CreateJob: chainCreateJob, Start: true}}
	if view := failed.View(); !strings.Contains(view, "The new VM needs attention") || !strings.Contains(view, "Not enough space") || !strings.Contains(view, "[!] Create the VM") {
		t.Fatal(view)
	}
}
