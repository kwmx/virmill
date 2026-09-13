package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

type doctorClient struct {
	data   any
	err    error
	method string
}

func (d *doctorClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	d.method = method
	if d.err != nil {
		return app.Response{}, d.err
	}
	return app.Response{APIVersion: domain.APIVersion, Data: d.data, Warnings: []string{}}, nil
}

func runDoctor(t *testing.T, client *doctorClient, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := New(client, &out, &out)
	c.SetArgs(append([]string{"doctor"}, args...))
	err := c.Execute()
	return out.String(), err
}

func doctorFixture() []domain.Capability {
	return []domain.Capability{
		{ID: "kvm", Status: "supported-with-prerequisites", ReasonCode: "KVM_ACCESS_DENIED", Reason: "/dev/kvm exists but you cannot open it",
			Purpose: "Hardware acceleration for VMs", Alternatives: []string{"sudo usermod -aG kvm $USER, then sign out and back in"}},
		{ID: "libvirt", Status: "unsupported-on-this-configuration", ReasonCode: "LIBVIRT_MISSING", Reason: "the libvirt service is not installed",
			Purpose: "Runs and manages VMs", Packages: []string{"libvirt-daemon-kvm"}, Installer: "sudo dnf install",
			Alternatives: []string{"Then start it: sudo systemctl enable --now virtqemud.socket"}},
		{ID: "qemu-img", Status: "supported-with-prerequisites", ReasonCode: "DEPENDENCY_PRESENT_UNVERIFIED", Reason: "found at /usr/bin/qemu-img",
			Purpose: "Converts disk images\x1b[2J", Alternatives: []string{}},
		{ID: "restic", Status: "unsupported-on-this-configuration", ReasonCode: "DEPENDENCY_MISSING", Reason: "not installed",
			Purpose: "Stores encrypted backups", Optional: true, Packages: []string{"restic"}, Installer: "sudo dnf install", Alternatives: []string{}},
		{ID: "swtpm", Status: "unsupported-on-this-configuration", ReasonCode: "DEPENDENCY_MISSING", Reason: "not installed",
			Purpose: "Virtual TPM chips", Optional: true, Packages: []string{"swtpm", "swtpm-tools"}, Installer: "sudo dnf install", Alternatives: []string{}},
		{ID: "host", Status: "supported-with-prerequisites", ReasonCode: "SUPPORT_UNCERTIFIED", Reason: "this host (linux/amd64) is not in a certified support matrix yet", Alternatives: []string{}},
	}
}

func TestDoctorReportGroupsChecksAndSuggestsInstalls(t *testing.T) {
	client := &doctorClient{data: doctorFixture()}
	out, err := runDoctor(t, client)
	if err != nil || client.method != "host.doctor" {
		t.Fatal(err, client.method)
	}
	for _, want := range []string{
		"Virmill host check (read-only; nothing was changed)",
		"Ready\n  ✓ qemu-img  Converts disk images\n",
		"Missing\n  ✗ libvirt   Runs and manages VMs\n",
		"The libvirt service is not installed\n",
		"Install: sudo dnf install libvirt-daemon-kvm\n",
		"Then start it: sudo systemctl enable --now virtqemud.socket\n",
		"Needs attention\n  ! KVM       Hardware acceleration for VMs\n",
		"sudo usermod -aG kvm $USER, then sign out and back in\n",
		"Optional, not installed\n  - restic    Stores encrypted backups\n" + strings.Repeat(" ", 14) + "Install: sudo dnf install restic\n",
		"Note: This host (linux/amd64) is not in a certified support matrix yet\n",
		"Install what's missing:\n  sudo dnf install libvirt-daemon-kvm\n",
		"Add the optional features:\n  sudo dnf install restic swtpm swtpm-tools\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, "{") || strings.Contains(out, "Not installed") {
		t.Fatalf("report leaked escapes or raw JSON:\n%s", out)
	}
}

func TestDoctorJSONKeepsMachineReadableFields(t *testing.T) {
	out, err := runDoctor(t, &doctorClient{data: doctorFixture()}, "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data []domain.Capability `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err, out)
	}
	if len(response.Data) != 6 || response.Data[1].Installer != "sudo dnf install" || response.Data[3].Optional != true {
		t.Fatalf("json lost guidance fields: %+v", response.Data)
	}
}

func TestDoctorReportHandlesOlderCoordinators(t *testing.T) {
	legacy := []domain.Capability{
		{ID: "qemu-img", Status: "supported-with-prerequisites", ReasonCode: "DEPENDENCY_PRESENT_UNVERIFIED", Reason: "/usr/bin/qemu-img",
			Alternatives: []string{"Review distro dependency setup instructions"}},
		{ID: "restic", Status: "unsupported-on-this-configuration", ReasonCode: "DEPENDENCY_MISSING", Reason: "executable missing from PATH",
			Alternatives: []string{"Review distro dependency setup instructions"}},
	}
	out, err := runDoctor(t, &doctorClient{data: legacy})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Ready\n  ✓ qemu-img  /usr/bin/qemu-img\n") || !strings.Contains(out, "Missing\n  ✗ restic    executable missing from PATH\n") ||
		strings.Contains(out, "Needs attention") {
		t.Fatalf("legacy report:\n%s", out)
	}
}

func TestDoctorExplainsStoppedCoordinator(t *testing.T) {
	out, err := runDoctor(t, &doctorClient{err: domain.Fail("COORDINATOR_UNAVAILABLE", "start virmilld as your user: dial failed")})
	var shown ShownError
	var failure *domain.Error
	if !errors.As(err, &shown) || !errors.As(err, &failure) || failure.Code != "COORDINATOR_UNAVAILABLE" ||
		!strings.Contains(out, "The Virmill coordinator isn't running") || !strings.Contains(out, "systemctl --user start virmilld.service") {
		t.Fatalf("stopped coordinator: %v\n%s", err, out)
	}
}

func TestDoctorSaysWhenEverythingIsReady(t *testing.T) {
	ready := []domain.Capability{{ID: "qemu-img", Status: "supported-with-prerequisites", Reason: "found at /usr/bin/qemu-img", Purpose: "Converts disk images", Alternatives: []string{}}}
	out, err := runDoctor(t, &doctorClient{data: ready})
	if err != nil || !strings.Contains(out, "Everything Virmill needs is ready.") || strings.Contains(out, "Install") {
		t.Fatalf("all-ready report: %v\n%s", err, out)
	}
}
