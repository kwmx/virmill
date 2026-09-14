package tui

import (
	"strings"
	"testing"
	"time"
)

func TestWorkspaceVMListShowsCPUAndRAM(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Section = 1
	view := m.View()
	if !strings.Contains(view, "CPU  RAM") || !strings.Contains(view, "4    4 GiB") {
		t.Fatalf("VM list lacks CPU/RAM:\n%s", view)
	}
	if memoryText(256<<20) != "256 MiB" || memoryText(3<<29) != "1.5 GiB" || memoryText(8<<30) != "8 GiB" {
		t.Fatal(memoryText(256<<20), memoryText(3<<29), memoryText(8<<30))
	}
}

func networkFixtures() []any {
	return []any{
		map[string]any{"name": "default", "active": true, "autostart": true, "persistent": true, "ownership": "external",
			"key":           map[string]any{"resourceUUID": "e4aa7897-db51-45de-a0dc-a13554eba163"},
			"persistentXML": "<network><name>default</name><forward mode='nat'/><bridge name='virbr0'/><ip address='192.168.122.1' netmask='255.255.255.0'><dhcp><range start='192.168.122.2' end='192.168.122.254'/></dhcp></ip></network>"},
		map[string]any{"name": "lab", "active": false, "persistentXML": "<network><name>lab</name><ip family='ipv6' address='fd00::1' prefix='64'/><ip address='10.0.0.1' prefix='24'/></network>"},
	}
}

func TestWorkspaceNetworksShowTypeAddressAndReadableDetails(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 100, 30
	m.Section = 2
	m.Data["networks"] = networkFixtures()
	view := m.View()
	for _, want := range []string{"TYPE", "ADDRESS", "NAT", "192.168.122.1/24", "isolated", "10.0.0.1/24"} {
		if !strings.Contains(view, want) {
			t.Fatalf("network list lacks %q:\n%s", want, view)
		}
	}
	m.Detail, m.DetailTitle = m.rows()[0], "Network details"
	view = m.View()
	for _, want := range []string{"Type: NAT (VMs reach outside networks through this host)", "Address: 192.168.122.1/24",
		"DHCP range: 192.168.122.2 to 192.168.122.254", "Host bridge: virbr0", "Starts with host: Yes", "Managed by: Created outside Virmill"} {
		if !strings.Contains(view, want) {
			t.Fatalf("network details lack %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Fingerprint") || strings.Contains(view, "Native XML") {
		t.Fatalf("readable details dump raw fields:\n%s", view)
	}
	m.Raw = true
	view = m.View()
	if !strings.Contains(view, "Network details · technical details") || !strings.Contains(view, "Persistent XML (used at next start)") ||
		!strings.Contains(view, "<forward mode='nat'/>") || strings.Contains(view, `\u003c`) {
		t.Fatalf("technical view:\n%s", view)
	}
}

func TestWorkspacePoolsShowCapacity(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 24
	m.Section = 3
	m.Data["pools"] = []any{map[string]any{"name": "images", "state": "running", "active": true, "type": "dir",
		"capacityBytes": float64(1 << 40), "allocatedBytes": float64(1 << 39), "availableBytes": float64(1 << 39),
		"xml": "<pool type='dir'><name>images</name><target><path>/var/lib/libvirt/images</path></target></pool>"}}
	view := m.View()
	if !strings.Contains(view, "SIZE") || !strings.Contains(view, "active") || !strings.Contains(view, "1.0 TiB") || !strings.Contains(view, "512.0 GiB") ||
		strings.Contains(view, "running") {
		t.Fatalf("pool list:\n%s", view)
	}
	m.Detail, m.DetailTitle = m.rows()[0], "Storage pool details"
	view = m.View()
	for _, want := range []string{"Type: Folder (dir)", "Location: /var/lib/libvirt/images", "Size: 1.0 TiB", "Free: 512.0 GiB"} {
		if !strings.Contains(view, want) {
			t.Fatalf("pool details lack %q:\n%s", want, view)
		}
	}
}

func TestWorkspaceJobsShowTaskTargetAndTime(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	m := fixtureWorkspace()
	m.Width, m.Height = 90, 24
	m.Section = 8
	m.Data["jobs"] = []any{
		map[string]any{"operationID": "11111111-1111-4111-8111-111111111111", "state": "succeeded", "createdAt": now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
			"operation": "vm.start", "resourceIDs": []any{"libvirt|qemu:///system|vm|" + workspaceVMID}},
		map[string]any{"operationID": "22222222-2222-4222-8222-222222222222", "state": "failed", "createdAt": now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
			"operation": "snapshot.capture-cold-v1", "targetName": "Build guest"},
		map[string]any{"operationID": "33333333-3333-4333-8333-333333333333", "state": "succeeded", "createdAt": now.Add(-40 * 24 * time.Hour).Format(time.RFC3339Nano)},
	}
	view := m.View()
	for _, want := range []string{"STARTED", "Capture stopped VM · Build guest", "5 min ago", "Start VM · Recovery workstation", "2 h ago",
		"33333333-3333-4333-8333-333333333333", "Aug 5"} {
		if !strings.Contains(view, want) {
			t.Fatalf("job list lacks %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "Capture stopped VM") > strings.Index(view, "Start VM") {
		t.Fatal("jobs are not newest first")
	}
	m.Detail, m.DetailTitle = m.rows()[1], "Job"
	view = m.View()
	if !strings.Contains(view, "Task: Start VM") || !strings.Contains(view, "For: Recovery workstation") || !strings.Contains(view, "(2 h ago)") {
		t.Fatalf("job details:\n%s", view)
	}
}

func TestJobLabelsAreTasks(t *testing.T) {
	for op, want := range map[string]string{"vm.start": "Start VM", "vm.stop": "Shut down VM", "vm.hard-stop": "Force off VM",
		"vm.create.devices-v1": "Create VM", "import.prepare-install": "Prepare ISO installer", "vm.remove-disks-v1": "Remove VM and disks",
		"vm.configure-guest-agent": "Enable guest integration", "vm.configure-resources": "Edit VM hardware",
		"snapshot.capture-cold-v1": "Capture stopped VM", "network.create": "Create network", "network.ipv6-filter": "network ipv6-filter", "": ""} {
		if got := operationLabel(op); got != want {
			t.Fatalf("operationLabel(%q) = %q, want %q", op, got, want)
		}
	}
}

func TestWorkspaceVMTechnicalViewShowsXML(t *testing.T) {
	m := fixtureWorkspace()
	m.Width, m.Height = 80, 40
	m.Section = 1
	m.Detail, m.DetailTitle, m.Raw = m.rows()[0], "VM details", true
	view := m.View()
	if !strings.Contains(view, "<domain><vcpu>4</vcpu>") || strings.Contains(view, `\u003c`) || strings.Contains(view, `"persistentXML"`) {
		t.Fatalf("VM technical view:\n%s", view)
	}
}
