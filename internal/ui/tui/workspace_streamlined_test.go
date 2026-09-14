package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func TestDefaultImportFolderIsNewAndSafe(t *testing.T) {
	stamp := `-\d{8}-\d{6}$`
	for _, tt := range []struct {
		draft ImportDraft
		want  string
	}{
		{ImportDraft{VMName: "web server"}, "^web-server" + stamp},
		{ImportDraft{Source: "/images/Debian 12.qcow2"}, "^Debian-12" + stamp},
		{ImportDraft{Source: "/images/..my disk.img"}, "^my-disk" + stamp},
		{ImportDraft{Source: "/images/..hidden"}, "^import" + stamp},
		{ImportDraft{VMName: "ضيف"}, "^import" + stamp},
		{ImportDraft{VMName: strings.Repeat("a", 90)}, "^a{48}" + stamp},
	} {
		got := defaultImportFolder(tt.draft)
		if !regexp.MustCompile(tt.want).MatchString(got) || filepath.Base(got) != got {
			t.Fatalf("%+v: folder %q", tt.draft, got)
		}
	}
	root := importStagingRoot()
	if root != filepath.Join(os.Getenv("XDG_DATA_HOME"), "virmill", "imports") {
		t.Fatal("staging root ignores XDG_DATA_HOME", root)
	}
}

