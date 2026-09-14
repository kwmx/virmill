package tui

import (
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/domain"
)

func homeChecks() []domain.Capability {
	return []domain.Capability{
		{ID: "kvm", Status: "supported-with-prerequisites", Reason: "present", Purpose: "Hardware acceleration for VMs", Alternatives: []string{}},
		{ID: "restic", Status: "unsupported-on-this-configuration", Reason: "not installed", Purpose: "Stores encrypted backups", Optional: true,
			Packages: []string{"restic"}, Installer: "sudo dnf install", Alternatives: []string{}},
		{ID: "libvirt", Status: "unsupported-on-this-configuration", Reason: "libvirt is installed but not running", Purpose: "Runs and manages VMs",
			Alternatives: []string{"sudo systemctl enable --now virtqemud.socket"}},
	}
}

func TestWorkspaceHomeSummarizesHostStorageAndJobs(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	free := 92.5 * (1 << 30)
	m.Data["pools"] = []any{map[string]any{"name": "a", "active": true, "availableBytes": free}, map[string]any{"name": "b", "active": true, "availableBytes": free}}
	m.Data["health"] = generic(homeChecks())
	m.Data["jobs"] = []any{
		map[string]any{"operationID": "11111111-1111-4111-8111-111111111111", "state": "failed", "createdAt": now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
			"operation": "vm.start", "resourceIDs": []any{"libvirt|qemu:///system|vm|" + workspaceVMID}},
		map[string]any{"operationID": "22222222-2222-4222-8222-222222222222", "state": "succeeded", "createdAt": now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
			"operation": "vm.stop", "targetName": "Build guest"},
	}
	view := m.View()
	for _, want := range []string{"2 VMs · 0 running · 2 stopped", "Storage   2 pools · up to 92.5 GiB free", "Host      1 problem needs fixing · press , for steps",
		"Jobs      1 job needs attention · press 9 to review", "Last job  Shut down VM · Build guest · succeeded · 10 min ago", "Virtual machines", "Recovery workstation"} {
		if !strings.Contains(view, want) {
			t.Fatalf("home lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "185.0 GiB") {
		t.Fatal("free space of pools on one filesystem was added up")
	}
	m.Data["health"] = generic(homeChecks()[:2])
	if view = m.View(); !strings.Contains(view, "Host      Ready · 1 optional tool not installed · press , to see") {
		t.Fatalf("ready host:\n%s", view)
	}
	m.Data["health"] = generic(homeChecks()[:1])
	m.Data["jobs"] = []any{map[string]any{"operationID": "33333333-3333-4333-8333-333333333333", "state": "succeeded"}}
	if view = m.View(); !strings.Contains(view, "Host      Ready\n") || !strings.Contains(view, "Jobs      No jobs running") || strings.Contains(view, "Last job") {
		t.Fatalf("quiet home:\n%s", view)
	}
}

func TestWorkspaceSettingsShowsReadableHostCheck(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 100, 60
	m.Section = 10
	m.Data["health"] = generic(homeChecks())
	view := m.View()
	for _, want := range []string{"Connection: qemu:///system", "Another connection: virmill tui --connection qemu:///session", "Version: " + buildinfo.Version,
		"Virmill host check (read-only; nothing was changed)", "Missing", "Libvirt is installed but not running",
		"sudo systemctl enable --now virtqemud.socket", "Add the optional features:", "sudo dnf install restic", "[ r Check again ]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("settings lack %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Reason code") || strings.Contains(view, "Evidence class") {
		t.Fatalf("settings still dump raw fields:\n%s", view)
	}
	m.ASCII = true
	if view = m.View(); strings.ContainsAny(view, "✓✗") || !strings.Contains(view, "plain ASCII symbols") {
		t.Fatalf("ASCII settings:\n%s", view)
	}
}
