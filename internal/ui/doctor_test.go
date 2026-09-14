package ui

import (
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func TestDoctorReportGroupsChecksAndUsesASCIIMarks(t *testing.T) {
	checks := []domain.Capability{
		{ID: "kvm", Status: "supported-with-prerequisites", Reason: "present", Purpose: "Hardware acceleration", Alternatives: []string{}},
		{ID: "qemu-img", Status: "unsupported-on-this-configuration", Reason: "not installed", Purpose: "Disk images",
			Packages: []string{"qemu-utils"}, Installer: "sudo apt install", Alternatives: []string{}},
		{ID: "restic", Status: "unsupported-on-this-configuration", Reason: "not installed", Purpose: "Backups", Optional: true,
			Packages: []string{"restic"}, Installer: "sudo apt install", Alternatives: []string{}},
		{ID: "host", Status: "supported-with-prerequisites", Reason: "this host is not certified", Alternatives: []string{}},
	}
	g := GroupDoctor(checks)
	if len(g.Ready) != 1 || len(g.Missing) != 1 || len(g.Optional) != 1 || len(g.Notes) != 1 || len(g.Attention) != 0 {
		t.Fatalf("groups %+v", g)
	}
	text := DoctorReport(checks, true)
	for _, want := range []string{"  + KVM       Hardware acceleration\n", "  x qemu-img  Disk images\n", "  - restic    Backups\n",
		"Note: This host is not certified\n", "Install what's missing:\n  sudo apt install qemu-utils\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report lacks %q:\n%s", want, text)
		}
	}
	if strings.ContainsAny(text, "✓✗") || !strings.Contains(DoctorReport(checks, false), "  ✓ KVM") {
		t.Fatalf("marks do not follow the ascii choice:\n%s", text)
	}
}