func TestSuggestedVMNameDropsImageExtensions(t *testing.T) {
	for in, want := range map[string]string{"debian-12.qcow2": "debian-12", "Win11.ISO": "Win11", "disk.vmdk": "disk", "appliance.v2": "appliance.v2", ".qcow2": ".qcow2", "": ""} {
		if got := suggestedVMName(in); got != want {
			t.Fatalf("suggestedVMName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImportSkipsFolderStepWithPrivateDefault(t *testing.T) {
	root := filepath.Join(t.TempDir(), "virmill", "imports")
	f := applianceSummaryFixture(t)
	f.StagingRoot = root
	next, intent := importFocus(t, f, "next").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if intent.Kind != "" || next.Page != 2 || next.Draft.DestinationParent != root || !strings.HasPrefix(next.Draft.DestinationName, "Windows-guest-") {
		t.Fatal("default folder not applied", next.Page, next.Error, next.Draft.DestinationParent, next.Draft.DestinationName)
	}
	if next.previousPage() != 1 {
		t.Fatal("folder step is no longer reachable")
	}
	found := false
	for _, c := range next.controls() {
		found = found || c.id == "saveInfo" && c.value == filepath.Join(root, next.Draft.DestinationName)
	}
	if !found {
		t.Fatal("save location is not shown")
	}
	chosen := f
	chosen.Draft.DestinationParent, chosen.Draft.DestinationName = "/chosen", "mine"
	again, _ := importFocus(t, chosen, "next").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if again.Page != 1 || again.Draft.DestinationParent != "/chosen" || again.Draft.DestinationName != "mine" {
		t.Fatal("chosen folder replaced or skipped")
	}
	if without, _ := importFocus(t, applianceSummaryFixture(t), "next").Update(tea.KeyMsg{Type: tea.KeyEnter}); without.Page != 1 || without.Draft.DestinationParent != "" {
		t.Fatal("folder step skipped without a default folder")
	}
	m := fixtureWorkspace()
	m.Import = &next
	if err := m.ensureImportStaging(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatal("default folder not created privately", info, err)
	}
	other := filepath.Join(t.TempDir(), "absent")
	m.Import.Draft.DestinationParent = other
	if err := m.ensureImportStaging(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("a folder the user chose was created")
	}
}

// After preparation a complete setup opens its review directly; an incomplete
// one opens the form at the missing setting.
func TestPreparedImagesOpenVMReviewDirectly(t *testing.T) {
	base := creationFormFixture()
	source := CreationSource{Kind: "PreparedDiskSet", Disks: []CreationSourceDisk{{SourceID: "boot"}}}
	pools := []domain.StoragePool{{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "storage-pool", UUID: creationFormPool}, Name: "default", Type: "dir", Active: true}}
	for _, tt := range []struct {
		name   string
		pools  []domain.StoragePool
		review bool
	}{{"complete", pools, true}, {"no-pool", nil, false}} {
		t.Run(tt.name, func(t *testing.T) {
			m := fixtureWorkspace()
			c := &workspaceClient{}
			m.Client = c
			m.CreationAutoReview = creationFormOperation
			m.Pending["creation-load"] = 41
			bundle := creationBundle{OperationID: creationFormOperation, Source: source, Options: base.Options, Pools: tt.pools}
			next, cmd := m.Update(workspaceReply{Kind: "creation-load", Token: 41, Response: app.Response{Data: bundle}})
			m = next.(Workspace)
			if m.Creation == nil || m.CreationAutoReview != "" {
				t.Fatal("creation form not loaded or auto-review kept")
			}
			if !tt.review {
				if cmd != nil || m.Creation.Error == "" || !strings.Contains(m.Notice, "Finish the highlighted setting") {
					t.Fatal("incomplete setup did not stop at the form", m.Creation.Error, m.Notice)
				}
				return
			}
			if cmd == nil {
				t.Fatal("complete setup did not request its review", m.Creation.Error)
			}
			cmd()
			if !reflect.DeepEqual(c.calls, []string{"vm.create"}) || c.requests[0].ID != creationFormOperation {
				t.Fatal("review request", c.calls, c.requests)
			}
		})
	}
}

// Importing the same appliance again suggests a free name instead of failing
// creation after preparation.
func TestCreationSuggestsAFreeNameWhenTaken(t *testing.T) {
	base := creationFormFixture()
	m := fixtureWorkspace()
	m.Client = &workspaceClient{}
	m.Pending["creation-load"] = 51
	taken := []domain.VM{workspaceVM(workspaceVMID, "appliance"), workspaceVM("22345678-1234-4234-8234-123456789abc", "appliance 2")}
	bundle := creationBundle{OperationID: creationFormOperation, Source: base.Source, Options: base.Options, Pools: base.Pools, Networks: base.Networks, VMs: taken}
	next, _ := m.Update(workspaceReply{Kind: "creation-load", Token: 51, Response: app.Response{Data: bundle}})
	m = next.(Workspace)
	if m.Creation == nil || m.Creation.Spec.Name != "appliance 3" || !strings.Contains(m.Creation.NameOrigin, "already exists") {
		t.Fatal("taken name not replaced by a free suggestion", m.Creation.Spec.Name)
	}
	f := creationFocus(t, *m.Creation, "name")
	if !strings.Contains(f.controls()[f.Focus].help, "a VM named appliance already exists") {
		t.Fatal("name suggestion not labelled")
	}
	free := NewCreationForm(creationFormOperation, base.Source, base.Options, nil, nil)
	free.uniqueName([]domain.VM{workspaceVM(workspaceVMID, "other")})
	if free.Spec.Name != "appliance" || free.NameOrigin != "" {
		t.Fatal("free name changed")
	}
}

func TestCreatedVMCanBeStartedFromItsResult(t *testing.T) {
	m := fixtureWorkspace()
	m.Client = &workspaceClient{}
	job := domain.Job{ID: creationPoolJobID, PlanID: creationPoolPlanID, State: "succeeded"}
	m.Section, m.NavIndex = 8, 8
	m.Detail = generic(job)
	m.JobOutcome = &jobOutcome{JobID: job.ID, PlanID: job.PlanID, State: "succeeded", Connection: m.Connection, Title: "VM created", VMID: workspaceVMID}
	labels := []string{}
	for _, b := range m.jobButtons() {
		labels = append(labels, b.label)
	}
	if !strings.Contains(strings.Join(labels, ","), "Start VM,Open VM") {
		t.Fatal("result lacks Start VM", labels)
	}
	cmd := m.openJobVM()
	m.JobStartVM = cmd != nil
	vm := workspaceVM(workspaceVMID, "Recovery workstation")
	vm.Fingerprint = "fixture"
	m.receiveJobVM(generic(vm))
	if m.Section != 1 || m.JobStartVM || !m.Busy || m.Pending["plan"] == 0 || !strings.Contains(m.Notice, "Preparing a review for Recovery workstation") {
		t.Fatal("Start VM did not open the VM and review starting it", m.Section, m.Busy, m.Notice)
	}
}
